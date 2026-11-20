package sync

import (
	"testing"

	"stillpoets/internal/domain"
)

func TestMergeBooksPreservesLocalStateAndMarksStale(t *testing.T) {
	local := []domain.Book{
		{
			ID:          "b1",
			Title:       "海子诗全集",
			ContentHash: "hash_v1",
			LocalState:  domain.LocalStateDownloaded,
			UpdatedAt:   100,
		},
		{
			ID:          "b2",
			Title:       "顾城诗选",
			ContentHash: "hash_g1",
			LocalState:  domain.LocalStateDownloaded,
			UpdatedAt:   200,
		},
	}
	remote := []domain.Book{
		{
			ID:          "b1",
			Title:       "海子诗全集（修订）",
			ContentHash: "hash_v2", // 远端正文更新
			UpdatedAt:   150,
		},
		{
			ID:          "b3",
			Title:       "里尔克诗选",
			ContentHash: "hash_r1",
			UpdatedAt:   300,
		},
	}

	got := MergeBooks(local, remote)
	if len(got) != 3 {
		t.Fatalf("expected 3 merged books, got %d", len(got))
	}

	byID := map[string]domain.Book{}
	for _, b := range got {
		byID[b.ID] = b
	}

	if byID["b1"].LocalState != domain.LocalStateStale {
		t.Fatalf("b1 local state = %q, want stale", byID["b1"].LocalState)
	}
	if byID["b1"].Title != "海子诗全集（修订）" {
		t.Fatalf("b1 title = %q, want updated remote title", byID["b1"].Title)
	}
	if byID["b2"].LocalState != domain.LocalStateDownloaded {
		t.Fatalf("b2 local state = %q, want downloaded", byID["b2"].LocalState)
	}
	if byID["b3"].LocalState != domain.LocalStateRemote {
		t.Fatalf("b3 local state = %q, want remote (lazy download)", byID["b3"].LocalState)
	}
}

func TestSPBookEncodeDecodeRoundTrip(t *testing.T) {
	orig := &domain.SPBook{
		BookID:       "b1",
		Title:        "海子诗选",
		Author:       "海子",
		SourceFormat: "epub",
		ParsedAt:     1763654400,
		Poems: []domain.Poem{
			{
				ID:         "p1",
				BookID:     "b1",
				OrderIndex: 0,
				Title:      "面朝大海，春暖花开",
				Stanzas: []domain.Stanza{
					{Lines: []string{"从明天起，做一个幸福的人", "喂马、劈柴，周游世界"}},
				},
			},
		},
	}

	compressed, hash, err := EncodeSPBook(orig)
	if err != nil {
		t.Fatalf("EncodeSPBook failed: %v", err)
	}
	decoded, err := DecodeSPBook(compressed, hash)
	if err != nil {
		t.Fatalf("DecodeSPBook failed: %v", err)
	}
	if decoded.Title != orig.Title || len(decoded.Poems) != 1 || decoded.Poems[0].Title != "面朝大海，春暖花开" {
		t.Fatalf("decoded mismatch: %+v", decoded)
	}
	if _, err := DecodeSPBook(compressed, "bad_hash"); err == nil {
		t.Fatalf("expected hash mismatch error")
	}
}
