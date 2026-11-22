package sync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	InstallID    string       // 当前本机的唯一安装 ID（不随系统备份克隆）
	LocalRev     uint64       // 本机上次已确认发布成功的代次号（首次为 0）
	LocalCounter uint64       // 本机持久化的 Lamport 逻辑时钟计数
	LocalData    SnapshotData // 本机待合并的完整元数据快照
	HasDirty     bool         // 本机是否存在尚未发布的变更（或合并后需要主动发布）
	ForcePush    bool         // 强制发布新代次（例如首次建库或退役旧设备前）
}

// SyncResult 返回单次同步的合并结果、推进后的逻辑时钟与发布状态。
type SyncResult struct {
	MergedData        SnapshotData
	MaxCounter        uint64
	Published         bool
	PublishedRev      uint64
	PartialSync       bool
	Warnings          []DeviceWarning
	RemoteDevices     []string          // 本轮成功读取到有效快照的远端 installId 列表
	SyncedDeviceRevs  map[string]uint64 // 本轮已完整读取其最新代次并合并的远端 installId -> rev（用于安全退役）
	IncompleteDevices map[string]bool   // 本轮最新代次读取失败或发生降级回退的远端设备集合
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

	if err := e.client.EnsureDir(ctx, StateDir); err != nil {
		return nil, fmt.Errorf("sync: ensure state dir: %w", err)
	}

	entries, err := e.client.ListDir(ctx, StateDir)
	if err != nil {
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
		MergedData:        MergeSnapshotData(SnapshotData{}, req.LocalData),
		MaxCounter:        MaxCounterInData(req.LocalCounter, req.LocalData),
		SyncedDeviceRevs:  make(map[string]uint64),
		IncompleteDevices: make(map[string]bool),
	}

	var ownRemoteMaxRev uint64
	var ownValidRevs []uint64

	for _, devID := range deviceIDs {
		revs := byDevice[devID]
		if devID == req.InstallID {
			if len(revs) > 0 {
				ownRemoteMaxRev = revs[0]
			}
			// 仅在本地尚未发布过（如首次恢复）或远端存在比本地记录更高的代次时读取自身远端快照；
			// 注意此处不把未验证的目录项直接当成 ownValidRevs，回收阶段会单独验证保留代次（P1-11）。
			if req.LocalRev > 0 && ownRemoteMaxRev <= req.LocalRev {
				continue
			}
		}

		loadedSnap, validRev, warnings := e.loadLatestValidSnapshot(ctx, devID, revs)
		if len(warnings) > 0 {
			res.PartialSync = true
			res.Warnings = append(res.Warnings, warnings...)
			if devID != req.InstallID {
				res.IncompleteDevices[devID] = true
			}
		}
		if loadedSnap == nil {
			if devID != req.InstallID {
				res.IncompleteDevices[devID] = true
			}
			continue
		}
		if devID == req.InstallID {
			ownValidRevs = append(ownValidRevs, validRev)
		} else {
			res.RemoteDevices = append(res.RemoteDevices, devID)
			if len(warnings) == 0 && len(revs) > 0 && validRev == revs[0] {
				res.SyncedDeviceRevs[devID] = validRev
			}
		}

		res.MergedData = MergeSnapshotData(res.MergedData, loadedSnap.Data)
		if loadedSnap.MaxCounter > res.MaxCounter {
			res.MaxCounter = loadedSnap.MaxCounter
		}
		res.MaxCounter = MaxCounterInData(res.MaxCounter, res.MergedData)
	}

	shouldPush := req.HasDirty || req.ForcePush
	if !shouldPush {
		return res, nil
	}

	nextRev := req.LocalRev
	if ownRemoteMaxRev > nextRev {
		nextRev = ownRemoteMaxRev
	}
	nextRev++

	snap := &DeviceSnapshot{
		SchemaVersion: ProtocolSchemaVersion,
		InstallID:     req.InstallID,
		Rev:           nextRev,
		MaxCounter:    res.MaxCounter,
		GeneratedAt:   domain.NowMillis(),
		Data:          res.MergedData,
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
	e.cleanupOwnOldRevisions(ctx, req.InstallID, nextRev, byDevice[req.InstallID], ownValidRevs)

	return res, nil
}

// readBackWithRetry 在 PUT 完成后做有界 404 退避回读，兼容 AList/115 等挂载点的写后最终一致性延迟（P1-8）。
func (e *Engine) readBackWithRetry(ctx context.Context, relPath string, maxBytes int64) ([]byte, error) {
	delays := []time.Duration{300 * time.Millisecond, 700 * time.Millisecond, 1500 * time.Millisecond}
	var lastErr error
	for attempt := 0; attempt <= len(delays); attempt++ {
		raw, _, err := e.client.GetWithLimit(ctx, relPath, maxBytes)
		if err == nil {
			return raw, nil
		}
		lastErr = err
		if !errors.Is(err, webdav.ErrNotFound) || attempt == len(delays) {
			break
		}
		timer := time.NewTimer(delays[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, lastErr
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
func (e *Engine) cleanupOwnOldRevisions(ctx context.Context, installID string, nextRev uint64, allRemoteDesc, knownValidDesc []uint64) {
	if len(allRemoteDesc) < KeepSnapshotGenerations {
		return
	}
	keep := map[uint64]struct{}{nextRev: {}}
	for _, rev := range knownValidDesc {
		if len(keep) >= KeepSnapshotGenerations {
			break
		}
		if rev < nextRev {
			keep[rev] = struct{}{}
		}
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

// UploadBookAssets 在发布书目元数据前，先确保 books/<bookId>/ 下的解析包与原始导入文件均上传且回读校验通过。
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

	// 2. 上传原始导入文件（如 .epub）并回读校验哈希，保证原文件备份完整可读
	rawPath := BookRawPath(book.ID, book.RawFilename)
	rawSum := sha256.Sum256(rawOriginal)
	wantRawHash := hex.EncodeToString(rawSum[:])
	if _, err := e.client.Put(ctx, rawPath, rawOriginal, "application/octet-stream"); err != nil {
		return nil, fmt.Errorf("sync: put raw file %s: %w", rawPath, err)
	}
	readBackRaw, err := e.readBackWithRetry(ctx, rawPath, int64(len(rawOriginal))+1024)
	if err != nil {
		return nil, fmt.Errorf("sync: verify raw file %s: %w", rawPath, err)
	}
	gotRawSum := sha256.Sum256(readBackRaw)
	if hex.EncodeToString(gotRawSum[:]) != wantRawHash {
		return nil, fmt.Errorf("sync: verify raw file %s: sha256 mismatch", rawPath)
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
