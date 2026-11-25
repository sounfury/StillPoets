package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/egoist/mygo/plugins/sqlite"

	"stillpoets/internal/domain"
)

const groupColumns = `id, name, name_ver_c, name_ver_i, sort_order, sort_ver_c, sort_ver_i,
	created_at, deleted, deleted_at, deleted_ver_c, deleted_ver_i`

func decodeGroup(r row) (domain.BookGroup, error) {
	return domain.BookGroup{
		ID: r.text(0), Name: r.text(1), NameVer: r.version(2), SortOrder: int(r.integer(4)),
		SortVer: r.version(5), CreatedAt: r.integer(7), Deleted: r.flag(8), DeletedAt: r.integer(9),
		DeletedVer: r.version(10),
	}, nil
}

func putGroup(g domain.BookGroup) sqlite.Statement {
	return stmt(`INSERT INTO book_groups (`+groupColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, name_ver_c = excluded.name_ver_c,
		name_ver_i = excluded.name_ver_i, sort_order = excluded.sort_order, sort_ver_c = excluded.sort_ver_c,
		sort_ver_i = excluded.sort_ver_i, created_at = excluded.created_at, deleted = excluded.deleted,
		deleted_at = excluded.deleted_at, deleted_ver_c = excluded.deleted_ver_c, deleted_ver_i = excluded.deleted_ver_i`,
		g.ID, g.Name, g.NameVer.Counter, g.NameVer.InstallID, g.SortOrder, g.SortVer.Counter, g.SortVer.InstallID,
		g.CreatedAt, g.Deleted, g.DeletedAt, g.DeletedVer.Counter, g.DeletedVer.InstallID)
}

func (s *Store) CreateGroup(ctx context.Context, name string) (domain.BookGroup, error) {
	var group domain.BookGroup
	err := s.mutate(ctx, func(v domain.Version, now int64) ([]sqlite.Statement, error) {
		r, err := s.firstRow(ctx, `SELECT COALESCE(MAX(sort_order) + 1, 0) FROM book_groups WHERE deleted = 0`)
		if err != nil {
			return nil, err
		}
		group = domain.BookGroup{
			ID: domain.NewID(), Name: name, NameVer: v, SortOrder: int(r.integer(0)), SortVer: v, CreatedAt: now,
		}
		return []sqlite.Statement{putGroup(group)}, nil
	})
	if err != nil {
		return domain.BookGroup{}, err
	}
	return group, nil
}

func (s *Store) RenameGroup(ctx context.Context, id, name string) error {
	return s.mutate(ctx, func(v domain.Version, _ int64) ([]sqlite.Statement, error) {
		if err := s.requireActiveGroup(ctx, id); err != nil {
			return nil, err
		}
		return []sqlite.Statement{stmt(`UPDATE book_groups SET name = ?, name_ver_c = ?, name_ver_i = ? WHERE id = ?`,
			name, v.Counter, v.InstallID, id)}, nil
	})
}

func (s *Store) ReorderGroups(ctx context.Context, orderedIDs []string) error {
	if len(orderedIDs) == 0 {
		return ctx.Err()
	}
	return s.mutate(ctx, func(v domain.Version, _ int64) ([]sqlite.Statement, error) {
		groups, err := s.ListGroups(ctx)
		if err != nil {
			return nil, err
		}
		orders := make(map[string]int, len(groups))
		for _, group := range groups {
			orders[group.ID] = group.SortOrder
		}
		var statements []sqlite.Statement
		setOrder := func(id string, index int) {
			if orders[id] != index {
				statements = append(statements, stmt(`UPDATE book_groups SET sort_order = ?, sort_ver_c = ?, sort_ver_i = ? WHERE id = ?`,
					index, v.Counter, v.InstallID, id))
			}
			delete(orders, id)
		}
		for index, id := range orderedIDs {
			if _, ok := orders[id]; !ok {
				return nil, fmt.Errorf("storage: invalid group order at %q", id)
			}
			setOrder(id, index)
		}
		index := len(orderedIDs)
		for _, group := range groups {
			if _, ok := orders[group.ID]; ok {
				setOrder(group.ID, index)
				index++
			}
		}
		return statements, nil
	})
}

func (s *Store) DissolveGroup(ctx context.Context, id string) error {
	return s.mutate(ctx, func(v domain.Version, now int64) ([]sqlite.Statement, error) {
		if err := s.requireActiveGroup(ctx, id); err != nil {
			return nil, err
		}
		return []sqlite.Statement{
			stmt(`UPDATE book_groups SET deleted = 1, deleted_at = ?, deleted_ver_c = ?, deleted_ver_i = ? WHERE id = ?`,
				now, v.Counter, v.InstallID, id),
			stmt(`UPDATE books SET group_id = '', group_ver_c = ?, group_ver_i = ? WHERE group_id = ? AND deleted = 0`,
				v.Counter, v.InstallID, id),
		}, nil
	})
}

func (s *Store) ListGroups(ctx context.Context) ([]domain.BookGroup, error) {
	return selectRows(ctx, s, decodeGroup, `SELECT `+groupColumns+` FROM book_groups WHERE deleted = 0 ORDER BY sort_order, id`)
}

func (s *Store) requireActiveGroup(ctx context.Context, id string) error {
	_, err := s.firstRow(ctx, `SELECT id FROM book_groups WHERE id = ? AND deleted = 0`, id)
	return err
}

const bookColumns = `id, title, author, dynasty_or_genre, cover_hash, content_hash, content_size,
	raw_filename, raw_size, poem_count, content_ver_c, content_ver_i, group_id, group_ver_c, group_ver_i,
	is_starred, starred_ver_c, starred_ver_i, sort_order, sort_ver_c, sort_ver_i, created_at,
	deleted, deleted_at, deleted_ver_c, deleted_ver_i`

func decodeBook(r row) (domain.Book, error) {
	return domain.Book{
		ID: r.text(0), Title: r.text(1), Author: r.text(2), DynastyOrGenre: r.text(3), CoverHash: r.text(4),
		ContentHash: r.text(5), ContentSize: r.integer(6), RawFilename: r.text(7), RawSize: r.integer(8),
		PoemCount: int(r.integer(9)), ContentVer: r.version(10), GroupID: r.text(12), GroupVer: r.version(13),
		IsStarred: r.flag(15), StarredVer: r.version(16), SortOrder: int(r.integer(18)), SortVer: r.version(19),
		CreatedAt: r.integer(21), Deleted: r.flag(22), DeletedAt: r.integer(23), DeletedVer: r.version(24),
	}, nil
}

const activeBookGroup = `EXISTS (SELECT 1 FROM book_groups WHERE book_groups.id = books.group_id AND book_groups.deleted = 0)`
const localBookColumns = bookColumns + `, local_content_hash, ` + activeBookGroup

func decodeLocalBook(r row) (domain.Book, error) {
	book, _ := decodeBook(r)
	// A group tombstone also hides assignments made on another device before it saw the dissolution.
	if !r.flag(27) {
		book.GroupID = ""
	}
	book.LocalContentHash = r.text(26)
	book.LocalState = domain.DeriveLocalBookState(book, book.LocalContentHash)
	return book, nil
}

// The shared metadata upsert leaves local_content_hash to the cache methods.
func putBook(b domain.Book) sqlite.Statement {
	return stmt(`INSERT INTO books (`+bookColumns+`) VALUES
		(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET title = excluded.title, author = excluded.author,
		dynasty_or_genre = excluded.dynasty_or_genre, cover_hash = excluded.cover_hash, content_hash = excluded.content_hash,
		content_size = excluded.content_size, raw_filename = excluded.raw_filename, raw_size = excluded.raw_size,
		poem_count = excluded.poem_count, content_ver_c = excluded.content_ver_c, content_ver_i = excluded.content_ver_i,
		group_id = excluded.group_id, group_ver_c = excluded.group_ver_c, group_ver_i = excluded.group_ver_i,
		is_starred = excluded.is_starred, starred_ver_c = excluded.starred_ver_c, starred_ver_i = excluded.starred_ver_i,
		sort_order = excluded.sort_order, sort_ver_c = excluded.sort_ver_c, sort_ver_i = excluded.sort_ver_i,
		created_at = excluded.created_at, deleted = excluded.deleted, deleted_at = excluded.deleted_at,
		deleted_ver_c = excluded.deleted_ver_c, deleted_ver_i = excluded.deleted_ver_i`,
		b.ID, b.Title, b.Author, b.DynastyOrGenre, b.CoverHash, b.ContentHash, b.ContentSize, b.RawFilename, b.RawSize,
		b.PoemCount, b.ContentVer.Counter, b.ContentVer.InstallID, b.GroupID, b.GroupVer.Counter, b.GroupVer.InstallID,
		b.IsStarred, b.StarredVer.Counter, b.StarredVer.InstallID, b.SortOrder, b.SortVer.Counter, b.SortVer.InstallID,
		b.CreatedAt, b.Deleted, b.DeletedAt, b.DeletedVer.Counter, b.DeletedVer.InstallID)
}

func (s *Store) UpsertImportedBook(ctx context.Context, book domain.Book, sp *domain.SPBook) error {
	return s.UpsertImportedBookWithUpload(ctx, book, sp, nil)
}

// UpsertImportedBookWithUpload 在同一 SQLite 事务内写入诗集元数据、正文、FTS 与待上传资产任务，
// 消除导书与并发同步导出之间的幽灵书目窗口（P0-3）。
func (s *Store) UpsertImportedBookWithUpload(ctx context.Context, book domain.Book, sp *domain.SPBook, upload *PendingUpload) error {
	content, err := replaceBookContent(book.ID, book.ContentHash, sp)
	if err != nil {
		return err
	}
	return s.mutate(ctx, func(v domain.Version, now int64) ([]sqlite.Statement, error) {
		r, err := s.firstRow(ctx, `SELECT `+bookColumns+` FROM books WHERE id = ?`, book.ID)
		switch {
		case errors.Is(err, ErrNotFound):
			if book.GroupID != "" {
				if err := s.requireActiveGroup(ctx, book.GroupID); err != nil {
					return nil, err
				}
			}
			book.GroupVer, book.StarredVer, book.SortVer = v, v, v
			if book.CreatedAt == 0 {
				book.CreatedAt = now
			}
		case err != nil:
			return nil, err
		default:
			current, _ := decodeBook(r)
			if current.Deleted {
				return nil, fmt.Errorf("storage: book %q was deleted", book.ID)
			}
			book.GroupID, book.GroupVer = current.GroupID, current.GroupVer
			book.IsStarred, book.StarredVer = current.IsStarred, current.StarredVer
			book.SortOrder, book.SortVer = current.SortOrder, current.SortVer
			book.CreatedAt = current.CreatedAt
		}
		book.ContentVer = v
		book.Deleted, book.DeletedAt, book.DeletedVer = false, 0, domain.Version{}
		stmts := append([]sqlite.Statement{putBook(book)}, content...)
		if upload != nil {
			u := *upload
			if u.CreatedAt == 0 {
				u.CreatedAt = now
			}
			u.UpdatedAt = now
			if u.State == "" {
				u.State = UploadPending
			}
			stmts = append(stmts, putPendingUploadStmt(u))
		}
		return stmts, nil
	})
}

func (s *Store) SaveBookContent(ctx context.Context, bookID, contentHash string, sp *domain.SPBook) error {
	statements, err := replaceBookContent(bookID, contentHash, sp)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.firstRow(ctx, `SELECT content_hash FROM books WHERE id = ? AND deleted = 0`, bookID)
	if err != nil {
		return err
	}
	if curHash := r.text(0); curHash != "" && contentHash != "" && curHash != contentHash {
		return fmt.Errorf("storage: stale content hash for book %q (want %s, got %s)", bookID, curHash, contentHash)
	}
	return s.transact(ctx, statements)
}

func (s *Store) ClearBookCache(ctx context.Context, bookID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transact(ctx, clearBookContent(bookID))
}

func clearBookContent(bookID string) []sqlite.Statement {
	return []sqlite.Statement{
		stmt(`DELETE FROM poems_fts WHERE book_id = ?`, bookID),
		stmt(`DELETE FROM poems WHERE book_id = ?`, bookID),
		stmt(`UPDATE books SET local_content_hash = '' WHERE id = ?`, bookID),
	}
}

func replaceBookContent(bookID, contentHash string, sp *domain.SPBook) ([]sqlite.Statement, error) {
	if bookID == "" || sp == nil || (sp.BookID != "" && sp.BookID != bookID) {
		return nil, errors.New("storage: content does not identify the target book")
	}
	statements := clearBookContent(bookID)
	for _, poem := range sp.Poems {
		var plain strings.Builder
		for _, stanza := range poem.Stanzas {
			for _, line := range stanza.Lines {
				plain.WriteString(line)
				plain.WriteByte('\n')
			}
		}
		text := strings.TrimSuffix(plain.String(), "\n")
		// These domain slices contain only JSON-compatible values.
		stanzas, _ := json.Marshal(poem.Stanzas)
		notes, _ := json.Marshal(poem.Notes)
		images, _ := json.Marshal(poem.Images)
		statements = append(statements,
			stmt(`INSERT INTO poems (`+poemColumns+`, plain_text) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				poem.ID, bookID, poem.OrderIndex, poem.EffectiveKind(), poem.Section, poem.Title, poem.Subtitle, poem.Author,
				poem.Epigraph, string(stanzas), poem.Colophon, string(notes), string(images), text),
			stmt(`INSERT INTO poems_fts (poem_id, book_id, title, author, plain_text) VALUES (?, ?, ?, ?, ?)`,
				poem.ID, bookID, ftsText(poem.Title), ftsText(poem.Author), ftsText(text)),
		)
	}
	return append(statements, stmt(`UPDATE books SET local_content_hash = ? WHERE id = ?`, contentHash, bookID)), nil
}

