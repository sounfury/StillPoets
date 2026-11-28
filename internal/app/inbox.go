package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"stillpoets/internal/domain"
	"stillpoets/internal/parser"
	"stillpoets/internal/storage"
	booksync "stillpoets/internal/sync"
	"stillpoets/internal/sync/webdav"
)

type inboxReadyCleanup struct {
	readyPath    string
	expectedHash string
}

// consumeRemoteInbox 在第一阶段元数据同步落库之后，扫描并无感消费 WebDAV inbox/*.ready.json。
// 任意联网设备先到先消费：
//   - 若书目已在合并后的远端书架中（活跃且非本机待传，或永久墓碑），跳过解析直接清理清单；
//   - 若为新投递，校验远端原件字节数与 SHA-256 后使用确定性 ParsedAt 解析，写入不可变 builds/<attemptId>/ 路径；
//   - 等第二轮快照发布并回读验证实际包含该 bookId 与预期 contentHash 后，才逐书删除对应的 ready.json。
func (s *Service) consumeRemoteInbox(
	ctx context.Context,
	client *webdav.Client,
	engine *booksync.Engine,
	startEpoch uint64,
	firstRes *booksync.SyncResult,
) (*booksync.SyncResult, bool) {
	entries, err := client.ListDir(ctx, booksync.InboxDir)
	if errors.Is(err, webdav.ErrNotFound) {
		return firstRes, false
	}
	if err != nil {
		s.LogEvent("sync", "list inbox skipped: %v", err)
		firstRes.PartialSync = true
		return firstRes, false
	}

	type readyEntry struct {
		importID string
		name     string
	}
	var readyList []readyEntry
	for _, e := range entries {
		if e.IsDir {
			continue
		}
		if id, ok := booksync.ParseInboxReadyFilename(e.Name); ok {
			readyList = append(readyList, readyEntry{importID: id, name: e.Name})
		}
	}
	if len(readyList) == 0 {
		return firstRes, false
	}
	slices.SortFunc(readyList, func(a, b readyEntry) int {
		return strings.Compare(a.importID, b.importID)
	})

	pendingSet := make(map[string]struct{})
	if pendingList, pErr := s.store.ListPendingUploads(ctx); pErr == nil {
		for _, p := range pendingList {
			pendingSet[p.BookID] = struct{}{}
		}
	}

	pendingCleanups := make(map[string]inboxReadyCleanup)
	consumedAny := false
	anyIssue := false

	for _, entry := range readyList {
		if ctx.Err() != nil {
			anyIssue = true
			break
		}
		s.mu.Lock()
		epochChanged := s.workspaceEpoch != startEpoch
		s.mu.Unlock()
		if epochChanged {
			anyIssue = true
			break
		}

		readyPath := booksync.InboxDir + "/" + entry.name
		rawManifest, _, err := client.GetWithLimit(ctx, readyPath, booksync.MaxInboxReadyBytes)
		if errors.Is(err, webdav.ErrNotFound) {
			continue
		}
		if err != nil {
			s.LogEvent("sync", "inbox read %s failed: %v", entry.name, err)
			anyIssue = true
			continue
		}
		item, err := booksync.DecodeInboxItem(rawManifest, entry.importID)
		if err != nil {
			s.LogEvent("sync", "inbox invalid manifest %s: %v", entry.name, err)
			anyIssue = true
			continue
		}

		existing, getErr := s.store.GetBookIncludingDeleted(ctx, item.BookID)
		switch {
		case getErr == nil && existing.Deleted:
			// 永久墓碑：不再导入，直接清理遗留清单
			_ = client.Delete(ctx, readyPath)
			s.LogEvent("sync", "inbox skipped tombstoned book %s and cleaned ready manifest", item.BookID)
			continue
		case getErr == nil:
			// 书已存在：若不在本机待传队列，说明已被远端或本机成功发布过，可安全清理清单
			if _, isPending := pendingSet[item.BookID]; !isPending {
				if isBookPublishedInRemoteOrResult(item.BookID, existing.ContentHash, firstRes) {
					_ = client.Delete(ctx, readyPath)
					s.LogEvent("sync", "inbox book %s already published, cleaned ready manifest", item.BookID)
				} else {
					pendingCleanups[item.BookID] = inboxReadyCleanup{
						readyPath:    readyPath,
						expectedHash: existing.ContentHash,
					}
					consumedAny = true
				}
				continue
			}
			// 仍在 pending_uploads 中，等待常规重试上传
			anyIssue = true
			continue
		case !errors.Is(getErr, storage.ErrNotFound):
			s.LogEvent("sync", "inbox lookup book %s failed: %v", item.BookID, getErr)
			anyIssue = true
			continue
		}

		cleanup, ok := s.importSingleInboxItem(ctx, client, engine, startEpoch, item, readyPath)
		if !ok {
			anyIssue = true
			continue
		}
		pendingCleanups[item.BookID] = cleanup
		consumedAny = true
	}

	if !consumedAny {
		if anyIssue {
			firstRes.PartialSync = true
		}
		return firstRes, false
	}

	// 有一本及以上新书资产上传完成：立刻发布新代次快照，并仅在回读验证包含该书后删除对应 ready.json
	s.emitSyncToast(SyncToastState{Phase: SyncToastSyncing, Message: "正在发布新导入诗集…"})
	localData, meta, remainingPending, err := s.store.ExportSnapshotWithPending(ctx)
	if err != nil {
		s.LogEvent("sync", "inbox export snapshot failed: %v", err)
		firstRes.PartialSync = true
		return firstRes, true
	}
	blockedBooks := make(map[string]struct{}, len(remainingPending))
	for _, p := range remainingPending {
		blockedBooks[p.BookID] = struct{}{}
	}
	pubRes, err := engine.Sync(ctx, booksync.SyncRequest{
		InstallID:       meta.InstallID,
		LocalRev:        meta.LocalRev,
		LocalCounter:    meta.LamportCounter,
		LocalData:       localData,
		HasDirty:        true,
		ForcePush:       false,
		KnownRemoteRevs: meta.KnownRemoteRevs,
		PendingBooks:    blockedBooks,
	})
	if err != nil {
		s.LogEvent("sync", "inbox publish snapshot failed: %v", err)
		firstRes.PartialSync = true
		return firstRes, true
	}

	remoteDeviceRevs := maps.Clone(pubRes.SyncedDeviceRevs)
	for id := range pubRes.IncompleteDevices {
		if rev := meta.KnownRemoteRevs[id]; rev > 0 {
			remoteDeviceRevs[id] = rev
		}
	}
	keepDirty := len(remainingPending) > 0
	if keepDirty || anyIssue {
		pubRes.PartialSync = true
	}

	s.mu.Lock()
	if s.workspaceEpoch != startEpoch {
		s.mu.Unlock()
		firstRes.PartialSync = true
		return firstRes, true
	}
	applyErr := s.store.ApplyMergedSnapshotEx(ctx, pubRes.MergedData, meta.DirtySeq, pubRes.MaxCounter, pubRes.PublishedRev, pubRes.Published, keepDirty, remoteDeviceRevs)
	if applyErr == nil {
		s.remoteDevices = slices.Clone(pubRes.RemoteDevices)
	}
	s.mu.Unlock()
	if applyErr != nil {
		s.LogEvent("sync", "inbox apply published snapshot failed: %v", applyErr)
		pubRes.PartialSync = true
		return pubRes, true
	}

	// 逐书核对：仅在已回读验证的发布载荷包含该 bookId（且内容哈希匹配或已合并到远端/墓碑）时才删除 ready.json
	for bookID, cleanup := range pendingCleanups {
		if canDeleteInboxReadyAfterPublish(bookID, cleanup.expectedHash, pubRes) {
			if delErr := client.Delete(ctx, cleanup.readyPath); delErr != nil && !errors.Is(delErr, webdav.ErrNotFound) {
				s.LogEvent("sync", "inbox delete ready %s warning: %v", cleanup.readyPath, delErr)
			}
		} else {
			pubRes.PartialSync = true
			s.LogEvent("sync", "inbox kept ready %s because book %s was not confirmed in verified published snapshot", cleanup.readyPath, bookID)
		}
	}
	return pubRes, true
}

