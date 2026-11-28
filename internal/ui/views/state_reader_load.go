package views

import (
	"context"
	"time"

	"stillpoets/internal/app"
	"stillpoets/internal/domain"
	"stillpoets/internal/storage"
	"stillpoets/internal/ui/theme"
	"stillpoets/modules/widgetbridge"
)

func (a *AppUI) reloadSync(ctx context.Context) {
	// 上游网络同步超时后，仍需用有效上下文刷新本地 SQLite。
	if ctx == nil || ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
	}

	shelf := a.loadShelfRefresh(ctx)
	selection := a.captureRefreshSelection()
	stats := a.loadStatsRefresh(ctx, selection)
	local := a.loadLocalRefresh(ctx)
	reader := a.loadReaderRefresh(ctx, selection.bookID)

	a.dispatchUI(func() {
		a.applyShelfRefresh(shelf)
		matches := a.matchRefreshSelection(selection)
		a.applyStatsRefresh(stats, matches)
		a.applyLocalRefresh(local)
		if shelf.booksErr == nil && a.closeDeletedReader() {
			return
		}
		if selection.bookID != "" && matches.reader {
			a.applyReaderRefresh(reader)
		}
	})
}

type shelfRefresh struct {
	settings     domain.Settings
	settingsErr  error
	groups       []domain.BookGroup
	groupsErr    error
	books        []domain.Book
	booksErr     error
	progress     map[string]domain.ReadingProgress
	progressErr  error
	continueItem *storage.ContinueReadingItem
	continueErr  error
	today        *app.TodayReread
	todayErr     error
}

func (a *AppUI) loadShelfRefresh(ctx context.Context) (s shelfRefresh) {
	s.settings, s.settingsErr = a.svc.LoadSettings(ctx)
	if s.settingsErr == nil {
		if s.settings.FontSize <= 0 {
			s.settings.FontSize = 18
		}
		if s.settings.LineHeight <= 0 {
			s.settings.LineHeight = 1.85
		}
	}
	s.groups, s.groupsErr = a.svc.ListGroups(ctx)
	s.books, s.booksErr = a.svc.ListBooks(ctx, "")
	progress, err := a.svc.ListProgress(ctx)
	s.progressErr = err
	if err == nil {
		s.progress = make(map[string]domain.ReadingProgress, len(progress))
		for _, p := range progress {
			s.progress[p.BookID] = p
		}
	}
	s.continueItem, s.continueErr = a.svc.GetContinueReading(ctx)
	s.today, s.todayErr = a.svc.PickTodayReread(ctx)
	return s
}

func (a *AppUI) applyShelfRefresh(s shelfRefresh) {
	if s.settingsErr == nil {
		a.settings = s.settings
		a.fontSizeSlider = float64(s.settings.FontSize)
		a.lineHeightSlider = float64(s.settings.LineHeight)
	} else if a.settings.FontSize <= 0 {
		a.settings = domain.Settings{ThemePreset: string(theme.PresetArtbook), FontSize: 18, LineHeight: 1.85}
		a.fontSizeSlider = 18
		a.lineHeightSlider = 1.85
	}
	a.syncNativeAppearance(a.palette())
	if s.groupsErr == nil {
		a.groups = s.groups
	}
	if s.booksErr == nil {
		a.books = s.books
	}
	if s.progressErr == nil {
		a.progressMap = s.progress
	}
	if s.continueErr == nil {
		a.continueItem = s.continueItem
	}
	if s.todayErr == nil {
		a.todayReread = s.today
	}
}

type refreshSelection struct {
	quoteFilter storage.QuoteFilter
	statsQuery  storage.StatsQuery
	bookID      string
	date        string
	openSeq     uint64
	quoteSeq    uint64
	statsSeq    uint64
	daySeq      uint64
}

func (a *AppUI) captureRefreshSelection() refreshSelection {
	a.mu.Lock()
	defer a.mu.Unlock()
	return refreshSelection{
		quoteFilter: storage.QuoteFilter{Kind: a.quoteKind, SourceBook: a.quoteBook, Author: a.quoteAuthor},
		statsQuery:  storage.StatsQuery{Period: a.statsPeriod, Year: a.statsHeatmapYear, AuthorMode: a.statsAuthorMode},
		bookID:      a.activeBookID,
		date:        a.statsSelectedDate,
		openSeq:     a.openSeq,
		quoteSeq:    a.quoteFilterSeq,
		statsSeq:    a.statsSeq,
		daySeq:      a.statsDaySeq,
	}
}

type refreshMatches struct {
	quote, reader, stats, day bool
}

func (a *AppUI) matchRefreshSelection(s refreshSelection) refreshMatches {
	a.mu.Lock()
	defer a.mu.Unlock()
	return refreshMatches{
		quote:  a.quoteFilterSeq == s.quoteSeq,
		reader: a.openSeq == s.openSeq && a.activeBookID == s.bookID,
		stats:  a.statsSeq == s.statsSeq,
		day:    a.statsDaySeq == s.daySeq,
	}
}

