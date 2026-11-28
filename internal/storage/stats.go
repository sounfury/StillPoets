package storage

import (
	"context"
	"errors"
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

// RecordReadingEventInput 封装记录读诗事件的参数。
type RecordReadingEventInput struct {
	BookID        string
	PoemID        string
	PoemTitle     string
	Author        string
	ReadAt        time.Time
	WasReadBefore bool
}

// RecordReadingEvent 记录一次有效读诗事件（单日同书同诗自动去重，并区分初读与重读）。
// 若当日已有该诗的记录，则不产生冗余写入，返回 inserted=false。
func (s *Store) RecordReadingEvent(ctx context.Context, in RecordReadingEventInput) (domain.ReadingEvent, bool, error) {
	bookID := strings.TrimSpace(in.BookID)
	poemID := strings.TrimSpace(in.PoemID)
	poemTitle := in.PoemTitle
	author := in.Author
	readAt := in.ReadAt
	wasReadBefore := in.WasReadBefore
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
