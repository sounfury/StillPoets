package views

import (
	"context"
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"slices"
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
	"stillpoets/internal/ui/layout"
	"stillpoets/internal/ui/theme"
	"stillpoets/modules/widgetbridge"
)

// PickFileFunc 抽象原生文件选择框（EPUB 或字体导入），便于解耦 mygo.Dialog。
type PickFileFunc func(title string, extensions []string) (string, error)

// readerEntrySource 区分主线顺序阅读与各类偶遇直达入口。
// 偶遇模式下仅计入已读集合 ReadPoemIDs，不覆写顺序续读位置 LastPoemID，避免打断顺序阅读。
type readerEntrySource string

const (
	entryMainline readerEntrySource = ""     // 书架封面 / 继续阅读 / 统计页 / 默认选中
	entryWidget   readerEntrySource = "小组件"  // 桌面小组件 Deeplink
	entryToday    readerEntrySource = "今日重读" // 书架首页「今日重读」
	entryQuote    readerEntrySource = "散页"   // 散页列表/详情「读完整首」
	entrySearch   readerEntrySource = "检索"   // 全局检索命中诗篇
)

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

	// 「我的」设置页二级导航与表单状态
	profileSub        profileSubpage
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

	// 「关于 StillPoets」二级页状态（检查更新 / 更新日志 / 分析日志导出）
	updateChecking  bool
	updateResult    *app.UpdateCheckResult
	updateError     string
	releasesLoading bool
	releasesLoaded  bool
	releases        []app.ReleaseInfo
	releasesError   string
	exportingLog    bool
	exportLogStatus string

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
}