type statsRefresh struct {
	quotes    []storage.QuoteView
	quotesErr error
	facets    storage.QuoteFacets
	facetsErr error
	overview  storage.StatsOverview
	statsErr  error
	dashboard storage.StatsDashboard
	dashErr   error
	date      string
	dayEvents []storage.ReadingEventItem
	dayErr    error
}

func (a *AppUI) loadStatsRefresh(ctx context.Context, selection refreshSelection) (s statsRefresh) {
	s.quotes, s.quotesErr = a.svc.ListQuotes(ctx, selection.quoteFilter)
	s.facets, s.facetsErr = a.svc.ListQuoteFacets(ctx)
	s.overview, s.statsErr = a.svc.GetStatsOverview(ctx)
	s.dashboard, s.dashErr = a.svc.GetStatsDashboard(ctx, selection.statsQuery)
	s.date = selection.date
	if s.date == "" && s.dashErr == nil {
		s.date = s.dashboard.Heatmap.LatestActiveDate
	}
	if s.date != "" {
		s.dayEvents, s.dayErr = a.svc.ListReadingEventsByDate(ctx, s.date)
	}
	return s
}

func (a *AppUI) applyStatsRefresh(s statsRefresh, matches refreshMatches) {
	if matches.quote && s.quotesErr == nil {
		a.quotes = s.quotes
	}
	if matches.quote && s.facetsErr == nil {
		a.quoteFacets = s.facets
	}
	if s.statsErr == nil {
		a.stats = s.overview
	}
	if matches.stats && s.dashErr == nil {
		a.statsDashboard = s.dashboard
		a.refreshFinishedBookDetail(s.dashboard.FinishedBooks)
	}
	if matches.day && s.dayErr == nil && s.date != "" {
		if a.statsSelectedDate == "" {
			a.statsSelectedDate = s.date
		}
		a.statsDayEvents = s.dayEvents
		a.statsDayLoading = false
	}
}

func (a *AppUI) refreshFinishedBookDetail(books []storage.FinishedBookItem) {
	if a.detailFinishedBook == nil {
		return
	}
	for _, item := range books {
		if item.Book.ID == a.detailFinishedBook.Book.ID {
			a.detailFinishedBook = &item
			return
		}
	}
	a.showFinishedModal = false
	a.detailFinishedBook = nil
}

type localRefresh struct {
	syncOverview   app.SyncOverview
	syncErr        error
	fonts          []app.LocalFont
	cacheSize      int64
	diagSize       int64
	widgetTimeline domain.WidgetTimeline
	widgetErr      error
	activeWidgets  int
	hasSecret      bool
}

func (a *AppUI) loadLocalRefresh(ctx context.Context) (s localRefresh) {
	s.syncOverview, s.syncErr = a.svc.GetSyncOverview(ctx)
	s.fonts = a.svc.ListLocalFonts()
	s.cacheSize = a.svc.CacheSizeBytes()
	s.diagSize = a.svc.DiagnosticLogSizeBytes()
	s.widgetTimeline, s.widgetErr = a.svc.ReadWidgetTimeline()
	s.activeWidgets = widgetbridge.ActiveWidgetCount()
	s.hasSecret = a.svc.HasWebDAVPassword()
	return s
}

func (a *AppUI) applyLocalRefresh(s localRefresh) {
	if s.syncErr == nil {
		a.syncOverview = s.syncOverview
	}
	a.localFonts = s.fonts
	a.cacheSizeBytes = s.cacheSize
	a.diagLogSizeBytes = s.diagSize
	a.activeWidgetCount = s.activeWidgets
	if s.widgetErr == nil {
		a.widgetPreview = &s.widgetTimeline
		a.widgetCount = len(s.widgetTimeline.Entries)
	}
	a.davHasSecret = s.hasSecret
}

type readerRefresh struct {
	poems        []domain.Poem
	poemsErr     error
	favorites    map[string]domain.FavoritePoem
	favoritesErr error
}

func (a *AppUI) loadReaderRefresh(ctx context.Context, bookID string) (s readerRefresh) {
	if bookID == "" {
		return s
	}
	s.poems, s.poemsErr = a.svc.ListPoems(ctx, bookID)
	favorites, err := a.svc.ListFavorites(ctx, bookID)
	s.favoritesErr = err
	if err == nil {
		s.favorites = indexReaderFavorites(favorites)
	}
	return s
}

func indexReaderFavorites(favorites []domain.FavoritePoem) map[string]domain.FavoritePoem {
	indexed := make(map[string]domain.FavoritePoem, len(favorites))
	for _, f := range favorites {
		indexed[f.PoemID] = f
	}
	return indexed
}

func (a *AppUI) applyReaderRefresh(s readerRefresh) {
	if s.poemsErr == nil {
		a.activePoems = s.poems
		if a.activePoemIdx >= len(s.poems) {
			a.activePoemIdx = max(0, len(s.poems)-1)
		}
	}
	if s.favoritesErr == nil {
		a.activeFavorites = s.favorites
	}
}

// 若当前在读书已被其他设备删除，退出全屏阅读层，避免滞留无返回按钮的空态。
func (a *AppUI) closeDeletedReader() bool {
	if a.activeBookID == "" {
		return false
	}
	if _, exists := a.activeBook(); exists {
		return false
	}
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
	return true
}
