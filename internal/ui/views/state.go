package views

import (
	"context"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"stillpoets/internal/app"
	"stillpoets/internal/domain"
	"stillpoets/internal/storage"
	"stillpoets/internal/sync/webdav"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/sharecard"
	"stillpoets/internal/ui/theme"
)

// PickFileFunc 抽象原生文件选择框（EPUB 或字体导入），便于解耦 mygo.Dialog。
type PickFileFunc func(title string, extensions []string) (string, error)

// readerEntrySource 区分主线顺序阅读与各类偶遇直达入口。
// 偶遇模式下仅计入已读集合 ReadPoemIDs，不覆写顺序续读位置 LastPoemID，避免打断顺序阅读。
type readerEntrySource string

const (
	entryMainline readerEntrySource = ""     // 书架封面 / 继续阅读 / 默认选中
	entryWidget   readerEntrySource = "小组件"  // 桌面小组件 Deeplink
	entryToday    readerEntrySource = "今日重读" // 书架首页「今日重读」
	entryQuote    readerEntrySource = "散页"   // 散页列表/详情「读完整首」
	entrySearch   readerEntrySource = "检索"   // 全局检索命中诗篇
	entryStats    readerEntrySource = "统计"   // 统计热力图 / 诗人档案下钻
)

const (
	// 有效读诗事件判定阈值：停留满 5 秒自动记录；若读完短诗（>= 1.5 秒）后切走也计入，过滤快速扫目录噪声。
	poemDwellConfirmDelay = 5 * time.Second
	poemDwellMinLeave     = 1500 * time.Millisecond

	// 前台回切自动同步冷却窗口：防止冷启动 WhenReady + 首帧 OnActivate 或高频切屏连打 WebDAV。
	foregroundSyncCooldown = 15 * time.Second
)

type poemDwellSession struct {
	bookID        string
	poemID        string
	poemTitle     string
	author        string
	wasReadBefore bool
	startedAt     time.Time
	committed     bool
	timer         *time.Timer
}

func (s readerEntrySource) isPeek() bool {
	return s != entryMainline
}

type coverCacheEntry struct {
	hash string
	img  ui.ImageSource
}

// horizontalPagerState 保存横向手势翻页轨道的瞬时拖拽与同步状态。
type horizontalPagerState struct {
	scopeKey   string
	epoch      int
	width      float32
	offset     ui.Point
	syncedPage int
	progress   float32
}

// profileSubpage 表示「我的」设置页的二级子页面（点击设置项从右侧推入，左上角返回）。
type profileSubpage int

const (
	profileSubpageNone profileSubpage = iota
	profileSubpageWebDAV
	profileSubpageFonts
	profileSubpageWidget
	profileSubpageAbout
)

