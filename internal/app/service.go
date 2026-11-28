package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
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
	maxCoverBytes         = 8 << 20
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
	Size     int64
}

// SyncOverview 聚合「我的」页面展示的同步元状态与远端设备列表。
type SyncOverview struct {
	Configured    bool
	InstallID     string
	LocalRev      uint64
	Dirty         bool
	LastSyncAt    int64
	PendingCount  int
	PendingError  string
	RemoteDevices []string
}

// Service 是 StillPoets 的无界面核心应用服务层，统筹 SQLite 仓储、EPUB 导入、
// 本地缓存、WebDAV 串行同步、系统密钥链与桌面小组件快照编译。
type Service struct {
	dataDir string
	store   *storage.Store
	secrets SecretStore
	diag    *diagLogger

	mu                 sync.Mutex
	widgetMu           sync.Mutex
	widgetSyncedOnce   bool
	syncSem            chan struct{}
	syncWG             sync.WaitGroup
	syncCancel         context.CancelFunc // 当前同步或退役操作，由 mu 保护
	lifeCtx            context.Context
	lifeCancel         context.CancelFunc
	profile            WebDAVProfile
	workspacePasswords bool
	migrationSources   []WebDAVProfile
	workspaceEpoch     uint64
	webdavClient       *webdav.Client // 仅用于当前 workspaceEpoch，由 mu 保护
	remoteDevices      []string
	debounceDelay      time.Duration
	debounceTimer      *time.Timer
	debounceGen        uint64
	closed             bool

	onSyncToast     func(SyncToastState)
	onDataChanged   func()
	onWidgetUpdated func(timelinePath, fontsDir string)
}

// Open 初始化数据目录、SQLite 仓储与本地 WebDAV 配置。
func Open(ctx context.Context, dataDir string, secrets SecretStore) (*Service, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("app: dataDir is required")
	}
	for _, sub := range []string{"", "cache/raw", "cache/spbook", "cache/covers", "fonts", "logs"} {
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

	lifeCtx, lifeCancel := context.WithCancel(context.Background())
	svc := &Service{
		dataDir:       dataDir,
		store:         store,
		secrets:       secrets,
		diag:          newDiagLogger(dataDir),
		syncSem:       make(chan struct{}, 1),
		lifeCtx:       lifeCtx,
		lifeCancel:    lifeCancel,
		debounceDelay: defaultSyncDebounce,
		profile: WebDAVProfile{
			AppFolder: webdav.DefaultAppFolder,
		},
	}
	svc.loadWebDAVProfileFromDisk()
	svc.LogEvent("lifecycle", "service opened (webdavEnabled=%v, folder=%s)", svc.profile.Enabled, svc.profile.AppFolder)
	return svc, nil
}

// Close 停止后台防抖计时器、取消并等待所有在途同步退出，最后再安全关闭数据库（P1-3）。
func (s *Service) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.debounceGen++
	if s.debounceTimer != nil {
		s.debounceTimer.Stop()
		s.debounceTimer = nil
	}
	s.onSyncToast = nil
	s.onDataChanged = nil
	s.onWidgetUpdated = nil
	client := s.webdavClient
	s.webdavClient = nil
	s.mu.Unlock()

	s.lifeCancel()
	s.syncWG.Wait()
	if client != nil {
		client.CloseIdleConnections()
	}
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

// ExportsDir 返回高清分享卡片 PNG 临时导出目录（位于 dataDir 内，受 Android FileProvider 覆盖）。
func (s *Service) ExportsDir() string { return filepath.Join(s.dataDir, "exports") }

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

// UpdateSettings 原子应用局部设置补丁，避免并发修改主题/字号/字体时旧快照覆盖最新意图（P1-4）。
func (s *Service) UpdateSettings(ctx context.Context, patch func(*domain.Settings)) (domain.Settings, error) {
	st, err := s.store.UpdateSettings(ctx, patch)
	if err != nil {
		return domain.Settings{}, err
	}
	_, _ = s.RebuildWidgetTimeline(ctx)
	s.notifyDataChanged()
	return st, nil
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
	if err := os.MkdirAll(filepath.Join(s.dataDir, "fonts"), 0o755); err != nil {
		return LocalFont{}, nil, err
	}
	dst := filepath.Join(s.dataDir, "fonts", base)
	if err := os.WriteFile(dst, raw, 0o600); err != nil {
		return LocalFont{}, nil, err
	}
	s.LogEvent("font", "imported font family=%s file=%s sizeBytes=%d", family, base, len(raw))
	return LocalFont{Family: family, Filename: base, Path: dst, Size: int64(len(raw))}, raw, nil
}

// ListLocalFonts 列出本地私有字体目录中所有已导入的字体文件（按名称稳定排序）。
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
		var sz int64
		if info, err := e.Info(); err == nil {
			sz = info.Size()
		}
		out = append(out, LocalFont{
			Family:   strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())),
			Filename: e.Name(),
			Path:     filepath.Join(s.dataDir, "fonts", e.Name()),
			Size:     sz,
		})
	}
	slices.SortFunc(out, func(a, b LocalFont) int {
		if cmp := strings.Compare(strings.ToLower(a.Family), strings.ToLower(b.Family)); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Filename, b.Filename)
	})
	return out
}

