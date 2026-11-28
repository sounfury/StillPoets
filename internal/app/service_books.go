package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"stillpoets/internal/domain"
	"stillpoets/internal/parser"
	"stillpoets/internal/storage"
	booksync "stillpoets/internal/sync"
)

// --- 诗集导入、缓存与管理 ---

// ImportEPUB 本地解析 EPUB、压缩生成 .spbook.json.gz、落库并入队待上传资产。
func (s *Service) ImportEPUB(ctx context.Context, filePath, groupID string) (ImportBookResult, error) {
	rawBytes, err := os.ReadFile(filePath)
	if err != nil {
		return ImportBookResult{}, fmt.Errorf("读取 EPUB 失败：%w", err)
	}
	bookID := domain.NewID()
	parsed, err := parser.ParseEPUBFile(filePath, bookID)
	if err != nil {
		return ImportBookResult{}, fmt.Errorf("解析 EPUB 失败：%w", err)
	}

	spCompressed, contentHash, err := booksync.EncodeSPBook(parsed.Book)
	if err != nil {
		return ImportBookResult{}, fmt.Errorf("打包诗集正文失败：%w", err)
	}

	rawFilename := domain.BuildRawFilename(filepath.Base(filePath), domain.NewShortRandID())
	localRawPath := filepath.Join(s.dataDir, "cache", "raw", bookID+"_"+rawFilename)
	localSPPath := filepath.Join(s.dataDir, "cache", "spbook", bookID+".spbook.json.gz")
	localCoverPath := ""
	coverHash := ""

	if err := os.WriteFile(localRawPath, rawBytes, 0o600); err != nil {
		return ImportBookResult{}, err
	}
	if err := os.WriteFile(localSPPath, spCompressed, 0o600); err != nil {
		_ = os.Remove(localRawPath)
		return ImportBookResult{}, err
	}
	if len(parsed.Cover) > 0 {
		sum := sha256.Sum256(parsed.Cover)
		coverHash = hex.EncodeToString(sum[:])
		localCoverPath = filepath.Join(s.dataDir, "cache", "covers", bookID+".img")
		_ = os.WriteFile(localCoverPath, parsed.Cover, 0o600)
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
		title = strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	}
	author := strings.TrimSpace(parsed.Book.Author)
	if author == "" {
		author = "佚名"
	}

	book := domain.Book{
		ID:          bookID,
		Title:       title,
		Author:      author,
		CoverHash:   coverHash,
		ContentHash: contentHash,
		ContentSize: int64(len(spCompressed)),
		RawFilename: rawFilename,
		RawSize:     int64(len(rawBytes)),
		PoemCount:   poemCount,
		GroupID:     groupID,
	}

	pendingUpload := storage.PendingUpload{
		BookID:          bookID,
		RawFilename:     rawFilename,
		LocalRawPath:    localRawPath,
		LocalSPBookPath: localSPPath,
		LocalCoverPath:  localCoverPath,
		ContentHash:     contentHash,
		State:           storage.UploadPending,
	}
	// 在同一 SQLite 事务内写入诗集元数据、正文与待上传资产记录，消除导书与并发同步间的幽灵书目窗口（P0-3）。
	if err := s.store.UpsertImportedBookWithUpload(ctx, book, parsed.Book, &pendingUpload); err != nil {
		_ = os.Remove(localRawPath)
		_ = os.Remove(localSPPath)
		if localCoverPath != "" {
			_ = os.Remove(localCoverPath)
		}
		return ImportBookResult{}, err
	}

	storedBook, err := s.store.GetBook(ctx, bookID)
	if err == nil {
		book = storedBook
	}
	_, _ = s.RebuildWidgetTimeline(ctx)
	s.notifyDataChanged()
	s.scheduleDebouncedSync()

	return ImportBookResult{
		Book:          book,
		FilteredCount: len(parsed.Filtered),
	}, nil
}

