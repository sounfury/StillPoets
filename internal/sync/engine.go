package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"stillpoets/internal/domain"
	"stillpoets/internal/sync/webdav"
)

// DeviceWarning 记录单台设备快照读取或校验异常（不阻塞其余健康设备的同步）。
type DeviceWarning struct {
	InstallID string
	Rev       uint64
	Err       error
}

// SyncRequest 描述一次串行同步执行的输入状态。
type SyncRequest struct {
	InstallID       string              // 当前本机的唯一安装 ID（不随系统备份克隆）
	LocalRev        uint64              // 本机上次已确认发布成功的代次号（首次为 0）
	LocalCounter    uint64              // 本机持久化的 Lamport 逻辑时钟计数
	LocalData       SnapshotData        // 本机待合并的完整元数据快照
	HasDirty        bool                // 本机是否存在尚未发布的变更（或合并后需要主动发布）
	ForcePush       bool                // 强制发布新代次（例如首次建库或退役旧设备前）
	KnownRemoteRevs map[string]uint64   // 已验证且已提交到本地数据库的远端代次
	PendingBooks    map[string]struct{} // 目标工作区尚未验证资产的本机诗集
}

// SyncResult 返回单次同步的合并结果、推进后的逻辑时钟与发布状态。
type SyncResult struct {
	MergedData         SnapshotData
	MaxCounter         uint64
	Published          bool
	PublishedRev       uint64
	PartialSync        bool
	PendingBookUploads int
	Warnings           []DeviceWarning
	RemoteDevices      []string          // 本轮确认已合并有效快照的远端 installId 列表
	SyncedDeviceRevs   map[string]uint64 // 本轮确认已完整合并最新代次的远端 installId -> rev（用于安全退役）
	IncompleteDevices  map[string]bool   // 本轮最新代次读取失败或发生降级回退的远端设备集合
}

// Engine 负责单个 WebDAV 工作区的串行同步调度（防抖任务与手动同步共用同一串行锁）。
type Engine struct {
	mu     sync.Mutex
	client *webdav.Client
}

// NewEngine 创建绑定到指定 WebDAV 客户端的同步引擎。
func NewEngine(client *webdav.Client) *Engine {
	return &Engine{client: client}
}

