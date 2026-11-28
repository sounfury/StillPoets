package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
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

// The profile file tracks which workspaces can supply books during a migration.
// Each workspace password has its own entry in the system credential store.
type webDAVConfigFile struct {
	WebDAVProfile
	WorkspacePasswords bool            `json:"workspacePasswords,omitempty"`
	MigrationSources   []WebDAVProfile `json:"migrationSources,omitempty"`
}

func webDAVProfileFromInput(input WebDAVConfigInput) WebDAVProfile {
	folder := strings.TrimSpace(input.AppFolder)
	if folder == "" {
		folder = webdav.DefaultAppFolder
	}
	return WebDAVProfile{
		Enabled:      input.Enabled,
		BaseURL:      strings.TrimSpace(input.BaseURL),
		Username:     strings.TrimSpace(input.Username),
		AppFolder:    folder,
		AtomicUpload: input.AtomicUpload,
	}
}

func webDAVWorkspaceID(p WebDAVProfile) [sha256.Size]byte {
	return sha256.Sum256([]byte(strings.TrimRight(p.BaseURL, "/") + "\x00" +
		strings.Trim(p.AppFolder, "/") + "\x00" + p.Username))
}

func webDAVWorkspacePasswordKey(p WebDAVProfile) string {
	id := webDAVWorkspaceID(p)
	return webdavPasswordKey + "_" + hex.EncodeToString(id[:])
}

func (s *Service) activeWebDAVPasswordLocked() (string, error) {
	key := webdavPasswordKey
	if s.workspacePasswords {
		key = webDAVWorkspacePasswordKey(s.profile)
	}
	return s.readWebDAVPassword(key)
}

func (s *Service) webDAVPasswordForInputLocked(p WebDAVProfile) (string, error) {
	current := s.profile
	if strings.TrimRight(current.BaseURL, "/") == strings.TrimRight(p.BaseURL, "/") && current.Username == p.Username {
		return s.activeWebDAVPasswordLocked()
	}
	for _, source := range s.migrationSources {
		if webDAVWorkspaceID(source) == webDAVWorkspaceID(p) {
			return s.readWebDAVPassword(webDAVWorkspacePasswordKey(source))
		}
	}
	return "", nil
}

func (s *Service) readWebDAVPassword(key string) (string, error) {
	password, err := s.secrets.Get(key)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(password), err
}

func (s *Service) saveWebDAVPassword(key, password string) error {
	if password == "" {
		return nil
	}
	if err := s.secrets.Set(key, []byte(password)); err != nil {
		return fmt.Errorf("保存 WebDAV 密码到系统安全存储失败：%w", err)
	}
	return nil
}

func (s *Service) deleteWebDAVPassword(key string) error {
	if err := s.secrets.Delete(key); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("清除系统安全存储中的密码失败：%w", err)
	}
	return nil
}

func (s *Service) deleteWebDAVSourcePasswords() error {
	var errs []error
	for _, p := range s.migrationSources {
		errs = append(errs, s.deleteWebDAVPassword(webDAVWorkspacePasswordKey(p)))
	}
	return errors.Join(errs...)
}

// finishWebDAVMigrationLocked drops the source credentials once all queued books have been verified.
func (s *Service) finishWebDAVMigrationLocked() error {
	if len(s.migrationSources) == 0 {
		return nil
	}
	if err := s.deleteWebDAVSourcePasswords(); err != nil {
		return err
	}
	if err := s.writeWebDAVConfig(s.profile, nil); err != nil {
		return err
	}
	s.migrationSources = nil
	return nil
}

func (s *Service) writeWebDAVConfig(p WebDAVProfile, sources []WebDAVProfile) error {
	return writeJSONAtomic(filepath.Join(s.dataDir, webdavProfileFilename), webDAVConfigFile{
		WebDAVProfile: p, WorkspacePasswords: true, MigrationSources: sources,
	})
}

func newWebDAVClient(p WebDAVProfile, password string, timeout time.Duration) (*webdav.Client, error) {
	return webdav.NewClient(webdav.Config{
		BaseURL:                     p.BaseURL,
		Username:                    p.Username,
		Password:                    password,
		AppFolder:                   p.AppFolder,
		AtomicUpload:                p.AtomicUpload,
		AllowCrossOriginGetRedirect: true,
		Timeout:                     timeout,
	})
}

