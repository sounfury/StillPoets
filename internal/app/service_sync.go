package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"stillpoets/internal/domain"
	"stillpoets/internal/storage"
	booksync "stillpoets/internal/sync"
	"stillpoets/internal/sync/webdav"
)

// --- WebDAV 配置、串行同步与旧设备退役 ---

func (s *Service) LoadWebDAVProfile() WebDAVProfile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.profile
}

// HasWebDAVPassword 检查系统安全凭据库中是否已存有 WebDAV 密码。
func (s *Service) HasWebDAVPassword() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	password, err := s.activeWebDAVPasswordLocked()
	return err == nil && password != ""
}

// TestWebDAVConnection 在不落盘修改配置的前提下验证连通性并初始化远端工作区。
func (s *Service) TestWebDAVConnection(ctx context.Context, input WebDAVConfigInput) (*booksync.WorkspaceInitResult, error) {
	profile := webDAVProfileFromInput(input)
	password := input.Password
	if password == "" {
		s.mu.Lock()
		var err error
		password, err = s.webDAVPasswordForInputLocked(profile)
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
	}
	client, err := newWebDAVClient(profile, password, 25*time.Second)
	if err != nil {
		s.LogEvent("webdav", "test connection failed (host=%s): %v", sanitizeBaseURLForDiag(input.BaseURL), err)
		return nil, err
	}
	defer client.CloseIdleConnections()
	res, err := booksync.EnsureWorkspace(ctx, client)
	if err != nil {
		s.LogEvent("webdav", "ensure workspace failed (host=%s folder=%s): %v", sanitizeBaseURLForDiag(input.BaseURL), input.AppFolder, err)
		return nil, err
	}
	s.LogEvent("webdav", "test connection ok (host=%s folder=%s existingRemote=%v devices=%d legacy=%v)",
		sanitizeBaseURLForDiag(input.BaseURL), input.AppFolder, res.ExistingRemote, res.DeviceCount, res.LegacyWorkspace)
	return res, nil
}

// SaveWebDAVConfig 保存 WebDAV 非敏感配置到本地 JSON，密码仅写入系统级安全存储。
// 切换工作区时记录待迁移诗集，由下一轮串行同步验证或上传资产。
func (s *Service) SaveWebDAVConfig(ctx context.Context, input WebDAVConfigInput) error {
	profile := webDAVProfileFromInput(input)
	if parsed, err := url.Parse(profile.BaseURL); err != nil {
		return fmt.Errorf("WebDAV 地址无效：%w", err)
	} else if parsed.User != nil {
		return errors.New("WebDAV 地址中不可内嵌账号密码，请在下方账号密码框中单独填写")
	}

	workspaceChanged, err := s.saveWebDAVProfile(ctx, profile, input.Password)
	if err != nil {
		return err
	}
	s.LogEvent("webdav", "saved config (host=%s folder=%s atomic=%v workspaceChanged=%v)",
		sanitizeBaseURLForDiag(profile.BaseURL), profile.AppFolder, profile.AtomicUpload, workspaceChanged)
	s.notifyDataChanged()
	return nil
}

func (s *Service) saveWebDAVProfile(ctx context.Context, profile WebDAVProfile, password string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false, errors.New("app: service closed")
	}
	prev := s.profile
	workspaceChanged := webDAVWorkspaceID(prev) != webDAVWorkspaceID(profile)
	sources := s.migrationSources
	if workspaceChanged {
		sources = migrationSourcesAfterSwitch(sources, prev, profile)
	}
	if !s.workspacePasswords && prev.BaseURL != "" && workspaceChanged {
		previousPassword, err := s.activeWebDAVPasswordLocked()
		if err != nil {
			return false, err
		}
		if err := s.saveWebDAVPassword(webDAVWorkspacePasswordKey(prev), previousPassword); err != nil {
			return false, err
		}
	}
	if password != "" || !s.workspacePasswords || workspaceChanged {
		if password == "" {
			var err error
			password, err = s.webDAVPasswordForInputLocked(profile)
			if err != nil {
				return false, err
			}
		}
		if err := s.saveWebDAVPassword(webDAVWorkspacePasswordKey(profile), password); err != nil {
			return false, err
		}
		if !workspaceChanged {
			// A same-workspace password can already have changed even if writing the profile fails.
			s.releaseWebDAVClientLocked()
		}
	}
	if workspaceChanged {
		// 先持久化发布屏障，重启时也不会在未验证资产前向新工作区发布书目。
		if err := s.store.PrepareWorkspaceSwitch(ctx); err != nil {
			return false, err
		}
	}
	if err := s.writeWebDAVConfig(profile, sources); err != nil {
		return false, err
	}

	s.workspacePasswords = true
	s.migrationSources = sources
	s.setWebDAVProfileLocked(profile, workspaceChanged || !profile.Enabled)
	if err := s.deleteWebDAVPassword(webdavPasswordKey); err != nil {
		s.LogEvent("webdav", "legacy credential cleanup failed: %v", err)
	}
	return workspaceChanged, nil
}

