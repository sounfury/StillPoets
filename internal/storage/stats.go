package storage

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"

	"stillpoets/internal/domain"
)

// StatsPeriod 定义统计看板顶部的时间筛选窗口。
type StatsPeriod string

const (
	StatsPeriodWeek  StatsPeriod = "week"
	StatsPeriodMonth StatsPeriod = "month"
	StatsPeriodYear  StatsPeriod = "year"
	StatsPeriodAll   StatsPeriod = "all"
)

// AuthorWeightMode 定义诗人作者云的排序与权重维度。
type AuthorWeightMode string

const (
	AuthorWeightCombined   AuthorWeightMode = "combined"   // 综合权重（读过 + 精选×3 + 散页×2）
	AuthorWeightRead       AuthorWeightMode = "read"       // 按读过诗篇数
	AuthorWeightCollection AuthorWeightMode = "collection" // 按精选诗篇 ❤️ 与散页收藏数
)

// StatsQuery 描述一次完整统计看板聚合查询的参数。
type StatsQuery struct {
	Period     StatsPeriod
	Year       int // 0 表示近 52 周滚动热力图；>0 表示指定自然年
	AuthorMode AuthorWeightMode
	Now        time.Time // 零值时默认 time.Now()
}

// WindowOverview 汇总当前时间窗口的核心指标与书库总况。
type WindowOverview struct {
	BookCount          int
	OfflineBookCount   int
	FinishedBookCount  int
	TotalFinishedCount int
	ReadPoemCount      int
	ReadEventCount     int
	FavoriteCount      int
	QuoteCount         int
	ActiveDays         int
}

// FinishedBookItem 表示完读诗集墙中的单本已完读诗集及其精选摘要。
type FinishedBookItem struct {
	Book          domain.Book
	Progress      domain.ReadingProgress
	FavoriteCount int
	QuoteCount    int
	TopFavorites  []domain.FavoritePoem
}

// ReadingBookItem 表示尚未完读的在读/未读诗集（支持手动完读校准）。
type ReadingBookItem struct {
	Book          domain.Book
	Progress      domain.ReadingProgress
	FavoriteCount int
}

// HeatmapDay 表示日历热力图中的单日数据格。
type HeatmapDay struct {
	Date         string // YYYY-MM-DD
	Count        int    // 当日去重有效读诗篇数
	InitialCount int    // 初读篇数
	RereadCount  int    // 重读篇数
	Level        int    // 0..4 色阶
	InFuture     bool
	OutOfRange   bool
}

// HeatmapWeek 表示日历热力图的一列（周一至周日）。
type HeatmapWeek struct {
	MonthLabel string
	Days       [7]HeatmapDay
}

// HeatmapData 聚合热力图网格与活跃统计。
type HeatmapData struct {
	Year             int
	ModeLabel        string
	AvailableYears   []int
	Weeks            []HeatmapWeek
	ActiveDays       int
	TotalEvents      int
	MaxDayCount      int
	LatestActiveDate string
}

// ReadingEventItem 是供热力图按日下钻与诗人档案展示的读诗明细。
type ReadingEventItem struct {
	Event          domain.ReadingEvent
	BookTitle      string
	BookLocalState domain.LocalBookState
	IsFavorite     bool
}

// TimeSlotStat 描述单个诗意阅读时段的统计。
type TimeSlotStat struct {
	Key       string
	Label     string
	RangeText string
	Count     int
	Ratio     float64
}

// ReadingRhythm 描述用户的昼夜读诗节律（5 时段 + 24 小时细粒度 + 洞察文案）。
type ReadingRhythm struct {
	TotalEvents   int
	Slots         [5]TimeSlotStat
	Hourly        [24]int
	PeakHour      int
	PeakHourCount int
	Insight       string
}

// AuthorStat 表示作者云中的单个诗人权重与分项计数。
type AuthorStat struct {
	Author        string
	ReadCount     int
	FavoriteCount int
	QuoteCount    int
	BookCount     int
	CombinedScore int
	Score         int
	Tier          int // 1..5，用于排版字号与墨色浓淡
}

// AuthorBookEntry 表示诗人档案中关联的某部诗集或合集。
type AuthorBookEntry struct {
	Book          domain.Book
	ReadCount     int
	FavoriteCount int
}

