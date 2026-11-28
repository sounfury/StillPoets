package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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

	readyList := inboxReadyEntries(entries)
	if len(readyList) == 0 {
		return firstRes, false
	}
	pending, err := s.store.ListPendingUploads(ctx)
	if err != nil {
		pending = nil
	}
	scan := inboxScan{
		client: client, engine: engine, epoch: startEpoch, firstResult: firstRes,
		pendingBooks: pendingBookIDs(pending), cleanups: make(map[string]inboxReadyCleanup),
	}
	s.scanInboxItems(ctx, &scan, readyList)
	if len(scan.cleanups) == 0 {
		if scan.partial {
			firstRes.PartialSync = true
		}
		return firstRes, false
	}
	return s.publishInboxItems(ctx, &scan), true
}

type inboxReadyEntry struct {
	importID string
	name     string
}

func inboxReadyEntries(entries []webdav.Entry) []inboxReadyEntry {
	var readyList []inboxReadyEntry
	for _, e := range entries {
		if e.IsDir {
			continue
		}
		if id, ok := booksync.ParseInboxReadyFilename(e.Name); ok {
			readyList = append(readyList, inboxReadyEntry{importID: id, name: e.Name})
		}
	}
	slices.SortFunc(readyList, func(a, b inboxReadyEntry) int {
		return strings.Compare(a.importID, b.importID)
	})

	return readyList
}

type inboxScan struct {
	client       *webdav.Client
	engine       *booksync.Engine
	epoch        uint64
	firstResult  *booksync.SyncResult
	pendingBooks map[string]struct{}
	cleanups     map[string]inboxReadyCleanup
	partial      bool
}

func (s *Service) scanInboxItems(ctx context.Context, scan *inboxScan, readyList []inboxReadyEntry) {
	for _, entry := range readyList {
		if ctx.Err() != nil {
			scan.partial = true
			break
		}
		s.mu.Lock()
		epochChanged := s.workspaceEpoch != scan.epoch
		s.mu.Unlock()
		if epochChanged {
			scan.partial = true
			break
		}

		s.consumeInboxEntry(ctx, scan, entry)
	}
}

func (s *Service) consumeInboxEntry(ctx context.Context, scan *inboxScan, entry inboxReadyEntry) {
	readyPath := booksync.InboxDir + "/" + entry.name
	rawManifest, _, err := scan.client.GetWithLimit(ctx, readyPath, booksync.MaxInboxReadyBytes)
	if errors.Is(err, webdav.ErrNotFound) {
		return
	}
	if err != nil {
		s.LogEvent("sync", "inbox read %s failed: %v", entry.name, err)
		scan.partial = true
		return
	}
	item, err := booksync.DecodeInboxItem(rawManifest, entry.importID)
	if err != nil {
		s.LogEvent("sync", "inbox invalid manifest %s: %v", entry.name, err)
		scan.partial = true
		return
	}

	existing, err := s.store.GetBookIncludingDeleted(ctx, item.BookID)
	switch {
	case err == nil:
		s.handleExistingInboxBook(ctx, scan, existing, readyPath)
	case errors.Is(err, storage.ErrNotFound):
		cleanup, ok := s.importSingleInboxItem(ctx, scan.client, scan.engine, scan.epoch, item, readyPath)
		if !ok {
			scan.partial = true
			return
		}
		scan.cleanups[item.BookID] = cleanup
	default:
		s.LogEvent("sync", "inbox lookup book %s failed: %v", item.BookID, err)
		scan.partial = true
	}
}

func (s *Service) handleExistingInboxBook(ctx context.Context, scan *inboxScan, book domain.Book, readyPath string) {
	if book.Deleted {
		_ = scan.client.Delete(ctx, readyPath)
		s.LogEvent("sync", "inbox skipped tombstoned book %s and cleaned ready manifest", book.ID)
		return
	}
	if _, pending := scan.pendingBooks[book.ID]; pending {
		scan.partial = true
		return
	}
	if isBookPublishedInRemoteOrResult(book.ID, book.ContentHash, scan.firstResult) {
		_ = scan.client.Delete(ctx, readyPath)
		s.LogEvent("sync", "inbox book %s already published, cleaned ready manifest", book.ID)
		return
	}
	scan.cleanups[book.ID] = inboxReadyCleanup{readyPath: readyPath, expectedHash: book.ContentHash}
}