// NewAppUI 创建绑定到核心 Service 的 V3 界面控制器。
func NewAppUI(svc *app.Service, pickFile PickFileFunc, repaint func()) *AppUI {
	a := &AppUI{
		svc:              svc,
		pickFile:         pickFile,
		appVersion:       app.DefaultAppVersion,
		repaintFn:        repaint,
		registerFn:       ui.RegisterFont,
		progressMap:      map[string]domain.ReadingProgress{},
		activeFavorites:  map[string]domain.FavoritePoem{},
		selectedLines:    map[int]bool{},
		downloadingBooks: map[string]bool{},
		coverCache:       map[string]coverCacheEntry{},
		coverFetching:    map[string]bool{},
		davFolder:        webdav.DefaultAppFolder,
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

func (a *AppUI) reloadSync(ctx context.Context) {
	// 始终确保本地 SQLite 刷新使用未过期的独立上下文，防止上游网络同步超时后把本机书架/状态冲刷为空。
	if ctx == nil || ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
	}

	settings, settingsErr := a.svc.LoadSettings(ctx)
	if settingsErr == nil {
		if settings.FontSize <= 0 {
			settings.FontSize = 18
		}
		if settings.LineHeight <= 0 {
			settings.LineHeight = 1.85
		}
	}
	groups, groupsErr := a.svc.ListGroups(ctx)
	books, booksErr := a.svc.ListBooks(ctx, "")
	progressList, progressErr := a.svc.ListProgress(ctx)
	var pMap map[string]domain.ReadingProgress
	if progressErr == nil {
		pMap = make(map[string]domain.ReadingProgress, len(progressList))
		for _, p := range progressList {
			pMap[p.BookID] = p
		}
	}
	cont, contErr := a.svc.GetContinueReading(ctx)
	today, todayErr := a.svc.PickTodayReread(ctx)

	var (
		qFilter      storage.QuoteFilter
		curBookID    string
		snapOpenSeq  uint64
		snapQuoteSeq uint64
	)
	a.mu.Lock()
	qFilter = storage.QuoteFilter{
		Kind:       a.quoteKind,
		SourceBook: a.quoteBook,
		Author:     a.quoteAuthor,
	}
	curBookID = a.activeBookID
	snapOpenSeq = a.openSeq
	snapQuoteSeq = a.quoteFilterSeq
	a.mu.Unlock()

	quotes, quotesErr := a.svc.ListQuotes(ctx, qFilter)
	facets, facetsErr := a.svc.ListQuoteFacets(ctx)
	stats, statsErr := a.svc.GetStatsOverview(ctx)
	syncOv, syncOvErr := a.svc.GetSyncOverview(ctx)
	localFonts := a.svc.ListLocalFonts()
	cacheSize := a.svc.CacheSizeBytes()
	diagLogSize := a.svc.DiagnosticLogSizeBytes()
	widgetTL, widgetErr := a.svc.ReadWidgetTimeline()
	activeWidgets := widgetbridge.ActiveWidgetCount()
	hasSecret := a.svc.HasWebDAVPassword()

	var (
		poems    []domain.Poem
		poemsErr error
		favMap   map[string]domain.FavoritePoem
		favsErr  error
	)
	if curBookID != "" {
		poems, poemsErr = a.svc.ListPoems(ctx, curBookID)
		var favs []domain.FavoritePoem
		favs, favsErr = a.svc.ListFavorites(ctx, curBookID)
		if favsErr == nil {
			favMap = make(map[string]domain.FavoritePoem, len(favs))
			for _, f := range favs {
				favMap[f.PoemID] = f
			}
		}
	}

	a.dispatchUI(func() {
		if settingsErr == nil {
			a.settings = settings
			a.fontSizeSlider = float64(settings.FontSize)
			a.lineHeightSlider = float64(settings.LineHeight)
		} else if a.settings.FontSize <= 0 {
			a.settings = domain.Settings{ThemePreset: string(theme.PresetArtbook), FontSize: 18, LineHeight: 1.85}
			a.fontSizeSlider = 18
			a.lineHeightSlider = 1.85
		}
		a.syncNativeAppearance(a.palette())
		if groupsErr == nil {
			a.groups = groups
		}
		if booksErr == nil {
			a.books = books
		}
		if progressErr == nil {
			a.progressMap = pMap
		}
		if contErr == nil {
			a.continueItem = cont
		}
		if todayErr == nil {
			a.todayReread = today
		}
		a.mu.Lock()
		quoteMatch := a.quoteFilterSeq == snapQuoteSeq
		openMatch := a.openSeq == snapOpenSeq && a.activeBookID == curBookID
		a.mu.Unlock()
		if quoteMatch {
			if quotesErr == nil {
				a.quotes = quotes
			}
			if facetsErr == nil {
				a.quoteFacets = facets
			}
		}
		if statsErr == nil {
			a.stats = stats
		}
		if syncOvErr == nil {
			a.syncOverview = syncOv
		}
		a.localFonts = localFonts
		a.cacheSizeBytes = cacheSize
		a.diagLogSizeBytes = diagLogSize
		a.activeWidgetCount = activeWidgets
		if widgetErr == nil {
			cp := widgetTL
			a.widgetPreview = &cp
			a.widgetCount = len(widgetTL.Entries)
		}
		a.davHasSecret = hasSecret

		// 若当前在读书已被其他设备删除，退出全屏阅读层避免滞留无返回按钮的空态（P1-7）
		if booksErr == nil && a.activeBookID != "" {
			stillExists := false
			for _, b := range a.books {
				if b.ID == a.activeBookID {
					stillExists = true
					break
				}
			}
			if !stillExists {
				a.mu.Lock()
				a.openSeq++
				a.activeBookID = ""
				a.mu.Unlock()
				a.activePoems = nil
				a.activePoemIdx = 0
				a.activeFavorites = map[string]domain.FavoritePoem{}
				a.readerMobile = false
				a.readerError = ""
				a.readerEntrySource = entryMainline
				a.peekBannerHidden = false
				a.pendingPoemID = ""
				return
			}
		}

		if curBookID != "" && openMatch {
			if poemsErr == nil {
				a.activePoems = poems
				if a.activePoemIdx >= len(poems) {
					a.activePoemIdx = max(0, len(poems)-1)
				}
			}
			if favsErr == nil {
				a.activeFavorites = favMap
			}
		}
	})
}

func (a *AppUI) autoSelectInitialBook() {
	a.mu.Lock()
	if a.activeBookID != "" || a.deeplinkHandled {
		a.mu.Unlock()
		return
	}
	var targetBookID, targetPoemID string
	if a.continueItem != nil {
		targetBookID = a.continueItem.Book.ID
		targetPoemID = a.continueItem.Progress.LastPoemID
	} else {
		for _, b := range a.books {
			if b.LocalState == domain.LocalStateDownloaded {
				targetBookID = b.ID
				break
			}
		}
		if targetBookID == "" && len(a.books) > 0 {
			targetBookID = a.books[0].ID
		}
	}
	a.mu.Unlock()

	if targetBookID != "" {
		a.openBook(targetBookID, targetPoemID, nil, false)
	}
}

// HandleDeeplink 处理来自桌面小组件或系统路由的 stillpoets:// 协议。
func (a *AppUI) HandleDeeplink(rawURL string) {
	target, err := app.ParseDeeplink(rawURL)
	if err != nil {
		a.showTransientToast("无法识别的链接")
		return
	}
	switch target.Kind {
	case app.DeeplinkPoem:
		a.dispatchUI(func() {
			a.deeplinkHandled = true
			a.activeTab = components.TabShelf
			a.shelfSubTab = components.SubTabBooks
			a.shelfPager.scopeKey = ""
			a.poemPager.scopeKey = ""
			a.showQuoteDetail = false
			a.openBookLocked(target.BookID, target.PoemID, target.LineRanges, true, rawURL, entryWidget)
		})
	case app.DeeplinkQuote:
		var reqSeq uint64
		a.dispatchUI(func() {
			a.deeplinkHandled = true
			a.mu.Lock()
			a.openSeq++
			reqSeq = a.openSeq
			a.mu.Unlock()
		})
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			all, err := a.svc.ListQuotes(ctx, storage.QuoteFilter{})
			if err != nil {
				return
			}
			var found *storage.QuoteView
			for i := range all {
				if all[i].Quote.ID == target.QuoteID {
					q := all[i]
					found = &q
					break
				}
			}
			a.dispatchUI(func() {
				a.mu.Lock()
				stale := a.openSeq != reqSeq
				a.mu.Unlock()
				if stale {
					return
				}
				if found != nil {
					a.activeTab = components.TabShelf
					a.shelfSubTab = components.SubTabQuotes
					a.shelfPager.scopeKey = ""
					a.readerMobile = false
					a.detailQuote = found
					a.showQuoteDetail = true
				} else {
					a.mu.Lock()
					a.pendingDeeplink = rawURL
					a.mu.Unlock()
					a.showTransientToastOnUI("未在本地找到该散页，同步完成后将自动重试")
				}
			})
		}()
	}
}