// AuthorPoemEntry 表示诗人档案中该诗人的精选诗篇。
type AuthorPoemEntry struct {
	BookID         string
	BookTitle      string
	BookLocalState domain.LocalBookState
	PoemID         string
	PoemTitle      string
	Excerpt        string
	HighlightLines string
}

// AuthorProfile 聚合点击词云中某位诗人后的个人档案卡数据。
type AuthorProfile struct {
	Author        string
	ReadPoemCount int
	BookCount     int
	FavoriteCount int
	QuoteCount    int
	Books         []AuthorBookEntry
	Favorites     []AuthorPoemEntry
	Quotes        []QuoteView
	RecentReads   []ReadingEventItem
}

// StatsDashboard 是 Phase 3 完整统计看板的一次性聚合快照。
type StatsDashboard struct {
	Period        StatsPeriod
	AuthorMode    AuthorWeightMode
	Overview      WindowOverview
	FinishedBooks []FinishedBookItem
	ReadingBooks  []ReadingBookItem
	Heatmap       HeatmapData
	Rhythm        ReadingRhythm
	Authors       []AuthorStat
}

const readingEventColumns = `id, book_id, poem_id, poem_title, author, read_date, read_hour, read_at, is_reread, device_id`

func decodeReadingEvent(r row) (domain.ReadingEvent, error) {
	return domain.ReadingEvent{
		ID:        r.text(0),
		BookID:    r.text(1),
		PoemID:    r.text(2),
		PoemTitle: r.text(3),
		Author:    r.text(4),
		ReadDate:  r.text(5),
		ReadHour:  int(r.integer(6)),
		ReadAt:    r.integer(7),
		IsReread:  r.flag(8),
		DeviceID:  r.text(9),
	}, nil
}

func putReadingEventStmts(ev domain.ReadingEvent) []sqlite.Statement {
	if ev.BookID == "" || ev.PoemID == "" || ev.ReadDate == "" {
		return nil
	}
	if ev.ID == "" {
		ev.ID = domain.NewID()
	}
	hour := ev.ReadHour
	if hour < 0 || hour > 23 {
		hour = 0
	}
	return []sqlite.Statement{
		stmt(`DELETE FROM reading_events WHERE book_id = ? AND poem_id = ? AND read_date = ?`,
			ev.BookID, ev.PoemID, ev.ReadDate),
		stmt(`INSERT OR REPLACE INTO reading_events (`+readingEventColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			ev.ID, ev.BookID, ev.PoemID, ev.PoemTitle, ev.Author, ev.ReadDate, hour, ev.ReadAt, ev.IsReread, ev.DeviceID),
	}
}