func (s *Store) SetBookGroup(ctx context.Context, bookID, groupID string) error {
	return s.mutate(ctx, func(v domain.Version, _ int64) ([]sqlite.Statement, error) {
		if err := s.requireActiveBook(ctx, bookID); err != nil {
			return nil, err
		}
		if groupID != "" {
			if err := s.requireActiveGroup(ctx, groupID); err != nil {
				return nil, err
			}
		}
		return []sqlite.Statement{stmt(`UPDATE books SET group_id = ?, group_ver_c = ?, group_ver_i = ? WHERE id = ?`,
			groupID, v.Counter, v.InstallID, bookID)}, nil
	})
}

// SetBooksGroup 在单次事务与单一 Lamport 版本内将多本未删除诗集批量移入指定分组（空串表示移回未分组）。
func (s *Store) SetBooksGroup(ctx context.Context, bookIDs []string, groupID string) error {
	seen := make(map[string]struct{}, len(bookIDs))
	unique := make([]string, 0, len(bookIDs))
	for _, id := range bookIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return nil
	}
	return s.mutate(ctx, func(v domain.Version, _ int64) ([]sqlite.Statement, error) {
		if groupID != "" {
			if err := s.requireActiveGroup(ctx, groupID); err != nil {
				return nil, err
			}
		}
		stmts := make([]sqlite.Statement, 0, len(unique))
		for _, bookID := range unique {
			stmts = append(stmts, stmt(
				`UPDATE books SET group_id = ?, group_ver_c = ?, group_ver_i = ? WHERE id = ? AND deleted = 0`,
				groupID, v.Counter, v.InstallID, bookID,
			))
		}
		return stmts, nil
	})
}