// EnsureBookContent 按需恢复或从 WebDAV 下载诗集 .spbook.json.gz 正文包并写入本地 SQLite 缓存。
func (s *Service) EnsureBookContent(ctx context.Context, bookID string) error {
	book, err := s.store.GetBook(ctx, bookID)
	if err != nil {
		return err
	}
	if book.LocalState == domain.LocalStateDownloaded {
		return nil
	}

	// 若本机 cache/spbook 下仍保留匹配当前 ContentHash 的解析包，直接从本地瞬时恢复（P1-15）
	localSPPath := filepath.Join(s.dataDir, "cache", "spbook", book.ID+".spbook.json.gz")
	if raw, readErr := os.ReadFile(localSPPath); readErr == nil && len(raw) > 0 {
		if sp, decErr := booksync.DecodeSPBook(raw, book.ContentHash, book.ID); decErr == nil {
			if saveErr := s.store.SaveBookContent(ctx, book.ID, book.ContentHash, sp); saveErr == nil {
				_, _ = s.RebuildWidgetTimeline(ctx)
				s.notifyDataChanged()
				return nil
			}
		}
	}

	client, err := s.newWebDAVClient()
	if err != nil {
		return fmt.Errorf("无法按需下载云端诗集：%w", err)
	}

	remotePath, err := booksync.BookParsedPathFor(book)
	if err != nil {
		return fmt.Errorf("诗集正文路径异常：%w", err)
	}
	raw, _, err := client.GetWithLimit(ctx, remotePath, booksync.MaxSPBookCompressedBytes)
	if err != nil {
		return fmt.Errorf("下载诗集正文失败：%w", err)
	}
	sp, err := booksync.DecodeSPBook(raw, book.ContentHash, book.ID)
	if err != nil {
		return fmt.Errorf("校验诗集正文包失败：%w", err)
	}

	if err := s.store.SaveBookContent(ctx, book.ID, book.ContentHash, sp); err != nil {
		return err
	}
	// 顺手保留已校验的 .spbook.json.gz 离线副本，方便后续换盘无损补传或清理后瞬时恢复
	if mkErr := os.MkdirAll(filepath.Dir(localSPPath), 0o755); mkErr == nil {
		_ = os.WriteFile(localSPPath, raw, 0o600)
	}
	if book.CoverHash != "" {
		_, _ = s.EnsureBookCover(ctx, book.ID)
	}
	_, _ = s.RebuildWidgetTimeline(ctx)
	s.notifyDataChanged()
	return nil
}

// BookCoverPath 返回某本诗集原书封面在本地缓存中的路径。
func (s *Service) BookCoverPath(bookID string) string {
	return filepath.Join(s.dataDir, "cache", "covers", bookID+".img")
}

