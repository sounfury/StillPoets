package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"stillpoets/internal/domain"
	"stillpoets/internal/parser"
	"stillpoets/internal/storage"
	booksync "stillpoets/internal/sync"
	"stillpoets/internal/sync/webdav"
)

const (
	webdavProfileFilename = "webdav_profile.json"
	webdavPasswordKey     = "webdav_password"
	defaultSyncDebounce   = 4 * time.Second
)

// SecretStore 抽象系统级安全凭据存储（生产环境对接 mygo.Secrets，测试使用内存实现）。
type SecretStore interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte) error
	Delete(key string) error
}

// MemorySecretStore 供无 Keychain 环境或单元测试使用的内存凭据实现。
type MemorySecretStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func NewMemorySecretStore() *MemorySecretStore {
	return &MemorySecretStore{data: map[string][]byte{}}
}

func (m *MemorySecretStore) Get(key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), v...), nil
}

func (m *MemorySecretStore) Set(key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = append([]byte(nil), value...)
	return nil
}

func (m *MemorySecretStore) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

// WebDAVProfile 仅保存非敏感的 WebDAV 连接配置（严禁包含密码）。
type WebDAVProfile struct {
	Enabled      bool   `json:"enabled"`
	BaseURL      string `json:"baseUrl"`
	Username     string `json:"username"`
	AppFolder    string `json:"appFolder"`
	AtomicUpload bool   `json:"atomicUpload"`
}

// WebDAVConfigInput 供配置表单提交连接参数（含一次性内存密码）。
type WebDAVConfigInput struct {
	BaseURL      string
	Username     string
	Password     string
	AppFolder    string
	AtomicUpload bool
	Enabled      bool
}

// SyncToastPhase 表示瞬态云同步提示胶囊的状态阶段。
type SyncToastPhase int

const (
	SyncToastIdle SyncToastPhase = iota
	SyncToastSyncing
	SyncToastSuccess
	SyncToastPartial
	SyncToastError
)

// SyncToastState 传递给 UI 渲染瞬态同步轻胶囊。
type SyncToastState struct {
	Phase   SyncToastPhase
	Message string
}

// TodayReread 描述书架首页「今日重读」卡片内容。
type TodayReread struct {
	BookID         string
	PoemID         string
	BookTitle      string
	PoemTitle      string
	Author         string
	Excerpt        string
	HighlightLines string
}

// ImportBookResult 描述本地导入一本 EPUB 后的摘要信息。
type ImportBookResult struct {
	Book          domain.Book
	FilteredCount int
}

// SearchResults 聚合全局检索命中的诗篇与散页。
type SearchResults struct {
	Poems  []domain.Poem
	Quotes []storage.QuoteView
}

// LocalFont 描述导入到应用私有目录的自定义阅读字体。
type LocalFont struct {
	Family   string
	Filename string
	Path     string
}

// SyncOverview 聚合「我的」页面展示的同步元状态与远端设备列表。
type SyncOverview struct {
	Configured    bool
	InstallID     string
	LocalRev      uint64
	Dirty         bool
	LastSyncAt    int64
	PendingCount  int
	RemoteDevices []string
}

// Service 是 StillPoets 的无界面核心应用服务层，统筹 SQLite 仓储、EPUB 导入、
// 本地缓存、WebDAV 串行同步、系统密钥链与桌面小组件快照编译。
type Service struct {
	dataDir string
	store   *storage.Store
	secrets SecretStore

	mu            sync.Mutex
	syncMu        sync.Mutex
	profile       WebDAVProfile
	remoteDevices []string
	debounceDelay time.Duration
	debounceTimer *time.Timer
	closed        bool

	onSyncToast     func(SyncToastState)
	onDataChanged   func()
	onWidgetUpdated func(timelinePath, fontsDir string)
}

