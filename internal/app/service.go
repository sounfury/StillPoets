package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"stillpoets/internal/domain"
	"stillpoets/internal/storage"
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