func (s *Service) importSingleInboxItem(
	ctx context.Context,
	client *webdav.Client,
	engine *booksync.Engine,
	startEpoch uint64,
	item *booksync.InboxItem,
	readyPath string,
) (inboxReadyCleanup, bool) {
	displayName := strings.TrimSpace(strings.TrimSuffix(item.OriginalFilename, filepath.Ext(item.OriginalFilename)))
	if displayName == "" {
		displayName = item.BookID
	}
	s.emitSyncToast(SyncToastState{
		Phase:   SyncToastSyncing,
		Message: fmt.Sprintf("正在导入《%s》…", displayName),
	})

	rawPath := booksync.BookRawPath(item.BookID, item.RawFilename)
	rawBytes, _, err := client.GetWithLimit(ctx, rawPath, booksync.MaxInboxEPUBBytes)
	if err != nil {
		s.LogEvent("sync", "inbox download raw %s failed: %v", rawPath, err)
		return inboxReadyCleanup{}, false
	}
	if int64(len(rawBytes)) != item.RawSize {
		s.LogEvent("sync", "inbox raw size mismatch for %s: got %d want %d", item.BookID, len(rawBytes), item.RawSize)
		return inboxReadyCleanup{}, false
	}
	sum := sha256.Sum256(rawBytes)
	gotHash := hex.EncodeToString(sum[:])
	if gotHash != item.RawSHA256 {
		s.LogEvent("sync", "inbox raw sha256 mismatch for %s: got %s want %s", item.BookID, gotHash, item.RawSHA256)
		return inboxReadyCleanup{}, false
	}

	parsed, err := parser.ParseEPUBReaderAtTime(bytes.NewReader(rawBytes), int64(len(rawBytes)), item.BookID, item.SubmittedAt)
	if err != nil {
		s.LogEvent("sync", "inbox parse EPUB %s failed: %v", item.BookID, err)
		return inboxReadyCleanup{}, false
	}

	spCompressed, contentHash, err := booksync.EncodeSPBook(parsed.Book)
	if err != nil {
		s.LogEvent("sync", "inbox encode spbook %s failed: %v", item.BookID, err)
		return inboxReadyCleanup{}, false
	}

	attemptID := domain.NewShortRandID()
	contentPath := booksync.BookBuildParsedRelPath(attemptID)

	localRawPath := filepath.Join(s.dataDir, "cache", "raw", item.BookID+"_"+item.RawFilename)
	localSPPath := filepath.Join(s.dataDir, "cache", "spbook", item.BookID+".spbook.json.gz")
	localCoverPath := ""
	coverHash := ""

	if err := os.WriteFile(localRawPath, rawBytes, 0o600); err != nil {
		s.LogEvent("sync", "inbox cache raw %s failed: %v", item.BookID, err)
		return inboxReadyCleanup{}, false
	}
	if err := os.WriteFile(localSPPath, spCompressed, 0o600); err != nil {
		_ = os.Remove(localRawPath)
		s.LogEvent("sync", "inbox cache spbook %s failed: %v", item.BookID, err)
		return inboxReadyCleanup{}, false
	}
	if len(parsed.Cover) > 0 {
		csum := sha256.Sum256(parsed.Cover)
		coverHash = hex.EncodeToString(csum[:])
		localCoverPath = filepath.Join(s.dataDir, "cache", "covers", item.BookID+".img")
		if err := os.WriteFile(localCoverPath, parsed.Cover, 0o600); err != nil {
			localCoverPath = ""
			coverHash = ""
		}
	}

	poemCount := 0
	for _, p := range parsed.Book.Poems {
		if p.IsPoem() {
			poemCount++
		}
	}
	if poemCount == 0 {
		poemCount = len(parsed.Book.Poems)
	}

	title := strings.TrimSpace(parsed.Book.Title)
	if title == "" {
		title = displayName
	}
	author := strings.TrimSpace(parsed.Book.Author)
	if author == "" {
		author = "佚名"
	}

	groupID := strings.TrimSpace(item.GroupID)
	if groupID != "" {
		active, gErr := s.store.IsActiveGroup(ctx, groupID)
		if gErr != nil {
			s.LogEvent("sync", "inbox check group %s failed: %v", groupID, gErr)
			return inboxReadyCleanup{}, false
		}
		if !active {
			groupID = ""
		}
	}

	book := domain.Book{
		ID:          item.BookID,
		Title:       title,
		Author:      author,
		CoverHash:   coverHash,
		ContentHash: contentHash,
		ContentSize: int64(len(spCompressed)),
		ContentPath: contentPath,
		RawFilename: item.RawFilename,
		RawSize:     item.RawSize,
		PoemCount:   poemCount,
		GroupID:     groupID,
		GroupVer:    item.InitialGroupVer,
		StarredVer:  item.InitialStarredVer,
		SortVer:     item.InitialSortVer,
		CreatedAt:   item.SubmittedAt,
	}
	pendingUpload := storage.PendingUpload{
		BookID:          item.BookID,
		RawFilename:     item.RawFilename,
		LocalRawPath:    localRawPath,
		LocalSPBookPath: localSPPath,
		LocalCoverPath:  localCoverPath,
		ContentHash:     contentHash,
		State:           storage.UploadPending,
	}

	s.mu.Lock()
	if s.workspaceEpoch != startEpoch {
		s.mu.Unlock()
		return inboxReadyCleanup{}, false
	}
	upsertErr := s.store.UpsertImportedBookWithUpload(ctx, book, parsed.Book, &pendingUpload)
	s.mu.Unlock()
	if upsertErr != nil {
		s.LogEvent("sync", "inbox upsert book %s failed: %v", item.BookID, upsertErr)
		return inboxReadyCleanup{}, false
	}

	storedBook, err := s.store.GetBook(ctx, item.BookID)
	if err != nil {
		return inboxReadyCleanup{}, false
	}
	if _, err := engine.UploadInboxParsedAssets(ctx, storedBook, spCompressed, parsed.Cover); err != nil {
		s.LogEvent("sync", "inbox upload parsed assets %s failed: %v", item.BookID, err)
		_ = s.store.UpdatePendingUploadState(ctx, item.BookID, storage.UploadFailed, err.Error())
		return inboxReadyCleanup{}, false
	}

	s.mu.Lock()
	if s.workspaceEpoch != startEpoch {
		s.mu.Unlock()
		return inboxReadyCleanup{}, false
	}
	if err := s.store.DeletePendingUpload(ctx, item.BookID); err != nil {
		s.mu.Unlock()
		s.LogEvent("sync", "inbox clear pending upload %s failed: %v", item.BookID, err)
		return inboxReadyCleanup{}, false
	}
	s.mu.Unlock()

	s.LogEvent("sync", "inbox imported book %s (%s, %d poems, path=%s)", item.BookID, title, poemCount, contentPath)
	return inboxReadyCleanup{
		readyPath:    readyPath,
		expectedHash: contentHash,
	}, true
}