// DeleteFont 删除本地私有字体目录中的指定自定义字体；若当前正启用该字体则自动回退到系统默认字体。
func (s *Service) DeleteFont(ctx context.Context, filename string) error {
	name := strings.TrimSpace(filename)
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `/\`) {
		return errors.New("app: invalid font filename")
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".ttf" && ext != ".otf" {
		return fmt.Errorf("app: unsupported font extension %q", ext)
	}
	target := filepath.Join(s.dataDir, "fonts", name)
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("app: remove font: %w", err)
	}
	s.LogEvent("font", "deleted font file=%s", name)

	deletedFamily := strings.TrimSuffix(name, filepath.Ext(name))
	st, err := s.store.LoadSettings(ctx)
	if err == nil && (st.FontFamily == deletedFamily || st.WidgetTheme.FontFile == name || st.WidgetTheme.FontFamily == deletedFamily) {
		_, err = s.UpdateSettings(ctx, func(cur *domain.Settings) {
			if cur.FontFamily == deletedFamily {
				cur.FontFamily = ""
			}
			if cur.WidgetTheme.FontFile == name || cur.WidgetTheme.FontFamily == deletedFamily {
				cur.WidgetTheme.FontFile = ""
				cur.WidgetTheme.FontFamily = cur.FontFamily
			}
		})
		return err
	}
	s.notifyDataChanged()
	return nil
}

// CacheSizeBytes 统计本地离线缓存目录（raw / spbook / covers）的磁盘占用总字节数。
func (s *Service) CacheSizeBytes() int64 {
	var total int64
	_ = filepath.WalkDir(filepath.Join(s.dataDir, "cache"), func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, statErr := d.Info(); statErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
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

// SetFavoriteCustomExcerpt 将用户手动划选的诗行设为该诗的小组件金句（单事务原子激活收藏并写入节选，P1-5）。
func (s *Service) SetFavoriteCustomExcerpt(ctx context.Context, book domain.Book, poem domain.Poem, excerpt, highlightLines string) error {
	excerpt = strings.TrimSpace(excerpt)
	if excerpt == "" {
		return errors.New("请先选择要设为金句的诗行")
	}
	author := poem.Author
	if author == "" {
		author = book.Author
	}
	if _, err := s.store.SetFavoriteCustomExcerpt(ctx, book.ID, poem.ID, poem.Title, author, excerpt, highlightLines); err != nil {
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

// RecordPoemPeek 记录偶遇/直达浏览：仅计入 ReadPoemIDs，不修改续读位置 LastPoemID。
func (s *Service) RecordPoemPeek(ctx context.Context, bookID, poemID string, totalPoems int) (domain.ReadingProgress, error) {
	p, changed, err := s.store.RecordPoemPeek(ctx, bookID, poemID, totalPoems)
	if err == nil && changed {
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

// RecordReadingEvent 记录一次有效读诗事件（单日同书同诗去重、自动区分初读/重读），
// 仅当真正插入新事件时才通知 UI 刷新并触发防抖同步。
func (s *Service) RecordReadingEvent(ctx context.Context, bookID, poemID, poemTitle, author string, wasReadBefore bool) (domain.ReadingEvent, bool, error) {
	ev, inserted, err := s.store.RecordReadingEvent(ctx, bookID, poemID, poemTitle, author, time.Now(), wasReadBefore)
	if err == nil && inserted {
		s.notifyDataChanged()
		s.scheduleDebouncedSync()
	}
	return ev, inserted, err
}

// GetStatsDashboard 聚合完整统计看板数据（时间窗口总览、完读墙、热力图、时段节律、诗人云）。
func (s *Service) GetStatsDashboard(ctx context.Context, query storage.StatsQuery) (storage.StatsDashboard, error) {
	return s.store.GetStatsDashboard(ctx, query)
}

// ListReadingEventsByDate 查询热力图指定日期（YYYY-MM-DD）的读诗明细列表。
func (s *Service) ListReadingEventsByDate(ctx context.Context, date string) ([]storage.ReadingEventItem, error) {
	return s.store.ListReadingEventsByDate(ctx, date)
}

// GetAuthorProfile 查询诗人云中点击某位诗人后展开的个人档案卡数据。
func (s *Service) GetAuthorProfile(ctx context.Context, author string) (storage.AuthorProfile, error) {
	return s.store.GetAuthorProfile(ctx, author)
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
	err = s.store.ApplyMergedSnapshotEx(runCtx, res.MergedData, meta.DirtySeq, res.MaxCounter, res.PublishedRev, res.Published, keepDirty, remoteDeviceRevs)
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

	// 先释放同步串行锁，再派发小组件重建与 UI 回调，避免外部回调重入同步锁（P2-2）
	release()

	_, _ = s.RebuildWidgetTimeline(context.Background())
	s.notifyDataChanged()

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