// RecordReadingEvent 记录一次有效读诗事件（单日同书同诗自动去重，并区分初读与重读）。
// 若当日已有该诗的记录，则不产生冗余写入，返回 inserted=false。
func (s *Store) RecordReadingEvent(
	ctx context.Context,
	bookID, poemID, poemTitle, author string,
	readAt time.Time,
	wasReadBefore bool,
) (domain.ReadingEvent, bool, error) {
	bookID = strings.TrimSpace(bookID)
	poemID = strings.TrimSpace(poemID)
	if bookID == "" || poemID == "" {
		return domain.ReadingEvent{}, false, errors.New("storage: bookID and poemID are required")
	}
	if readAt.IsZero() {
		readAt = time.Now()
	}
	readDate := readAt.Format("2006-01-02")
	readHour := readAt.Hour()
	readAtMs := readAt.UTC().UnixMilli()

	var (
		out      domain.ReadingEvent
		inserted bool
	)
	err := s.mutate(ctx, func(v domain.Version, _ int64) ([]sqlite.Statement, error) {
		bookRow, err := s.firstRow(ctx, `SELECT author FROM books WHERE id = ? AND deleted = 0`, bookID)
		if err != nil {
			return nil, err
		}
		// 单日同书同诗去重：同一天反复翻看只计 1 篇，保留首次有效阅读记录
		if existingRow, err := s.firstRow(ctx, `SELECT `+readingEventColumns+` FROM reading_events WHERE book_id = ? AND poem_id = ? AND read_date = ?`,
			bookID, poemID, readDate); err == nil {
			out, err = decodeReadingEvent(existingRow)
			return nil, err
		} else if !errors.Is(err, ErrNotFound) {
			return nil, err
		}

		// 若尚未传入诗题或作者，从本地已缓存正文或书目元数据补齐（保证未下载设备下钻可用）
		title := strings.TrimSpace(poemTitle)
		poet := strings.TrimSpace(author)
		if title == "" || poet == "" {
			if pRow, err := s.firstRow(ctx, `SELECT title, author FROM poems WHERE book_id = ? AND id = ?`, bookID, poemID); err == nil {
				if title == "" {
					title = strings.TrimSpace(pRow.text(0))
				}
				if poet == "" {
					poet = strings.TrimSpace(pRow.text(1))
				}
			}
		}
		if poet == "" {
			poet = strings.TrimSpace(bookRow.text(0))
		}

		// 判断初读 / 重读：更早日期存在阅读事件或此前已在已读集合中均视为重读
		isReread := wasReadBefore
		if !isReread {
			if _, err := s.firstRow(ctx, `SELECT 1 FROM reading_events WHERE book_id = ? AND poem_id = ? AND read_date <> ? LIMIT 1`,
				bookID, poemID, readDate); err == nil {
				isReread = true
			}
		}

		out = domain.ReadingEvent{
			ID:        domain.NewID(),
			BookID:    bookID,
			PoemID:    poemID,
			PoemTitle: title,
			Author:    poet,
			ReadDate:  readDate,
			ReadHour:  readHour,
			ReadAt:    readAtMs,
			IsReread:  isReread,
			DeviceID:  v.InstallID,
		}
		inserted = true
		return putReadingEventStmts(out), nil
	})
	if err != nil {
		return domain.ReadingEvent{}, false, err
	}
	return out, inserted, nil
}