// Open 初始化数据目录、SQLite 仓储与本地 WebDAV 配置。
func Open(ctx context.Context, dataDir string, secrets SecretStore) (*Service, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("app: dataDir is required")
	}
	for _, sub := range []string{"", "cache/raw", "cache/spbook", "cache/covers", "fonts"} {
		if err := os.MkdirAll(filepath.Join(dataDir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("app: create dir %s: %w", sub, err)
		}
	}
	if secrets == nil {
		secrets = NewMemorySecretStore()
	}

	store, err := storage.Open(ctx, filepath.Join(dataDir, "stillpoets.db"))
	if err != nil {
		return nil, err
	}

	svc := &Service{
		dataDir:       dataDir,
		store:         store,
		secrets:       secrets,
		debounceDelay: defaultSyncDebounce,
		profile: WebDAVProfile{
			AppFolder: webdav.DefaultAppFolder,
		},
	}
	svc.loadWebDAVProfileFromDisk()
	return svc, nil
}

// Close 停止后台防抖计时器并关闭数据库。
func (s *Service) Close() error {
	s.mu.Lock()
	s.closed = true
	if s.debounceTimer != nil {
		s.debounceTimer.Stop()
		s.debounceTimer = nil
	}
	s.mu.Unlock()
	return s.store.Close()
}

// SetCallbacks 注册同步提示与数据变化通知回调（供 UI 刷新使用）。
func (s *Service) SetCallbacks(onSyncToast func(SyncToastState), onDataChanged func()) {
	s.mu.Lock()
	s.onSyncToast = onSyncToast
	s.onDataChanged = onDataChanged
	s.mu.Unlock()
}

// SetWidgetUpdatedCallback 注册小组件时间线写入后的原生通知回调（供 Android AppWidget / iOS WidgetKit 实时刷新）。
func (s *Service) SetWidgetUpdatedCallback(fn func(timelinePath, fontsDir string)) {
	s.mu.Lock()
	s.onWidgetUpdated = fn
	s.mu.Unlock()
}

// FontsDir 返回本地自定义字体存放目录。
func (s *Service) FontsDir() string { return filepath.Join(s.dataDir, "fonts") }

// SetSyncDebounce 允许测试或调用方调整写入后的防抖延迟。
func (s *Service) SetSyncDebounce(d time.Duration) {
	s.mu.Lock()
	s.debounceDelay = d
	s.mu.Unlock()
}

// Store 暴露底层只读/高级存储访问。
func (s *Service) Store() *storage.Store { return s.store }

// DataDir 返回当前应用私有数据目录路径。
func (s *Service) DataDir() string { return s.dataDir }

// --- 设置与字体 ---

func (s *Service) LoadSettings(ctx context.Context) (domain.Settings, error) {
	return s.store.LoadSettings(ctx)
}

func (s *Service) SaveSettings(ctx context.Context, settings domain.Settings) error {
	settings.UpdatedAt = domain.NowMillis()
	if err := s.store.SaveSettings(ctx, settings); err != nil {
		return err
	}
	_, _ = s.RebuildWidgetTimeline(ctx)
	s.notifyDataChanged()
	return nil
}

// ImportFont 将外部 .ttf/.otf 字体复制到私有目录，返回字体元数据与字节内容供 UI 注册。
func (s *Service) ImportFont(srcPath string) (LocalFont, []byte, error) {
	ext := strings.ToLower(filepath.Ext(srcPath))
	if ext != ".ttf" && ext != ".otf" {
		return LocalFont{}, nil, fmt.Errorf("仅支持 .ttf 或 .otf 字体文件，收到 %q", ext)
	}
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return LocalFont{}, nil, err
	}
	base := filepath.Base(srcPath)
	family := strings.TrimSpace(strings.TrimSuffix(base, filepath.Ext(base)))
	if family == "" {
		family = "CustomFont"
	}
	dst := filepath.Join(s.dataDir, "fonts", base)
	if err := os.WriteFile(dst, raw, 0o600); err != nil {
		return LocalFont{}, nil, err
	}
	return LocalFont{Family: family, Filename: base, Path: dst}, raw, nil
}

