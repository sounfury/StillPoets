package views

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
	"stillpoets/internal/domain"
	"stillpoets/internal/storage"
	"stillpoets/internal/sync/webdav"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/layout"
	"stillpoets/internal/ui/theme"
)

// PickFileFunc 抽象原生文件选择框（EPUB 或字体导入），便于解耦 mygo.Dialog。
type PickFileFunc func(title string, extensions []string) (string, error)

// AppUI 持有 StillPoets V3 界面的全部视图状态与交互调度。
type AppUI struct {
	svc        *app.Service
	pickFile   PickFileFunc
	repaintFn  func()
	registerFn func(data []byte, family string) error

	mu sync.Mutex

	// 主导航状态
	activeTab    components.NavTab
	shelfSubTab  components.ShelfSubTab
	selectedGrp  string // "" 表示「全部」
	quoteKind    domain.QuoteKind
	quoteBook    string
	quoteAuthor  string
	readerMobile bool // 手机单栏模式下是否处于全屏阅读页

	// 已加载的持久化视图数据
	settings      domain.Settings
	groups        []domain.BookGroup
	books         []domain.Book
	progressMap   map[string]domain.ReadingProgress
	continueItem  *storage.ContinueReadingItem
	todayReread   *app.TodayReread
	quotes        []storage.QuoteView
	quoteFacets   storage.QuoteFacets
	stats         storage.StatsOverview
	syncOverview  app.SyncOverview
	localFonts    []app.LocalFont
	widgetCount   int

	// 阅读器状态
	activeBookID    string
	activePoems     []domain.Poem
	activePoemIdx   int
	activeFavorites map[string]domain.FavoritePoem
	highlightRanges []app.LineRange
	selectingLines  bool
	selectedLines   map[int]bool
	downloadingBook string
	readerError     string

	// 瞬态云同步胶囊与操作提示
	toastMessage string
	toastTimer   *time.Timer
	bannerMsg    string

	// 弹窗/抽屉开关与表单状态
	showAddModal    bool
	showQuoteModal  bool
	showGroupModal  bool
	showSearchModal bool
	showTOCModal    bool
	showTypoModal   bool
	showQuoteDetail bool

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

	// 「我的」WebDAV 表单状态
	davURL       string
	davUser      string
	davPass      string
	davFolder    string
	davAtomic    bool
	davStatus    string
	davBusy      bool
	davHasSecret bool

	// 排版滑块中间值（float64 适配 ui.Slider）
	fontSizeSlider   float64
	lineHeightSlider float64
}

// NewAppUI 创建绑定到核心 Service 的 V3 界面控制器。
func NewAppUI(svc *app.Service, pickFile PickFileFunc, repaint func()) *AppUI {
	a := &AppUI{
		svc:             svc,
		pickFile:        pickFile,
		repaintFn:       repaint,
		registerFn:      ui.RegisterFont,
		progressMap:     map[string]domain.ReadingProgress{},
		activeFavorites: map[string]domain.FavoritePoem{},
		selectedLines:   map[int]bool{},
		davFolder:       webdav.DefaultAppFolder,
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
		go a.ReloadAsync()
	})

	a.reloadSync(context.Background())
	// 若已有藏书，平板首屏自动定位到续读或第一本已缓存诗集
	a.autoSelectInitialBook()
	return a
}

// SetRepaint 绑定窗口重绘函数（通常为 win.Update）。
func (a *AppUI) SetRepaint(fn func()) {
	a.mu.Lock()
	a.repaintFn = fn
	a.mu.Unlock()
}

func (a *AppUI) requestRepaint() {
	a.mu.Lock()
	fn := a.repaintFn
	a.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// ReloadAsync 在后台刷新所有视图数据并触发界面重绘。
func (a *AppUI) ReloadAsync() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.reloadSync(ctx)
	a.requestRepaint()
}