// TriggerStartupSync 应用启动时若已启用 WebDAV，则在后台静默拉取最新元数据；完成后如有挂起的冷启动 Deeplink 则自动重试（P1-7）。
func (a *AppUI) TriggerStartupSync() {
	if !a.svc.LoadWebDAVProfile().Enabled {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		_, err := a.svc.SyncNow(ctx, false)
		if err == nil {
			a.mu.Lock()
			pending := a.pendingDeeplink
			a.pendingDeeplink = ""
			a.mu.Unlock()
			if pending != "" {
				a.HandleDeeplink(pending)
			}
		}
	}()
}

// Render 是 MyGo 原生窗口的根视图函数。
func (a *AppUI) Render(c *ui.Context) {
	a.mu.Lock()
	headless := a.uiDispatch == nil
	a.mu.Unlock()
	if headless {
		a.uiThreadMu.Lock()
		defer a.uiThreadMu.Unlock()
	}

	if sysTheme := c.Theme(); sysTheme != nil {
		a.systemDark = sysTheme.Dark
	}
	pal := a.palette()
	c.SetTheme(pal.UITheme())
	c.Root().Background(pal.Background)
	a.syncNativeAppearance(pal)

	isTablet := layout.IsTablet(c)

	if isTablet {
		a.renderTabletShell(c, pal)
	} else {
		a.renderPhoneShell(c, pal)
	}

	// 瞬态云同步轻胶囊（浮于底部，成功约 2.2 秒后自动淡出，平时零占位）
	a.renderSyncToastOverlay(c, pal, isTablet)

	// 所有轻弹窗与抽屉
	a.renderModals(c, pal)
}

