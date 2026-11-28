package sync

import (
	"strings"
	"testing"

	"stillpoets/internal/domain"
)

func TestBookParsedPathForAndValidation(t *testing.T) {
	legacy := domain.Book{ID: "book-1"}
	got, err := BookParsedPathFor(legacy)
	if err != nil || got != "books/book-1/parsed.spbook.json.gz" {
		t.Fatalf("legacy BookParsedPathFor = %q, %v", got, err)
	}

	built := domain.Book{ID: "book-1", ContentPath: BookBuildParsedRelPath("a1b2c3")}
	got, err = BookParsedPathFor(built)
	if err != nil || got != "books/book-1/builds/a1b2c3/parsed.spbook.json.gz" {
		t.Fatalf("built BookParsedPathFor = %q, %v", got, err)
	}

	for _, bad := range []string{"../evil.gz", "/abs/path.gz", `builds\a\parsed.gz`, "builds/../x.gz"} {
		if _, err := NormalizeBookContentPath(bad); err == nil {
			t.Fatalf("expected error for invalid contentPath %q", bad)
		}
	}
}

func TestInboxItemRoundTripAndValidation(t *testing.T) {
	validSHA := strings.Repeat("a", 64)
	item := InboxItem{
		ImportID:         "bk-01",
		BookID:           "bk-01",
		OriginalFilename: "海子诗选.epub",
		RawFilename:      "海子诗选_1a2b3c.epub",
		RawSize:          1024,
		RawSHA256:        strings.ToUpper(validSHA),
		GroupID:          "grp-1",
		InitialGroupVer:  domain.Version{Counter: 3, InstallID: "tm-abc"},
		SubmittedAt:      1700000000000,
		Source:           "zlibrary",
	}
	raw, err := EncodeInboxItem(item)
	if err != nil {
		t.Fatalf("EncodeInboxItem: %v", err)
	}
	decoded, err := DecodeInboxItem(raw, "bk-01")
	if err != nil {
		t.Fatalf("DecodeInboxItem: %v", err)
	}
	if decoded.RawSHA256 != validSHA || decoded.SchemaVersion != InboxSchemaVersion || decoded.ParserRuleVersion != InboxParserRuleVersion {
		t.Fatalf("decoded unexpected: %+v", decoded)
	}

	if _, err := DecodeInboxItem(raw, "other-id"); err == nil {
		t.Fatal("expected importId mismatch error")
	}

	futureRule := item
	futureRule.SchemaVersion = InboxSchemaVersion
	futureRule.ParserRuleVersion = InboxParserRuleVersion + 1
	if err := ValidateInboxItem(futureRule, "bk-01"); err == nil {
		t.Fatal("expected unsupported parserRuleVersion error")
	}

	badPath := item
	badPath.SchemaVersion = InboxSchemaVersion
	badPath.ParserRuleVersion = InboxParserRuleVersion
	badPath.RawFilename = "../escape.epub"
	if err := ValidateInboxItem(badPath, "bk-01"); err == nil {
		t.Fatal("expected path traversal rejection for rawFilename")
	}
}

func TestSparseWebSnapshotMergesWithoutDroppingOtherCollections(t *testing.T) {
	mobile := SnapshotData{
		Groups: []domain.BookGroup{
			{ID: "g1", Name: "现代诗", NameVer: domain.Version{Counter: 1, InstallID: "ios-1"}, SortOrder: 0, SortVer: domain.Version{Counter: 1, InstallID: "ios-1"}, CreatedAt: 100},
		},
		Books: []domain.Book{
			{
				ID: "b1", Title: "海子诗全集", Author: "海子",
				ContentHash: "hash-v1", ContentSize: 100, ContentPath: "builds/abc/parsed.spbook.json.gz",
				RawFilename: "haizi_1.epub", PoemCount: 10,
				ContentVer: domain.Version{Counter: 2, InstallID: "ios-1"},
				GroupID:    "g1", GroupVer: domain.Version{Counter: 2, InstallID: "ios-1"},
				IsStarred: false, StarredVer: domain.Version{Counter: 2, InstallID: "ios-1"},
				CreatedAt: 100,
			},
		},
		Favorites: []domain.FavoritePoem{
			{BookID: "b1", PoemID: "b1:0001", PoemTitle: "面朝大海", Active: true, ActiveVer: domain.Version{Counter: 3, InstallID: "ios-1"}, Excerpt: "从明天起"},
		},
		Quotes: []domain.Quote{
			{ID: "q1", Content: "做一个幸福的人", BodyVer: domain.Version{Counter: 4, InstallID: "ios-1"}},
		},
		Progress: []domain.ReadingProgress{
			{BookID: "b1", LastPoemID: "b1:0002", PositionVer: domain.Version{Counter: 5, InstallID: "ios-1"}, ReadPoemIDs: []string{"b1:0001", "b1:0002"}},
		},
		ReadingEvents: []domain.ReadingEvent{
			{ID: "ev1", BookID: "b1", PoemID: "b1:0001", ReadDate: "2026-11-28", ReadHour: 21, ReadAt: 1700000100000},
		},
	}

	// 模拟油猴 2A 累计稀疏快照：仅携带 groups + books，其余 4 个集合留空。
	webSparse := SnapshotData{
		Groups: []domain.BookGroup{
			{ID: "g1", Name: "现代诗", NameVer: domain.Version{Counter: 1, InstallID: "ios-1"}, SortOrder: 0, SortVer: domain.Version{Counter: 1, InstallID: "ios-1"}, CreatedAt: 100},
			{ID: "g2", Name: "外国诗", NameVer: domain.Version{Counter: 6, InstallID: "tm-web1"}, SortOrder: 1, SortVer: domain.Version{Counter: 6, InstallID: "tm-web1"}, CreatedAt: 200},
		},
		Books: []domain.Book{
			{
				ID: "b1", Title: "海子诗全集", Author: "海子",
				ContentHash: "hash-v1", ContentSize: 100, ContentPath: "builds/abc/parsed.spbook.json.gz",
				RawFilename: "haizi_1.epub", PoemCount: 10,
				ContentVer: domain.Version{Counter: 2, InstallID: "ios-1"},
				GroupID:    "g2", GroupVer: domain.Version{Counter: 7, InstallID: "tm-web1"},
				IsStarred: true, StarredVer: domain.Version{Counter: 7, InstallID: "tm-web1"},
				CreatedAt: 100,
			},
		},
	}

	merged := MergeSnapshotData(mobile, webSparse)
	if len(merged.Groups) != 2 {
		t.Fatalf("merged groups = %d, want 2", len(merged.Groups))
	}
	if len(merged.Books) != 1 || merged.Books[0].GroupID != "g2" || !merged.Books[0].IsStarred || merged.Books[0].ContentPath != "builds/abc/parsed.spbook.json.gz" {
		t.Fatalf("merged book = %+v", merged.Books)
	}
	if len(merged.Favorites) != 1 || len(merged.Quotes) != 1 || len(merged.Progress) != 1 || len(merged.ReadingEvents) != 1 {
		t.Fatalf("sparse merge dropped other collections: fav=%d quotes=%d prog=%d events=%d",
			len(merged.Favorites), len(merged.Quotes), len(merged.Progress), len(merged.ReadingEvents))
	}
}