func (s *Store) SetBookStarred(ctx context.Context, bookID string, starred bool) error {
	return s.mutate(ctx, func(v domain.Version, _ int64) ([]sqlite.Statement, error) {
		if err := s.requireActiveBook(ctx, bookID); err != nil {
			return nil, err
		}
		return []sqlite.Statement{stmt(`UPDATE books SET is_starred = ?, starred_ver_c = ?, starred_ver_i = ? WHERE id = ?`,
			starred, v.Counter, v.InstallID, bookID)}, nil
	})
}

// UpdateBookParsedContentMeta 在重建并上传 .spbook.json.gz 后原子更新书籍正文哈希、体积与 ContentVer，
// 同步维护 local_content_hash 保持本机已下载态，并由 Lamport 合并广播给其他端。
func (s *Store) UpdateBookParsedContentMeta(ctx context.Context, bookID, contentHash string, contentSize int64) error {
	if bookID == "" || contentHash == "" || contentSize <= 0 {
		return errors.New("storage: invalid parsed content metadata")
	}
	return s.mutate(ctx, func(v domain.Version, _ int64) ([]sqlite.Statement, error) {
		if err := s.requireActiveBook(ctx, bookID); err != nil {
			return nil, err
		}
		return []sqlite.Statement{
			stmt(`UPDATE books SET content_hash = ?, content_size = ?, content_ver_c = ?, content_ver_i = ?, local_content_hash = ? WHERE id = ? AND deleted = 0`,
				contentHash, contentSize, v.Counter, v.InstallID, contentHash, bookID),
		}, nil
	})
}