// AppUI 持有 StillPoets V3 界面的全部视图状态与交互调度。
type AppUI struct {
	svc              *app.Service
	pickFile         PickFileFunc
	openURLFn        func(string) error
	exportFileFn     func(path string) (string, error)
	savePhotoFn      func(path string) (string, error)
	shareImageFn     func(path, caption string) error
	copyTextFn       func(string)
	appVersion       string
	repaintFn        func()
	uiDispatch       func(func())
	windowBgFn       func(string)
	lastWindowBgHex  string
	systemBarStyleFn func(darkContent bool)
	lastBarDarkSet   bool
	lastBarDark      bool
	systemDark       bool
	registerFn       func(data []byte, family string) error

	mu          sync.Mutex // 保护回调指针、防重标记与请求序号等轻量控制状态
	uiThreadMu  sync.Mutex // 无窗口无头测试模式下串行化 Render 与 dispatchUI
	readOrderMu sync.Mutex // 保证阅读进度按 UI 触发顺序串行落库（P1-14）

	// 请求代次与并发控制
	openSeq         uint64
	quoteFilterSeq  uint64
	searchSeq       uint64
	toastSeq        uint64
	reloadRunning   bool
	reloadPending   bool
	pendingDeeplink string

	// 主导航状态
	activeTab    components.NavTab
	shelfSubTab  components.ShelfSubTab
	selectedGrp  string // "" 表示「全部」
	quoteKind    domain.QuoteKind
	quoteBook    string
	quoteAuthor  string
	readerMobile bool // 手机单栏模式下是否处于全屏阅读页

	// 已加载的持久化视图数据
	settings          domain.Settings
	groups            []domain.BookGroup
	books             []domain.Book
	progressMap       map[string]domain.ReadingProgress
	continueItem      *storage.ContinueReadingItem
	todayReread       *app.TodayReread
	quotes            []storage.QuoteView
	quoteFacets       storage.QuoteFacets
	stats             storage.StatsOverview
	syncOverview      app.SyncOverview
	localFonts        []app.LocalFont
	widgetCount       int
	activeWidgetCount int
	widgetPreview     *domain.WidgetTimeline
	deeplinkHandled   bool

	// 阅读器状态
	activeBookID      string
	activePoems       []domain.Poem
	activePoemIdx     int
	activeFavorites   map[string]domain.FavoritePoem
	highlightRanges   []app.LineRange
	selectingLines    bool
	selectedLines     map[int]bool
	poemCtxTarget     *poemContextTarget
	downloadingBooks  map[string]bool
	readerError       string
	readerEntrySource readerEntrySource
	peekBannerHidden  bool
	pendingPoemID     string

	// 瞬态云同步胶囊与操作提示
	toastMessage string
	toastPhase   app.SyncToastPhase
	toastTimer   *time.Timer
	syncInFlight bool
	syncStageMsg string
	bannerMsg    string

	// 弹窗/抽屉开关与表单状态
	showAddModal       bool
	showQuoteModal     bool
	showGroupModal     bool
	showSearchModal    bool
	showTOCModal       bool
	showTypoModal      bool
	showQuoteDetail    bool
	showChangelogModal bool
	showUpdateModal    bool

	changelogMDOpts  *markdown.Options
	changelogMDTheme theme.PresetID

	editingQuoteID   string
	quoteFormContent string
	quoteFormAuthor  string
	quoteFormSource  string
	quoteFormError   string
	detailQuote      *storage.QuoteView

	newGroupName    string
	renamingGroupID string
	renamingGroupTx string
	groupFormError  string

	searchQuery   string
	searchResults app.SearchResults
	tocQuery      string
	tocScroll     ui.ScrollState

	// 「我的」设置页二级导航与表单状态
	profileSub        profileSubpage
	inlineSpecimenIdx int
	fontsSpecimenIdx  int
	nowFn             func() time.Time
	cacheSizeBytes    int64
	diagLogSizeBytes  int64
	davURL            string
	davUser           string
	davPass           string
	davFolder         string
	davAtomic         bool
	davStatus         string
	davBusy           bool
	davRetiringID     string
	davHasSecret      bool
	confirmDisconnect bool
	confirmDeleteFont string
	autoSyncRunning   bool
	lastAutoSyncAt    time.Time

	// 「关于 StillPoets」二级页与自动更新状态（检查更新 / 应用内下载安装 / 更新日志 / 分析日志导出）
	appUpdater         AppUpdaterBridge
	updateChecking     bool
	updateResult       *app.UpdateCheckResult
	updateError        string
	lastAutoCheckAt    time.Time
	dismissedUpdateTag string
	updateStage        updateInstallStage
	updateModalRel     *app.ReleaseInfo
	updateAPKAsset     *app.ReleaseAsset
	updateDownloaded   int64
	updateTotal        int64
	updateSpeedBPS     float64
	updateAPKPath      string
	updateVerifiedSHA  bool
	updateStageError   string
	updateCancelFn     context.CancelFunc
	releasesLoading    bool
	releasesLoaded     bool
	releases           []app.ReleaseInfo
	releasesError      string
	exportingLog       bool
	exportLogStatus    string

	// 阅读器与双入口手势分页状态
	poemPager          horizontalPagerState
	shelfPager         horizontalPagerState
	shelfDragging      bool
	phoneQuotesScroll  ui.ScrollState
	tabletQuotesScroll ui.ScrollState

	// 排版滑块中间值（float64 适配 ui.Slider）
	fontSizeSlider   float64
	lineHeightSlider float64

	// 原书封面异步解码内存缓存（仅在开启「使用原书封面」时按需填充）
	coverCache    map[string]coverCacheEntry
	coverFetching map[string]bool

	// 有效读诗停留追踪与当前布局形态
	lastIsTablet bool
	dwell        *poemDwellSession

	// Phase 3 完整统计看板状态（完读墙 / 热力图 / 时段节律 / 诗人云）
	statsPeriod         storage.StatsPeriod
	statsHeatmapYear    int // 0 表示近一年（52 周）滚动视图
	statsAuthorMode     storage.AuthorWeightMode
	statsRhythm24h      bool
	statsDashboard      storage.StatsDashboard
	statsSelectedDate   string
	statsDayEvents      []storage.ReadingEventItem
	statsDayLoading     bool
	statsSeq            uint64
	statsDaySeq         uint64
	statsHeatmapScroll  ui.ScrollState
	showFinishedModal   bool
	detailFinishedBook  *storage.FinishedBookItem
	showAuthorModal     bool
	detailAuthorName    string
	detailAuthorProfile *storage.AuthorProfile
	detailAuthorLoading bool
	authorProfileSeq    uint64

	// 书架原地批量整理模式与单本长按快捷操作
	shelfOrganizing     bool
	shelfSelectedBooks  map[string]bool
	shelfTargetGroupID  string
	showBatchGroupModal bool
	batchNewGroupName   string
	batchGroupError     string
	showBookActionModal bool
	actionBookID        string
	actionNewGroupName  string
	actionGroupError    string

	// 高清分享卡片预览与导出弹窗状态（统一承载单句 / 整诗 / 合辑 / 完读 / 读诗报告）
	showShareCardModal bool
	shareCardData      sharecard.CardData
	shareCardOpts      sharecard.Options
	shareCardBusy      bool
	shareCardBusyLabel string
	shareCardStatus    string
	shareCardScroll    ui.ScrollState
}