// ListLocalFonts 列出本地私有字体目录中所有已导入的字体文件。
func (s *Service) ListLocalFonts() []LocalFont {
	entries, err := os.ReadDir(filepath.Join(s.dataDir, "fonts"))
	if err != nil {
		return nil
	}
	var out []LocalFont
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".ttf" && ext != ".otf" {
			continue
		}
		out = append(out, LocalFont{
			Family:   strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())),
			Filename: e.Name(),
			Path:     filepath.Join(s.dataDir, "fonts", e.Name()),
		})
	}
	return out
}

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

	if err := s.store.UpsertImportedBook(ctx, book, parsed.Book); err != nil {
		_ = os.Remove(localRawPath)
		_ = os.Remove(localSPPath)
		if localCoverPath != "" {
			_ = os.Remove(localCoverPath)
		}
		return ImportBookResult{}, err
	}

	// 始终先记入待上传队列；即使此时尚未配置 WebDAV，配置后也会自动补传并验证资产。
	_ = s.store.UpsertPendingUpload(ctx, storage.PendingUpload{
		BookID:          bookID,
		RawFilename:     rawFilename,
		LocalRawPath:    localRawPath,
		LocalSPBookPath: localSPPath,
		LocalCoverPath:  localCoverPath,
		ContentHash:     contentHash,
		State:           storage.UploadPending,
	})

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

// EnsureBookContent 按需从 WebDAV 下载诗集 .spbook.json.gz 正文包并写入本地 SQLite 缓存。
func (s *Service) EnsureBookContent(ctx context.Context, bookID string) error {
	book, err := s.store.GetBook(ctx, bookID)
	if err != nil {
		return err
	}
	if book.LocalState == domain.LocalStateDownloaded {
		return nil
	}

	client, err := s.newWebDAVClient()
	if err != nil {
		return fmt.Errorf("无法按需下载云端诗集：%w", err)
	}

	remotePath := booksync.BookParsedPath(book.ID)
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
	_, _ = s.RebuildWidgetTimeline(ctx)
	s.notifyDataChanged()
	return nil
}

func (s *Service) ClearBookCache(ctx context.Context, bookID string) error {
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

// --- 分组、书架属性、精选、散页与阅读进度 ---

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

func (s *Service) ToggleFavoritePoem(ctx context.Context, book domain.Book, poem domain.Poem, active bool) (domain.FavoritePoem, error) {
	author := poem.Author
	if author == "" {
		author = book.Author
	}
	fav, err := s.store.ToggleFavoritePoem(ctx, book.ID, poem.ID, poem.Title, author, parser.BuildDefaultExcerpt(poem), active)
	if err == nil {
		_, _ = s.RebuildWidgetTimeline(ctx)
		s.notifyDataChanged()
		s.scheduleDebouncedSync()
	}
	return fav, err
}

// SetFavoriteCustomExcerpt 将用户手动划选的诗行设为该诗的小组件金句（自动确保该诗已加入精选池）。
func (s *Service) SetFavoriteCustomExcerpt(ctx context.Context, book domain.Book, poem domain.Poem, excerpt, highlightLines string) error {
	excerpt = strings.TrimSpace(excerpt)
	if excerpt == "" {
		return errors.New("请先选择要设为金句的诗行")
	}
	if _, err := s.ToggleFavoritePoem(ctx, book, poem, true); err != nil {
		return err
	}
	if err := s.store.UpdateFavoriteExcerpt(ctx, book.ID, poem.ID, excerpt, highlightLines, true); err != nil {
		return err
	}
	_, _ = s.RebuildWidgetTimeline(ctx)
	s.notifyDataChanged()
	s.scheduleDebouncedSync()
	return nil
}

func (s *Service) ListFavorites(ctx context.Context, bookID string) ([]domain.FavoritePoem, error) {
	return s.store.ListFavorites(ctx, bookID)
}

func (s *Service) CreateStandaloneQuote(ctx context.Context, content, author, sourceBook string) (domain.Quote, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return domain.Quote{}, errors.New("句子内容不能为空")
	}
	q, err := s.store.CreateStandaloneQuote(ctx, content, strings.TrimSpace(author), strings.TrimSpace(sourceBook), "manual")
	if err == nil {
		_, _ = s.RebuildWidgetTimeline(ctx)
		s.notifyDataChanged()
		s.scheduleDebouncedSync()
	}
	return q, err
}

func (s *Service) CreatePoemExcerptQuote(ctx context.Context, book domain.Book, poem domain.Poem, content, highlightLines string) (domain.Quote, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return domain.Quote{}, errors.New("摘录内容不能为空")
	}
	author := poem.Author
	if author == "" {
		author = book.Author
	}
	q, err := s.store.CreatePoemExcerptQuote(ctx, book.ID, poem.ID, poem.Title, book.Title, author, content, highlightLines)
	if err == nil {
		_, _ = s.RebuildWidgetTimeline(ctx)
		s.notifyDataChanged()
		s.scheduleDebouncedSync()
	}
	return q, err
}