func (s *Store) DeleteBook(ctx context.Context, bookID string) error {
	return s.mutate(ctx, func(v domain.Version, now int64) ([]sqlite.Statement, error) {
		err := s.requireActiveBook(ctx, bookID)
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return append(clearBookContent(bookID), stmt(
			`UPDATE books SET deleted = 1, deleted_at = ?, deleted_ver_c = ?, deleted_ver_i = ? WHERE id = ?`,
			now, v.Counter, v.InstallID, bookID)), nil
	})
}

func (s *Store) ListBooks(ctx context.Context, groupID string) ([]domain.Book, error) {
	const base = `SELECT ` + localBookColumns + ` FROM books WHERE deleted = 0`
	const order = ` ORDER BY sort_order, id`
	if groupID == "" {
		return selectRows(ctx, s, decodeLocalBook, base+order)
	}
	return selectRows(ctx, s, decodeLocalBook, base+` AND group_id = ? AND `+activeBookGroup+order, groupID)
}

func (s *Store) GetBook(ctx context.Context, bookID string) (domain.Book, error) {
	r, err := s.firstRow(ctx, `SELECT `+localBookColumns+` FROM books WHERE id = ? AND deleted = 0`, bookID)
	if err != nil {
		return domain.Book{}, err
	}
	return decodeLocalBook(r)
}

