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

	books, err := s.listActiveBooksLocked(ctx)
	if err != nil {
		return StatsDashboard{}, err
	}
	bookMap := make(map[string]domain.Book, len(books))
	offlineCount := 0
	for _, b := range books {
		bookMap[b.ID] = b
		if b.LocalState == domain.LocalStateDownloaded {
			offlineCount++
		}
	}

	progressList, err := selectRows(ctx, s, decodeLocalProgress, localProgressSelect+` ORDER BY book_id`)
	if err != nil {
		return StatsDashboard{}, err
	}
	progressMap := make(map[string]domain.ReadingProgress, len(progressList))
	for _, p := range progressList {
		progressMap[p.BookID] = p
	}

	favorites, err := selectRows(ctx, s, decodeFavorite,
		`SELECT `+favoriteColumns+` FROM favorite_poems WHERE `+activeFavorite+` ORDER BY created_at DESC, book_id, poem_id`)
	if err != nil {
		return StatsDashboard{}, err
	}

	quotes, err := selectRows(ctx, s, decodeQuote, `SELECT `+quoteColumns+` FROM quotes WHERE deleted = 0 ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return StatsDashboard{}, err
	}

	rawEvents, err := selectRows(ctx, s, decodeReadingEvent,
		`SELECT `+readingEventColumns+` FROM reading_events ORDER BY read_date ASC, read_at ASC, id ASC`)
	if err != nil {
		return StatsDashboard{}, err
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
		return StatsDashboard{}, err
	}

	startDate, endDate, startMs, endMs, bounded := statsWindowBounds(period, now)

	// 过滤当前时间窗口内的事件、精选与散页
	windowEvents := make([]domain.ReadingEvent, 0, len(events))
	windowReadPairs := make(map[poemPair]struct{})
	windowActiveDates := make(map[string]struct{})
	for _, ev := range events {
		if bounded && (ev.ReadDate < startDate || ev.ReadDate > endDate) {
			continue
		}
		windowEvents = append(windowEvents, ev)
		windowReadPairs[poemPair{bookID: ev.BookID, poemID: ev.PoemID}] = struct{}{}
		windowActiveDates[ev.ReadDate] = struct{}{}
	}

	windowFavorites := make([]domain.FavoritePoem, 0, len(favorites))
	favsByBook := make(map[string][]domain.FavoritePoem)
	for _, f := range favorites {
		favsByBook[f.BookID] = append(favsByBook[f.BookID], f)
		if !bounded || (f.CreatedAt >= startMs && f.CreatedAt <= endMs) {
			windowFavorites = append(windowFavorites, f)
		}
	}

	windowQuotes := make([]domain.Quote, 0, len(quotes))
	quoteCountByBook := make(map[string]int)
	for _, q := range quotes {
		if q.BookID != "" {
			if _, ok := bookMap[q.BookID]; ok {
				quoteCountByBook[q.BookID]++
			}
		}
		if !bounded || (q.CreatedAt >= startMs && q.CreatedAt <= endMs) {
			windowQuotes = append(windowQuotes, q)
		}
	}

	// 1. 完读墙 & 在读书架进度
	finishedAllCount := 0
	var finishedBooks []FinishedBookItem
	var readingBooks []ReadingBookItem
	historicalReadPairs := make(map[poemPair]struct{})

	for _, b := range books {
		prog := progressMap[b.ID]
		for _, pid := range prog.ReadPoemIDs {
			historicalReadPairs[poemPair{bookID: b.ID, poemID: pid}] = struct{}{}
		}
		bookFavs := favsByBook[b.ID]
		if prog.Status == domain.ReadStatusFinished {
			finishedAllCount++
			inWindow := !bounded || (prog.FinishedAt >= startMs && prog.FinishedAt <= endMs)
			if inWindow {
				topFavs := bookFavs
				if len(topFavs) > 6 {
					topFavs = topFavs[:6]
				}
				finishedBooks = append(finishedBooks, FinishedBookItem{
					Book:          b,
					Progress:      prog,
					FavoriteCount: len(bookFavs),
					QuoteCount:    quoteCountByBook[b.ID],
					TopFavorites:  topFavs,
				})
			}
		} else {
			readingBooks = append(readingBooks, ReadingBookItem{
				Book:          b,
				Progress:      prog,
				FavoriteCount: len(bookFavs),
			})
		}
	}

	slices.SortFunc(finishedBooks, func(a, b FinishedBookItem) int {
		if a.Progress.FinishedAt != b.Progress.FinishedAt {
			if a.Progress.FinishedAt > b.Progress.FinishedAt {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Book.Title, b.Book.Title)
	})
	slices.SortFunc(readingBooks, func(a, b ReadingBookItem) int {
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
	})

	// 全部窗口下兼容 V1.0 历史已读记录（reading_progress.read_poem_ids）
	readPoemCount := len(windowReadPairs)
	if !bounded {
		unionRead := make(map[poemPair]struct{}, len(historicalReadPairs)+len(windowReadPairs))
		for k := range historicalReadPairs {
			unionRead[k] = struct{}{}
		}
		for k := range windowReadPairs {
			unionRead[k] = struct{}{}
		}
		readPoemCount = len(unionRead)
	}

	overview := WindowOverview{
		BookCount:          len(books),
		OfflineBookCount:   offlineCount,
		FinishedBookCount:  len(finishedBooks),
		TotalFinishedCount: finishedAllCount,
		ReadPoemCount:      readPoemCount,
		ReadEventCount:     len(windowEvents),
		FavoriteCount:      len(windowFavorites),
		QuoteCount:         len(windowQuotes),
		ActiveDays:         len(windowActiveDates),
	}

	// 2. 热力图（全历史事件驱动，支持切换年份或近 52 周滚动）
	heatmap := buildHeatmap(events, query.Year, now)

	// 3. 阅读时段占比（跟随时间窗口）
	rhythm := buildReadingRhythm(windowEvents)

	// 4. 诗人作者云（综合入口 A 读诗/精选 + 入口 B 散页；全部窗口回填历史已读）
	authors := buildAuthorCloud(authorCloudInput{
		windowEvents:        windowEvents,
		historicalReadPairs: historicalReadPairs,
		includeHistorical:   !bounded,
		windowFavorites:     windowFavorites,
		windowQuotes:        windowQuotes,
		bookMap:             bookMap,
		poemAuthors:         poemAuthors,
		mode:                authorMode,
	})

	return StatsDashboard{
		Period:        period,
		AuthorMode:    authorMode,
		Overview:      overview,
		FinishedBooks: finishedBooks,
		ReadingBooks:  readingBooks,
		Heatmap:       heatmap,
		Rhythm:        rhythm,
		Authors:       authors,
	}, nil
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

	progressList, err := selectRows(ctx, s, decodeLocalProgress, localProgressSelect+` ORDER BY book_id`)
	if err != nil {
		return AuthorProfile{}, err
	}
	favorites, err := selectRows(ctx, s, decodeFavorite,
		`SELECT `+favoriteColumns+` FROM favorite_poems WHERE `+activeFavorite+` ORDER BY created_at DESC, book_id, poem_id`)
	if err != nil {
		return AuthorProfile{}, err
	}
	quotes, err := selectRows(ctx, s, decodeQuote, `SELECT `+quoteColumns+` FROM quotes WHERE deleted = 0 ORDER BY created_at DESC, id DESC`)
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

	profile := AuthorProfile{Author: target}
	readPairs := make(map[poemPair]struct{})
	bookStats := make(map[string]*AuthorBookEntry)
	ensureBook := func(bookID string) *AuthorBookEntry {
		b, ok := bookMap[bookID]
		if !ok {
			return nil
		}
		e := bookStats[bookID]
		if e == nil {
			e = &AuthorBookEntry{Book: b}
			bookStats[bookID] = e
		}
		return e
	}

	// 直接署名为该诗人的诗集
	for _, b := range books {
		if normalizePoetName(b.Author) == target {
			ensureBook(b.ID)
		}
	}

	// 历史已读集合（含合集中单首诗作者为该诗人的记录）
	for _, p := range progressList {
		b, ok := bookMap[p.BookID]
		if !ok {
			continue
		}
		for _, pid := range p.ReadPoemIDs {
			poet := resolvePoemAuthor("", poemAuthors[poemPair{bookID: p.BookID, poemID: pid}], b.Author)
			if poet != target {
				continue
			}
			pair := poemPair{bookID: p.BookID, poemID: pid}
			if _, seen := readPairs[pair]; seen {
				continue
			}
			readPairs[pair] = struct{}{}
			if entry := ensureBook(p.BookID); entry != nil {
				entry.ReadCount++
			}
		}
	}

	// 读诗事件（倒序）
	for _, ev := range events {
		b, ok := bookMap[ev.BookID]
		if !ok {
			continue
		}
		poet := resolvePoemAuthor(ev.Author, poemAuthors[poemPair{bookID: ev.BookID, poemID: ev.PoemID}], b.Author)
		if poet != target {
			continue
		}
		pair := poemPair{bookID: ev.BookID, poemID: ev.PoemID}
		if _, seen := readPairs[pair]; !seen {
			readPairs[pair] = struct{}{}
			if entry := ensureBook(ev.BookID); entry != nil {
				entry.ReadCount++
			}
		}
		if len(profile.RecentReads) < 15 {
			ev.Author = target
			profile.RecentReads = append(profile.RecentReads, ReadingEventItem{
				Event:          ev,
				BookTitle:      b.Title,
				BookLocalState: b.LocalState,
				IsFavorite:     favPairs[pair],
			})
		}
	}

	// 精选诗篇 ❤️
	for _, f := range favorites {
		b, ok := bookMap[f.BookID]
		if !ok {
			continue
		}
		poet := resolvePoemAuthor(f.Author, poemAuthors[poemPair{bookID: f.BookID, poemID: f.PoemID}], b.Author)
		if poet != target {
			continue
		}
		if entry := ensureBook(f.BookID); entry != nil {
			entry.FavoriteCount++
		}
		profile.Favorites = append(profile.Favorites, AuthorPoemEntry{
			BookID:         f.BookID,
			BookTitle:      b.Title,
			BookLocalState: b.LocalState,
			PoemID:         f.PoemID,
			PoemTitle:      f.PoemTitle,
			Excerpt:        f.Excerpt,
			HighlightLines: f.HighlightLines,
		})
	}

	// 散页（诗歌节选 + 独立散句）
	for _, q := range quotes {
		if normalizePoetName(q.Author) != target {
			continue
		}
		_, bookActive := bookMap[q.BookID]
		profile.Quotes = append(profile.Quotes, QuoteView{
			Quote:         q,
			EffectiveKind: q.EffectiveKind(bookActive),
			BookActive:    bookActive,
		})
	}

	bookEntries := make([]AuthorBookEntry, 0, len(bookStats))
	for _, e := range bookStats {
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

	profile.Books = bookEntries
	profile.ReadPoemCount = len(readPairs)
	profile.BookCount = len(bookEntries)
	profile.FavoriteCount = len(profile.Favorites)
	profile.QuoteCount = len(profile.Quotes)
	return profile, nil
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