func (s *Service) UpdateQuote(ctx context.Context, id, content, author, sourceBook string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return errors.New("句子内容不能为空")
	}
	err := s.store.UpdateQuote(ctx, id, content, strings.TrimSpace(author), strings.TrimSpace(sourceBook))
	if err == nil {
		_, _ = s.RebuildWidgetTimeline(ctx)
		s.notifyDataChanged()
		s.scheduleDebouncedSync()
	}
	return err
}

func (s *Service) DeleteQuote(ctx context.Context, id string) error {
	err := s.store.DeleteQuote(ctx, id)
	if err == nil {
		_, _ = s.RebuildWidgetTimeline(ctx)
		s.notifyDataChanged()
		s.scheduleDebouncedSync()
	}
	return err
}

func (s *Service) ListQuotes(ctx context.Context, filter storage.QuoteFilter) ([]storage.QuoteView, error) {
	return s.store.ListQuotes(ctx, filter)
}

func (s *Service) ListQuoteFacets(ctx context.Context) (storage.QuoteFacets, error) {
	return s.store.ListQuoteFacets(ctx)
}

func (s *Service) RecordPoemRead(ctx context.Context, bookID, poemID string, scrollRatio float64, totalPoems int) (domain.ReadingProgress, error) {
	p, err := s.store.RecordPoemRead(ctx, bookID, poemID, scrollRatio, totalPoems)
	if err == nil {
		s.notifyDataChanged()
		s.scheduleDebouncedSync()
	}
	return p, err
}

func (s *Service) MarkBookFinished(ctx context.Context, bookID string, finished bool) (domain.ReadingProgress, error) {
	p, err := s.store.MarkBookFinished(ctx, bookID, finished)
	if err == nil {
		s.notifyDataChanged()
		s.scheduleDebouncedSync()
	}
	return p, err
}

func (s *Service) GetProgress(ctx context.Context, bookID string) (domain.ReadingProgress, error) {
	return s.store.GetProgress(ctx, bookID)
}

func (s *Service) ListProgress(ctx context.Context) ([]domain.ReadingProgress, error) {
	return s.store.ListProgress(ctx)
}

func (s *Service) GetContinueReading(ctx context.Context) (*storage.ContinueReadingItem, error) {
	return s.store.GetContinueReading(ctx)
}

func (s *Service) GetStatsOverview(ctx context.Context) (storage.StatsOverview, error) {
	return s.store.GetStatsOverview(ctx)
}

// Search 联合检索已离线诗集正文与散页库（FTS5）。
func (s *Service) Search(ctx context.Context, query string, limit int) (SearchResults, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return SearchResults{}, nil
	}
	poems, err := s.store.SearchPoems(ctx, query, limit)
	if err != nil {
		return SearchResults{}, err
	}
	quotes, err := s.store.ListQuotes(ctx, storage.QuoteFilter{Query: query, Limit: limit})
	if err != nil {
		return SearchResults{}, err
	}
	return SearchResults{Poems: poems, Quotes: quotes}, nil
}