func (a *AppUI) selectShelfSubTab(sub components.ShelfSubTab) {
	if a.shelfSubTab == sub {
		return
	}
	a.shelfSubTab = sub
	if sub == components.SubTabQuotes {
		go a.reloadQuotesOnly()
	}
}

func (a *AppUI) renderTabletShell(c *ui.Context, pal theme.Palette) {
	ui.Row(c.Key("tablet-root")).Fill().Children(func() {
		components.TabletNavRail(c, pal, a.activeTab, func(tab components.NavTab) {
			a.activeTab = tab
			a.readerMobile = false
			a.profileSub = profileSubpageNone
			a.showChangelogModal = false
			a.confirmDisconnect = false
			a.confirmDeleteFont = ""
		})

		ui.Box(c.Key("tablet-content")).Grow(1).FillHeight().Clip().Children(func() {
			tabKey := fmt.Sprintf("tablet-tab-%d", a.activeTab)
			tabBox := ui.Box(c.Key(tabKey)).
				Absolute().
				Fill().
				Background(pal.Background).
				Transition(theme.TabPageTransition())
			if grain := pal.PaperGrainOpacity(); grain > 0 {
				isDark := pal.IsDark()
				tabBox.Draw(func(p *ui.Painter, r ui.Rect) {
					theme.DrawPaperTexture(p, r, 0, isDark, grain)
				})
			}
			tabBox.Children(func() {
				switch a.activeTab {
				case components.TabShelf:
					a.renderTabletShelfWorkspace(c, pal)
				case components.TabStats:
					a.renderStatsPage(c, pal, true)
				case components.TabProfile:
					a.renderProfilePage(c, pal, true)
				}
			})
		})
	})
}

func (a *AppUI) useFloatingBottomNav() bool {
	if a.settings.FloatingBottomBar != nil {
		return *a.settings.FloatingBottomBar
	}
	return components.DefaultFloatingBottomNav()
}

func (a *AppUI) renderPhoneShell(c *ui.Context, pal theme.Palette) {
	floatingNav := a.useFloatingBottomNav()
	renderBody := func() {
		tabKey := fmt.Sprintf("phone-tab-%d", a.activeTab)
		tabBox := ui.Box(c.Key(tabKey)).
			Absolute().
			Fill().
			Background(pal.Background).
			Transition(theme.TabPageTransition())
		if grain := pal.PaperGrainOpacity(); grain > 0 {
			isDark := pal.IsDark()
			tabBox.Draw(func(p *ui.Painter, r ui.Rect) {
				theme.DrawPaperTexture(p, r, 0, isDark, grain)
			})
		}
		tabBox.Children(func() {
			switch a.activeTab {
			case components.TabShelf:
				a.renderShelfPane(c, pal, false)
			case components.TabStats:
				a.renderStatsPage(c, pal, false)
			case components.TabProfile:
				a.renderProfilePage(c, pal, false)
			}
		})
	}

	ui.Box(c.Key("phone-stage")).Fill().Clip().Children(func() {
		if floatingNav {
			// iOS 液态玻璃悬浮底栏：正文全屏铺底以便滚动内容从胶囊下方透出折射
			ui.Box(c.Key("phone-body")).Fill().Clip().Children(renderBody)
			components.FloatingBottomNavBar(c, pal, a.activeTab, func(tab components.NavTab) {
				a.activeTab = tab
				a.readerMobile = false
				a.profileSub = profileSubpageNone
				a.showChangelogModal = false
				a.confirmDisconnect = false
				a.confirmDeleteFont = ""
			})
		} else {
			ui.Column(c.Key("phone-root")).Fill().Children(func() {
				ui.Box(c.Key("phone-body")).Grow(1).FillWidth().Clip().Children(renderBody)
				components.BottomNavBar(c, pal, a.activeTab, func(tab components.NavTab) {
					a.activeTab = tab
					a.readerMobile = false
					a.profileSub = profileSubpageNone
					a.confirmDisconnect = false
					a.confirmDeleteFont = ""
				})
			})
		}

		// 手机端设置二级页面以右侧推入转场叠在底栏之上，返回时保留设置主页滚动位置
		if a.activeTab == components.TabProfile && a.profileSub != profileSubpageNone {
			a.renderProfileSubpageLayer(c, pal, false)
		}

		// 手机端全屏阅读器以右侧推入转场叠在书架之上，返回时保留书架滚动位置
		if a.readerMobile && a.activeBookID != "" {
			ui.Box(c.Key("phone-reader-layer")).
				Absolute().
				Fill().
				Background(pal.ReaderPaper).
				Transition(theme.MobileReaderTransition()).
				Children(func() {
					a.renderReaderPane(c, pal, false)
				})
		}
	})
}