func isBookPublishedInRemoteOrResult(bookID, contentHash string, res *booksync.SyncResult) bool {
	if res == nil {
		return false
	}
	if res.Published && res.PublishedBookHashes[bookID] == contentHash && contentHash != "" {
		return true
	}
	for _, b := range res.MergedData.Books {
		if b.ID == bookID && (b.Deleted || (b.ContentHash != "" && b.ContentHash == contentHash)) {
			// 若本轮没有发布，但合并后的远端快照已经包含该书（且来自其他设备或既有远端代次）
			if !res.PartialSync || len(res.RemoteDevices) > 0 {
				return true
			}
		}
	}
	return false
}

func canDeleteInboxReadyAfterPublish(bookID, expectedHash string, res *booksync.SyncResult) bool {
	if res == nil || !res.Published {
		return false
	}
	if gotHash, ok := res.PublishedBookHashes[bookID]; ok && gotHash != "" {
		// 即使另一台设备在并发窗口中以更高 ContentVer 胜出并替换了 ContentHash，
		// 只要已回读验证的发布载荷里该 bookId 已有有效书目（或已成墓碑），该 ready 任务均已完成。
		return gotHash == expectedHash || gotHash != ""
	}
	for _, b := range res.MergedData.Books {
		if b.ID == bookID && b.Deleted {
			return true
		}
	}
	return false
}
