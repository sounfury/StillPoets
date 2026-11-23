package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/egoist/mygo/plugins/sqlite"

	"stillpoets/internal/domain"
)

type ContinueReadingItem struct {
	Book      domain.Book
	Progress  domain.ReadingProgress
	PoemTitle string
}

type StatsOverview struct {
	BookCount         int
	FinishedBookCount int
	ReadPoemCount     int
	FavoriteCount     int
	QuoteCount        int
	OfflineBookCount  int
}

const progressColumns = `book_id, last_poem_id, position_ver_c, position_ver_i, status,
	finished_at, status_ver_c, status_ver_i, read_poem_ids_json`

func decodeProgress(r row) (domain.ReadingProgress, error) {
	p := domain.ReadingProgress{
		BookID: r.text(0), LastPoemID: r.text(1), PositionVer: r.version(2), Status: domain.BookReadStatus(r.text(4)),
		FinishedAt: r.integer(5), StatusVer: r.version(6),
	}
	if err := json.Unmarshal([]byte(r.text(8)), &p.ReadPoemIDs); err != nil {
		return domain.ReadingProgress{}, err
	}
	return p, nil
}

func decodeLocalProgress(r row) (domain.ReadingProgress, error) {
	p, err := decodeProgress(r)
	if err != nil {
		return domain.ReadingProgress{}, err
	}
	p.LocalScrollRatio = r[9].(float64)
	return p, nil
}