func (a *AppUI) reloadSync(ctx context.Context) {
	settings, err := a.svc.LoadSettings(ctx)
	if err != nil {
		settings = domain.Settings{ThemePreset: string(theme.PresetArtbook), FontSize: 18, LineHeight: 1.85}
	}
	if settings.FontSize <= 0 {
		settings.FontSize = 18
	}
	if settings.LineHeight <= 0 {
		settings.LineHeight = 1.85
	}
	groups, _ := a.svc.ListGroups(ctx)
	books, _ := a.svc.ListBooks(ctx, "")
	progressList, _ := a.svc.ListProgress(ctx)
	pMap := make(map[string]domain.ReadingProgress, len(progressList))
	for _, p := range progressList {
		pMap[p.BookID] = p
	}
	cont, _ := a.svc.GetContinueReading(ctx)
	today, _ := a.svc.PickTodayReread(ctx)

	a.mu.Lock()
	qFilter := storage.QuoteFilter{
		Kind:       a.quoteKind,
		SourceBook: a.quoteBook,
		Author:     a.quoteAuthor,
	}
	curBookID := a.activeBookID
	a.mu.Unlock()

	quotes, _ := a.svc.ListQuotes(ctx, qFilter)
	facets, _ := a.svc.ListQuoteFacets(ctx)
	stats, _ := a.svc.GetStatsOverview(ctx)
	syncOv, _ := a.svc.GetSyncOverview(ctx)
	localFonts := a.svc.ListLocalFonts()

	var poems []domain.Poem
	favMap := map[string]domain.FavoritePoem{}
	if curBookID != "" {
		poems, _ = a.svc.ListPoems(ctx, curBookID)
		if favs, err := a.svc.ListFavorites(ctx, curBookID); err == nil {
			for _, f := range favs {
				favMap[f.PoemID] = f
			}
		}
	}

	a.mu.Lock()
	a.settings = settings
	a.fontSizeSlider = float64(settings.FontSize)
	a.lineHeightSlider = float64(settings.LineHeight)
	a.groups = groups
	a.books = books
	a.progressMap = pMap
	a.continueItem = cont
	a.todayReread = today
	a.quotes = quotes
	a.quoteFacets = facets
	a.stats = stats
	a.syncOverview = syncOv
	a.localFonts = localFonts
	a.davHasSecret = a.svc.HasWebDAVPassword()
	if curBookID != "" {
		a.activePoems = poems
		a.activeFavorites = favMap
		if a.activePoemIdx >= len(poems) {
			a.activePoemIdx = max(0, len(poems)-1)
		}
	}
	a.mu.Unlock()
}

func (a *AppUI) autoSelectInitialBook() {
	a.mu.Lock()
	if a.activeBookID != "" {
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
		a.mu.Lock()
		a.activeTab = components.TabShelf
		a.shelfSubTab = components.SubTabBooks
		a.mu.Unlock()
		a.openBook(target.BookID, target.PoemID, target.LineRanges, true)
	case app.DeeplinkQuote:
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			all, err := a.svc.ListQuotes(ctx, storage.QuoteFilter{})
			if err != nil {
				return
			}
			for i := range all {
				if all[i].Quote.ID == target.QuoteID {
					q := all[i]
					a.mu.Lock()
					a.activeTab = components.TabShelf
					a.shelfSubTab = components.SubTabQuotes
					a.detailQuote = &q
					a.showQuoteDetail = true
					a.mu.Unlock()
					a.requestRepaint()
					return
				}
			}
		}()
	}
}

// TriggerStartupSync 应用启动时若已启用 WebDAV，则在后台静默拉取最新元数据。
func (a *AppUI) TriggerStartupSync() {
	if !a.svc.LoadWebDAVProfile().Enabled {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()
		_, _ = a.svc.SyncNow(ctx, false)
	}()
}

