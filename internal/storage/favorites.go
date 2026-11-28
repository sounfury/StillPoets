package storage

import (
	"context"
	"errors"
	"strconv"

	"github.com/egoist/mygo/plugins/sqlite"

	"stillpoets/internal/domain"
)

const favoriteColumns = `id, book_id, poem_id, poem_title, author, active, active_ver_c, active_ver_i,
	deleted_at, excerpt, highlight_lines, is_custom_quote, excerpt_ver_c, excerpt_ver_i, created_at`

func decodeFavorite(r row) (domain.FavoritePoem, error) {
	return domain.FavoritePoem{
		ID: r.text(0), BookID: r.text(1), PoemID: r.text(2), PoemTitle: r.text(3), Author: r.text(4),
		Active: r.flag(5), ActiveVer: r.version(6), DeletedAt: r.integer(8), Excerpt: r.text(9),
		HighlightLines: r.text(10), IsCustomQuote: r.flag(11), ExcerptVer: r.version(12), CreatedAt: r.integer(14),
	}, nil
}

func putFavorite(f domain.FavoritePoem) sqlite.Statement {
	if f.ID == "" {
		// Older snapshots may omit the auxiliary ID. The length makes the pair unambiguous.
		f.ID = strconv.Itoa(len(f.BookID)) + ":" + f.BookID + f.PoemID
	}
	return stmt(`INSERT INTO favorite_poems (`+favoriteColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(book_id, poem_id) DO UPDATE SET id = excluded.id, poem_title = excluded.poem_title,
		author = excluded.author, active = excluded.active, active_ver_c = excluded.active_ver_c,
		active_ver_i = excluded.active_ver_i, deleted_at = excluded.deleted_at, excerpt = excluded.excerpt,
		highlight_lines = excluded.highlight_lines, is_custom_quote = excluded.is_custom_quote,
		excerpt_ver_c = excluded.excerpt_ver_c, excerpt_ver_i = excluded.excerpt_ver_i, created_at = excluded.created_at`,
		f.ID, f.BookID, f.PoemID, f.PoemTitle, f.Author, f.Active, f.ActiveVer.Counter, f.ActiveVer.InstallID,
		f.DeletedAt, f.Excerpt, f.HighlightLines, f.IsCustomQuote, f.ExcerptVer.Counter, f.ExcerptVer.InstallID, f.CreatedAt)
}

// FavoritePoemInput 封装收藏与自定义金句节选参数。
type FavoritePoemInput struct {
	BookID         string
	PoemID         string
	PoemTitle      string
	Author         string
	Excerpt        string
	HighlightLines string
	Active         bool
}

func (s *Store) upsertFavoritePoem(
	ctx context.Context,
	in FavoritePoemInput,
	apply func(fav *domain.FavoritePoem, v domain.Version, now int64, created bool),
) (domain.FavoritePoem, error) {
	var favorite domain.FavoritePoem
	err := s.mutate(ctx, func(v domain.Version, now int64) ([]sqlite.Statement, error) {
		if err := s.requireActiveBook(ctx, in.BookID); err != nil {
			return nil, err
		}
		r, err := s.firstRow(ctx, `SELECT `+favoriteColumns+` FROM favorite_poems WHERE book_id = ? AND poem_id = ?`, in.BookID, in.PoemID)
		created := false
		switch {
		case errors.Is(err, ErrNotFound):
			created = true
			favorite = domain.FavoritePoem{
				ID: domain.NewID(), BookID: in.BookID, PoemID: in.PoemID, CreatedAt: now,
			}
		case err != nil:
			return nil, err
		default:
			favorite, _ = decodeFavorite(r)
		}
		if in.PoemTitle != "" {
			favorite.PoemTitle = in.PoemTitle
		}
		if in.Author != "" {
			favorite.Author = in.Author
		}
		apply(&favorite, v, now, created)
		return []sqlite.Statement{putFavorite(favorite)}, nil
	})
	if err != nil {
		return domain.FavoritePoem{}, err
	}
	return favorite, nil
}

func (s *Store) ToggleFavoritePoem(ctx context.Context, in FavoritePoemInput) (domain.FavoritePoem, error) {
	return s.upsertFavoritePoem(ctx, in, func(favorite *domain.FavoritePoem, v domain.Version, now int64, created bool) {
		if created {
			favorite.Excerpt = in.Excerpt
			favorite.ExcerptVer = v
		} else if in.Active && !favorite.IsCustomQuote && favorite.Excerpt != in.Excerpt {
			favorite.Excerpt, favorite.HighlightLines, favorite.ExcerptVer = in.Excerpt, "", v
		}
		favorite.Active, favorite.ActiveVer = in.Active, v
		favorite.DeletedAt = 0
		if !in.Active {
			favorite.DeletedAt = now
		}
	})
}

func (s *Store) UpdateFavoriteExcerpt(ctx context.Context, bookID, poemID, excerpt, highlightLines string, isCustom bool) error {
	return s.mutate(ctx, func(v domain.Version, _ int64) ([]sqlite.Statement, error) {
		if _, err := s.firstRow(ctx, `SELECT id FROM favorite_poems WHERE book_id = ? AND poem_id = ?`, bookID, poemID); err != nil {
			return nil, err
		}
		return []sqlite.Statement{stmt(`UPDATE favorite_poems SET excerpt = ?, highlight_lines = ?, is_custom_quote = ?,
			excerpt_ver_c = ?, excerpt_ver_i = ? WHERE book_id = ? AND poem_id = ?`,
			excerpt, highlightLines, isCustom, v.Counter, v.InstallID, bookID, poemID)}, nil
	})
}

// SetFavoriteCustomExcerpt 在同一事务内原子确保收藏激活并更新手动金句节选，避免两步落库产生中间态（P1-5）。
func (s *Store) SetFavoriteCustomExcerpt(ctx context.Context, in FavoritePoemInput) (domain.FavoritePoem, error) {
	return s.upsertFavoritePoem(ctx, in, func(favorite *domain.FavoritePoem, v domain.Version, _ int64, _ bool) {
		favorite.Active = true
		favorite.ActiveVer = v
		favorite.DeletedAt = 0
		favorite.Excerpt = in.Excerpt
		favorite.HighlightLines = in.HighlightLines
		favorite.IsCustomQuote = true
		favorite.ExcerptVer = v
	})
}

const activeFavorite = `active = 1 AND EXISTS (SELECT 1 FROM books WHERE books.id = favorite_poems.book_id AND books.deleted = 0)`

func (s *Store) ListFavorites(ctx context.Context, bookID string) ([]domain.FavoritePoem, error) {
	const base = `SELECT ` + favoriteColumns + ` FROM favorite_poems WHERE ` + activeFavorite
	const order = ` ORDER BY book_id, poem_id`
	if bookID == "" {
		return selectRows(ctx, s, decodeFavorite, base+order)
	}
	return selectRows(ctx, s, decodeFavorite, base+` AND book_id = ?`+order, bookID)
}