func (a *AppUI) renderSyncToastOverlay(c *ui.Context, pal theme.Palette, isTablet bool) {
	a.mu.Lock()
	msg := a.toastMessage
	phase := a.toastPhase
	a.mu.Unlock()
	if msg == "" {
		return
	}
	w, h := c.Size()
	if w <= 0 || h <= 0 {
		return
	}
	_, _, safeBottom, _ := c.SafeArea()
	bottom := float32(76) + safeBottom
	if a.useFloatingBottomNav() {
		bottom = 84 + safeBottom
	}
	if !isTablet && a.activeTab == components.TabProfile && a.profileSub != profileSubpageNone {
		bottom = 24 + safeBottom
	}
	left := float32(0)
	width := w
	if isTablet {
		bottom = 20 + safeBottom
		left = layout.NavRailWidthDIP
		width = layout.MasterPaneWidthDIP
	}
	toastKind := components.ToastInfo
	switch phase {
	case app.SyncToastSyncing:
		toastKind = components.ToastLoading
	case app.SyncToastPartial, app.SyncToastError:
		toastKind = components.ToastWarning
	}
	ui.Overlay(c, func() {
		ui.Row(c.Key("toast-overlay")).
			Absolute().
			Left(left).
			Bottom(bottom).
			Width(width).
			Justify(ui.Center).
			Transition(theme.ToastTransition()).
			Children(func() {
				components.SyncToastPill(c, pal, toastKind, msg, func() {
					a.activeTab = components.TabProfile
				})
			})
	})
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

// --- 异步业务动作 ---

func (a *AppUI) openBook(bookID, poemID string, highlightRanges []app.LineRange, enterMobileReader bool) {
	a.openBookLocked(bookID, poemID, highlightRanges, enterMobileReader, "", entryMainline)
}

func (a *AppUI) openBookWithSource(bookID, poemID string, highlightRanges []app.LineRange, enterMobileReader bool, source readerEntrySource) {
	a.openBookLocked(bookID, poemID, highlightRanges, enterMobileReader, "", source)
}

// openBookLocked 必须在 UI 线程上调用（来自点击事件或 dispatchUI 闭包），通过单调递增 openSeq 彻底消除切书乱序与串书污染（P0-2）。
func (a *AppUI) openBookLocked(bookID, poemID string, highlightRanges []app.LineRange, enterMobileReader bool, deeplinkURL string, source readerEntrySource) {
	a.mu.Lock()
	a.openSeq++
	reqSeq := a.openSeq
	a.activeBookID = bookID
	a.mu.Unlock()

	a.highlightRanges = slices.Clone(highlightRanges)
	a.selectingLines = false
	a.selectedLines = map[int]bool{}
	a.readerError = ""
	a.readerEntrySource = source
	a.peekBannerHidden = false
	a.pendingPoemID = poemID
	if enterMobileReader {
		a.readerMobile = true
	}

	var targetBook *domain.Book
	for i := range a.books {
		if a.books[i].ID == bookID {
			cp := a.books[i]
			targetBook = &cp
			break
		}
	}

	if targetBook != nil && targetBook.LocalState != domain.LocalStateDownloaded {
		a.downloadBookAsync(bookID, poemID, highlightRanges, reqSeq, source)
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if targetBook == nil {
			b, err := a.svc.Store().GetBook(ctx, bookID)
			if err != nil {
				if deeplinkURL != "" {
					a.mu.Lock()
					a.pendingDeeplink = deeplinkURL
					a.mu.Unlock()
				}
				a.dispatchUI(func() {
					a.mu.Lock()
					stale := a.openSeq != reqSeq
					a.mu.Unlock()
					if !stale {
						a.readerError = "暂未在本地找到该诗集，请等待云同步完成后重试"
					}
				})
				return
			}
			if b.LocalState != domain.LocalStateDownloaded {
				a.downloadBookAsync(bookID, poemID, highlightRanges, reqSeq, source)
				return
			}
		}
		poems, err := a.svc.ListPoems(ctx, bookID)
		if err != nil {
			a.dispatchUI(func() {
				a.mu.Lock()
				stale := a.openSeq != reqSeq || a.activeBookID != bookID
				a.mu.Unlock()
				if !stale {
					a.readerError = fmt.Sprintf("加载诗集失败：%v", err)
				}
			})
			return
		}
		favs, _ := a.svc.ListFavorites(ctx, bookID)
		favMap := make(map[string]domain.FavoritePoem, len(favs))
		for _, f := range favs {
			favMap[f.PoemID] = f
		}
		prog, progErr := a.svc.GetProgress(ctx, bookID)

		// 定位目标诗篇（优先显式 poemID，其次续读 LastPoemID，其次首篇正式诗）
		idx := 0
		wantPoem := poemID
		if wantPoem == "" {
			wantPoem = prog.LastPoemID
		}
		found := false
		if wantPoem != "" {
			for i, p := range poems {
				if p.ID == wantPoem {
					idx = i
					found = true
					break
				}
			}
		}
		if !found {
			for i, p := range poems {
				if p.IsPoem() {
					idx = i
					break
				}
			}
		}

		// 若偶遇直达的恰好就是本书当前主线续读位置，则自然回归主线阅读，避免冗余提示条。
		effectiveSource := source
		if effectiveSource.isPeek() && len(poems) > 0 && idx < len(poems) && prog.LastPoemID != "" && poems[idx].ID == prog.LastPoemID {
			effectiveSource = entryMainline
		}

		var readPoem *domain.Poem
		a.dispatchUI(func() {
			a.mu.Lock()
			stale := a.openSeq != reqSeq || a.activeBookID != bookID
			a.mu.Unlock()
			if stale {
				return
			}
			if progErr == nil {
				a.progressMap[bookID] = prog
			}
			a.activePoems = poems
			a.activePoemIdx = idx
			a.activeFavorites = favMap
			a.poemPager.scopeKey = ""
			a.readerError = ""
			a.readerEntrySource = effectiveSource
			a.peekBannerHidden = false
			a.pendingPoemID = ""
			if len(poems) > 0 && idx < len(poems) && enterMobileReader {
				cp := poems[idx]
				readPoem = &cp
			}
		})

		if readPoem != nil {
			if effectiveSource.isPeek() {
				a.recordPoemPeekAsync(bookID, *readPoem, poems)
			} else {
				a.recordPoemReadAsync(bookID, *readPoem, poems)
			}
		}
	}()
}

func (a *AppUI) downloadBookAsync(bookID, poemID string, highlightRanges []app.LineRange, reqSeq uint64, source readerEntrySource) {
	a.mu.Lock()
	if a.downloadingBooks[bookID] {
		a.mu.Unlock()
		return
	}
	a.downloadingBooks[bookID] = true
	a.mu.Unlock()
	a.dispatchUI(func() {
		a.mu.Lock()
		match := a.activeBookID == bookID
		a.mu.Unlock()
		if match {
			a.readerError = ""
		}
	})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		err := a.svc.EnsureBookContent(ctx, bookID)
		a.mu.Lock()
		delete(a.downloadingBooks, bookID)
		stillTarget := a.openSeq == reqSeq && a.activeBookID == bookID
		a.mu.Unlock()

		a.reloadSync(nil)
		a.dispatchUI(func() {
			a.mu.Lock()
			match := a.openSeq == reqSeq && a.activeBookID == bookID
			a.mu.Unlock()
			if !match {
				return
			}
			if err != nil {
				a.readerError = err.Error()
				return
			}
			if stillTarget {
				a.openBookLocked(bookID, poemID, highlightRanges, a.readerMobile, "", source)
			}
		})
	}()
}