// ListReadingEventsByDate 返回某一天（YYYY-MM-DD）的全部有效读诗明细，按阅读时间倒序。
func (s *Store) ListReadingEventsByDate(ctx context.Context, date string) ([]ReadingEventItem, error) {
	date = strings.TrimSpace(date)
	if date == "" {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listReadingEventsByDateLocked(ctx, date)
}

func (s *Store) listReadingEventsByDateLocked(ctx context.Context, date string) ([]ReadingEventItem, error) {
	books, err := s.listActiveBooksLocked(ctx)
	if err != nil {
		return nil, err
	}
	bookMap := make(map[string]domain.Book, len(books))
	for _, b := range books {
		bookMap[b.ID] = b
	}

	favPairs, err := s.activeFavoritePairsLocked(ctx)
	if err != nil {
		return nil, err
	}

	events, err := selectRows(ctx, s, decodeReadingEvent,
		`SELECT `+readingEventColumns+` FROM reading_events WHERE read_date = ? ORDER BY read_at DESC, id DESC`, date)
	if err != nil {
		return nil, err
	}

	items := make([]ReadingEventItem, 0, len(events))
	for _, ev := range events {
		b, ok := bookMap[ev.BookID]
		if !ok {
			continue
		}
		if ev.Author == "" {
			ev.Author = b.Author
		}
		items = append(items, ReadingEventItem{
			Event:          ev,
			BookTitle:      b.Title,
			BookLocalState: b.LocalState,
			IsFavorite:     favPairs[poemPair{bookID: ev.BookID, poemID: ev.PoemID}],
		})
	}
	return items, nil
}

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
	authors := buildAuthorCloud(
		windowEvents,
		historicalReadPairs,
		!bounded,
		windowFavorites,
		windowQuotes,
		bookMap,
		poemAuthors,
		authorMode,
	)

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
			if poet == target {
				pair := poemPair{bookID: p.BookID, poemID: pid}
				if _, seen := readPairs[pair]; !seen {
					readPairs[pair] = struct{}{}
					if entry := ensureBook(p.BookID); entry != nil {
						entry.ReadCount++
					}
				}
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

// --- 内部聚合辅助函数 ---

type poemPair struct {
	bookID string
	poemID string
}

func (s *Store) listActiveBooksLocked(ctx context.Context) ([]domain.Book, error) {
	return selectRows(ctx, s, decodeLocalBook,
		`SELECT `+localBookColumns+` FROM books WHERE deleted = 0 ORDER BY sort_order, id`)
}

func (s *Store) activeFavoritePairsLocked(ctx context.Context) (map[poemPair]bool, error) {
	rows, err := s.db.Query(ctx, `SELECT book_id, poem_id FROM favorite_poems WHERE `+activeFavorite)
	if err != nil {
		return nil, err
	}
	out := make(map[poemPair]bool, len(rows.Values))
	for _, vals := range rows.Values {
		r := row(vals)
		out[poemPair{bookID: r.text(0), poemID: r.text(1)}] = true
	}
	return out, nil
}

func (s *Store) cachedPoemAuthorsLocked(ctx context.Context) (map[poemPair]string, error) {
	rows, err := s.db.Query(ctx, `SELECT book_id, id, author FROM poems WHERE author <> ''`)
	if err != nil {
		return nil, err
	}
	out := make(map[poemPair]string, len(rows.Values))
	for _, vals := range rows.Values {
		r := row(vals)
		out[poemPair{bookID: r.text(0), poemID: r.text(1)}] = strings.TrimSpace(r.text(2))
	}
	return out, nil
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

func buildHeatmap(events []domain.ReadingEvent, year int, now time.Time) HeatmapData {
	loc := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	todayStr := today.Format("2006-01-02")

	type dayAgg struct {
		total   int
		initial int
		reread  int
	}
	byDate := make(map[string]*dayAgg)
	yearSet := map[int]struct{}{now.Year(): {}}
	for _, ev := range events {
		if len(ev.ReadDate) >= 4 {
			if y, err := strconv.Atoi(ev.ReadDate[:4]); err == nil && y > 1970 {
				yearSet[y] = struct{}{}
			}
		}
		agg := byDate[ev.ReadDate]
		if agg == nil {
			agg = &dayAgg{}
			byDate[ev.ReadDate] = agg
		}
		agg.total++
		if ev.IsReread {
			agg.reread++
		} else {
			agg.initial++
		}
	}

	availYears := make([]int, 0, len(yearSet))
	for y := range yearSet {
		availYears = append(availYears, y)
	}
	slices.Sort(availYears)

	var (
		rangeStart time.Time
		rangeEnd   time.Time
		gridStart  time.Time
		gridEnd    time.Time
		modeLabel  string
	)
	if year <= 0 {
		// 默认近 52 周滚动视图，右端自然对齐本周日
		offset := (int(today.Weekday()) + 6) % 7
		thisMonday := today.AddDate(0, 0, -offset)
		gridStart = thisMonday.AddDate(0, 0, -51*7)
		gridEnd = thisMonday.AddDate(0, 0, 6)
		rangeStart = gridStart
		rangeEnd = today
		modeLabel = "近一年"
	} else {
		rangeStart = time.Date(year, 1, 1, 0, 0, 0, 0, loc)
		rangeEnd = time.Date(year, 12, 31, 0, 0, 0, 0, loc)
		startOffset := (int(rangeStart.Weekday()) + 6) % 7
		gridStart = rangeStart.AddDate(0, 0, -startOffset)
		endOffset := (7 - ((int(rangeEnd.Weekday()) + 6) % 7) - 1)
		gridEnd = rangeEnd.AddDate(0, 0, endOffset)
		modeLabel = fmt.Sprintf("%d 年", year)
	}

	hm := HeatmapData{
		Year:           year,
		ModeLabel:      modeLabel,
		AvailableYears: availYears,
	}

	lastLabeledMonth := time.Month(0)
	for weekStart := gridStart; !weekStart.After(gridEnd); weekStart = weekStart.AddDate(0, 0, 7) {
		var w HeatmapWeek
		for d := 0; d < 7; d++ {
			cur := weekStart.AddDate(0, 0, d)
			dateStr := cur.Format("2006-01-02")
			outOfRange := cur.Before(rangeStart) || cur.After(rangeEnd)
			inFuture := cur.After(today)

			// 标注每个自然月首次出现的列
			if !outOfRange && (cur.Day() == 1 || (len(hm.Weeks) == 0 && d == 0)) && cur.Month() != lastLabeledMonth {
				w.MonthLabel = fmt.Sprintf("%d月", int(cur.Month()))
				lastLabeledMonth = cur.Month()
			}

			cell := HeatmapDay{
				Date:       dateStr,
				InFuture:   inFuture,
				OutOfRange: outOfRange,
			}
			if !outOfRange && !inFuture {
				if agg := byDate[dateStr]; agg != nil && agg.total > 0 {
					cell.Count = agg.total
					cell.InitialCount = agg.initial
					cell.RereadCount = agg.reread
					cell.Level = heatmapLevel(agg.total)
					hm.ActiveDays++
					hm.TotalEvents += agg.total
					if agg.total > hm.MaxDayCount {
						hm.MaxDayCount = agg.total
					}
					if dateStr <= todayStr && dateStr > hm.LatestActiveDate {
						hm.LatestActiveDate = dateStr
					}
				}
			}
			w.Days[d] = cell
		}
		hm.Weeks = append(hm.Weeks, w)
	}
	return hm
}

func heatmapLevel(count int) int {
	switch {
	case count <= 0:
		return 0
	case count <= 2:
		return 1
	case count <= 5:
		return 2
	case count <= 10:
		return 3
	default:
		return 4
	}
}

func buildReadingRhythm(events []domain.ReadingEvent) ReadingRhythm {
	r := ReadingRhythm{
		TotalEvents: len(events),
		Slots: [5]TimeSlotStat{
			{Key: "dawn", Label: "清晨", RangeText: "05:00–09:00"},
			{Key: "morning", Label: "上午", RangeText: "09:00–12:00"},
			{Key: "afternoon", Label: "午后", RangeText: "12:00–18:00"},
			{Key: "evening", Label: "入夜", RangeText: "18:00–23:00"},
			{Key: "late_night", Label: "深夜", RangeText: "23:00–05:00"},
		},
	}
	if len(events) == 0 {
		r.Insight = "开始静静读几首诗后，这里会浮现你的昼夜读诗节律。"
		return r
	}

	for _, ev := range events {
		h := ev.ReadHour
		if h < 0 || h > 23 {
			h = 0
		}
		r.Hourly[h]++
		r.Slots[timeSlotIndex(h)].Count++
	}

	topSlotIdx := 0
	for i := range r.Slots {
		r.Slots[i].Ratio = float64(r.Slots[i].Count) / float64(len(events))
		if r.Slots[i].Count > r.Slots[topSlotIdx].Count {
			topSlotIdx = i
		}
	}
	for h, cnt := range r.Hourly {
		if cnt > r.PeakHourCount {
			r.PeakHour = h
			r.PeakHourCount = cnt
		}
	}

	// 计算最集中的连续 4 小时窗口，生成克制的个人节律洞察文案
	bestStart, bestSum := 0, -1
	for start := 0; start < 24; start++ {
		sum := 0
		for k := 0; k < 4; k++ {
			sum += r.Hourly[(start+k)%24]
		}
		if sum > bestSum {
			bestSum = sum
			bestStart = start
		}
	}
	bestEnd := (bestStart + 4) % 24
	pct := int(math.Round(float64(bestSum) * 100 / float64(len(events))))
	r.Insight = fmt.Sprintf("你偏爱在%s时分读诗，%02d:00–%02d:00 贡献了 %d%% 的读诗时刻。",
		r.Slots[topSlotIdx].Label, bestStart, bestEnd, pct)
	return r
}

func timeSlotIndex(hour int) int {
	switch {
	case hour >= 5 && hour < 9:
		return 0 // 清晨
	case hour >= 9 && hour < 12:
		return 1 // 上午
	case hour >= 12 && hour < 18:
		return 2 // 午后
	case hour >= 18 && hour < 23:
		return 3 // 入夜
	default:
		return 4 // 深夜 23..04
	}
}

func buildAuthorCloud(
	windowEvents []domain.ReadingEvent,
	historicalReadPairs map[poemPair]struct{},
	includeHistorical bool,
	windowFavorites []domain.FavoritePoem,
	windowQuotes []domain.Quote,
	bookMap map[string]domain.Book,
	poemAuthors map[poemPair]string,
	mode AuthorWeightMode,
) []AuthorStat {
	type acc struct {
		readPairs map[poemPair]struct{}
		books     map[string]struct{}
		favCount  int
		quoteCnt  int
	}
	byAuthor := make(map[string]*acc)
	ensure := func(name string) *acc {
		a := byAuthor[name]
		if a == nil {
			a = &acc{
				readPairs: make(map[poemPair]struct{}),
				books:     make(map[string]struct{}),
			}
			byAuthor[name] = a
		}
		return a
	}

	for _, ev := range windowEvents {
		b := bookMap[ev.BookID]
		poet := resolvePoemAuthor(ev.Author, poemAuthors[poemPair{bookID: ev.BookID, poemID: ev.PoemID}], b.Author)
		if poet == "" {
			continue
		}
		a := ensure(poet)
		a.readPairs[poemPair{bookID: ev.BookID, poemID: ev.PoemID}] = struct{}{}
		a.books[ev.BookID] = struct{}{}
	}

	if includeHistorical {
		for pair := range historicalReadPairs {
			b, ok := bookMap[pair.bookID]
			if !ok {
				continue
			}
			poet := resolvePoemAuthor("", poemAuthors[pair], b.Author)
			if poet == "" {
				continue
			}
			a := ensure(poet)
			a.readPairs[pair] = struct{}{}
			a.books[pair.bookID] = struct{}{}
		}
	}

	for _, f := range windowFavorites {
		b := bookMap[f.BookID]
		poet := resolvePoemAuthor(f.Author, poemAuthors[poemPair{bookID: f.BookID, poemID: f.PoemID}], b.Author)
		if poet == "" {
			continue
		}
		a := ensure(poet)
		a.favCount++
		a.books[f.BookID] = struct{}{}
	}

	for _, q := range windowQuotes {
		poet := normalizePoetName(q.Author)
		if poet == "" {
			continue
		}
		a := ensure(poet)
		a.quoteCnt++
		if q.BookID != "" {
			if _, ok := bookMap[q.BookID]; ok {
				a.books[q.BookID] = struct{}{}
			}
		}
	}

	list := make([]AuthorStat, 0, len(byAuthor))
	for name, a := range byAuthor {
		readCnt := len(a.readPairs)
		combined := readCnt + a.favCount*3 + a.quoteCnt*2
		var score int
		switch mode {
		case AuthorWeightRead:
			score = readCnt
		case AuthorWeightCollection:
			score = a.favCount + a.quoteCnt
		default:
			score = combined
		}
		if score <= 0 {
			continue
		}
		list = append(list, AuthorStat{
			Author:        name,
			ReadCount:     readCnt,
			FavoriteCount: a.favCount,
			QuoteCount:    a.quoteCnt,
			BookCount:     len(a.books),
			CombinedScore: combined,
			Score:         score,
		})
	}

	slices.SortFunc(list, func(a, b AuthorStat) int {
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		if a.CombinedScore != b.CombinedScore {
			return b.CombinedScore - a.CombinedScore
		}
		return strings.Compare(a.Author, b.Author)
	})
	if len(list) > 36 {
		list = list[:36]
	}

	// 按对数分档映射到 1..5 视觉阶梯，拉开高频与低频诗人的排版层次感
	if len(list) > 0 {
		maxScore := float64(list[0].Score)
		minScore := float64(list[len(list)-1].Score)
		logMax := math.Log1p(maxScore)
		logMin := math.Log1p(minScore)
		for i := range list {
			if logMax <= logMin {
				list[i].Tier = 4
				continue
			}
			norm := (math.Log1p(float64(list[i].Score)) - logMin) / (logMax - logMin)
			tier := 1 + int(math.Round(norm*4))
			list[i].Tier = max(1, min(5, tier))
		}
	}
	return list
}

func resolvePoemAuthor(explicitAuthor, cachedPoemAuthor, bookAuthor string) string {
	for _, cand := range []string{explicitAuthor, cachedPoemAuthor, bookAuthor} {
		if n := normalizePoetName(cand); n != "" {
			return n
		}
	}
	return ""
}

func normalizePoetName(raw string) string {
	s := strings.TrimSpace(raw)
	switch s {
	case "", "佚名", "未知", "无名氏", "Unknown", "unknown":
		return ""
	default:
		return s
	}
}