// Render 是 MyGo 原生窗口的根视图函数。
func (a *AppUI) Render(c *ui.Context) {
	pal := a.palette()
	c.SetTheme(pal.UITheme())
	c.Root().Background(pal.Background)

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

func (a *AppUI) renderTabletShell(c *ui.Context, pal theme.Palette) {
	ui.Row(c.Key("tablet-root")).Fill().Children(func() {
		components.TabletNavRail(c, pal, a.activeTab, func(tab components.NavTab) {
			a.activeTab = tab
			a.readerMobile = false
		})

		ui.Column(c.Key("tablet-content")).Grow(1).FillHeight().Children(func() {
			switch a.activeTab {
			case components.TabShelf:
				if a.shelfSubTab == components.SubTabBooks {
					layout.MasterDetail(c, layout.MasterPaneWidthDIP, pal.Border,
						func() { a.renderShelfPane(c, pal, true) },
						func() { a.renderReaderPane(c, pal, true) },
					)
				} else {
					a.renderShelfPane(c, pal, true)
				}
			case components.TabStats:
				a.renderStatsPage(c, pal, true)
			case components.TabProfile:
				a.renderProfilePage(c, pal, true)
			}
		})
	})
}

func (a *AppUI) renderPhoneShell(c *ui.Context, pal theme.Palette) {
	if a.readerMobile && a.activeBookID != "" {
		a.renderReaderPane(c, pal, false)
		return
	}

	ui.Column(c.Key("phone-root")).Fill().Children(func() {
		ui.Column(c.Key("phone-body")).Grow(1).FillWidth().Clip().Children(func() {
			switch a.activeTab {
			case components.TabShelf:
				a.renderShelfPane(c, pal, false)
			case components.TabStats:
				a.renderStatsPage(c, pal, false)
			case components.TabProfile:
				a.renderProfilePage(c, pal, false)
			}
		})
		components.BottomNavBar(c, pal, a.activeTab, func(tab components.NavTab) {
			a.activeTab = tab
			a.readerMobile = false
		})
	})
}

func (a *AppUI) renderSyncToastOverlay(c *ui.Context, pal theme.Palette, isTablet bool) {
	a.mu.Lock()
	msg := a.toastMessage
	a.mu.Unlock()
	if msg == "" {
		return
	}
	w, h := c.Size()
	if w <= 0 || h <= 0 {
		return
	}
	bottom := float32(76)
	left := float32(0)
	width := w
	if isTablet {
		bottom = 20
		left = layout.NavRailWidthDIP
		width = layout.MasterPaneWidthDIP
	}
	ui.Overlay(c, func() {
		ui.Row(c.Key("toast-overlay")).
			Absolute().
			Left(left).
			Bottom(bottom).
			Width(width).
			Justify(ui.Center).
			Children(func() {
				components.SyncToastPill(c, pal, msg, func() {
					a.activeTab = components.TabProfile
				})
			})
	})
}

func (a *AppUI) palette() theme.Palette {
	a.mu.Lock()
	preset := a.settings.ThemePreset
	a.mu.Unlock()
	return theme.ByID(theme.PresetID(preset))
}

func (a *AppUI) handleSyncToast(st app.SyncToastState) {
	a.mu.Lock()
	if a.toastTimer != nil {
		a.toastTimer.Stop()
		a.toastTimer = nil
	}
	a.toastMessage = st.Message
	if st.Phase == app.SyncToastSuccess || st.Phase == app.SyncToastPartial || st.Phase == app.SyncToastError {
		delay := 2200 * time.Millisecond
		if st.Phase != app.SyncToastSuccess {
			delay = 3500 * time.Millisecond
		}
		a.toastTimer = time.AfterFunc(delay, func() {
			a.mu.Lock()
			a.toastMessage = ""
			a.mu.Unlock()
			a.requestRepaint()
		})
	}
	a.mu.Unlock()
	a.requestRepaint()
}

func (a *AppUI) showTransientToast(msg string) {
	a.handleSyncToast(app.SyncToastState{Phase: app.SyncToastSuccess, Message: msg})
}

// --- 异步业务动作 ---

func (a *AppUI) openBook(bookID, poemID string, highlightRanges []app.LineRange, enterMobileReader bool) {
	a.mu.Lock()
	a.activeBookID = bookID
	a.highlightRanges = slices.Clone(highlightRanges)
	a.selectingLines = false
	a.selectedLines = map[int]bool{}
	a.readerError = ""
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
	a.mu.Unlock()

	if targetBook != nil && targetBook.LocalState != domain.LocalStateDownloaded {
		a.downloadBookAsync(bookID, poemID, highlightRanges)
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		poems, err := a.svc.ListPoems(ctx, bookID)
		if err != nil {
			a.mu.Lock()
			a.readerError = fmt.Sprintf("加载诗集失败：%v", err)
			a.mu.Unlock()
			a.requestRepaint()
			return
		}
		favs, _ := a.svc.ListFavorites(ctx, bookID)
		favMap := make(map[string]domain.FavoritePoem, len(favs))
		for _, f := range favs {
			favMap[f.PoemID] = f
		}
		prog, _ := a.svc.GetProgress(ctx, bookID)

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

		a.mu.Lock()
		a.activePoems = poems
		a.activePoemIdx = idx
		a.activeFavorites = favMap
		a.mu.Unlock()

		// 仅在显式进入阅读或有效诗篇时记录阅读位置
		if len(poems) > 0 && idx < len(poems) && enterMobileReader {
			a.recordPoemReadAsync(bookID, poems[idx], poems)
		}
		a.requestRepaint()
	}()
}

func (a *AppUI) downloadBookAsync(bookID, poemID string, highlightRanges []app.LineRange) {
	a.mu.Lock()
	if a.downloadingBook == bookID {
		a.mu.Unlock()
		return
	}
	a.downloadingBook = bookID
	a.readerError = ""
	a.mu.Unlock()
	a.requestRepaint()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		err := a.svc.EnsureBookContent(ctx, bookID)
		a.mu.Lock()
		a.downloadingBook = ""
		if err != nil {
			a.readerError = err.Error()
		}
		a.mu.Unlock()
		a.reloadSync(ctx)
		if err == nil {
			a.openBook(bookID, poemID, highlightRanges, a.readerMobile)
		} else {
			a.requestRepaint()
		}
	}()
}

func (a *AppUI) recordPoemReadAsync(bookID string, poem domain.Poem, allPoems []domain.Poem) {
	if !poem.IsPoem() {
		return
	}
	totalPoems := 0
	for _, p := range allPoems {
		if p.IsPoem() {
			totalPoems++
		}
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = a.svc.RecordPoemRead(ctx, bookID, poem.ID, 0, totalPoems)
	}()
}

func (a *AppUI) triggerImportEPUB() {
	if a.pickFile == nil {
		a.showTransientToast("当前环境未注册文件选择器")
		return
	}
	a.showAddModal = false
	groupID := a.selectedGrp
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
		a.reloadSync(ctx)
		a.openBook(res.Book.ID, "", nil, false)
		a.showTransientToast(fmt.Sprintf("已导入《%s》(%d 首)", res.Book.Title, res.Book.PoemCount))
	}()
}

func (a *AppUI) triggerImportFont() {
	if a.pickFile == nil {
		a.showTransientToast("当前环境未注册文件选择器")
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
		st, _ := a.svc.LoadSettings(ctx)
		st.FontFamily = lf.Family
		st.WidgetTheme.FontFamily = lf.Family
		st.WidgetTheme.FontFile = lf.Filename
		_ = a.svc.SaveSettings(ctx, st)
		a.reloadSync(ctx)
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