func (s *Service) publishInboxItems(ctx context.Context, scan *inboxScan) *booksync.SyncResult {
	// 有一本及以上新书资产上传完成：立刻发布新代次快照，并仅在回读验证包含该书后删除对应 ready.json
	s.emitSyncToast(SyncToastState{Phase: SyncToastSyncing, Message: "正在发布新导入诗集…"})
	localData, meta, remainingPending, err := s.store.ExportSnapshotWithPending(ctx)
	if err != nil {
		s.LogEvent("sync", "inbox export snapshot failed: %v", err)
		scan.firstResult.PartialSync = true
		return scan.firstResult
	}
	pubRes, err := scan.engine.Sync(ctx, booksync.SyncRequest{
		InstallID:       meta.InstallID,
		LocalRev:        meta.LocalRev,
		LocalCounter:    meta.LamportCounter,
		LocalData:       localData,
		HasDirty:        true,
		ForcePush:       false,
		KnownRemoteRevs: meta.KnownRemoteRevs,
		PendingBooks:    pendingBookIDs(remainingPending),
	})
	if err != nil {
		s.LogEvent("sync", "inbox publish snapshot failed: %v", err)
		scan.firstResult.PartialSync = true
		return scan.firstResult
	}

	keepDirty := len(remainingPending) > 0
	if keepDirty || scan.partial {
		pubRes.PartialSync = true
	}
	if err := s.applyWorkspaceSnapshot(ctx, pubRes, meta, scan.epoch, keepDirty, false); err != nil {
		if err == errSyncWorkspaceChanged {
			scan.firstResult.PartialSync = true
			return scan.firstResult
		}
		s.LogEvent("sync", "inbox apply published snapshot failed: %v", err)
		pubRes.PartialSync = true
		return pubRes
	}

	s.cleanPublishedInboxItems(ctx, scan, pubRes)
	return pubRes
}

func (s *Service) cleanPublishedInboxItems(ctx context.Context, scan *inboxScan, pubRes *booksync.SyncResult) {
	// 逐书核对：仅在已回读验证的发布载荷包含该 bookId（且内容哈希匹配或已合并到远端/墓碑）时才删除 ready.json
	for bookID, cleanup := range scan.cleanups {
		if canDeleteInboxReadyAfterPublish(bookID, cleanup.expectedHash, pubRes) {
			if delErr := scan.client.Delete(ctx, cleanup.readyPath); delErr != nil && !errors.Is(delErr, webdav.ErrNotFound) {
				s.LogEvent("sync", "inbox delete ready %s warning: %v", cleanup.readyPath, delErr)
			}
		} else {
			pubRes.PartialSync = true
			s.LogEvent("sync", "inbox kept ready %s because book %s was not confirmed in verified published snapshot", cleanup.readyPath, bookID)
		}
	}
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

	parsed, rawBytes, spCompressed, contentHash, ok := s.parseInboxEPUB(ctx, client, item)
	if !ok {
		return inboxReadyCleanup{}, false
	}
	contentPath := booksync.BookBuildParsedRelPath(domain.NewShortRandID())
	// 原件只在校验和解析成功后进入本地缓存。
	pendingUpload, coverHash, ok := s.cacheInboxAssets(item, rawBytes, spCompressed, parsed.Cover, contentHash)
	if !ok {
		return inboxReadyCleanup{}, false
	}
	book := domain.Book{ContentHash: contentHash, ContentSize: int64(len(spCompressed)), ContentPath: contentPath, CoverHash: coverHash}
	if !s.populateInboxBook(ctx, &book, item, parsed.Book, displayName) {
		return inboxReadyCleanup{}, false
	}
	if !s.storeInboxBook(ctx, startEpoch, book, parsed.Book, &pendingUpload) {
		return inboxReadyCleanup{}, false
	}
	if !s.uploadInboxBook(ctx, engine, startEpoch, item.BookID, spCompressed, parsed.Cover) {
		return inboxReadyCleanup{}, false
	}

	s.LogEvent("sync", "inbox imported book %s (%s, %d poems, path=%s)", item.BookID, book.Title, book.PoemCount, contentPath)
	return inboxReadyCleanup{
		readyPath:    readyPath,
		expectedHash: contentHash,
	}, true
}

func (s *Service) parseInboxEPUB(ctx context.Context, client *webdav.Client, item *booksync.InboxItem) (*parser.ParseResult, []byte, []byte, string, bool) {
	rawPath := booksync.BookRawPath(item.BookID, item.RawFilename)
	rawBytes, _, err := client.GetWithLimit(ctx, rawPath, booksync.MaxInboxEPUBBytes)
	if err != nil {
		s.LogEvent("sync", "inbox download raw %s failed: %v", rawPath, err)
		return nil, nil, nil, "", false
	}
	if int64(len(rawBytes)) != item.RawSize {
		s.LogEvent("sync", "inbox raw size mismatch for %s: got %d want %d", item.BookID, len(rawBytes), item.RawSize)
		return nil, nil, nil, "", false
	}
	sum := sha256.Sum256(rawBytes)
	gotHash := hex.EncodeToString(sum[:])
	if gotHash != item.RawSHA256 {
		s.LogEvent("sync", "inbox raw sha256 mismatch for %s: got %s want %s", item.BookID, gotHash, item.RawSHA256)
		return nil, nil, nil, "", false
	}

	parsed, err := parser.ParseEPUBReaderAtTime(bytes.NewReader(rawBytes), int64(len(rawBytes)), item.BookID, item.SubmittedAt)
	if err != nil {
		s.LogEvent("sync", "inbox parse EPUB %s failed: %v", item.BookID, err)
		return nil, nil, nil, "", false
	}

	spCompressed, contentHash, err := booksync.EncodeSPBook(parsed.Book)
	if err != nil {
		s.LogEvent("sync", "inbox encode spbook %s failed: %v", item.BookID, err)
		return nil, nil, nil, "", false
	}

	return parsed, rawBytes, spCompressed, contentHash, true
}