func (s *Service) setWebDAVProfileLocked(profile WebDAVProfile, clearDevices bool) {
	s.profile = profile
	s.workspaceEpoch++
	if s.syncCancel != nil {
		s.syncCancel()
	}
	s.releaseWebDAVClientLocked()
	if clearDevices {
		s.remoteDevices = nil
	}
}

func (s *Service) releaseWebDAVClientLocked() {
	if s.webdavClient != nil {
		s.webdavClient.CloseIdleConnections()
		s.webdavClient = nil
	}
}

// ClearWebDAVConfig 断开 WebDAV 并删除系统安全存储中的密码。
func (s *Service) ClearWebDAVConfig() error {
	secretErr, err := s.clearWebDAVProfile(context.Background())
	if err != nil {
		return err
	}
	s.LogEvent("webdav", "disconnected webdav and cleared keychain secret (secretErr=%v)", secretErr != nil)
	s.notifyDataChanged()
	return secretErr
}

func (s *Service) clearWebDAVProfile(ctx context.Context) (secretErr, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("app: service closed")
	}
	if err := s.store.ResetWorkspaceSyncState(ctx); err != nil {
		return nil, err
	}
	profile := WebDAVProfile{AppFolder: webdav.DefaultAppFolder}
	if err := s.writeWebDAVConfig(profile, nil); err != nil {
		return nil, err
	}
	secretErr = errors.Join(s.deleteWebDAVPassword(webdavPasswordKey),
		s.deleteWebDAVPassword(webDAVWorkspacePasswordKey(s.profile)), s.deleteWebDAVSourcePasswords())
	s.workspacePasswords = true
	s.migrationSources = nil
	s.setWebDAVProfileLocked(profile, true)
	return secretErr, nil
}

// GetSyncOverview 返回本机同步状态、待上传数量和最近发现的远端设备列表（轻量查元表，不导出 5 张全库表，P2-1）。
func (s *Service) GetSyncOverview(ctx context.Context) (SyncOverview, error) {
	meta, err := s.store.GetSyncMeta(ctx)
	if err != nil {
		return SyncOverview{}, err
	}
	pending, err := s.store.ListPendingUploads(ctx)
	if err != nil {
		return SyncOverview{}, err
	}
	s.mu.Lock()
	configured := s.profile.Enabled && s.profile.BaseURL != ""
	devices := slices.Clone(s.remoteDevices)
	s.mu.Unlock()
	pendingErr := ""
	for _, p := range pending {
		if msg := strings.TrimSpace(p.LastError); msg != "" {
			pendingErr = SanitizeLogText(msg)
			break
		}
	}
	return SyncOverview{
		Configured:    configured,
		InstallID:     meta.InstallID,
		LocalRev:      meta.LocalRev,
		Dirty:         meta.Dirty,
		LastSyncAt:    meta.LastSyncAt,
		PendingCount:  len(pending),
		PendingError:  pendingErr,
		RemoteDevices: devices,
	}, nil
}

func (s *Service) beginSyncOp(ctx context.Context) (context.Context, context.CancelFunc, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, nil, errors.New("app: service closed")
	}
	s.syncWG.Add(1)
	s.mu.Unlock()

	select {
	case s.syncSem <- struct{}{}:
	case <-ctx.Done():
		s.syncWG.Done()
		return nil, nil, ctx.Err()
	case <-s.lifeCtx.Done():
		s.syncWG.Done()
		return nil, nil, errors.New("app: service closed")
	}

	runCtx, cancel := context.WithCancel(ctx)
	stopWatch := context.AfterFunc(s.lifeCtx, cancel)
	s.mu.Lock()
	s.syncCancel = cancel
	s.mu.Unlock()
	release := func() {
		stopWatch()
		cancel()
		s.mu.Lock()
		s.syncCancel = nil
		s.mu.Unlock()
		<-s.syncSem
		s.syncWG.Done()
	}
	return runCtx, release, nil
}

