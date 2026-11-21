package storage

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/egoist/mygo/plugins/sqlite"

	"stillpoets/internal/domain"
)

type QuoteFilter struct {
	Kind       domain.QuoteKind
	Author     string
	SourceBook string
	Query      string
	Limit      int
}

type QuoteView struct {
	Quote         domain.Quote
	BookActive    bool
	EffectiveKind domain.QuoteKind
	Deeplink      string
}

type QuoteFacets struct {
	SourceBooks []string
	Authors     []string
}

const quoteColumns = `id, kind, content, author, source_book, book_id, poem_id, poem_title, highlight_lines,
	import_source, tags_json, body_ver_c, body_ver_i, is_favorite, favorite_ver_c, favorite_ver_i,
	created_at, deleted, deleted_at, deleted_ver_c, deleted_ver_i`

func decodeQuote(r row) (domain.Quote, error) {
	q := domain.Quote{
		ID: r.text(0), Kind: domain.QuoteKind(r.text(1)), Content: r.text(2), Author: r.text(3), SourceBook: r.text(4),
		BookID: r.text(5), PoemID: r.text(6), PoemTitle: r.text(7), HighlightLines: r.text(8), ImportSource: r.text(9),
		BodyVer: r.version(11), IsFavorite: r.flag(13), FavoriteVer: r.version(14), CreatedAt: r.integer(16),
		Deleted: r.flag(17), DeletedAt: r.integer(18), DeletedVer: r.version(19),
	}
	if err := json.Unmarshal([]byte(r.text(10)), &q.Tags); err != nil {
		return domain.Quote{}, err
	}
	return q, nil
}

func putQuote(q domain.Quote) sqlite.Statement {
	tagsJSON := "[]"
	if len(q.Tags) > 0 {
		if b, err := json.Marshal(q.Tags); err == nil {
			tagsJSON = string(b)
		}
	}
	return stmt(`INSERT INTO quotes (`+quoteColumns+`) VALUES
		(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET kind = excluded.kind, content = excluded.content, author = excluded.author,
		source_book = excluded.source_book, book_id = excluded.book_id, poem_id = excluded.poem_id,
		poem_title = excluded.poem_title, highlight_lines = excluded.highlight_lines, import_source = excluded.import_source,
		tags_json = excluded.tags_json, body_ver_c = excluded.body_ver_c, body_ver_i = excluded.body_ver_i,
		is_favorite = excluded.is_favorite, favorite_ver_c = excluded.favorite_ver_c, favorite_ver_i = excluded.favorite_ver_i,
		created_at = excluded.created_at, deleted = excluded.deleted, deleted_at = excluded.deleted_at,
		deleted_ver_c = excluded.deleted_ver_c, deleted_ver_i = excluded.deleted_ver_i`,
		q.ID, q.Kind, q.Content, q.Author, q.SourceBook, q.BookID, q.PoemID, q.PoemTitle, q.HighlightLines, q.ImportSource,
		tagsJSON, q.BodyVer.Counter, q.BodyVer.InstallID, q.IsFavorite, q.FavoriteVer.Counter, q.FavoriteVer.InstallID,
		q.CreatedAt, q.Deleted, q.DeletedAt, q.DeletedVer.Counter, q.DeletedVer.InstallID)
}

func indexQuote(id, content, author, sourceBook string) sqlite.Statement {
	return stmt(`INSERT INTO quotes_fts (quote_id, content, author, source_book) VALUES (?, ?, ?, ?)`,
		id, ftsText(content), ftsText(author), ftsText(sourceBook))
}

func (s *Store) CreateStandaloneQuote(ctx context.Context, content, author, sourceBook, importSource string) (domain.Quote, error) {
	return s.createQuote(ctx, domain.Quote{
		Kind: domain.QuoteKindStandalone, Content: content, Author: author, SourceBook: sourceBook, ImportSource: importSource,
	})
}

func (s *Store) CreatePoemExcerptQuote(ctx context.Context, bookID, poemID, poemTitle, sourceBook, author, content, highlightLines string) (domain.Quote, error) {
	return s.createQuote(ctx, domain.Quote{
		Kind: domain.QuoteKindExcerpt, BookID: bookID, PoemID: poemID, PoemTitle: poemTitle, SourceBook: sourceBook,
		Author: author, Content: content, HighlightLines: highlightLines, ImportSource: "reader",
	})
}

