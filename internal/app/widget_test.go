package app

import (
	"strings"
	"testing"

	"stillpoets/internal/domain"
)

func TestBuildSinglePoemWidgetEntryLargeText(t *testing.T) {
	book := domain.Book{ID: "b1", Title: "海子诗选", Author: "海子"}
	poem := domain.Poem{
		ID:     "p1",
		Title:  "亚洲铜",
		Author: "海子",
		Kind:   domain.EntryKindPoem,
		Stanzas: []domain.Stanza{
			{Lines: []string{"亚洲铜，亚洲铜", "祖父死在这里，父亲死在这里，我也会死在这里"}},
			{Lines: []string{"你是唯一的一块埋人的地方", "亚洲铜，亚洲铜", "爱怀疑和飞翔的是鸟，淹没一切的是海水"}},
			{Lines: []string{"你的主人却是青草，住在自己细小的腰上，守住野花的手掌和秘密"}},
		},
	}

	t.Run("default poem fills both displayText and multi-stanza largeText", func(t *testing.T) {
		entry, ok := buildSinglePoemWidgetEntry(book, poem, domain.FavoritePoem{}, false)
		if !ok {
			t.Fatalf("expected ok")
		}
		if len(strings.Split(entry.DisplayText, "\n")) != 2 {
			t.Fatalf("expected 2-line compact displayText, got %q", entry.DisplayText)
		}
		if !strings.Contains(entry.LargeText, "爱怀疑和飞翔的是鸟") || !strings.Contains(entry.LargeText, "\n\n") {
			t.Fatalf("expected multi-stanza largeText, got %q", entry.LargeText)
		}
	})

	t.Run("default heart favorite without highlight lines still expands largeText", func(t *testing.T) {
		fav := domain.FavoritePoem{
			BookID:  book.ID,
			PoemID:  poem.ID,
			Excerpt: "亚洲铜，亚洲铜\n祖父死在这里，父亲死在这里，我也会死在这里",
		}
		entry, ok := buildSinglePoemWidgetEntry(book, poem, fav, true)
		if !ok {
			t.Fatalf("expected ok")
		}
		if !strings.Contains(entry.LargeText, "你的主人却是青草") {
			t.Fatalf("expected auto-excerpt favorite to populate largeText, got %q", entry.LargeText)
		}
	})

	t.Run("manual highlighted quote keeps user selection", func(t *testing.T) {
		fav := domain.FavoritePoem{
			BookID:         book.ID,
			PoemID:         poem.ID,
			Excerpt:        "爱怀疑和飞翔的是鸟，淹没一切的是海水",
			HighlightLines: "4-4",
		}
		entry, ok := buildSinglePoemWidgetEntry(book, poem, fav, true)
		if !ok {
			t.Fatalf("expected ok")
		}
		if entry.DisplayText != fav.Excerpt {
			t.Fatalf("expected manual excerpt %q, got %q", fav.Excerpt, entry.DisplayText)
		}
		if entry.LargeText != "" {
			t.Fatalf("expected manual quote not overwritten by largeText, got %q", entry.LargeText)
		}
		if !strings.Contains(entry.Deeplink, "lines=4-4") {
			t.Fatalf("expected highlight deeplink, got %q", entry.Deeplink)
		}
	})
}