// SyncNow 执行一次完整串行同步：先上传待发布导书资产并回读确认，再原子导出快照并合并发布。
func (s *Service) SyncNow(ctx context.Context, forcePush bool) (*booksync.SyncResult, error) {
	runCtx, release, err := s.beginSyncOp(ctx)
	if err != nil {
		return nil, err
	}

	client, startEpoch, err := s.newWebDAVClientWithEpoch()
	if err != nil {
		release()
		return nil, err
	}

	s.emitSyncToast(SyncToastState{Phase: SyncToastSyncing, Message: "同步中…"})
	s.LogEvent("sync", "sync started (forcePush=%v)", forcePush)

	engine := booksync.NewEngine(client)

	// 0. 巡检当前远端 books/ 目录，若发现本机已下载/持有正文的诗集在远端缺失（例如升版前已切盘），自动补入待上传队列
	s.reconcileRemoteMissingBooks(runCtx, client, startEpoch)

	// 1. 先处理 pending_uploads（遵守「先传资产、回读确认、再发布书目元数据」铁律）
	if pending, pErr := s.store.ListPendingUploads(runCtx); pErr == nil && len(pending) > 0 {
		s.emitSyncToast(SyncToastState{
			Phase:   SyncToastSyncing,
			Message: fmt.Sprintf("正在同步待上传诗集（%d 本）…", len(pending)),
		})
	}
	uploadedAny, uploadFailed := s.flushPendingUploads(runCtx, engine, startEpoch)

	s.emitSyncToast(SyncToastState{Phase: SyncToastSyncing, Message: "正在拉取并合并远端快照…"})

	// 2. 在同一 Store.mu 快照内同时读取元数据与最新待上传队列，彻底消除导书夹塞导致的幽灵书目（P0-3）
	localData, meta, remainingPending, err := s.store.ExportSnapshotWithPending(runCtx)
	if err != nil {
		release()
		s.emitSyncToast(SyncToastState{Phase: SyncToastError, Message: "同步失败"})
		return nil, err
	}
	blockedBooks := make(map[string]struct{}, len(remainingPending))
	for _, p := range remainingPending {
		blockedBooks[p.BookID] = struct{}{}
	}
	res, err := engine.Sync(runCtx, booksync.SyncRequest{
		InstallID:       meta.InstallID,
		LocalRev:        meta.LocalRev,
		LocalCounter:    meta.LamportCounter,
		LocalData:       localData,
		HasDirty:        meta.Dirty || uploadedAny,
		ForcePush:       forcePush || meta.LocalRev == 0,
		KnownRemoteRevs: meta.KnownRemoteRevs,
		PendingBooks:    blockedBooks,
	})
	if err != nil {
		release()
		s.LogEvent("sync", "sync failed: %v", err)
		s.emitSyncToast(SyncToastState{Phase: SyncToastError, Message: "云同步失败"})
		return nil, err
	}

	// 保留仍存在但本轮读取不完整的设备上次已合并的代次；移除未列出的设备。
	remoteDeviceRevs := maps.Clone(res.SyncedDeviceRevs)
	for id := range res.IncompleteDevices {
		if rev := meta.KnownRemoteRevs[id]; rev > 0 {
			remoteDeviceRevs[id] = rev
		}
	}

	// 若存在未上传完成的本地新书，保留 dirty=1 确保下轮不会跳过发布（P1-1）
	keepDirty := uploadFailed || len(remainingPending) > 0
	if keepDirty {
		res.PartialSync = true
	}
	// 配置切换和快照落库共用 mu，避免旧工作区在重置后提交数据。
	s.mu.Lock()
	if s.workspaceEpoch != startEpoch {
		s.mu.Unlock()
		release()
		return nil, errors.New("同步期间 WebDAV 工作区已变更，已中止旧工作区提交")
	}
	err = s.store.ApplyMergedSnapshotEx(runCtx, res.MergedData, storage.ApplySnapshotOptions{
		ExportedDirtySeq: meta.DirtySeq,
		MaxCounter:       res.MaxCounter,
		PublishedRev:     res.PublishedRev,
		Published:        res.Published,
		KeepDirty:        keepDirty,
		RemoteDeviceRevs: remoteDeviceRevs,
	})
	if err == nil {
		s.remoteDevices = slices.Clone(res.RemoteDevices)
		if !keepDirty {
			if cleanupErr := s.finishWebDAVMigrationLocked(); cleanupErr != nil {
				s.LogEvent("webdav", "migration source cleanup failed: %v", cleanupErr)
			}
		}
	}
	s.mu.Unlock()
	if err != nil {
		release()
		s.LogEvent("sync", "apply merged snapshot failed: %v", err)
		s.emitSyncToast(SyncToastState{Phase: SyncToastError, Message: "同步写库失败"})
		return nil, err
	}

	// 第一阶段元数据已落库：先通知书架刷新，确保浏览器整理的分组/书架秒见，不被后续大 EPUB 下载阻塞
	s.notifyDataChanged()

	// 第二阶段：扫描并无感消费 WebDAV inbox/*.ready.json（任意联网设备先到先消费）
	var inboxConsumed bool
	res, inboxConsumed = s.consumeRemoteInbox(runCtx, client, engine, startEpoch, res)

	// 先释放同步串行锁，再派发小组件重建与 UI 回调，避免外部回调重入同步锁（P2-2）
	release()

	_, _ = s.RebuildWidgetTimeline(context.Background())
	if inboxConsumed {
		s.notifyDataChanged()
	}

	if res.PartialSync {
		s.LogEvent("sync", "partial sync finished (published=%v rev=%d remoteDevices=%d warnings=%d remainingPending=%d)",
			res.Published, res.PublishedRev, len(res.RemoteDevices), len(res.Warnings), len(remainingPending))
		s.emitSyncToast(SyncToastState{Phase: SyncToastPartial, Message: "部分同步"})
	} else {
		s.LogEvent("sync", "sync succeeded (published=%v rev=%d remoteDevices=%d)",
			res.Published, res.PublishedRev, len(res.RemoteDevices))
		s.emitSyncToast(SyncToastState{Phase: SyncToastSuccess, Message: "云同步成功"})
	}
	return res, nil
}