func (s *Store) createQuote(ctx context.Context, q domain.Quote) (domain.Quote, error) {
	err := s.mutate(ctx, func(v domain.Version, now int64) ([]sqlite.Statement, error) {
		q.ID, q.CreatedAt, q.BodyVer = domain.NewID(), now, v
		return []sqlite.Statement{putQuote(q), indexQuote(q.ID, q.Content, q.Author, q.SourceBook)}, nil
	})
	if err != nil {
		return domain.Quote{}, err
	}
	return q, nil
}

func (s *Store) UpdateQuote(ctx context.Context, id, content, author, sourceBook string) error {
	return s.mutate(ctx, func(v domain.Version, _ int64) ([]sqlite.Statement, error) {
		if _, err := s.firstRow(ctx, `SELECT id FROM quotes WHERE id = ? AND deleted = 0`, id); err != nil {
			return nil, err
		}
		return []sqlite.Statement{
			stmt(`UPDATE quotes SET content = ?, author = ?, source_book = ?, body_ver_c = ?, body_ver_i = ? WHERE id = ?`,
				content, author, sourceBook, v.Counter, v.InstallID, id),
			stmt(`DELETE FROM quotes_fts WHERE quote_id = ?`, id),
			indexQuote(id, content, author, sourceBook),
		}, nil
	})
}

func (s *Store) DeleteQuote(ctx context.Context, id string) error {
	return s.mutate(ctx, func(v domain.Version, now int64) ([]sqlite.Statement, error) {
		_, err := s.firstRow(ctx, `SELECT id FROM quotes WHERE id = ? AND deleted = 0`, id)
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return []sqlite.Statement{
			stmt(`UPDATE quotes SET deleted = 1, deleted_at = ?, deleted_ver_c = ?, deleted_ver_i = ? WHERE id = ?`,
				now, v.Counter, v.InstallID, id),
			stmt(`DELETE FROM quotes_fts WHERE quote_id = ?`, id),
		}, nil
	})
}

func decodeQuoteView(r row) (QuoteView, error) {
	q, err := decodeQuote(r)
	if err != nil {
		return QuoteView{}, err
	}
	active := r.flag(21)
	return QuoteView{Quote: q, BookActive: active, EffectiveKind: q.EffectiveKind(active), Deeplink: q.WidgetDeeplink(active)}, nil
}

func (s *Store) ListQuotes(ctx context.Context, filter QuoteFilter) ([]QuoteView, error) {
	query := `SELECT ` + quoteColumns + `, active_book_id IS NOT NULL FROM quotes
		LEFT JOIN (SELECT id AS active_book_id FROM books WHERE deleted = 0) ON active_book_id = quotes.book_id
		WHERE quotes.deleted = 0`
	var args []any
	if filter.Kind != "" {
		// Match Quote.EffectiveKind before LIMIT, including legacy excerpts with no stored kind.
		query += ` AND (CASE WHEN active_book_id IS NOT NULL AND book_id <> '' AND poem_id <> '' AND kind IN ('excerpt', '')
			THEN 'excerpt' ELSE 'standalone' END) = ?`
		args = append(args, filter.Kind)
	}
	if filter.Author != "" {
		query += ` AND author = ?`
		args = append(args, filter.Author)
	}
	if filter.SourceBook != "" {
		query += ` AND source_book = ?`
		args = append(args, filter.SourceBook)
	}
	if strings.TrimSpace(filter.Query) != "" {
		query += ` AND id IN (SELECT quote_id FROM quotes_fts WHERE quotes_fts MATCH ?)`
		args = append(args, ftsPhrase(filter.Query))
	}
	query += ` ORDER BY created_at DESC, id DESC`
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}
	return selectRows(ctx, s, decodeQuoteView, query, args...)
}

func (s *Store) ListQuoteFacets(ctx context.Context) (QuoteFacets, error) {
	rows, err := s.db.Query(ctx, `SELECT 0 AS facet, source_book AS value FROM quotes WHERE deleted = 0 AND source_book <> ''
		UNION SELECT 1, author FROM quotes WHERE deleted = 0 AND author <> '' ORDER BY facet, value`)
	if err != nil {
		return QuoteFacets{}, err
	}
	facets := QuoteFacets{SourceBooks: []string{}, Authors: []string{}}
	for _, values := range rows.Values {
		r := row(values)
		if r.flag(0) {
			facets.Authors = append(facets.Authors, r.text(1))
		} else {
			facets.SourceBooks = append(facets.SourceBooks, r.text(1))
		}
	}
	return facets, nil
}
