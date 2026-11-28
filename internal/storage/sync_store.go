package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/egoist/mygo/plugins/sqlite"

	"stillpoets/internal/domain"
	booksync "stillpoets/internal/sync"
)

func (s *Store) ExportSnapshot(ctx context.Context) (booksync.SnapshotData, SyncMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, err := s.syncMeta(ctx)
	if err != nil {
		return booksync.SnapshotData{}, SyncMeta{}, err
	}
	data, err := s.snapshotData(ctx)
	return data, meta, err
}

// ExportSnapshotWithPending 在同一 Store.mu 锁下原子导出元数据快照与待上传资产列表，
// 保证同步不会把刚导入却尚未进入待传列表的新书误发到远端（P0-3）。
func (s *Store) ExportSnapshotWithPending(ctx context.Context) (booksync.SnapshotData, SyncMeta, []PendingUpload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, err := s.syncMeta(ctx)
	if err != nil {
		return booksync.SnapshotData{}, SyncMeta{}, nil, err
	}
	data, err := s.snapshotData(ctx)
	if err != nil {
		return booksync.SnapshotData{}, SyncMeta{}, nil, err
	}
	pending, err := s.listPendingUploadsLocked(ctx)
	if err != nil {
		return booksync.SnapshotData{}, SyncMeta{}, nil, err
	}
	return data, meta, pending, nil
}

func (s *Store) snapshotData(ctx context.Context) (booksync.SnapshotData, error) {
	var data booksync.SnapshotData
	var err error
	data.Groups, err = selectRows(ctx, s, decodeGroup, `SELECT `+groupColumns+` FROM book_groups ORDER BY sort_order, id`)
	if err != nil {
		return booksync.SnapshotData{}, err
	}
	data.Books, err = selectRows(ctx, s, decodeBook, `SELECT `+bookColumns+` FROM books ORDER BY sort_order, id`)
	if err != nil {
		return booksync.SnapshotData{}, err
	}
	data.Favorites, err = selectRows(ctx, s, decodeFavorite, `SELECT `+favoriteColumns+` FROM favorite_poems ORDER BY book_id, poem_id`)
	if err != nil {
		return booksync.SnapshotData{}, err
	}
	data.Quotes, err = selectRows(ctx, s, decodeQuote, `SELECT `+quoteColumns+` FROM quotes ORDER BY id`)
	if err != nil {
		return booksync.SnapshotData{}, err
	}
	data.Progress, err = selectRows(ctx, s, decodeProgress, `SELECT `+progressColumns+` FROM reading_progress ORDER BY book_id`)
	if err != nil {
		return booksync.SnapshotData{}, err
	}
	data.ReadingEvents, err = selectRows(ctx, s, decodeReadingEvent, `SELECT `+readingEventColumns+` FROM reading_events ORDER BY read_date, read_at, book_id, poem_id, id`)
	if err != nil {
		return booksync.SnapshotData{}, err
	}
	return data, nil
}

// ApplySnapshotOptions 封装合并快照提交参数。
type ApplySnapshotOptions struct {
	ExportedDirtySeq uint64
	MaxCounter       uint64
	PublishedRev     uint64
	Published        bool
	KeepDirty        bool
	RemoteDeviceRevs map[string]uint64
}

// ApplyMergedSnapshot receives the DirtySeq from the export used for this sync round.
func (s *Store) ApplyMergedSnapshot(ctx context.Context, merged booksync.SnapshotData, exportedDirtySeq, maxCounter, publishedRev uint64, published bool) error {
	return s.ApplyMergedSnapshotEx(ctx, merged, ApplySnapshotOptions{
		ExportedDirtySeq: exportedDirtySeq,
		MaxCounter:       maxCounter,
		PublishedRev:     publishedRev,
		Published:        published,
	})
}