// The shared metadata upsert preserves the device's scroll ratio.
func putProgress(p domain.ReadingProgress) sqlite.Statement {
	idsJSON := "[]"
	if len(p.ReadPoemIDs) > 0 {
		if b, err := json.Marshal(p.ReadPoemIDs); err == nil {
			idsJSON = string(b)
		}
	}
	return stmt(`INSERT INTO reading_progress (`+progressColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(book_id) DO UPDATE SET last_poem_id = excluded.last_poem_id,
		position_ver_c = excluded.position_ver_c, position_ver_i = excluded.position_ver_i,
		status = excluded.status, finished_at = excluded.finished_at,
		status_ver_c = excluded.status_ver_c, status_ver_i = excluded.status_ver_i,
		read_poem_ids_json = excluded.read_poem_ids_json`, p.BookID, p.LastPoemID, p.PositionVer.Counter,
		p.PositionVer.InstallID, p.Status, p.FinishedAt, p.StatusVer.Counter, p.StatusVer.InstallID, idsJSON)
}

func setLocalScroll(bookID string, ratio float64) sqlite.Statement {
	return stmt(`UPDATE reading_progress SET local_scroll_ratio = ? WHERE book_id = ?`, ratio, bookID)
}

func (s *Store) RecordPoemRead(ctx context.Context, bookID, poemID string, scrollRatio float64, totalPoems int) (domain.ReadingProgress, error) {
	p, _, err := s.recordPoemProgress(ctx, bookID, poemID, scrollRatio, totalPoems, true)
	return p, err
}

// RecordPoemPeek 记录一次偶遇/直达浏览（如小组件、今日重读、散页跳原诗、全局检索）：
// 计入已读诗篇集合 ReadPoemIDs 并维持完读状态流转，但严格冻结主线续读位置 LastPoemID、PositionVer 与本机滚动比例，
// 避免打断顺序阅读进度或抢占首页「继续阅读」。若本诗已读且状态未变，则不产生脏写。
func (s *Store) RecordPoemPeek(ctx context.Context, bookID, poemID string, totalPoems int) (domain.ReadingProgress, bool, error) {
	return s.recordPoemProgress(ctx, bookID, poemID, 0, totalPoems, false)
}

func (s *Store) recordPoemProgress(ctx context.Context, bookID, poemID string, scrollRatio float64, totalPoems int, updatePosition bool) (domain.ReadingProgress, bool, error) {
	var progress domain.ReadingProgress
	changed := false
	err := s.mutate(ctx, func(v domain.Version, now int64) ([]sqlite.Statement, error) {
		var err error
		progress, err = s.GetProgress(ctx, bookID)
		if err != nil {
			return nil, err
		}
		// 若该书正文已离线缓存，校验 poemID 确实属于该书，杜绝跨书脏进度污染同步数据（P0-2）。
		if poemID != "" {
			if r, err := s.firstRow(ctx, `SELECT local_content_hash FROM books WHERE id = ? AND deleted = 0`, bookID); err == nil && r.text(0) != "" {
				if _, err := s.firstRow(ctx, `SELECT id FROM poems WHERE book_id = ? AND id = ?`, bookID, poemID); err != nil {
					return nil, fmt.Errorf("storage: poem %q does not belong to book %q", poemID, bookID)
				}
			}
		}

		readChanged := false
		if poemID != "" && !slices.Contains(progress.ReadPoemIDs, poemID) {
			progress.ReadPoemIDs = append(progress.ReadPoemIDs, poemID)
			slices.Sort(progress.ReadPoemIDs)
			progress.ReadPoemIDs = slices.Compact(progress.ReadPoemIDs)
			readChanged = true
		}

		statusChanged := false
		if progress.Status != domain.ReadStatusFinished {
			switch {
			case totalPoems > 0 && len(progress.ReadPoemIDs) >= totalPoems:
				progress.Status, progress.FinishedAt, progress.StatusVer = domain.ReadStatusFinished, now, v
				statusChanged = true
			case progress.Status == domain.ReadStatusUnread:
				progress.Status, progress.StatusVer = domain.ReadStatusReading, v
				statusChanged = true
			}
		}

		if updatePosition {
			progress.LastPoemID, progress.PositionVer, progress.LocalScrollRatio = poemID, v, scrollRatio
			changed = true
			return []sqlite.Statement{putProgress(progress), setLocalScroll(bookID, scrollRatio)}, nil
		}

		if !readChanged && !statusChanged {
			return nil, nil
		}
		changed = true
		return []sqlite.Statement{putProgress(progress)}, nil
	})
	if err != nil {
		return domain.ReadingProgress{}, false, err
	}
	return progress, changed, nil
}

func (s *Store) UpdateLocalScroll(ctx context.Context, bookID string, ratio float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	statement := setLocalScroll(bookID, ratio)
	_, err := s.db.Execute(ctx, statement.SQL, statement.Args...)
	return err
}

func (s *Store) MarkBookFinished(ctx context.Context, bookID string, finished bool) (domain.ReadingProgress, error) {
	var progress domain.ReadingProgress
	err := s.mutate(ctx, func(v domain.Version, now int64) ([]sqlite.Statement, error) {
		var err error
		progress, err = s.GetProgress(ctx, bookID)
		if err != nil {
			return nil, err
		}
		progress.StatusVer = v
		if finished {
			progress.Status, progress.FinishedAt = domain.ReadStatusFinished, now
		} else {
			progress.Status, progress.FinishedAt = domain.ReadStatusReading, 0
		}
		return []sqlite.Statement{putProgress(progress)}, nil
	})
	if err != nil {
		return domain.ReadingProgress{}, err
	}
	return progress, nil
}

const activeProgressBook = `EXISTS (SELECT 1 FROM books WHERE books.id = reading_progress.book_id AND books.deleted = 0)`
const localProgressSelect = `SELECT ` + progressColumns + `, local_scroll_ratio FROM reading_progress WHERE ` + activeProgressBook

func (s *Store) GetProgress(ctx context.Context, bookID string) (domain.ReadingProgress, error) {
	r, err := s.firstRow(ctx, localProgressSelect+` AND book_id = ?`, bookID)
	if errors.Is(err, ErrNotFound) {
		if err := s.requireActiveBook(ctx, bookID); err != nil {
			return domain.ReadingProgress{}, err
		}
		return domain.ReadingProgress{BookID: bookID, Status: domain.ReadStatusUnread}, nil
	}
	if err != nil {
		return domain.ReadingProgress{}, err
	}
	return decodeLocalProgress(r)
}

func (s *Store) ListProgress(ctx context.Context) ([]domain.ReadingProgress, error) {
	return selectRows(ctx, s, decodeLocalProgress, localProgressSelect+` ORDER BY book_id`)
}

func (s *Store) GetContinueReading(ctx context.Context) (*ContinueReadingItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Only a reading position contributes to recency. A finish toggle does not.
	r, err := s.firstRow(ctx, localProgressSelect+` AND last_poem_id <> ''
		ORDER BY position_ver_c DESC, position_ver_i DESC, book_id DESC LIMIT 1`)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	progress, err := decodeLocalProgress(r)
	if err != nil {
		return nil, err
	}
	book, err := s.GetBook(ctx, progress.BookID)
	if err != nil {
		return nil, err
	}
	title, err := s.firstRow(ctx, `SELECT COALESCE(
		(SELECT title FROM poems WHERE book_id = ? AND id = ?),
		(SELECT NULLIF(poem_title, '') FROM favorite_poems WHERE book_id = ? AND poem_id = ?), '')`,
		progress.BookID, progress.LastPoemID, progress.BookID, progress.LastPoemID)
	if err != nil {
		return nil, err
	}
	return &ContinueReadingItem{Book: book, Progress: progress, PoemTitle: title.text(0)}, nil
}

func (s *Store) GetStatsOverview(ctx context.Context) (StatsOverview, error) {
	r, err := s.firstRow(ctx, `SELECT
		(SELECT COUNT(*) FROM books WHERE deleted = 0),
		(SELECT COUNT(*) FROM reading_progress WHERE status = 'finished' AND `+activeProgressBook+`),
		(SELECT COALESCE(SUM(json_array_length(read_poem_ids_json)), 0) FROM reading_progress WHERE `+activeProgressBook+`),
		(SELECT COUNT(*) FROM favorite_poems WHERE `+activeFavorite+`),
		(SELECT COUNT(*) FROM quotes WHERE deleted = 0),
		(SELECT COUNT(*) FROM books WHERE deleted = 0 AND local_content_hash <> ''
			AND (content_hash = '' OR local_content_hash = content_hash))`)
	if err != nil {
		return StatsOverview{}, err
	}
	return StatsOverview{
		BookCount: int(r.integer(0)), FinishedBookCount: int(r.integer(1)), ReadPoemCount: int(r.integer(2)),
		FavoriteCount: int(r.integer(3)), QuoteCount: int(r.integer(4)), OfflineBookCount: int(r.integer(5)),
	}, nil
}