// PickTodayReread 为书架首页「今日重读」按当天日期稳定挑选一节诗（优先精选池，其次已缓存诗集）。
func (s *Service) PickTodayReread(ctx context.Context) (*TodayReread, error) {
	daySeed := stableDayHash(time.Now().Format("2006-01-02"))
	books, err := s.store.ListBooks(ctx, "")
	if err != nil {
		return nil, err
	}
	bookMap := make(map[string]domain.Book, len(books))
	for _, b := range books {
		bookMap[b.ID] = b
	}

	// 1. 优先从诗篇精选池抽取
	favs, err := s.store.ListFavorites(ctx, "")
	if err == nil && len(favs) > 0 {
		chosen := favs[int(daySeed%uint64(len(favs)))]
		excerpt := strings.TrimSpace(chosen.Excerpt)
		if excerpt == "" {
			if p, err := s.store.GetPoem(ctx, chosen.BookID, chosen.PoemID); err == nil {
				excerpt = parser.BuildDefaultExcerpt(p)
			}
		}
		if excerpt != "" {
			b := bookMap[chosen.BookID]
			author := chosen.Author
			if author == "" {
				author = b.Author
			}
			return &TodayReread{
				BookID:         chosen.BookID,
				PoemID:         chosen.PoemID,
				BookTitle:      b.Title,
				PoemTitle:      chosen.PoemTitle,
				Author:         author,
				Excerpt:        excerpt,
				HighlightLines: chosen.HighlightLines,
			}, nil
		}
	}

	// 2. 其次从已离线缓存诗集的正式诗篇中抽取
	type candidate struct {
		book domain.Book
		poem domain.Poem
	}
	var pool []candidate
	for _, b := range books {
		if b.LocalState != domain.LocalStateDownloaded {
			continue
		}
		poems, err := s.store.ListPoems(ctx, b.ID)
		if err != nil {
			continue
		}
		for _, p := range poems {
			if !p.IsPoem() {
				continue
			}
			if parser.BuildDefaultExcerpt(p) != "" {
				pool = append(pool, candidate{book: b, poem: p})
			}
		}
	}
	if len(pool) > 0 {
		c := pool[int(daySeed%uint64(len(pool)))]
		author := c.poem.Author
		if author == "" {
			author = c.book.Author
		}
		return &TodayReread{
			BookID:    c.book.ID,
			PoemID:    c.poem.ID,
			BookTitle: c.book.Title,
			PoemTitle: c.poem.Title,
			Author:    author,
			Excerpt:   parser.BuildDefaultExcerpt(c.poem),
		}, nil
	}
	return nil, nil
}

// --- WebDAV 配置、串行同步与旧设备退役 ---

func (s *Service) LoadWebDAVProfile() WebDAVProfile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.profile
}

// HasWebDAVPassword 检查系统安全凭据库中是否已存有 WebDAV 密码。
func (s *Service) HasWebDAVPassword() bool {
	b, err := s.secrets.Get(webdavPasswordKey)
	return err == nil && len(b) > 0
}

