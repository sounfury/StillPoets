package storage

import (
	"context"
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
	return data, nil
}

// ApplyMergedSnapshot receives the DirtySeq from the export used for this sync round.
func (s *Store) ApplyMergedSnapshot(ctx context.Context, merged booksync.SnapshotData, exportedDirtySeq, maxCounter, publishedRev uint64, published bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, err := s.syncMeta(ctx)
	if err != nil {
		return err
	}
	changedDuringSync := meta.DirtySeq != exportedDirtySeq
	if changedDuringSync {
		current, err := s.snapshotData(ctx)
		if err != nil {
			return err
		}
		merged = booksync.MergeSnapshotData(merged, current)
	}

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
	for _, progress := range merged.Progress {
		statements = append(statements, putProgress(progress))
	}

	now := domain.NowMillis()
	statements = append(statements,
		setSyncState("lamport_counter", strconv.FormatUint(max(meta.LamportCounter, maxCounter), 10), now),
		setSyncState("last_sync_at", strconv.FormatInt(now, 10), now),
	)
	if published {
		statements = append(statements, setSyncState("local_rev", strconv.FormatUint(publishedRev, 10), now))
		if !changedDuringSync {
			statements = append(statements, setSyncState("dirty", "0", now))
		}
	}
	return s.transact(ctx, statements)
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

func (s *Store) UpsertPendingUpload(ctx context.Context, upload PendingUpload) error {
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
	_, err := s.db.Execute(ctx, `INSERT INTO pending_uploads (`+pendingUploadColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(book_id) DO UPDATE SET raw_filename = excluded.raw_filename, local_raw_path = excluded.local_raw_path,
		local_spbook_path = excluded.local_spbook_path, local_cover_path = excluded.local_cover_path,
		content_hash = excluded.content_hash, state = excluded.state, last_error = excluded.last_error, updated_at = excluded.updated_at`,
		upload.BookID, upload.RawFilename, upload.LocalRawPath, upload.LocalSPBookPath, upload.LocalCoverPath,
		upload.ContentHash, upload.State, upload.LastError, upload.CreatedAt, upload.UpdatedAt)
	return err
}

func (s *Store) UpdatePendingUploadState(ctx context.Context, bookID, state, lastError string) error {
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

func (s *Store) ListPendingUploads(ctx context.Context) ([]PendingUpload, error) {
	return selectRows(ctx, s, decodePendingUpload, `SELECT `+pendingUploadColumns+` FROM pending_uploads ORDER BY created_at, book_id`)
}

func (s *Store) DeletePendingUpload(ctx context.Context, bookID string) error {
	_, err := s.db.Execute(ctx, `DELETE FROM pending_uploads WHERE book_id = ?`, bookID)
	return err
}