// RetireRemoteDevice 先确保完整拉取并合并目标设备的最新代次且发布现役快照，再仅删除已覆盖的旧代次（P0-4）。
func (s *Service) RetireRemoteDevice(ctx context.Context, targetInstallID string) error {
	s.mu.Lock()
	startEpoch := s.workspaceEpoch
	s.mu.Unlock()
	syncRes, err := s.SyncNow(ctx, true)
	if err != nil {
		return fmt.Errorf("退役前发布合并快照失败：%w", err)
	}
	if !syncRes.Published || syncRes.PublishedRev == 0 {
		return errors.New("退役前未能确认现役设备已发布新代次快照，已中止退役")
	}
	if syncRes.PendingBookUploads > 0 {
		return errors.New("仍有诗集待上传，请同步完成后再退役旧设备")
	}
	if syncRes.IncompleteDevices[targetInstallID] {
		return fmt.Errorf("设备 %s 的最新远端快照在本轮读取失败或已损坏，为防止数据丢失已拒绝退役", targetInstallID)
	}
	coveredRev, ok := syncRes.SyncedDeviceRevs[targetInstallID]
	if !ok || coveredRev == 0 {
		return fmt.Errorf("未确认设备 %s 的最新远端代次已完整合并，已拒绝退役", targetInstallID)
	}

	runCtx, release, err := s.beginSyncOp(ctx)
	if err != nil {
		return err
	}
	client, epoch, err := s.newWebDAVClientWithEpoch()
	if err != nil {
		release()
		return err
	}
	if epoch != startEpoch {
		release()
		return errors.New("退役期间 WebDAV 工作区已变更，已中止退役")
	}
	meta, err := s.store.GetSyncMeta(runCtx)
	if err != nil {
		release()
		return err
	}

	engine := booksync.NewEngine(client)
	err = engine.RetireDeviceCovered(runCtx, meta.InstallID, syncRes.PublishedRev, targetInstallID, coveredRev)
	if err != nil {
		release()
		s.LogEvent("sync", "retire device %s failed: %v", targetInstallID, err)
		return err
	}
	s.LogEvent("sync", "retired remote device %s (coveredRev=%d)", targetInstallID, coveredRev)
	s.mu.Lock()
	if s.workspaceEpoch == epoch {
		s.remoteDevices = slices.DeleteFunc(s.remoteDevices, func(id string) bool { return id == targetInstallID })
	}
	s.mu.Unlock()
	release()
	s.notifyDataChanged()
	return nil
}