// TestWebDAVConnection 在不落盘修改配置的前提下验证连通性并初始化远端工作区。
func (s *Service) TestWebDAVConnection(ctx context.Context, input WebDAVConfigInput) (*booksync.WorkspaceInitResult, error) {
	pass := input.Password
	if pass == "" {
		if stored, err := s.secrets.Get(webdavPasswordKey); err == nil {
			pass = string(stored)
		}
	}
	client, err := webdav.NewClient(webdav.Config{
		BaseURL:      strings.TrimSpace(input.BaseURL),
		Username:     strings.TrimSpace(input.Username),
		Password:     pass,
		AppFolder:    strings.TrimSpace(input.AppFolder),
		AtomicUpload: input.AtomicUpload,
		Timeout:      20 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	return booksync.EnsureWorkspace(ctx, client)
}

// SaveWebDAVConfig 保存 WebDAV 非敏感配置到本地 JSON，密码仅写入系统级安全存储。
func (s *Service) SaveWebDAVConfig(ctx context.Context, input WebDAVConfigInput) error {
	folder := strings.TrimSpace(input.AppFolder)
	if folder == "" {
		folder = webdav.DefaultAppFolder
	}
	profile := WebDAVProfile{
		Enabled:      input.Enabled,
		BaseURL:      strings.TrimSpace(input.BaseURL),
		Username:     strings.TrimSpace(input.Username),
		AppFolder:    folder,
		AtomicUpload: input.AtomicUpload,
	}
	if input.Password != "" {
		if err := s.secrets.Set(webdavPasswordKey, []byte(input.Password)); err != nil {
			return fmt.Errorf("保存 WebDAV 密码到系统安全存储失败：%w", err)
		}
	}
	if err := writeJSONAtomic(filepath.Join(s.dataDir, webdavProfileFilename), profile); err != nil {
		return err
	}
	s.mu.Lock()
	s.profile = profile
	s.mu.Unlock()
	s.notifyDataChanged()
	return nil
}

// ClearWebDAVConfig 断开 WebDAV 并删除系统安全存储中的密码。
func (s *Service) ClearWebDAVConfig() error {
	_ = s.secrets.Delete(webdavPasswordKey)
	profile := WebDAVProfile{AppFolder: webdav.DefaultAppFolder}
	if err := writeJSONAtomic(filepath.Join(s.dataDir, webdavProfileFilename), profile); err != nil {
		return err
	}
	s.mu.Lock()
	s.profile = profile
	s.remoteDevices = nil
	s.mu.Unlock()
	s.notifyDataChanged()
	return nil
}

// GetSyncOverview 返回本机同步状态、待上传数量和最近发现的远端设备列表。
func (s *Service) GetSyncOverview(ctx context.Context) (SyncOverview, error) {
	_, meta, err := s.store.ExportSnapshot(ctx)
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
	return SyncOverview{
		Configured:    configured,
		InstallID:     meta.InstallID,
		LocalRev:      meta.LocalRev,
		Dirty:         meta.Dirty,
		LastSyncAt:    meta.LastSyncAt,
		PendingCount:  len(pending),
		RemoteDevices: devices,
	}, nil
}

// SyncNow 执行一次完整串行同步：先上传待发布导书资产并回读确认，再拉取合并并按需发布新代次快照。
func (s *Service) SyncNow(ctx context.Context, forcePush bool) (*booksync.SyncResult, error) {
	client, err := s.newWebDAVClient()
	if err != nil {
		return nil, err
	}

	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	s.emitSyncToast(SyncToastState{Phase: SyncToastSyncing, Message: "同步中…"})

	engine := booksync.NewEngine(client)

	// 1. 先处理 pending_uploads（遵守「先传资产、回读确认、再发布书目元数据」铁律）
	unverifiedBooks, uploadedAny, uploadFailed := s.flushPendingUploads(ctx, engine)

	// 2. 导出本机快照（剔除资产尚未回读通过的新导诗集，防止远端出现幽灵书目）
	localData, meta, err := s.store.ExportSnapshot(ctx)
	if err != nil {
		s.emitSyncToast(SyncToastState{Phase: SyncToastError, Message: "同步失败"})
		return nil, err
	}
	outgoingData := localData
	if len(unverifiedBooks) > 0 {
		filtered := make([]domain.Book, 0, len(localData.Books))
		for _, b := range localData.Books {
			if _, blocked := unverifiedBooks[b.ID]; blocked {
				continue
			}
			filtered = append(filtered, b)
		}
		outgoingData.Books = filtered
	}

	res, err := engine.Sync(ctx, booksync.SyncRequest{
		InstallID:    meta.InstallID,
		LocalRev:     meta.LocalRev,
		LocalCounter: meta.LamportCounter,
		LocalData:    outgoingData,
		HasDirty:     meta.Dirty || uploadedAny,
		ForcePush:    forcePush || meta.LocalRev == 0,
	})
	if err != nil {
		s.emitSyncToast(SyncToastState{Phase: SyncToastError, Message: "云同步失败"})
		return nil, err
	}

	// 若存在未能上传资产的本地新书，合并结果落盘时仍保留其 dirty 标记，等待下轮继续补传。
	applyPublished := res.Published
	if uploadFailed {
		res.PartialSync = true
	}
	if err := s.store.ApplyMergedSnapshot(ctx, res.MergedData, meta.DirtySeq, res.MaxCounter, res.PublishedRev, applyPublished); err != nil {
		s.emitSyncToast(SyncToastState{Phase: SyncToastError, Message: "同步写库失败"})
		return nil, err
	}

	s.mu.Lock()
	s.remoteDevices = slices.Clone(res.RemoteDevices)
	s.mu.Unlock()

	_, _ = s.RebuildWidgetTimeline(ctx)
	s.notifyDataChanged()

	if res.PartialSync {
		s.emitSyncToast(SyncToastState{Phase: SyncToastPartial, Message: "部分同步"})
	} else {
		s.emitSyncToast(SyncToastState{Phase: SyncToastSuccess, Message: "云同步成功"})
	}
	return res, nil
}

// RetireRemoteDevice 先确保本机已发布最新合并快照，再安全退役指定旧 installId。
func (s *Service) RetireRemoteDevice(ctx context.Context, targetInstallID string) error {
	if _, err := s.SyncNow(ctx, true); err != nil {
		return fmt.Errorf("退役前发布合并快照失败：%w", err)
	}
	client, err := s.newWebDAVClient()
	if err != nil {
		return err
	}
	_, meta, err := s.store.ExportSnapshot(ctx)
	if err != nil {
		return err
	}

	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	engine := booksync.NewEngine(client)
	if err := engine.RetireDevice(ctx, meta.InstallID, meta.LocalRev, targetInstallID); err != nil {
		return err
	}
	s.mu.Lock()
	s.remoteDevices = slices.DeleteFunc(s.remoteDevices, func(id string) bool { return id == targetInstallID })
	s.mu.Unlock()
	s.notifyDataChanged()
	return nil
}

func (s *Service) flushPendingUploads(ctx context.Context, engine *booksync.Engine) (map[string]struct{}, bool, bool) {
	pending, err := s.store.ListPendingUploads(ctx)
	if err != nil || len(pending) == 0 {
		return nil, false, false
	}
	unverified := make(map[string]struct{})
	uploadedAny := false
	anyFailed := false

	for _, item := range pending {
		book, err := s.store.GetBook(ctx, item.BookID)
		if errors.Is(err, storage.ErrNotFound) {
			_ = s.store.DeletePendingUpload(ctx, item.BookID)
			continue
		}
		if err != nil {
			unverified[item.BookID] = struct{}{}
			anyFailed = true
			continue
		}
		spBytes, err1 := os.ReadFile(item.LocalSPBookPath)
		rawBytes, err2 := os.ReadFile(item.LocalRawPath)
		if err1 != nil || err2 != nil {
			_ = s.store.UpdatePendingUploadState(ctx, item.BookID, storage.UploadFailed, "本地待上传文件缺失")
			unverified[item.BookID] = struct{}{}
			anyFailed = true
			continue
		}
		var coverBytes []byte
		if item.LocalCoverPath != "" {
			coverBytes, _ = os.ReadFile(item.LocalCoverPath)
		}
		if _, err := engine.UploadBookAssets(ctx, book, spBytes, rawBytes, coverBytes); err != nil {
			_ = s.store.UpdatePendingUploadState(ctx, item.BookID, storage.UploadFailed, err.Error())
			unverified[item.BookID] = struct{}{}
			anyFailed = true
			continue
		}
		_ = s.store.DeletePendingUpload(ctx, item.BookID)
		uploadedAny = true
	}
	return unverified, uploadedAny, anyFailed
}

func (s *Service) newWebDAVClient() (*webdav.Client, error) {
	s.mu.Lock()
	p := s.profile
	s.mu.Unlock()
	if !p.Enabled || strings.TrimSpace(p.BaseURL) == "" {
		return nil, errors.New("尚未配置并启用 WebDAV 同步")
	}
	passBytes, _ := s.secrets.Get(webdavPasswordKey)
	return webdav.NewClient(webdav.Config{
		BaseURL:      p.BaseURL,
		Username:     p.Username,
		Password:     string(passBytes),
		AppFolder:    p.AppFolder,
		AtomicUpload: p.AtomicUpload,
		Timeout:      25 * time.Second,
	})
}

func (s *Service) loadWebDAVProfileFromDisk() {
	raw, err := os.ReadFile(filepath.Join(s.dataDir, webdavProfileFilename))
	if err != nil {
		return
	}
	var p WebDAVProfile
	if err := json.Unmarshal(raw, &p); err == nil {
		if p.AppFolder == "" {
			p.AppFolder = webdav.DefaultAppFolder
		}
		s.profile = p
	}
}

func (s *Service) scheduleDebouncedSync() {
	s.mu.Lock()
	if s.closed || !s.profile.Enabled || strings.TrimSpace(s.profile.BaseURL) == "" {
		s.mu.Unlock()
		return
	}
	delay := s.debounceDelay
	if s.debounceTimer != nil {
		s.debounceTimer.Stop()
	}
	s.debounceTimer = time.AfterFunc(delay, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		_, _ = s.SyncNow(ctx, false)
	})
	s.mu.Unlock()
}

func (s *Service) emitSyncToast(st SyncToastState) {
	s.mu.Lock()
	cb := s.onSyncToast
	s.mu.Unlock()
	if cb != nil {
		cb(st)
	}
}

func (s *Service) notifyDataChanged() {
	s.mu.Lock()
	cb := s.onDataChanged
	s.mu.Unlock()
	if cb != nil {
		cb()
	}
}