func (a *AppUI) recordPoemReadAsync(bookID string, poem domain.Poem, allPoems []domain.Poem) {
	if !poem.IsPoem() {
		return
	}
	totalPoems := countFormalPoems(allPoems)
	go func() {
		// 串行写阅读进度，避免快速连续翻页时旧篇目后拿锁覆盖新续读位置（P1-14）
		a.readOrderMu.Lock()
		defer a.readOrderMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = a.svc.RecordPoemRead(ctx, bookID, poem.ID, 0, totalPoems)
	}()
}

func (a *AppUI) recordPoemPeekAsync(bookID string, poem domain.Poem, allPoems []domain.Poem) {
	if !poem.IsPoem() {
		return
	}
	totalPoems := countFormalPoems(allPoems)
	go func() {
		// 与主线写进度共享串行锁，确保偶遇写入与主线写入严格按触发顺序落库
		a.readOrderMu.Lock()
		defer a.readOrderMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = a.svc.RecordPoemPeek(ctx, bookID, poem.ID, totalPoems)
	}()
}

func countFormalPoems(allPoems []domain.Poem) int {
	totalPoems := 0
	for _, p := range allPoems {
		if p.IsPoem() {
			totalPoems++
		}
	}
	return totalPoems
}

func (a *AppUI) triggerImportEPUB() {
	if a.pickFile == nil {
		a.showTransientToastOnUI("当前环境未注册文件选择器")
		return
	}
	a.showAddModal = false
	groupID := a.selectedGrp
	a.mu.Lock()
	startSeq := a.openSeq
	a.mu.Unlock()
	go func() {
		filePath, err := a.pickFile("选择 EPUB 诗集文件", []string{"epub"})
		if err != nil || strings.TrimSpace(filePath) == "" {
			return
		}
		a.showTransientToast("正在解析诗集…")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := a.svc.ImportEPUB(ctx, filePath, groupID)
		if err != nil {
			a.showTransientToast("导入失败：" + err.Error())
			return
		}
		a.reloadSync(nil)
		a.dispatchUI(func() {
			a.mu.Lock()
			userNavigated := a.openSeq != startSeq
			a.mu.Unlock()
			if !userNavigated {
				a.openBookLocked(res.Book.ID, "", nil, false, "", entryMainline)
			}
		})
		a.showTransientToast(fmt.Sprintf("已导入《%s》(%d 首)", res.Book.Title, res.Book.PoemCount))
	}()
}