func (s *Service) flushPendingUploads(ctx context.Context, engine *booksync.Engine, epoch uint64) (bool, bool) {
	pending, err := s.store.ListPendingUploads(ctx)
	if err != nil {
		return false, true
	}
	uploadedAny := false
	anyFailed := false
	var sources []*webdav.Client
	var sourceErr error
	if slices.ContainsFunc(pending, isWorkspaceMigration) {
		sources, sourceErr = s.migrationClients(epoch)
		defer func() {
			for _, source := range sources {
				source.CloseIdleConnections()
			}
		}()
	}

	for _, item := range pending {
		if ctx.Err() != nil {
			return uploadedAny, true
		}
		book, bookErr := s.store.GetBook(ctx, item.BookID)
		var uploadErr error
		if bookErr == nil {
			if isWorkspaceMigration(item) {
				uploadErr = s.uploadMigratingBook(ctx, engine, sources, sourceErr, book, epoch)
			} else {
				uploadErr = uploadPendingBook(ctx, engine, book, item)
			}
		}

		s.mu.Lock()
		if s.workspaceEpoch != epoch {
			s.mu.Unlock()
			return uploadedAny, true
		}
		switch {
		case errors.Is(bookErr, storage.ErrNotFound):
			anyFailed = s.store.DeletePendingUpload(ctx, item.BookID) != nil || anyFailed
		case bookErr != nil:
			anyFailed = true
		case uploadErr != nil:
			s.LogEvent("sync", "book upload %s failed: %v", item.BookID, uploadErr)
			_ = s.store.UpdatePendingUploadState(ctx, item.BookID, storage.UploadFailed, uploadErr.Error())
			anyFailed = true
		default:
			if err := s.store.DeletePendingUpload(ctx, item.BookID); err != nil {
				anyFailed = true
			} else {
				uploadedAny = true
			}
		}
		s.mu.Unlock()
	}
	return uploadedAny, anyFailed
}

func uploadPendingBook(ctx context.Context, engine *booksync.Engine, book domain.Book, item storage.PendingUpload) error {
	spBytes, rawBytes, coverBytes, err := readLocalBookAssets(book, item)
	if err != nil {
		return err
	}
	// 来自 inbox 消费且带不可变构建路径的书目，远端大概率已有浏览器上传的原件，优先校验原件免重传
	if book.ContentPath != "" {
		if _, err := engine.UploadInboxParsedAssets(ctx, book, spBytes, coverBytes); err == nil {
			return nil
		}
	}
	_, err = engine.UploadBookAssets(ctx, book, spBytes, rawBytes, coverBytes)
	return err
}

func (s *Service) newWebDAVClient() (*webdav.Client, error) {
	c, _, err := s.newWebDAVClientWithEpoch()
	return c, err
}

func (s *Service) newWebDAVClientWithEpoch() (*webdav.Client, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	epoch := s.workspaceEpoch
	if s.closed {
		return nil, epoch, errors.New("app: service closed")
	}
	if s.webdavClient != nil {
		return s.webdavClient, epoch, nil
	}
	p := s.profile
	if !p.Enabled || strings.TrimSpace(p.BaseURL) == "" {
		return nil, epoch, errors.New("尚未配置并启用 WebDAV 同步")
	}
	password, err := s.activeWebDAVPasswordLocked()
	if err != nil {
		return nil, epoch, err
	}
	client, err := newWebDAVClient(p, password, 60*time.Second)
	if err == nil {
		s.webdavClient = client
	}
	return client, epoch, err
}

func (s *Service) loadWebDAVProfileFromDisk() {
	raw, err := os.ReadFile(filepath.Join(s.dataDir, webdavProfileFilename))
	if err != nil {
		return
	}
	var config webDAVConfigFile
	if err := json.Unmarshal(raw, &config); err == nil {
		if config.AppFolder == "" {
			config.AppFolder = webdav.DefaultAppFolder
		}
		s.profile = config.WebDAVProfile
		s.workspacePasswords = config.WorkspacePasswords
		s.migrationSources = config.MigrationSources
	}
}

func (s *Service) scheduleDebouncedSync() {
	s.mu.Lock()
	if s.closed || !s.profile.Enabled || strings.TrimSpace(s.profile.BaseURL) == "" {
		s.mu.Unlock()
		return
	}
	delay := s.debounceDelay
	s.debounceGen++
	gen := s.debounceGen
	if s.debounceTimer != nil {
		s.debounceTimer.Stop()
	}
	s.debounceTimer = time.AfterFunc(delay, func() {
		s.mu.Lock()
		stale := s.closed || s.debounceGen != gen
		s.mu.Unlock()
		if stale {
			return
		}
		ctx, cancel := context.WithTimeout(s.lifeCtx, 3*time.Minute)
		defer cancel()
		_, _ = s.SyncNow(ctx, false)
	})
	s.mu.Unlock()
}
