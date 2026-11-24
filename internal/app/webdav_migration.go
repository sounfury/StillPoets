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

func (s *Service) uploadMigratingBook(ctx context.Context, target *booksync.Engine, sources []*webdav.Client, sourceErr error, book domain.Book) error {
	if !validBookAssetName(book.ID) || !validBookAssetName(book.RawFilename) || book.ContentHash == "" {
		return errors.New("缺少有效的诗集资产标识")
	}
	verifyErr := target.VerifyBookAssets(ctx, book)
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

	errs := []error{fmt.Errorf("新工作区诗集资产未通过校验：%w", verifyErr), sourceErr}
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
	return fmt.Errorf("旧 WebDAV 工作区缺少可迁移的正文或原始文件：%w", errors.Join(errs...))
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
	sp, _, err = source.GetWithLimit(ctx, booksync.BookParsedPath(book.ID), booksync.MaxSPBookCompressedBytes)
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