// Sync 执行完整的「拉取其他设备代次快照 -> 内存确定性合并 -> 按需发布新代次并回读校验 -> 清理旧代次」流程。
func (e *Engine) Sync(ctx context.Context, req SyncRequest) (*SyncResult, error) {
	if !IsValidInstallID(req.InstallID) {
		return nil, fmt.Errorf("sync: invalid local installId %q", req.InstallID)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	entries, err := e.client.ListDir(ctx, StateDir)
	if errors.Is(err, webdav.ErrNotFound) {
		if err := e.client.EnsureDir(ctx, StateDir); err != nil {
			return nil, fmt.Errorf("sync: create state dir: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("sync: list state dir: %w", err)
	}

	// 按 installId 收集各设备的所有代次号并降序排列
	byDevice := make(map[string][]uint64)
	for _, entry := range entries {
		if entry.IsDir {
			continue
		}
		id, rev, ok := ParseSnapshotFilename(entry.Name)
		if !ok {
			continue
		}
		byDevice[id] = append(byDevice[id], rev)
	}
	for id := range byDevice {
		slices.SortFunc(byDevice[id], func(a, b uint64) int {
			if a > b {
				return -1
			}
			if a < b {
				return 1
			}
			return 0
		})
	}

	// 按字典序遍历设备，保证日志和结果完全确定
	deviceIDs := make([]string, 0, len(byDevice))
	for id := range byDevice {
		deviceIDs = append(deviceIDs, id)
	}
	slices.Sort(deviceIDs)

	res := &SyncResult{
		MergedData:         MergeSnapshotData(SnapshotData{}, req.LocalData),
		MaxCounter:         MaxCounterInData(req.LocalCounter, req.LocalData),
		PartialSync:        len(req.PendingBooks) > 0,
		PendingBookUploads: len(req.PendingBooks),
		SyncedDeviceRevs:   make(map[string]uint64),
		IncompleteDevices:  make(map[string]bool),
	}

	var ownRemoteMaxRev, ownValidRev uint64
	if revs := byDevice[req.InstallID]; len(revs) > 0 {
		ownRemoteMaxRev = revs[0]
		if req.LocalRev > 0 && slices.Contains(revs, req.LocalRev) {
			ownValidRev = req.LocalRev
		}
	}
	var toLoad []string
	for _, devID := range deviceIDs {
		if devID == req.InstallID {
			// 本机仅在首次恢复或发现更高远端代次时拉取。
			if req.LocalRev > 0 && ownRemoteMaxRev <= req.LocalRev {
				continue
			}
		} else if rev := req.KnownRemoteRevs[devID]; req.LocalRev > 0 && rev > 0 && rev == byDevice[devID][0] {
			res.SyncedDeviceRevs[devID] = rev
			res.RemoteDevices = append(res.RemoteDevices, devID)
			continue
		}
		toLoad = append(toLoad, devID)
	}

	// 每批最多同时下载两个设备，按字典序合并并及时释放快照内存。
	for batch := range slices.Chunk(toLoad, 2) {
		var wg sync.WaitGroup
		var reads [2]snapshotRead
		for i, id := range batch {
			wg.Go(func() {
				reads[i].snapshot, reads[i].rev, reads[i].warnings = e.loadLatestValidSnapshot(ctx, id, byDevice[id])
			})
		}
		wg.Wait()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for i, id := range batch {
			read := reads[i]
			if id == req.InstallID {
				ownValidRev = max(ownValidRev, read.rev)
			}
			res.mergeRead(id, id == req.InstallID, read)
		}
	}
	slices.Sort(res.RemoteDevices)

	shouldPush := req.HasDirty || req.ForcePush
	if !shouldPush {
		return res, nil
	}
	if err := e.client.EnsureDir(ctx, StateDir); err != nil {
		return nil, fmt.Errorf("sync: ensure state dir before publishing: %w", err)
	}

	nextRev := req.LocalRev
	if ownRemoteMaxRev > nextRev {
		nextRev = ownRemoteMaxRev
	}
	nextRev++

	dataToPublish := res.MergedData
	if len(req.PendingBooks) > 0 {
		dataToPublish.Books = slices.DeleteFunc(slices.Clone(dataToPublish.Books), func(book domain.Book) bool {
			_, pending := req.PendingBooks[book.ID]
			return pending && !book.Deleted
		})
	}
	snap := &DeviceSnapshot{
		SchemaVersion: ProtocolSchemaVersion,
		InstallID:     req.InstallID,
		Rev:           nextRev,
		MaxCounter:    res.MaxCounter,
		GeneratedAt:   domain.NowMillis(),
		Data:          dataToPublish,
	}
	payload, err := EncodeSnapshot(snap)
	if err != nil {
		return nil, err
	}
	targetPath, err := SnapshotPath(req.InstallID, nextRev)
	if err != nil {
		return nil, err
	}

	// 1. 上传新代次（不覆盖旧代次）
	if _, err := e.client.Put(ctx, targetPath, payload, ContentTypeGzip); err != nil {
		return nil, fmt.Errorf("sync: put snapshot %s: %w", targetPath, err)
	}

	// 2. 回读确认新代次可读且内容完全一致后，才确认本轮提交成功（兼容网盘写后短暂 404）
	verifyBytes, err := e.readBackWithRetry(ctx, targetPath, MaxSnapshotCompressedBytes)
	if err != nil {
		return nil, fmt.Errorf("sync: verify uploaded snapshot %s: %w", targetPath, err)
	}
	if !bytes.Equal(verifyBytes, payload) {
		return nil, fmt.Errorf("sync: verify uploaded snapshot %s: byte mismatch after read-back", targetPath)
	}
	if _, err := DecodeSnapshot(verifyBytes, req.InstallID, nextRev); err != nil {
		return nil, fmt.Errorf("sync: verify decoded snapshot %s: %w", targetPath, err)
	}

	res.Published = true
	res.PublishedRev = nextRev

	// 3. 提交确认后再清理本设备更早的旧代次，严格保留最近 KeepSnapshotGenerations (2) 份已校验有效的代次
	e.cleanupOwnOldRevisions(ctx, req.InstallID, nextRev, byDevice[req.InstallID], ownValidRev)

	return res, nil
}

type snapshotRead struct {
	snapshot *DeviceSnapshot
	rev      uint64
	warnings []DeviceWarning
}

func (res *SyncResult) mergeRead(id string, local bool, read snapshotRead) {
	if len(read.warnings) > 0 {
		res.PartialSync = true
		res.Warnings = append(res.Warnings, read.warnings...)
	}
	if !local {
		if read.snapshot == nil || len(read.warnings) > 0 {
			res.IncompleteDevices[id] = true
		} else {
			res.SyncedDeviceRevs[id] = read.rev
		}
		if read.snapshot != nil {
			res.RemoteDevices = append(res.RemoteDevices, id)
		}
	}
	if read.snapshot != nil {
		res.MergedData = MergeSnapshotData(res.MergedData, read.snapshot.Data)
		res.MaxCounter = MaxCounterInData(max(res.MaxCounter, read.snapshot.MaxCounter), res.MergedData)
	}
}

func (e *Engine) readBackWithRetry(ctx context.Context, relPath string, maxBytes int64) ([]byte, error) {
	return verifyAfterWrite(ctx, func() ([]byte, error) {
		raw, _, err := e.client.GetWithLimit(ctx, relPath, maxBytes)
		return raw, err
	})
}

func (e *Engine) statWithRetry(ctx context.Context, relPath string) (*webdav.Entry, error) {
	return verifyAfterWrite(ctx, func() (*webdav.Entry, error) {
		return e.client.Stat(ctx, relPath)
	})
}

// verifyAfterWrite 对短暂的写后 404 做有界退避，兼容 AList/OpenList 挂载点的最终一致性。
func verifyAfterWrite[T any](ctx context.Context, verify func() (T, error)) (T, error) {
	delays := [...]time.Duration{300 * time.Millisecond, 700 * time.Millisecond, 1500 * time.Millisecond}
	for attempt := 0; ; attempt++ {
		result, err := verify()
		if !errors.Is(err, webdav.ErrNotFound) || attempt == len(delays) {
			return result, err
		}
		timer := time.NewTimer(delays[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, ctx.Err()
		case <-timer.C:
		}
	}
}

// loadLatestValidSnapshot 从最新代次向旧代次回退，直到找到一份完整通过 gzip+JSON+协议校验的快照。
func (e *Engine) loadLatestValidSnapshot(ctx context.Context, installID string, descRevs []uint64) (*DeviceSnapshot, uint64, []DeviceWarning) {
	var warnings []DeviceWarning
	for _, rev := range descRevs {
		relPath, err := SnapshotPath(installID, rev)
		if err != nil {
			continue
		}
		raw, _, err := e.client.GetWithLimit(ctx, relPath, MaxSnapshotCompressedBytes)
		if err != nil {
			warnings = append(warnings, DeviceWarning{InstallID: installID, Rev: rev, Err: err})
			continue
		}
		snap, err := DecodeSnapshot(raw, installID, rev)
		if err != nil {
			warnings = append(warnings, DeviceWarning{InstallID: installID, Rev: rev, Err: err})
			continue
		}
		return snap, rev, warnings
	}
	return nil, 0, warnings
}

// cleanupOwnOldRevisions 仅清理当前安装 ID 名下的过期代次，保留最新提交的 nextRev 以及上一份经真实可读校验的代次（P1-11）。
func (e *Engine) cleanupOwnOldRevisions(ctx context.Context, installID string, nextRev uint64, allRemoteDesc []uint64, knownValidRev uint64) {
	if len(allRemoteDesc) < KeepSnapshotGenerations {
		return
	}
	keep := map[uint64]struct{}{nextRev: {}}
	if knownValidRev > 0 && knownValidRev < nextRev {
		keep[knownValidRev] = struct{}{}
	}

	// 若尚未确认到足够的有效旧代次，先按降序回读校验，确认至少留住 1 份真实完好的旧快照后再删除更早代次
	for _, rev := range allRemoteDesc {
		if len(keep) >= KeepSnapshotGenerations {
			break
		}
		if rev >= nextRev {
			continue
		}
		relPath, err := SnapshotPath(installID, rev)
		if err != nil {
			continue
		}
		raw, _, err := e.client.GetWithLimit(ctx, relPath, MaxSnapshotCompressedBytes)
		if err != nil {
			// 网络或瞬时错误时直接延后清理，避免误删唯一完好旧代次
			if !errors.Is(err, webdav.ErrNotFound) {
				return
			}
			continue
		}
		if _, err := DecodeSnapshot(raw, installID, rev); err == nil {
			keep[rev] = struct{}{}
		}
	}

	if len(keep) < KeepSnapshotGenerations {
		return
	}

	for _, rev := range allRemoteDesc {
		if _, ok := keep[rev]; ok {
			continue
		}
		relPath, err := SnapshotPath(installID, rev)
		if err != nil {
			continue
		}
		_ = e.client.Delete(ctx, relPath)
	}
}

// BookAssetUploadResult 描述导书正文与原文件上传结果。
type BookAssetUploadResult struct {
	CoverWarning error // 封面上传失败不阻断书目发布，由读端占位图降级
}

// UploadBookAssets 在发布书目元数据前，验证解析包的内容以及原始文件的存在性和可用的长度属性。
func (e *Engine) UploadBookAssets(
	ctx context.Context,
	book domain.Book,
	spbookCompressed []byte,
	rawOriginal []byte,
	coverWebP []byte,
) (*BookAssetUploadResult, error) {
	if book.ID == "" {
		return nil, errors.New("sync: book id is empty")
	}
	if book.ContentHash == "" {
		return nil, errors.New("sync: book contentHash is empty")
	}
	if book.RawFilename == "" {
		return nil, errors.New("sync: book rawFilename is empty")
	}
	if len(spbookCompressed) == 0 || len(rawOriginal) == 0 {
		return nil, errors.New("sync: spbook and raw original bytes are required")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	bookDir := fmt.Sprintf("%s/%s", BooksDir, book.ID)
	if err := e.client.EnsureDir(ctx, bookDir); err != nil {
		return nil, fmt.Errorf("sync: ensure book dir %s: %w", bookDir, err)
	}

	// 1. 上传解析包并回读校验 SHA-256 + 解压可读性 + BookID 一致（带写后短暂 404 退避）
	parsedPath := BookParsedPath(book.ID)
	if _, err := e.client.Put(ctx, parsedPath, spbookCompressed, ContentTypeGzip); err != nil {
		return nil, fmt.Errorf("sync: put parsed spbook %s: %w", parsedPath, err)
	}
	readBackParsed, err := e.readBackWithRetry(ctx, parsedPath, MaxSPBookCompressedBytes)
	if err != nil {
		return nil, fmt.Errorf("sync: verify parsed spbook %s: %w", parsedPath, err)
	}
	if _, err := DecodeSPBook(readBackParsed, book.ContentHash, book.ID); err != nil {
		return nil, fmt.Errorf("sync: verify decoded spbook %s: %w", parsedPath, err)
	}

	// 2. 检查原始导入文件的存在性和可用长度，避免下载整个备份。
	rawPath := BookRawPath(book.ID, book.RawFilename)
	if _, err := e.client.Put(ctx, rawPath, rawOriginal, "application/octet-stream"); err != nil {
		return nil, fmt.Errorf("sync: put raw file %s: %w", rawPath, err)
	}
	rawEntry, err := e.statWithRetry(ctx, rawPath)
	if err != nil {
		return nil, fmt.Errorf("sync: verify raw file %s: %w", rawPath, err)
	}
	if err := validateRawBookEntry(rawEntry, int64(len(rawOriginal))); err != nil {
		return nil, fmt.Errorf("sync: verify raw file %s: %w", rawPath, err)
	}

	// 3. 封面图非强依赖，上传失败仅记录警告，不阻断书目发布
	result := &BookAssetUploadResult{}
	if len(coverWebP) > 0 {
		coverPath := BookCoverPath(book.ID)
		if _, err := e.client.Put(ctx, coverPath, coverWebP, "image/webp"); err != nil {
			result.CoverWarning = err
		}
	}
	return result, nil
}

// UploadBookParsedAssets 仅上传已解析的 .spbook.json.gz 正文包（及可选封面）并回读校验，
// 用于本机仅持有已下载正文（无原始 EPUB 备份且旧 WebDAV 源不可达）时的跨工作区救援迁移。
func (e *Engine) UploadBookParsedAssets(
	ctx context.Context,
	book domain.Book,
	spbookCompressed []byte,
	coverWebP []byte,
) (*BookAssetUploadResult, error) {
	if book.ID == "" {
		return nil, errors.New("sync: book id is empty")
	}
	if book.ContentHash == "" {
		return nil, errors.New("sync: book contentHash is empty")
	}
	if len(spbookCompressed) == 0 {
		return nil, errors.New("sync: spbook bytes are required")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	bookDir := fmt.Sprintf("%s/%s", BooksDir, book.ID)
	if err := e.client.EnsureDir(ctx, bookDir); err != nil {
		return nil, fmt.Errorf("sync: ensure book dir %s: %w", bookDir, err)
	}

	parsedPath := BookParsedPath(book.ID)
	if _, err := e.client.Put(ctx, parsedPath, spbookCompressed, ContentTypeGzip); err != nil {
		return nil, fmt.Errorf("sync: put parsed spbook %s: %w", parsedPath, err)
	}
	readBackParsed, err := e.readBackWithRetry(ctx, parsedPath, MaxSPBookCompressedBytes)
	if err != nil {
		return nil, fmt.Errorf("sync: verify parsed spbook %s: %w", parsedPath, err)
	}
	if _, err := DecodeSPBook(readBackParsed, book.ContentHash, book.ID); err != nil {
		return nil, fmt.Errorf("sync: verify decoded spbook %s: %w", parsedPath, err)
	}

	result := &BookAssetUploadResult{}
	if len(coverWebP) > 0 {
		coverPath := BookCoverPath(book.ID)
		if _, err := e.client.Put(ctx, coverPath, coverWebP, "image/webp"); err != nil {
			result.CoverWarning = err
		}
	}
	return result, nil
}

// VerifyBookParsedAsset 确认目标工作区已存在且可解码该版本的 parsed.spbook.json.gz。
func (e *Engine) VerifyBookParsedAsset(ctx context.Context, book domain.Book) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	parsed, _, err := e.client.GetWithLimit(ctx, BookParsedPath(book.ID), MaxSPBookCompressedBytes)
	if err != nil {
		return err
	}
	_, err = DecodeSPBook(parsed, book.ContentHash, book.ID)
	return err
}

// VerifyBookAssets 确认目标工作区已有该版本的正文与原始文件，可跳过重复迁移。
func (e *Engine) VerifyBookAssets(ctx context.Context, book domain.Book) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	parsed, _, err := e.client.GetWithLimit(ctx, BookParsedPath(book.ID), MaxSPBookCompressedBytes)
	if err != nil {
		return err
	}
	if _, err := DecodeSPBook(parsed, book.ContentHash, book.ID); err != nil {
		return err
	}
	entry, err := e.client.Stat(ctx, BookRawPath(book.ID, book.RawFilename))
	if err != nil {
		return err
	}
	return validateRawBookEntry(entry, book.RawSize)
}

func validateRawBookEntry(entry *webdav.Entry, expectedSize int64) error {
	if entry.IsDir {
		return errors.New("expected a file, found a collection")
	}
	if expectedSize > 0 && entry.ContentLength > 0 && entry.ContentLength != expectedSize {
		return fmt.Errorf("content length mismatch: got %d, want %d", entry.ContentLength, expectedSize)
	}
	return nil
}

// RetireDevice 手动退役旧安装设备（例如重装 App 或更换手机后）。
// 安全护栏：仅在确认现役设备 activeInstallID 的最新远端快照可读有效后，才删除 retiredInstallID 的快照文件。
func (e *Engine) RetireDevice(ctx context.Context, activeInstallID string, activeRev uint64, retiredInstallID string) error {
	return e.RetireDeviceCovered(ctx, activeInstallID, activeRev, retiredInstallID, 0)
}

// RetireDeviceCovered 带已覆盖代次上界的退役接口（P0-4）：
// 当 maxCoveredRev > 0 时，若远端发现目标设备存在 rev > maxCoveredRev 的更新代次，拒绝退役以防删除尚未合并的新数据。
func (e *Engine) RetireDeviceCovered(ctx context.Context, activeInstallID string, activeRev uint64, retiredInstallID string, maxCoveredRev uint64) error {
	if !IsValidInstallID(activeInstallID) || !IsValidInstallID(retiredInstallID) {
		return errors.New("sync: invalid installId for retire")
	}
	if activeInstallID == retiredInstallID {
		return errors.New("sync: cannot retire active installId")
	}
	if activeRev == 0 {
		return errors.New("sync: active device must publish a merged snapshot before retiring another device")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// 先验证现役设备的合并后快照在远端真实可读，防止未备份就误删旧设备数据
	activePath, err := SnapshotPath(activeInstallID, activeRev)
	if err != nil {
		return err
	}
	raw, _, err := e.client.GetWithLimit(ctx, activePath, MaxSnapshotCompressedBytes)
	if err != nil {
		return fmt.Errorf("sync: verify active snapshot before retire: %w", err)
	}
	if _, err := DecodeSnapshot(raw, activeInstallID, activeRev); err != nil {
		return fmt.Errorf("sync: active snapshot invalid before retire: %w", err)
	}

	entries, err := e.client.ListDir(ctx, StateDir)
	if err != nil {
		return fmt.Errorf("sync: list state dir for retire: %w", err)
	}
	var toDelete []uint64
	for _, entry := range entries {
		if entry.IsDir {
			continue
		}
		id, rev, ok := ParseSnapshotFilename(entry.Name)
		if !ok || id != retiredInstallID {
			continue
		}
		if maxCoveredRev > 0 && rev > maxCoveredRev {
			return fmt.Errorf("sync: target device %s published newer rev %d (> covered %d), aborting retire", retiredInstallID, rev, maxCoveredRev)
		}
		toDelete = append(toDelete, rev)
	}
	for _, rev := range toDelete {
		relPath, err := SnapshotPath(retiredInstallID, rev)
		if err != nil {
			continue
		}
		if err := e.client.Delete(ctx, relPath); err != nil {
			return fmt.Errorf("sync: delete retired snapshot %s: %w", relPath, err)
		}
	}
	return nil
}