// NewAppUI 创建绑定到核心 Service 的 V3 界面控制器。
func NewAppUI(svc *app.Service, pickFile PickFileFunc, repaint func()) *AppUI {
	a := &AppUI{
		svc:                svc,
		pickFile:           pickFile,
		appVersion:         app.DefaultAppVersion,
		repaintFn:          repaint,
		registerFn:         ui.RegisterFont,
		progressMap:        map[string]domain.ReadingProgress{},
		activeFavorites:    map[string]domain.FavoritePoem{},
		selectedLines:      map[int]bool{},
		downloadingBooks:   map[string]bool{},
		coverCache:         map[string]coverCacheEntry{},
		coverFetching:      map[string]bool{},
		davFolder:          webdav.DefaultAppFolder,
		statsPeriod:        storage.StatsPeriodAll,
		statsAuthorMode:    storage.AuthorWeightCombined,
		statsHeatmapScroll: ui.ScrollState{X: math.MaxFloat32},
		shelfSelectedBooks: map[string]bool{},
	}

	// 加载本地自定义字体并注册到 MyGo 字体引擎
	for _, f := range svc.ListLocalFonts() {
		if raw, err := os.ReadFile(f.Path); err == nil {
			_ = ui.RegisterFont(raw, f.Family)
		}
	}

	// 初始化 WebDAV 表单
	prof := svc.LoadWebDAVProfile()
	a.davURL = prof.BaseURL
	a.davUser = prof.Username
	a.davFolder = prof.AppFolder
	if a.davFolder == "" {
		a.davFolder = webdav.DefaultAppFolder
	}
	a.davAtomic = prof.AtomicUpload
	a.davHasSecret = svc.HasWebDAVPassword()

	svc.SetCallbacks(a.handleSyncToast, func() {
		a.ReloadAsync()
	})

	a.reloadSync(context.Background())
	// 若已有藏书，平板首屏自动定位到续读或第一本已缓存诗集
	a.autoSelectInitialBook()
	return a
}

// SetUIDispatch 绑定 UI 主线程状态提交与重绘调度函数（生产环境为 win.Update，消除跨线程竞态 P0-1）。
func (a *AppUI) SetUIDispatch(fn func(func())) {
	a.mu.Lock()
	a.uiDispatch = fn
	a.mu.Unlock()
}

// SetRepaint 绑定窗口重绘函数（兼容测试或仅触发重绘的调用方）。
func (a *AppUI) SetRepaint(fn func()) {
	a.mu.Lock()
	a.repaintFn = fn
	a.mu.Unlock()
}

// SetAppVersion 注入当前应用打包版本号（生产环境来自 mygo.App.Version）。
func (a *AppUI) SetAppVersion(version string) {
	v := strings.TrimSpace(version)
	if v == "" {
		return
	}
	a.mu.Lock()
	a.appVersion = v
	a.mu.Unlock()
}