// EnsureBookCover 返回本地缓存的原书封面路径；若本地缺失且云端存在对应封面则按需拉取并校验哈希。
func (s *Service) EnsureBookCover(ctx context.Context, bookID string) (string, error) {
	if strings.TrimSpace(bookID) == "" {
		return "", nil
	}
	localPath := s.BookCoverPath(bookID)
	if info, err := os.Stat(localPath); err == nil && info.Size() > 0 {
		return localPath, nil
	}
	book, err := s.store.GetBook(ctx, bookID)
	if err != nil {
		return "", err
	}
	if book.CoverHash == "" {
		return "", nil
	}
	client, err := s.newWebDAVClient()
	if err != nil {
		return "", err
	}
	raw, _, err := client.GetWithLimit(ctx, booksync.BookCoverPath(book.ID), maxCoverBytes)
	if err != nil || len(raw) == 0 {
		return "", err
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != book.CoverHash {
		return "", errors.New("app: remote cover hash mismatch")
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(localPath, raw, 0o600); err != nil {
		return "", err
	}
	return localPath, nil
}

func (s *Service) ClearBookCache(ctx context.Context, bookID string) error {
	// 与批量清理保持一致：拒绝清理尚未完成云端上传的新导诗集正文（P1-15）
	isPending, err := s.store.HasPendingUpload(ctx, bookID)
	if err != nil {
		return err
	}
	if isPending {
		return errors.New("该诗集尚未完成云端上传，暂不可清理本地正文")
	}
	if err := s.store.ClearBookCache(ctx, bookID); err != nil {
		return err
	}
	_, _ = s.RebuildWidgetTimeline(ctx)
	s.notifyDataChanged()
	return nil
}

func (s *Service) ClearAllBookCaches(ctx context.Context) (int, error) {
	books, err := s.store.ListBooks(ctx, "")
	if err != nil {
		return 0, err
	}
	// 仅清理已完成上传（不在 pending_uploads 中）的离线正文缓存，防止尚未同步的新导诗集丢正文。
	pendingList, err := s.store.ListPendingUploads(ctx)
	if err != nil {
		return 0, err
	}
	pendingSet := make(map[string]struct{}, len(pendingList))
	for _, p := range pendingList {
		pendingSet[p.BookID] = struct{}{}
	}
	cleared := 0
	for _, b := range books {
		if b.LocalState != domain.LocalStateDownloaded {
			continue
		}
		if _, isPending := pendingSet[b.ID]; isPending {
			continue
		}
		if err := s.store.ClearBookCache(ctx, b.ID); err != nil {
			return cleared, err
		}
		cleared++
	}
	_, _ = s.RebuildWidgetTimeline(ctx)
	s.notifyDataChanged()
	return cleared, nil
}

func (s *Service) DeleteBook(ctx context.Context, bookID string) error {
	if err := s.store.DeleteBook(ctx, bookID); err != nil {
		return err
	}
	_ = s.store.DeletePendingUpload(ctx, bookID)
	s.removeLocalBookCacheFiles(bookID)
	_, _ = s.RebuildWidgetTimeline(ctx)
	s.notifyDataChanged()
	s.scheduleDebouncedSync()
	return nil
}

func (s *Service) removeLocalBookCacheFiles(bookID string) {
	_ = os.Remove(filepath.Join(s.dataDir, "cache", "spbook", bookID+".spbook.json.gz"))
	_ = os.Remove(filepath.Join(s.dataDir, "cache", "covers", bookID+".img"))
	if matches, err := filepath.Glob(filepath.Join(s.dataDir, "cache", "raw", bookID+"_*")); err == nil {
		for _, m := range matches {
			_ = os.Remove(m)
		}
	}
}

// --- 分组与书架属性 ---

func (s *Service) ListGroups(ctx context.Context) ([]domain.BookGroup, error) {
	return s.store.ListGroups(ctx)
}

func (s *Service) CreateGroup(ctx context.Context, name string) (domain.BookGroup, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.BookGroup{}, errors.New("分组名称不能为空")
	}
	g, err := s.store.CreateGroup(ctx, name)
	if err == nil {
		s.notifyDataChanged()
		s.scheduleDebouncedSync()
	}
	return g, err
}

func (s *Service) RenameGroup(ctx context.Context, id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("分组名称不能为空")
	}
	err := s.store.RenameGroup(ctx, id, name)
	if err == nil {
		s.notifyDataChanged()
		s.scheduleDebouncedSync()
	}
	return err
}

func (s *Service) ReorderGroups(ctx context.Context, orderedIDs []string) error {
	err := s.store.ReorderGroups(ctx, orderedIDs)
	if err == nil {
		s.notifyDataChanged()
		s.scheduleDebouncedSync()
	}
	return err
}

func (s *Service) DissolveGroup(ctx context.Context, id string) error {
	err := s.store.DissolveGroup(ctx, id)
	if err == nil {
		s.notifyDataChanged()
		s.scheduleDebouncedSync()
	}
	return err
}

func (s *Service) ListBooks(ctx context.Context, groupID string) ([]domain.Book, error) {
	return s.store.ListBooks(ctx, groupID)
}

func (s *Service) GetBook(ctx context.Context, bookID string) (domain.Book, error) {
	return s.store.GetBook(ctx, bookID)
}

func (s *Service) SetBookGroup(ctx context.Context, bookID, groupID string) error {
	err := s.store.SetBookGroup(ctx, bookID, groupID)
	if err == nil {
		s.notifyDataChanged()
		s.scheduleDebouncedSync()
	}
	return err
}

// SetBooksGroup 批量将多本诗集归入指定分组（空串表示移回未分组），单事务落库并只触发一次防抖同步。
func (s *Service) SetBooksGroup(ctx context.Context, bookIDs []string, groupID string) error {
	err := s.store.SetBooksGroup(ctx, bookIDs, groupID)
	if err == nil {
		s.notifyDataChanged()
		s.scheduleDebouncedSync()
	}
	return err
}

func (s *Service) SetBookStarred(ctx context.Context, bookID string, starred bool) error {
	err := s.store.SetBookStarred(ctx, bookID, starred)
	if err == nil {
		s.notifyDataChanged()
		s.scheduleDebouncedSync()
	}
	return err
}

func (s *Service) ListPoems(ctx context.Context, bookID string) ([]domain.Poem, error) {
	return s.store.ListPoems(ctx, bookID)
}

func (s *Service) GetPoem(ctx context.Context, bookID, poemID string) (domain.Poem, error) {
	return s.store.GetPoem(ctx, bookID, poemID)
}
