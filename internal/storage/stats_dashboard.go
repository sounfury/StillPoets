package storage

import (
	"context"
	"slices"
	"strings"
	"time"

	"stillpoets/internal/domain"
)

// GetStatsDashboard 一次性聚合统计看板的全部数据（窗口总览、完读墙、热力图、时段节律、诗人云）。
func (s *Store) GetStatsDashboard(ctx context.Context, query StatsQuery) (StatsDashboard, error) {
	period := normalizeStatsPeriod(query.Period)
	authorMode := normalizeAuthorMode(query.AuthorMode)
	now := query.Now
	if now.IsZero() {
		now = time.Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.loadStatsDashboardLocked(ctx)
	if err != nil {
		return StatsDashboard{}, err
	}
	startDate, endDate, startMs, endMs, bounded := statsWindowBounds(period, now)
	window := statsDashboardWindow{startDate, endDate, startMs, endMs, bounded}
	activity := collectStatsWindow(data, window)
	shelf := buildStatsDashboardShelf(data, activity, window)
	overview := WindowOverview{
		BookCount:          len(data.books),
		OfflineBookCount:   data.offlineCount,
		FinishedBookCount:  len(shelf.finishedBooks),
		TotalFinishedCount: shelf.finishedAllCount,
		ReadPoemCount:      countDashboardReadPoems(activity.readPairs, shelf.historicalReadPairs, bounded),
		ReadEventCount:     len(activity.events),
		FavoriteCount:      len(activity.favorites),
		QuoteCount:         len(activity.quotes),
		ActiveDays:         len(activity.activeDates),
	}

	// 2. 热力图（全历史事件驱动，支持切换年份或近 52 周滚动）
	heatmap := buildHeatmap(data.events, query.Year, now)

	// 3. 阅读时段占比（跟随时间窗口）
	rhythm := buildReadingRhythm(activity.events)

	// 4. 诗人作者云（综合入口 A 读诗/精选 + 入口 B 散页；全部窗口回填历史已读）
	authors := buildAuthorCloud(authorCloudInput{
		windowEvents:        activity.events,
		historicalReadPairs: shelf.historicalReadPairs,
		includeHistorical:   !bounded,
		windowFavorites:     activity.favorites,
		windowQuotes:        activity.quotes,
		bookMap:             data.bookMap,
		poemAuthors:         data.poemAuthors,
		mode:                authorMode,
	})

	return StatsDashboard{
		Period:        period,
		AuthorMode:    authorMode,
		Overview:      overview,
		FinishedBooks: shelf.finishedBooks,
		ReadingBooks:  shelf.readingBooks,
		Heatmap:       heatmap,
		Rhythm:        rhythm,
		Authors:       authors,
	}, nil
}

type statsCollections struct {
	progress  []domain.ReadingProgress
	favorites []domain.FavoritePoem
	quotes    []domain.Quote
}

func (s *Store) loadStatsCollectionsLocked(ctx context.Context) (statsCollections, error) {
	progressList, err := selectRows(ctx, s, decodeLocalProgress, localProgressSelect+` ORDER BY book_id`)
	if err != nil {
		return statsCollections{}, err
	}

	favorites, err := selectRows(ctx, s, decodeFavorite,
		`SELECT `+favoriteColumns+` FROM favorite_poems WHERE `+activeFavorite+` ORDER BY created_at DESC, book_id, poem_id`)
	if err != nil {
		return statsCollections{}, err
	}

	quotes, err := selectRows(ctx, s, decodeQuote, `SELECT `+quoteColumns+` FROM quotes WHERE deleted = 0 ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return statsCollections{}, err
	}

	return statsCollections{progress: progressList, favorites: favorites, quotes: quotes}, nil
}

type statsDashboardData struct {
	books        []domain.Book
	bookMap      map[string]domain.Book
	offlineCount int
	collections  statsCollections
	events       []domain.ReadingEvent
	poemAuthors  map[poemPair]string
}

func (s *Store) loadStatsDashboardLocked(ctx context.Context) (statsDashboardData, error) {
	books, err := s.listActiveBooksLocked(ctx)
	if err != nil {
		return statsDashboardData{}, err
	}
	bookMap := make(map[string]domain.Book, len(books))
	offlineCount := 0
	for _, b := range books {
		bookMap[b.ID] = b
		if b.LocalState == domain.LocalStateDownloaded {
			offlineCount++
		}
	}

	collections, err := s.loadStatsCollectionsLocked(ctx)
	if err != nil {
		return statsDashboardData{}, err
	}
	rawEvents, err := selectRows(ctx, s, decodeReadingEvent,
		`SELECT `+readingEventColumns+` FROM reading_events ORDER BY read_date ASC, read_at ASC, id ASC`)
	if err != nil {
		return statsDashboardData{}, err
	}
	// 仅统计当前未删除诗集的读诗事件
	events := make([]domain.ReadingEvent, 0, len(rawEvents))
	for _, ev := range rawEvents {
		if _, ok := bookMap[ev.BookID]; ok {
			events = append(events, ev)
		}
	}

	poemAuthors, err := s.cachedPoemAuthorsLocked(ctx)
	if err != nil {
		return statsDashboardData{}, err
	}

	return statsDashboardData{
		books: books, bookMap: bookMap, offlineCount: offlineCount,
		collections: collections, events: events, poemAuthors: poemAuthors,
	}, nil
}

type statsDashboardWindow struct {
	startDate, endDate string
	startMs, endMs     int64
	bounded            bool
}

func (w statsDashboardWindow) containsTime(ts int64) bool {
	return !w.bounded || (ts >= w.startMs && ts <= w.endMs)
}

type statsWindowActivity struct {
	events           []domain.ReadingEvent
	readPairs        map[poemPair]struct{}
	activeDates      map[string]struct{}
	favorites        []domain.FavoritePoem
	favsByBook       map[string][]domain.FavoritePoem
	quotes           []domain.Quote
	quoteCountByBook map[string]int
}

func collectStatsWindow(data statsDashboardData, w statsDashboardWindow) statsWindowActivity {
	// 过滤当前时间窗口内的事件、精选与散页
	windowEvents := make([]domain.ReadingEvent, 0, len(data.events))
	windowReadPairs := make(map[poemPair]struct{})
	windowActiveDates := make(map[string]struct{})
	for _, ev := range data.events {
		if w.bounded && (ev.ReadDate < w.startDate || ev.ReadDate > w.endDate) {
			continue
		}
		windowEvents = append(windowEvents, ev)
		windowReadPairs[poemPair{bookID: ev.BookID, poemID: ev.PoemID}] = struct{}{}
		windowActiveDates[ev.ReadDate] = struct{}{}
	}

	windowFavorites := make([]domain.FavoritePoem, 0, len(data.collections.favorites))
	favsByBook := make(map[string][]domain.FavoritePoem)
	for _, f := range data.collections.favorites {
		favsByBook[f.BookID] = append(favsByBook[f.BookID], f)
		if w.containsTime(f.CreatedAt) {
			windowFavorites = append(windowFavorites, f)
		}
	}

	windowQuotes := make([]domain.Quote, 0, len(data.collections.quotes))
	quoteCountByBook := make(map[string]int)
	for _, q := range data.collections.quotes {
		if q.BookID != "" {
			if _, ok := data.bookMap[q.BookID]; ok {
				quoteCountByBook[q.BookID]++
			}
		}
		if w.containsTime(q.CreatedAt) {
			windowQuotes = append(windowQuotes, q)
		}
	}

	return statsWindowActivity{
		events:           windowEvents,
		readPairs:        windowReadPairs,
		activeDates:      windowActiveDates,
		favorites:        windowFavorites,
		favsByBook:       favsByBook,
		quotes:           windowQuotes,
		quoteCountByBook: quoteCountByBook,
	}
}

type statsDashboardShelf struct {
	finishedAllCount    int
	finishedBooks       []FinishedBookItem
	readingBooks        []ReadingBookItem
	historicalReadPairs map[poemPair]struct{}
}

func buildStatsDashboardShelf(data statsDashboardData, activity statsWindowActivity, w statsDashboardWindow) statsDashboardShelf {
	progressMap := make(map[string]domain.ReadingProgress, len(data.collections.progress))
	for _, p := range data.collections.progress {
		progressMap[p.BookID] = p
	}

	// 1. 完读墙 & 在读书架进度
	finishedAllCount := 0
	var finishedBooks []FinishedBookItem
	var readingBooks []ReadingBookItem
	historicalReadPairs := make(map[poemPair]struct{})

	for _, b := range data.books {
		prog := progressMap[b.ID]
		for _, pid := range prog.ReadPoemIDs {
			historicalReadPairs[poemPair{bookID: b.ID, poemID: pid}] = struct{}{}
		}
		bookFavs := activity.favsByBook[b.ID]
		if prog.Status != domain.ReadStatusFinished {
			readingBooks = append(readingBooks, ReadingBookItem{
				Book:          b,
				Progress:      prog,
				FavoriteCount: len(bookFavs),
			})
			continue
		}

		finishedAllCount++
		if !w.containsTime(prog.FinishedAt) {
			continue
		}
		topFavs := bookFavs
		if len(topFavs) > 6 {
			topFavs = topFavs[:6]
		}
		finishedBooks = append(finishedBooks, FinishedBookItem{
			Book:          b,
			Progress:      prog,
			FavoriteCount: len(bookFavs),
			QuoteCount:    activity.quoteCountByBook[b.ID],
			TopFavorites:  topFavs,
		})
	}

	slices.SortFunc(finishedBooks, compareFinishedDashboardBooks)
	slices.SortFunc(readingBooks, compareReadingDashboardBooks)
	return statsDashboardShelf{
		finishedAllCount:    finishedAllCount,
		finishedBooks:       finishedBooks,
		readingBooks:        readingBooks,
		historicalReadPairs: historicalReadPairs,
	}
}

func compareFinishedDashboardBooks(a, b FinishedBookItem) int {
	if a.Progress.FinishedAt != b.Progress.FinishedAt {
		if a.Progress.FinishedAt > b.Progress.FinishedAt {
			return -1
		}
		return 1
	}
	return strings.Compare(a.Book.Title, b.Book.Title)
}

func compareReadingDashboardBooks(a, b ReadingBookItem) int {
	ra, rb := a.Progress.ReadPoemCount(), b.Progress.ReadPoemCount()
	if (ra > 0) != (rb > 0) {
		if ra > 0 {
			return -1
		}
		return 1
	}
	if cmp := b.Progress.PositionVer.Compare(a.Progress.PositionVer); cmp != 0 {
		return cmp
	}
	if a.Book.SortOrder != b.Book.SortOrder {
		return a.Book.SortOrder - b.Book.SortOrder
	}
	return strings.Compare(a.Book.Title, b.Book.Title)
}

// 全部窗口下兼容 V1.0 历史已读记录（reading_progress.read_poem_ids）。
func countDashboardReadPoems(windowReadPairs, historicalReadPairs map[poemPair]struct{}, bounded bool) int {
	count := len(windowReadPairs)
	if !bounded {
		for pair := range historicalReadPairs {
			if _, seen := windowReadPairs[pair]; !seen {
				count++
			}
		}
	}
	return count
}

// GetAuthorProfile 聚合点击诗人云后展示的诗人个人档案卡。
func (s *Store) GetAuthorProfile(ctx context.Context, author string) (AuthorProfile, error) {
	target := normalizePoetName(author)
	if target == "" {
		return AuthorProfile{}, ErrNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	books, err := s.listActiveBooksLocked(ctx)
	if err != nil {
		return AuthorProfile{}, err
	}
	bookMap := make(map[string]domain.Book, len(books))
	for _, b := range books {
		bookMap[b.ID] = b
	}

	collections, err := s.loadStatsCollectionsLocked(ctx)
	if err != nil {
		return AuthorProfile{}, err
	}
	events, err := selectRows(ctx, s, decodeReadingEvent,
		`SELECT `+readingEventColumns+` FROM reading_events ORDER BY read_at DESC, id DESC`)
	if err != nil {
		return AuthorProfile{}, err
	}
	poemAuthors, err := s.cachedPoemAuthorsLocked(ctx)
	if err != nil {
		return AuthorProfile{}, err
	}
	favPairs, err := s.activeFavoritePairsLocked(ctx)
	if err != nil {
		return AuthorProfile{}, err
	}

	profile := authorProfileBuilder{
		profile:     AuthorProfile{Author: target},
		bookMap:     bookMap,
		poemAuthors: poemAuthors,
		readPairs:   make(map[poemPair]struct{}),
		bookStats:   make(map[string]*AuthorBookEntry),
	}
	// 直接署名为该诗人的诗集
	for _, b := range books {
		if normalizePoetName(b.Author) == target {
			profile.ensureBook(b)
		}
	}
	profile.addProgress(collections.progress)
	profile.addEvents(events, favPairs)
	profile.addFavorites(collections.favorites)
	profile.addQuotes(collections.quotes)
	return profile.finish(), nil
}

type authorProfileBuilder struct {
	profile     AuthorProfile
	bookMap     map[string]domain.Book
	poemAuthors map[poemPair]string
	readPairs   map[poemPair]struct{}
	bookStats   map[string]*AuthorBookEntry
}

func (a *authorProfileBuilder) ensureBook(book domain.Book) *AuthorBookEntry {
	entry := a.bookStats[book.ID]
	if entry == nil {
		entry = &AuthorBookEntry{Book: book}
		a.bookStats[book.ID] = entry
	}
	return entry
}

func (a *authorProfileBuilder) recordRead(book domain.Book, pair poemPair) {
	if _, seen := a.readPairs[pair]; !seen {
		a.readPairs[pair] = struct{}{}
		a.ensureBook(book).ReadCount++
	}
}

// 历史已读集合包含合集中单首诗作者为该诗人的记录。
func (a *authorProfileBuilder) addProgress(progress []domain.ReadingProgress) {
	for _, p := range progress {
		b, ok := a.bookMap[p.BookID]
		if !ok {
			continue
		}
		for _, pid := range p.ReadPoemIDs {
			pair := poemPair{bookID: p.BookID, poemID: pid}
			if resolvePoemAuthor("", a.poemAuthors[pair], b.Author) == a.profile.Author {
				a.recordRead(b, pair)
			}
		}
	}
}

func (a *authorProfileBuilder) addEvents(events []domain.ReadingEvent, favPairs map[poemPair]bool) {
	// 读诗事件（倒序）
	for _, ev := range events {
		b, ok := a.bookMap[ev.BookID]
		if !ok {
			continue
		}
		poet := resolvePoemAuthor(ev.Author, a.poemAuthors[poemPair{bookID: ev.BookID, poemID: ev.PoemID}], b.Author)
		if poet != a.profile.Author {
			continue
		}
		pair := poemPair{bookID: ev.BookID, poemID: ev.PoemID}
		a.recordRead(b, pair)
		if len(a.profile.RecentReads) < 15 {
			ev.Author = a.profile.Author
			a.profile.RecentReads = append(a.profile.RecentReads, ReadingEventItem{
				Event:          ev,
				BookTitle:      b.Title,
				BookLocalState: b.LocalState,
				IsFavorite:     favPairs[pair],
			})
		}
	}

}

func (a *authorProfileBuilder) addFavorites(favorites []domain.FavoritePoem) {
	// 精选诗篇 ❤️
	for _, f := range favorites {
		b, ok := a.bookMap[f.BookID]
		if !ok {
			continue
		}
		poet := resolvePoemAuthor(f.Author, a.poemAuthors[poemPair{bookID: f.BookID, poemID: f.PoemID}], b.Author)
		if poet != a.profile.Author {
			continue
		}
		a.ensureBook(b).FavoriteCount++
		a.profile.Favorites = append(a.profile.Favorites, AuthorPoemEntry{
			BookID:         f.BookID,
			BookTitle:      b.Title,
			BookLocalState: b.LocalState,
			PoemID:         f.PoemID,
			PoemTitle:      f.PoemTitle,
			Excerpt:        f.Excerpt,
			HighlightLines: f.HighlightLines,
		})
	}

}

func (a *authorProfileBuilder) addQuotes(quotes []domain.Quote) {
	// 散页（诗歌节选 + 独立散句）
	for _, q := range quotes {
		if normalizePoetName(q.Author) != a.profile.Author {
			continue
		}
		_, bookActive := a.bookMap[q.BookID]
		a.profile.Quotes = append(a.profile.Quotes, QuoteView{
			Quote:         q,
			EffectiveKind: q.EffectiveKind(bookActive),
			BookActive:    bookActive,
		})
	}

}

func (a *authorProfileBuilder) finish() AuthorProfile {
	bookEntries := make([]AuthorBookEntry, 0, len(a.bookStats))
	for _, e := range a.bookStats {
		bookEntries = append(bookEntries, *e)
	}
	slices.SortFunc(bookEntries, func(a, b AuthorBookEntry) int {
		if a.ReadCount != b.ReadCount {
			return b.ReadCount - a.ReadCount
		}
		if a.FavoriteCount != b.FavoriteCount {
			return b.FavoriteCount - a.FavoriteCount
		}
		return strings.Compare(a.Book.Title, b.Book.Title)
	})

	a.profile.Books = bookEntries
	a.profile.ReadPoemCount = len(a.readPairs)
	a.profile.BookCount = len(bookEntries)
	a.profile.FavoriteCount = len(a.profile.Favorites)
	a.profile.QuoteCount = len(a.profile.Quotes)
	return a.profile
}

func normalizeStatsPeriod(p StatsPeriod) StatsPeriod {
	switch p {
	case StatsPeriodWeek, StatsPeriodMonth, StatsPeriodYear, StatsPeriodAll:
		return p
	default:
		return StatsPeriodAll
	}
}

func normalizeAuthorMode(m AuthorWeightMode) AuthorWeightMode {
	switch m {
	case AuthorWeightCombined, AuthorWeightRead, AuthorWeightCollection:
		return m
	default:
		return AuthorWeightCombined
	}
}

func statsWindowBounds(period StatsPeriod, now time.Time) (startDate, endDate string, startMs, endMs int64, bounded bool) {
	if period == StatsPeriodAll {
		return "", "", 0, 0, false
	}
	loc := now.Location()
	y, m, d := now.Date()
	endDay := time.Date(y, m, d, 23, 59, 59, int(time.Second-time.Millisecond), loc)
	var startDay time.Time
	switch period {
	case StatsPeriodWeek:
		// 按周一为一周起始
		offset := (int(now.Weekday()) + 6) % 7
		startDay = time.Date(y, m, d-offset, 0, 0, 0, 0, loc)
	case StatsPeriodMonth:
		startDay = time.Date(y, m, 1, 0, 0, 0, 0, loc)
	case StatsPeriodYear:
		startDay = time.Date(y, 1, 1, 0, 0, 0, 0, loc)
	}
	return startDay.Format("2006-01-02"), endDay.Format("2006-01-02"), startDay.UTC().UnixMilli(), endDay.UTC().UnixMilli(), true
}