func (s *Service) cacheInboxAssets(item *booksync.InboxItem, rawBytes, spCompressed, cover []byte, contentHash string) (storage.PendingUpload, string, bool) {
	localRawPath := filepath.Join(s.dataDir, "cache", "raw", item.BookID+"_"+item.RawFilename)
	localSPPath := filepath.Join(s.dataDir, "cache", "spbook", item.BookID+".spbook.json.gz")
	localCoverPath := ""
	coverHash := ""

	if err := os.WriteFile(localRawPath, rawBytes, 0o600); err != nil {
		s.LogEvent("sync", "inbox cache raw %s failed: %v", item.BookID, err)
		return storage.PendingUpload{}, "", false
	}
	if err := os.WriteFile(localSPPath, spCompressed, 0o600); err != nil {
		_ = os.Remove(localRawPath)
		s.LogEvent("sync", "inbox cache spbook %s failed: %v", item.BookID, err)
		return storage.PendingUpload{}, "", false
	}
	if len(cover) > 0 {
		csum := sha256.Sum256(cover)
		coverHash = hex.EncodeToString(csum[:])
		localCoverPath = filepath.Join(s.dataDir, "cache", "covers", item.BookID+".img")
		if err := os.WriteFile(localCoverPath, cover, 0o600); err != nil {
			localCoverPath = ""
			coverHash = ""
		}
	}

	return storage.PendingUpload{
		BookID:          item.BookID,
		RawFilename:     item.RawFilename,
		LocalRawPath:    localRawPath,
		LocalSPBookPath: localSPPath,
		LocalCoverPath:  localCoverPath,
		ContentHash:     contentHash,
		State:           storage.UploadPending,
	}, coverHash, true
}

func (s *Service) populateInboxBook(ctx context.Context, book *domain.Book, item *booksync.InboxItem, parsed *domain.SPBook, displayName string) bool {
	poemCount := 0
	for _, p := range parsed.Poems {
		if p.IsPoem() {
			poemCount++
		}
	}
	if poemCount == 0 {
		poemCount = len(parsed.Poems)
	}

	title := strings.TrimSpace(parsed.Title)
	if title == "" {
		title = displayName
	}
	author := strings.TrimSpace(parsed.Author)
	if author == "" {
		author = "佚名"
	}

	groupID := strings.TrimSpace(item.GroupID)
	if groupID != "" {
		active, gErr := s.store.IsActiveGroup(ctx, groupID)
		if gErr != nil {
			s.LogEvent("sync", "inbox check group %s failed: %v", groupID, gErr)
			return false
		}
		if !active {
			groupID = ""
		}
	}

	book.ID = item.BookID
	book.Title = title
	book.Author = author
	book.RawFilename = item.RawFilename
	book.RawSize = item.RawSize
	book.PoemCount = poemCount
	book.GroupID = groupID
	book.GroupVer = item.InitialGroupVer
	book.StarredVer = item.InitialStarredVer
	book.SortVer = item.InitialSortVer
	book.CreatedAt = item.SubmittedAt
	return true
}

func (s *Service) storeInboxBook(ctx context.Context, startEpoch uint64, book domain.Book, parsed *domain.SPBook, pendingUpload *storage.PendingUpload) bool {
	s.mu.Lock()
	if s.workspaceEpoch != startEpoch {
		s.mu.Unlock()
		return false
	}
	upsertErr := s.store.UpsertImportedBookWithUpload(ctx, book, parsed, pendingUpload)
	s.mu.Unlock()
	if upsertErr != nil {
		s.LogEvent("sync", "inbox upsert book %s failed: %v", book.ID, upsertErr)
		return false
	}

	return true
}

func (s *Service) uploadInboxBook(ctx context.Context, engine *booksync.Engine, startEpoch uint64, bookID string, spCompressed, cover []byte) bool {
	storedBook, err := s.store.GetBook(ctx, bookID)
	if err != nil {
		return false
	}
	if _, err := engine.UploadInboxParsedAssets(ctx, storedBook, spCompressed, cover); err != nil {
		s.LogEvent("sync", "inbox upload parsed assets %s failed: %v", bookID, err)
		_ = s.store.UpdatePendingUploadState(ctx, bookID, storage.UploadFailed, err.Error())
		return false
	}

	s.mu.Lock()
	if s.workspaceEpoch != startEpoch {
		s.mu.Unlock()
		return false
	}
	if err := s.store.DeletePendingUpload(ctx, bookID); err != nil {
		s.mu.Unlock()
		s.LogEvent("sync", "inbox clear pending upload %s failed: %v", bookID, err)
		return false
	}
	s.mu.Unlock()

	return true
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