func (s *Store) requireActiveBook(ctx context.Context, bookID string) error {
	_, err := s.firstRow(ctx, `SELECT id FROM books WHERE id = ? AND deleted = 0`, bookID)
	return err
}

const poemColumns = `id, book_id, order_index, kind, section, title, subtitle, author, epigraph,
	content_json, colophon, notes_json, images_json`

func decodePoem(r row) (domain.Poem, error) {
	p := domain.Poem{
		ID: r.text(0), BookID: r.text(1), OrderIndex: int(r.integer(2)), Kind: domain.EntryKind(r.text(3)),
		Section: r.text(4), Title: r.text(5), Subtitle: r.text(6), Author: r.text(7), Epigraph: r.text(8), Colophon: r.text(10),
	}
	if err := json.Unmarshal([]byte(r.text(9)), &p.Stanzas); err != nil {
		return domain.Poem{}, err
	}
	if err := json.Unmarshal([]byte(r.text(11)), &p.Notes); err != nil {
		return domain.Poem{}, err
	}
	if err := json.Unmarshal([]byte(r.text(12)), &p.Images); err != nil {
		return domain.Poem{}, err
	}
	return p, nil
}

const activePoemBook = `EXISTS (SELECT 1 FROM books WHERE books.id = poems.book_id AND books.deleted = 0)`

func (s *Store) ListPoems(ctx context.Context, bookID string) ([]domain.Poem, error) {
	return selectRows(ctx, s, decodePoem, `SELECT `+poemColumns+` FROM poems WHERE book_id = ? AND `+activePoemBook+
		` ORDER BY order_index, id`, bookID)
}

func (s *Store) GetPoem(ctx context.Context, bookID, poemID string) (domain.Poem, error) {
	r, err := s.firstRow(ctx, `SELECT `+poemColumns+` FROM poems WHERE book_id = ? AND id = ? AND `+activePoemBook, bookID, poemID)
	if err != nil {
		return domain.Poem{}, err
	}
	return decodePoem(r)
}

func (s *Store) SearchPoems(ctx context.Context, query string, limit int) ([]domain.Poem, error) {
	if strings.TrimSpace(query) == "" {
		return []domain.Poem{}, ctx.Err()
	}
	if limit <= 0 {
		limit = -1
	}
	return selectRows(ctx, s, decodePoem, `SELECT `+poemColumns+` FROM poems
		JOIN (SELECT poem_id, book_id AS indexed_book_id, rank AS relevance FROM poems_fts WHERE poems_fts MATCH ?) AS matches
		ON matches.poem_id = poems.id AND matches.indexed_book_id = poems.book_id WHERE `+activePoemBook+
		` ORDER BY matches.relevance, poems.id LIMIT ?`, ftsPhrase(query), limit)
}