// SetOpenURLFunc 注入外部浏览器 / 邮件客户端拉起能力（生产环境对接 mygo.Shell.OpenExternal）。
func (a *AppUI) SetOpenURLFunc(fn func(string) error) {
	a.mu.Lock()
	a.openURLFn = fn
	a.mu.Unlock()
}

// SetExportFileFunc 注入分析日志文件导出/系统分享能力（移动端对接 mygo.Share，桌面端回退 mygo.Dialog.SaveCopy）。
func (a *AppUI) SetExportFileFunc(fn func(path string) (string, error)) {
	a.mu.Lock()
	a.exportFileFn = fn
	a.mu.Unlock()
}

// SetSavePhotoFunc 注入高清图片保存至系统相册能力（移动端对接 photosaver，桌面端回退系统另存为）。
func (a *AppUI) SetSavePhotoFunc(fn func(path string) (string, error)) {
	a.mu.Lock()
	a.savePhotoFn = fn
	a.mu.Unlock()
}

// SetShareImageFunc 注入高清图片系统分享能力（移动端拉起 iOS UIActivityViewController / Android ACTION_SEND）。
func (a *AppUI) SetShareImageFunc(fn func(path, caption string) error) {
	a.mu.Lock()
	a.shareImageFn = fn
	a.mu.Unlock()
}

// SetCopyTextFunc 注入系统剪贴板写入能力（用于快捷复制联系邮箱等）。
func (a *AppUI) SetCopyTextFunc(fn func(string)) {
	a.mu.Lock()
	a.copyTextFn = fn
	a.mu.Unlock()
}

// dispatchUI 保证所有后台 goroutine 对 AppUI 状态字段的修改都在 UI 线程串行提交（P0-1）。
func (a *AppUI) dispatchUI(fn func()) {
	a.mu.Lock()
	disp := a.uiDispatch
	repaint := a.repaintFn
	a.mu.Unlock()
	if disp != nil {
		disp(fn)
		return
	}
	a.uiThreadMu.Lock()
	fn()
	a.uiThreadMu.Unlock()
	if repaint != nil {
		repaint()
	}
}

func (a *AppUI) requestRepaint() {
	a.dispatchUI(func() {})
}

// ReloadAsync 合并高频刷新通知，在后台串行刷新视图数据并通过 UI 线程提交（P2-1）。
func (a *AppUI) ReloadAsync() {
	a.mu.Lock()
	if a.reloadRunning {
		a.reloadPending = true
		a.mu.Unlock()
		return
	}
	a.reloadRunning = true
	a.mu.Unlock()

	go func() {
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			a.reloadSync(ctx)
			cancel()

			a.mu.Lock()
			if !a.reloadPending {
				a.reloadRunning = false
				a.mu.Unlock()
				return
			}
			a.reloadPending = false
			a.mu.Unlock()
		}
	}()
}

func (a *AppUI) palette() theme.Palette {
	return theme.Resolve(
		theme.PresetID(a.settings.ThemePreset),
		a.settings.ShouldFollowSystemDark(),
		a.systemDark,
	)
}

// SetInitialSystemDark 在窗口首次渲染前写入当前系统深色模式状态。
func (a *AppUI) SetInitialSystemDark(dark bool) {
	a.systemDark = dark
}

// NotifySystemDarkMode 接收来自宿主（mygo.Theme.OnUpdated）的系统深浅色切换事件，触发实时主题重绘与状态栏同步。
func (a *AppUI) NotifySystemDarkMode(dark bool) {
	a.dispatchUI(func() {
		a.systemDark = dark
		a.syncNativeAppearance(a.palette())
	})
}

// SetWindowBgFunc 注册原生窗口背景色同步回调，确保切换主题时状态栏/安全区底色同步更新。
func (a *AppUI) SetWindowBgFunc(fn func(hex string)) {
	a.mu.Lock()
	a.windowBgFn = fn
	a.lastWindowBgHex = ""
	a.mu.Unlock()
}

// SetSystemBarStyleFunc 注册系统状态栏/导航栏图标极性回调（darkContent=true 表示深色底配浅色图标）。
func (a *AppUI) SetSystemBarStyleFunc(fn func(darkContent bool)) {
	a.mu.Lock()
	a.systemBarStyleFn = fn
	a.lastBarDarkSet = false
	a.mu.Unlock()
}