func (s *Service) migrationClients(epoch uint64) ([]*webdav.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.workspaceEpoch != epoch {
		return nil, context.Canceled
	}
	clients := make([]*webdav.Client, 0, len(s.migrationSources))
	var errs []error
	for _, p := range s.migrationSources {
		password, err := s.readWebDAVPassword(webDAVWorkspacePasswordKey(p))
		if err == nil {
			var client *webdav.Client
			client, err = newWebDAVClient(p, password, 60*time.Second)
			if err == nil {
				clients = append(clients, client)
			}
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return clients, errors.Join(errs...)
}

func isWorkspaceMigration(item storage.PendingUpload) bool {
	return item.LocalSPBookPath == "" && item.LocalRawPath == ""
}

func (s *Service) uploadMigratingBook(ctx context.Context, target *booksync.Engine, sources []*webdav.Client, sourceErr error, book domain.Book, epoch uint64) error {
	if !validBookAssetName(book.ID) || book.ContentHash == "" {
		return errors.New("缺少有效的诗集资产标识")
	}
	hasValidRawName := validBookAssetName(book.RawFilename)
	var verifyErr error
	if hasValidRawName {
		verifyErr = target.VerifyBookAssets(ctx, book)
		if verifyErr == nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		item := storage.PendingUpload{
			LocalSPBookPath: filepath.Join(s.dataDir, "cache", "spbook", book.ID+".spbook.json.gz"),
			LocalRawPath:    filepath.Join(s.dataDir, "cache", "raw", book.ID+"_"+book.RawFilename),
			LocalCoverPath:  filepath.Join(s.dataDir, "cache", "covers", book.ID+".img"),
		}
		if sp, raw, cover, err := readLocalBookAssets(book, item); err == nil {
			_, err = target.UploadBookAssets(ctx, book, sp, raw, cover)
			return err
		}
	}

	errs := []error{sourceErr}
	if verifyErr != nil {
		errs = append([]error{fmt.Errorf("新工作区诗集资产未通过校验：%w", verifyErr)}, errs...)
	}
	if hasValidRawName {
		for _, source := range sources {
			if err := ctx.Err(); err != nil {
				return err
			}
			sp, raw, cover, err := readRemoteBookAssets(ctx, source, book)
			if err == nil {
				_, err = target.UploadBookAssets(ctx, book, sp, raw, cover)
				return err
			}
			errs = append(errs, err)
		}
	}

	// 若新工作区已存在校验通过的 parsed.spbook.json.gz，且本机与旧工作区都无原始 EPUB 可补传，则视为可正常阅读并跳过。
	if err := target.VerifyBookParsedAsset(ctx, book); err == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// 兜底救援：当书当初是从旧盘按需下载到本机（本机无原始 EPUB 且旧盘已不可达），
	// 直接使用本地缓存的 .spbook.json.gz 或从 SQLite 完整诗篇重建正文包并上传。
	parsedErr := s.uploadLocalParsedBookFallback(ctx, target, book, epoch)
	if parsedErr == nil {
		return nil
	}
	errs = append(errs, parsedErr)
	return fmt.Errorf("旧 WebDAV 工作区与本地缺少可迁移的正文或原始文件：%w", errors.Join(errs...))
}

func (s *Service) uploadLocalParsedBookFallback(ctx context.Context, target *booksync.Engine, book domain.Book, epoch uint64) error {
	coverPath := filepath.Join(s.dataDir, "cache", "covers", book.ID+".img")
	coverRaw, _ := os.ReadFile(coverPath)
	cover := validBookCover(book, coverRaw)

	// 1. 优先复用本地 cache/spbook 下哈希完全匹配的解析包，保持原 ContentHash 不变。
	localSPPath := filepath.Join(s.dataDir, "cache", "spbook", book.ID+".spbook.json.gz")
	if spBytes, err := os.ReadFile(localSPPath); err == nil && len(spBytes) > 0 {
		if _, decErr := booksync.DecodeSPBook(spBytes, book.ContentHash, book.ID); decErr == nil {
			if _, upErr := target.UploadBookParsedAssets(ctx, book, spBytes, cover); upErr != nil {
				return fmt.Errorf("上传本地正文包失败：%w", upErr)
			}
			s.LogEvent("sync", "migrated book %s from local cached spbook (no raw backup)", book.ID)
			return nil
		}
	}

	// 2. 若本机 SQLite 已下载完整诗篇，则重建 .spbook.json.gz 上传校验后再原子递增 ContentHash/ContentVer。
	if book.LocalState != domain.LocalStateDownloaded {
		return errors.New("本地尚未下载该诗集正文")
	}
	poems, err := s.store.ListPoems(ctx, book.ID)
	if err != nil {
		return fmt.Errorf("读取本地诗集正文失败：%w", err)
	}
	if len(poems) == 0 {
		return errors.New("本地诗集正文为空，无法重建上传包")
	}

	rebuilt := &domain.SPBook{
		SchemaVersion: booksync.SPBookSchemaVersion,
		BookID:        book.ID,
		Title:         book.Title,
		Author:        book.Author,
		SourceFormat:  "epub",
		Poems:         poems,
	}
	spBytes, newHash, err := booksync.EncodeSPBook(rebuilt)
	if err != nil {
		return fmt.Errorf("重建诗集正文包失败：%w", err)
	}

	updatedBook := book
	updatedBook.ContentHash = newHash
	updatedBook.ContentSize = int64(len(spBytes))
	if _, err := target.UploadBookParsedAssets(ctx, updatedBook, spBytes, cover); err != nil {
		return fmt.Errorf("上传重建的正文包失败：%w", err)
	}

	s.mu.Lock()
	stale := s.workspaceEpoch != epoch
	s.mu.Unlock()
	if stale {
		return errors.New("同步期间 WebDAV 工作区已变更")
	}

	// 顺手缓存到 cache/spbook，避免后续重复重建
	_ = os.MkdirAll(filepath.Dir(localSPPath), 0o755)
	_ = os.WriteFile(localSPPath, spBytes, 0o600)

	if newHash != book.ContentHash || int64(len(spBytes)) != book.ContentSize {
		if err := s.store.UpdateBookParsedContentMeta(ctx, book.ID, newHash, int64(len(spBytes))); err != nil {
			return fmt.Errorf("更新重建诗集元数据哈希失败：%w", err)
		}
	}
	s.LogEvent("sync", "migrated book %s by rebuilding spbook from local sqlite (poems=%d)", book.ID, len(poems))
	return nil
}

func (s *Service) canSupplyBookForMigration(book domain.Book, hasMigrationSources bool) bool {
	if hasMigrationSources {
		return true
	}
	if book.LocalState == domain.LocalStateDownloaded {
		return true
	}
	if validBookAssetName(book.ID) {
		localSPPath := filepath.Join(s.dataDir, "cache", "spbook", book.ID+".spbook.json.gz")
		if info, err := os.Stat(localSPPath); err == nil && info.Size() > 0 {
			return true
		}
	}
	return false
}

// reconcileRemoteMissingBooks 用单次 ListDir(books/) 巡检当前 WebDAV 工作区是否缺少本机可供源的诗集目录，
// 自动补入 pending_uploads，避免旧版切盘升级后长期停留在「待上传 0 本」。
func (s *Service) reconcileRemoteMissingBooks(ctx context.Context, client *webdav.Client, epoch uint64) {
	books, err := s.store.ListBooks(ctx, "")
	if err != nil || len(books) == 0 {
		return
	}

	s.mu.Lock()
	if s.workspaceEpoch != epoch {
		s.mu.Unlock()
		return
	}
	hasSources := len(s.migrationSources) > 0
	s.mu.Unlock()

	candidates := make([]domain.Book, 0, len(books))
	for _, b := range books {
		if b.Deleted || !validBookAssetName(b.ID) || b.ContentHash == "" {
			continue
		}
		if s.canSupplyBookForMigration(b, hasSources) {
			candidates = append(candidates, b)
		}
	}
	if len(candidates) == 0 {
		return
	}

	entries, err := client.ListDir(ctx, booksync.BooksDir)
	remoteDirs := make(map[string]struct{})
	switch {
	case err == nil:
		for _, e := range entries {
			if e.IsDir && e.Name != "" {
				remoteDirs[e.Name] = struct{}{}
			}
		}
	case errors.Is(err, webdav.ErrNotFound):
		// 远端尚未创建 books/ 目录，说明所有候选书目在远端均缺失
	default:
		s.LogEvent("sync", "reconcile remote books skipped: %v", err)
		return
	}

	var missingIDs []string
	for _, b := range candidates {
		if _, ok := remoteDirs[b.ID]; !ok {
			missingIDs = append(missingIDs, b.ID)
		}
	}
	if len(missingIDs) == 0 {
		return
	}

	s.mu.Lock()
	stale := s.workspaceEpoch != epoch
	s.mu.Unlock()
	if stale {
		return
	}

	if queued, qErr := s.store.EnqueueBookMigrations(ctx, missingIDs); qErr != nil {
		s.LogEvent("sync", "enqueue missing remote books failed: %v", qErr)
	} else if queued > 0 {
		s.LogEvent("sync", "auto-queued %d local books missing from remote books/ dir", queued)
	}
}

func validBookAssetName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\") && filepath.IsLocal(name)
}

func readLocalBookAssets(book domain.Book, item storage.PendingUpload) (sp, raw, cover []byte, err error) {
	sp, spErr := os.ReadFile(item.LocalSPBookPath)
	raw, rawErr := os.ReadFile(item.LocalRawPath)
	if spErr != nil || rawErr != nil {
		return nil, nil, nil, errors.New("本地待上传文件缺失")
	}
	if _, err := booksync.DecodeSPBook(sp, book.ContentHash, book.ID); err != nil {
		return nil, nil, nil, err
	}
	if err := validateOriginalSize(book, raw); err != nil {
		return nil, nil, nil, err
	}
	if item.LocalCoverPath != "" {
		cover, _ = os.ReadFile(item.LocalCoverPath)
	}
	return sp, raw, validBookCover(book, cover), nil
}

func readRemoteBookAssets(ctx context.Context, source *webdav.Client, book domain.Book) (sp, raw, cover []byte, err error) {
	parsedPath, err := booksync.BookParsedPathFor(book)
	if err != nil {
		return nil, nil, nil, err
	}
	sp, _, err = source.GetWithLimit(ctx, parsedPath, booksync.MaxSPBookCompressedBytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("读取旧工作区正文失败：%w", err)
	}
	if _, err := booksync.DecodeSPBook(sp, book.ContentHash, book.ID); err != nil {
		return nil, nil, nil, err
	}
	if book.RawSize == math.MaxInt64 {
		return nil, nil, nil, errors.New("原始文件大小超出范围")
	}
	// Use the recorded import size as the limit. Older records without a size use the client's default.
	raw, _, err = source.GetWithLimit(ctx, booksync.BookRawPath(book.ID, book.RawFilename), book.RawSize)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("读取旧工作区原始文件失败：%w", err)
	}
	if err := validateOriginalSize(book, raw); err != nil {
		return nil, nil, nil, err
	}
	if book.CoverHash != "" {
		cover, _, _ = source.GetWithLimit(ctx, booksync.BookCoverPath(book.ID), maxCoverBytes)
	}
	return sp, raw, validBookCover(book, cover), nil
}

func validateOriginalSize(book domain.Book, raw []byte) error {
	if len(raw) == 0 || (book.RawSize > 0 && int64(len(raw)) != book.RawSize) {
		return fmt.Errorf("原始文件大小与书目记录不符：%d/%d", len(raw), book.RawSize)
	}
	return nil
}

func validBookCover(book domain.Book, cover []byte) []byte {
	hash := sha256.Sum256(cover)
	if len(cover) > 0 && hex.EncodeToString(hash[:]) == book.CoverHash {
		return cover
	}
	return nil
}

func migrationSourcesAfterSwitch(sources []WebDAVProfile, previous, next WebDAVProfile) []WebDAVProfile {
	nextID := webDAVWorkspaceID(next)
	sources = slices.DeleteFunc(slices.Clone(sources), func(source WebDAVProfile) bool {
		return webDAVWorkspaceID(source) == nextID
	})
	if previous.BaseURL != "" {
		sources = append([]WebDAVProfile{previous}, sources...)
	}
	return sources
}
