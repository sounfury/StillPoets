package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"stillpoets/internal/domain"
	"stillpoets/internal/parser"
	"stillpoets/internal/storage"
)

// --- 精选、散页、阅读进度、统计与检索 ---

func (s *Service) ToggleFavoritePoem(ctx context.Context, book domain.Book, poem domain.Poem, active bool) (domain.FavoritePoem, error) {
	author := poem.Author
	if author == "" {
		author = book.Author
	}
	fav, err := s.store.ToggleFavoritePoem(ctx, storage.FavoritePoemInput{
		BookID:    book.ID,
		PoemID:    poem.ID,
		PoemTitle: poem.Title,
		Author:    author,
		Excerpt:   parser.BuildDefaultExcerpt(poem),
		Active:    active,
	})
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
	if _, err := s.store.SetFavoriteCustomExcerpt(ctx, storage.FavoritePoemInput{
		BookID:         book.ID,
		PoemID:         poem.ID,
		PoemTitle:      poem.Title,
		Author:         author,
		Excerpt:        excerpt,
		HighlightLines: highlightLines,
	}); err != nil {
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
	q, err := s.store.CreatePoemExcerptQuote(ctx, storage.PoemExcerptQuoteInput{
		BookID:         book.ID,
		PoemID:         poem.ID,
		PoemTitle:      poem.Title,
		SourceBook:     book.Title,
		Author:         author,
		Content:        content,
		HighlightLines: highlightLines,
	})
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
	ev, inserted, err := s.store.RecordReadingEvent(ctx, storage.RecordReadingEventInput{
		BookID:        bookID,
		PoemID:        poemID,
		PoemTitle:     poemTitle,
		Author:        author,
		ReadAt:        time.Now(),
		WasReadBefore: wasReadBefore,
	})
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

	if reread := s.pickFavoriteReread(ctx, bookMap, daySeed); reread != nil {
		return reread, nil
	}
	return s.pickCachedReread(ctx, books, daySeed), nil
}

func (s *Service) pickFavoriteReread(ctx context.Context, books map[string]domain.Book, daySeed uint64) *TodayReread {
	favs, err := s.store.ListFavorites(ctx, "")
	if err != nil || len(favs) == 0 {
		return nil
	}
	chosen := favs[int(daySeed%uint64(len(favs)))]
	excerpt := strings.TrimSpace(chosen.Excerpt)
	if excerpt == "" {
		if p, err := s.store.GetPoem(ctx, chosen.BookID, chosen.PoemID); err == nil {
			excerpt = parser.BuildDefaultExcerpt(p)
		}
	}
	if excerpt == "" {
		return nil
	}
	b := books[chosen.BookID]
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
	}
}

func (s *Service) pickCachedReread(ctx context.Context, books []domain.Book, daySeed uint64) *TodayReread {
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
		}
	}
	return nil
}