func (a *AppUI) syncNativeAppearance(pal theme.Palette) {
	a.syncNativeWindowBg(pal.BgHex)
	a.syncSystemBarStyle(pal.ID == theme.PresetDarkMinimal)
}

func (a *AppUI) syncNativeWindowBg(bgHex string) {
	if bgHex == "" {
		return
	}
	a.mu.Lock()
	fn := a.windowBgFn
	if fn == nil || a.lastWindowBgHex == bgHex {
		a.mu.Unlock()
		return
	}
	a.lastWindowBgHex = bgHex
	a.mu.Unlock()
	fn(bgHex)
}

func (a *AppUI) syncSystemBarStyle(darkContent bool) {
	a.mu.Lock()
	fn := a.systemBarStyleFn
	if fn == nil || (a.lastBarDarkSet && a.lastBarDark == darkContent) {
		a.mu.Unlock()
		return
	}
	a.lastBarDarkSet = true
	a.lastBarDark = darkContent
	a.mu.Unlock()
	fn(darkContent)
}

// coverImageFor 按需返回诗集原书封面的已解码图像；未开启原书封面、无封面或尚未加载完成时返回 nil（自动回退几何色块封面）。
func (a *AppUI) coverImageFor(book domain.Book) ui.ImageSource {
	if !a.settings.UseOriginalCover || book.ID == "" || book.CoverHash == "" {
		return nil
	}
	if entry, ok := a.coverCache[book.ID]; ok && entry.hash == book.CoverHash {
		return entry.img
	}
	if a.coverFetching[book.ID] {
		return nil
	}
	a.coverFetching[book.ID] = true

	bookID := book.ID
	coverHash := book.CoverHash
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		var decoded ui.ImageSource
		path, err := a.svc.EnsureBookCover(ctx, bookID)
		if err == nil && path != "" {
			if raw, readErr := os.ReadFile(path); readErr == nil && len(raw) > 0 {
				if img, decErr := ui.DecodeImage(raw); decErr == nil {
					decoded = img
				}
			}
		}

		a.dispatchUI(func() {
			delete(a.coverFetching, bookID)
			a.coverCache[bookID] = coverCacheEntry{
				hash: coverHash,
				img:  decoded,
			}
		})
	}()
	return nil
}

func (a *AppUI) setToastState(st app.SyncToastState, notifyRepaint bool) {
	a.mu.Lock()
	if a.toastTimer != nil {
		a.toastTimer.Stop()
		a.toastTimer = nil
	}
	a.toastSeq++
	seq := a.toastSeq
	a.toastMessage = st.Message
	a.toastPhase = st.Phase
	if st.Phase == app.SyncToastSuccess || st.Phase == app.SyncToastPartial || st.Phase == app.SyncToastError {
		delay := 2200 * time.Millisecond
		if st.Phase != app.SyncToastSuccess {
			delay = 3500 * time.Millisecond
		}
		a.toastTimer = time.AfterFunc(delay, func() {
			a.mu.Lock()
			match := a.toastSeq == seq
			if match {
				a.toastMessage = ""
				a.toastPhase = app.SyncToastIdle
			}
			a.mu.Unlock()
			if match {
				a.requestRepaint()
			}
		})
	}
	a.mu.Unlock()

	if notifyRepaint {
		a.requestRepaint()
	}
}

func (a *AppUI) handleSyncToast(st app.SyncToastState) {
	a.mu.Lock()
	if st.Phase == app.SyncToastSyncing {
		a.syncInFlight = true
		a.syncStageMsg = st.Message
	} else {
		a.syncInFlight = false
		a.syncStageMsg = ""
	}
	a.mu.Unlock()
	a.setToastState(st, true)
}

func (a *AppUI) showTransientToast(msg string) {
	a.setToastState(app.SyncToastState{Phase: app.SyncToastSuccess, Message: msg}, true)
}

// showTransientToastOnUI 供已在 UI 线程 / Render 点击事件中调用，避免同步重入 uiThreadMu 或 win.Update 导致死锁。
func (a *AppUI) showTransientToastOnUI(msg string) {
	a.setToastState(app.SyncToastState{Phase: app.SyncToastSuccess, Message: msg}, false)
}