// ApplyMergedSnapshotEx 允许在存在未验证资产（keepDirty=true）时推进 local_rev 但仍保留 dirty=1，
// 防止部分发布后误清脏标记导致新书长期漏发（P1-1），并拒绝超过 MaxInt64 的异常 Lamport 计数（P1-13）。
// remoteDeviceRevs 非 nil 时，随合并数据提交整个工作区的已验证远端代次记录。
func (s *Store) ApplyMergedSnapshotEx(ctx context.Context, merged booksync.SnapshotData, opts ApplySnapshotOptions) error {
	if opts.MaxCounter > math.MaxInt64 {
		return fmt.Errorf("storage: remote lamport counter %d exceeds int64 limit", opts.MaxCounter)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, err := s.syncMeta(ctx)
	if err != nil {
		return err
	}
	changedDuringSync := meta.DirtySeq != opts.ExportedDirtySeq
	if changedDuringSync {
		current, err := s.snapshotData(ctx)
		if err != nil {
			return err
		}
		merged = booksync.MergeSnapshotData(merged, current)
	}

	now := domain.NowMillis()
	statements := snapshotStatements(merged, now)
	statements, err = appendSnapshotSyncState(statements, meta.LamportCounter, opts, changedDuringSync, now)
	if err != nil {
		return err
	}
	return s.transact(ctx, statements)
}

func snapshotStatements(merged booksync.SnapshotData, now int64) []sqlite.Statement {
	statements := []sqlite.Statement{stmt(`DELETE FROM quotes_fts`)}
	for _, group := range merged.Groups {
		statements = append(statements, putGroup(group))
	}
	for _, book := range merged.Books {
		statements = append(statements, putBook(book))
		if book.Deleted {
			statements = append(statements, clearBookContent(book.ID)...)
		}
	}
	for _, favorite := range merged.Favorites {
		statements = append(statements, putFavorite(favorite))
	}
	for _, quote := range merged.Quotes {
		statements = append(statements, putQuote(quote))
		if !quote.Deleted {
			statements = append(statements, indexQuote(quote.ID, quote.Content, quote.Author, quote.SourceBook))
		}
	}
	bookPoemCounts := snapshotPoemCounts(merged.Books)
	for _, progress := range merged.Progress {
		reconcileSnapshotCompletion(&progress, bookPoemCounts[progress.BookID], now)
		statements = append(statements, putProgress(progress))
	}
	for _, ev := range merged.ReadingEvents {
		statements = append(statements, putReadingEventStmts(ev)...)
	}

	return statements
}

func snapshotPoemCounts(books []domain.Book) map[string]int {
	// 跨端读完收敛（P1-14）：若合并后的已读并集覆盖整本诗集全部有效诗篇，则自动推进为 finished。
	bookPoemCounts := make(map[string]int, len(books))
	for _, b := range books {
		if !b.Deleted && b.PoemCount > 0 {
			bookPoemCounts[b.ID] = b.PoemCount
		}
	}

	return bookPoemCounts
}

func reconcileSnapshotCompletion(progress *domain.ReadingProgress, total int, now int64) {
	if progress.Status == domain.ReadStatusFinished || total <= 0 || len(progress.ReadPoemIDs) < total {
		return
	}
	progress.Status = domain.ReadStatusFinished
	if progress.FinishedAt == 0 {
		progress.FinishedAt = now
	}
}

func appendSnapshotSyncState(statements []sqlite.Statement, localCounter uint64, opts ApplySnapshotOptions, changedDuringSync bool, now int64) ([]sqlite.Statement, error) {
	statements = append(statements,
		setSyncState("lamport_counter", strconv.FormatUint(max(localCounter, opts.MaxCounter), 10), now),
		setSyncState("last_sync_at", strconv.FormatInt(now, 10), now),
	)
	if opts.RemoteDeviceRevs != nil {
		revs, err := json.Marshal(opts.RemoteDeviceRevs)
		if err != nil {
			return nil, err
		}
		statements = append(statements, setSyncState("remote_device_revs", string(revs), now))
	}
	if opts.Published {
		statements = append(statements, setSyncState("local_rev", strconv.FormatUint(opts.PublishedRev, 10), now))
		if !changedDuringSync && !opts.KeepDirty {
			statements = append(statements, setSyncState("dirty", "0", now))
		} else if opts.KeepDirty {
			statements = append(statements, setSyncState("dirty", "1", now))
		}
	}
	return statements, nil
}

const (
	UploadPending        = "pending"
	UploadAssetsVerified = "assets_verified"
	UploadFailed         = "failed"
)

type PendingUpload struct {
	BookID          string
	RawFilename     string
	LocalRawPath    string
	LocalSPBookPath string
	LocalCoverPath  string
	ContentHash     string
	State           string
	LastError       string
	CreatedAt       int64
	UpdatedAt       int64
}

const pendingUploadColumns = `book_id, raw_filename, local_raw_path, local_spbook_path, local_cover_path,
	content_hash, state, last_error, created_at, updated_at`

func putPendingUploadStmt(upload PendingUpload) sqlite.Statement {
	return stmt(`INSERT INTO pending_uploads (`+pendingUploadColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(book_id) DO UPDATE SET raw_filename = excluded.raw_filename, local_raw_path = excluded.local_raw_path,
		local_spbook_path = excluded.local_spbook_path, local_cover_path = excluded.local_cover_path,
		content_hash = excluded.content_hash, state = excluded.state, last_error = excluded.last_error, updated_at = excluded.updated_at`,
		upload.BookID, upload.RawFilename, upload.LocalRawPath, upload.LocalSPBookPath, upload.LocalCoverPath,
		upload.ContentHash, upload.State, upload.LastError, upload.CreatedAt, upload.UpdatedAt)
}

func (s *Store) UpsertPendingUpload(ctx context.Context, upload PendingUpload) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := domain.NowMillis()
	if upload.CreatedAt == 0 {
		upload.CreatedAt = now
	}
	if upload.UpdatedAt == 0 {
		upload.UpdatedAt = now
	}
	if upload.State == "" {
		upload.State = UploadPending
	}
	st := putPendingUploadStmt(upload)
	_, err := s.db.Execute(ctx, st.SQL, st.Args...)
	return err
}

func (s *Store) UpdatePendingUploadState(ctx context.Context, bookID, state, lastError string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.Execute(ctx, `UPDATE pending_uploads SET state = ?, last_error = ?, updated_at = ? WHERE book_id = ?`,
		state, lastError, domain.NowMillis(), bookID)
	if err == nil && result.Changes == 0 {
		return ErrNotFound
	}
	return err
}

func decodePendingUpload(r row) (PendingUpload, error) {
	return PendingUpload{
		BookID: r.text(0), RawFilename: r.text(1), LocalRawPath: r.text(2), LocalSPBookPath: r.text(3),
		LocalCoverPath: r.text(4), ContentHash: r.text(5), State: r.text(6), LastError: r.text(7),
		CreatedAt: r.integer(8), UpdatedAt: r.integer(9),
	}, nil
}

func (s *Store) listPendingUploadsLocked(ctx context.Context) ([]PendingUpload, error) {
	return selectRows(ctx, s, decodePendingUpload, `SELECT `+pendingUploadColumns+` FROM pending_uploads ORDER BY created_at, book_id`)
}

func (s *Store) ListPendingUploads(ctx context.Context) ([]PendingUpload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listPendingUploadsLocked(ctx)
}

func (s *Store) DeletePendingUpload(ctx context.Context, bookID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Execute(ctx, `DELETE FROM pending_uploads WHERE book_id = ?`, bookID)
	return err
}

func (s *Store) HasPendingUpload(ctx context.Context, bookID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.firstRow(ctx, `SELECT book_id FROM pending_uploads WHERE book_id = ?`, bookID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