func (a *AppUI) triggerImportFont() {
	if a.pickFile == nil {
		a.showTransientToastOnUI("当前环境未注册文件选择器")
		return
	}
	go func() {
		filePath, err := a.pickFile("选择自定义字体 (.ttf / .otf)", []string{"ttf", "otf"})
		if err != nil || strings.TrimSpace(filePath) == "" {
			return
		}
		lf, raw, err := a.svc.ImportFont(filePath)
		if err != nil {
			a.showTransientToast("字体导入失败：" + err.Error())
			return
		}
		if a.registerFn != nil {
			if err := a.registerFn(raw, lf.Family); err != nil {
				a.showTransientToast("字体注册失败：" + err.Error())
				return
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = a.svc.UpdateSettings(ctx, func(st *domain.Settings) {
			st.FontFamily = lf.Family
			st.WidgetTheme.FontFamily = lf.Family
			st.WidgetTheme.FontFile = lf.Filename
		})
		a.reloadSync(nil)
		a.showTransientToast("已启用字体：" + lf.Family)
	}()
}

func (a *AppUI) activeBook() (domain.Book, bool) {
	for _, b := range a.books {
		if b.ID == a.activeBookID {
			return b, true
		}
	}
	return domain.Book{}, false
}

func (a *AppUI) filteredBooks() []domain.Book {
	if a.selectedGrp == "" {
		return a.books
	}
	out := make([]domain.Book, 0, len(a.books))
	for _, b := range a.books {
		if b.GroupID == a.selectedGrp {
			out = append(out, b)
		}
	}
	return out
}
