package storage

import (
	"context"
	"errors"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"stillpoets/internal/domain"
	booksync "stillpoets/internal/sync"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.Context(), ":memory:")
	must(t, err)
	t.Cleanup(func() { must(t, s.Close()) })
	return s
}

func bookFixture(id string) (domain.Book, *domain.SPBook) {
	book := domain.Book{ID: id, Title: "夜色", Author: "山人", DynastyOrGenre: "现代", CoverHash: "cover",
		ContentHash: "hash-" + id, ContentSize: 42, RawFilename: "夜色.epub", RawSize: 92, PoemCount: 2}
	sp := &domain.SPBook{BookID: id, Poems: []domain.Poem{
		{ID: id + "-p2", BookID: id, OrderIndex: 2, Kind: domain.EntryKindPoem, Title: "深林", Stanzas: []domain.Stanza{{Lines: []string{"quiet forest"}}}},
		{ID: id + "-p1", BookID: id, OrderIndex: 1, Kind: domain.EntryKindPoem, Section: "第一卷", Title: "山间", Subtitle: "夜读",
			Author: "山人", Epigraph: "题记", Stanzas: []domain.Stanza{{Lines: []string{"明月照山间", "又见远山"}}}, Colophon: "落款",
			Notes: []domain.Note{{ID: "n1", Label: "[1]", Body: "注"}}, Images: []domain.ImageRef{{Src: "a.png", Width: 20}}},
		{ID: id + "-intro", BookID: id, OrderIndex: 0, Kind: domain.EntryKindProse, Title: "序"},
	}}
	return book, sp
}

func importTestBook(t *testing.T, s *Store, id string) (domain.Book, *domain.SPBook) {
	t.Helper()
	book, sp := bookFixture(id)
	must(t, s.UpsertImportedBook(t.Context(), book, sp))
	return book, sp
}

func TestStoreReopenSettingsAndPendingUploads(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "library.db")
	s, err := Open(ctx, path)
	must(t, err)
	t.Cleanup(func() { must(t, s.Close()) })
	_, meta, err := s.ExportSnapshot(ctx)
	must(t, err)
	if !booksync.IsValidInstallID(meta.InstallID) || meta.Dirty || meta.LamportCounter != 0 {
		t.Fatalf("initial sync metadata = %+v", meta)
	}
	if _, err := s.db.Execute(ctx, `INSERT INTO poems (id, book_id, order_index, title, content_json, plain_text)
		VALUES ('orphan', 'missing', 0, '', '[]', '')`); err == nil {
		t.Fatal("foreign key accepted an orphan poem")
	}
	settings, err := s.LoadSettings(ctx)
	must(t, err)
	if settings.FontSize <= 0 || settings.LineHeight <= 0 || settings.WidgetMode == "" {
		t.Fatalf("settings defaults = %+v", settings)
	}
	settings.FontSize = 24
	settings.WidgetMode = domain.WidgetModeQuotesRandom
	must(t, s.SaveSettings(ctx, settings))
	queued := PendingUpload{
		BookID: "b", RawFilename: "夜色.epub", LocalRawPath: "raw.epub", LocalSPBookPath: "parsed.gz",
		LocalCoverPath: "cover.webp", ContentHash: "hash-b", CreatedAt: 1,
	}
	must(t, s.UpsertPendingUpload(ctx, queued))
	entries, err := s.ListPendingUploads(ctx)
	must(t, err)
	if len(entries) != 1 || entries[0].State != UploadPending || entries[0].UpdatedAt <= 0 {
		t.Fatalf("queued upload = %+v", entries)
	}
	queued = entries[0]
	queued.LocalSPBookPath = "retry.gz"
	must(t, s.UpsertPendingUpload(ctx, queued))
	must(t, s.UpdatePendingUploadState(ctx, "b", UploadFailed, "connection reset"))
	must(t, s.Close())

	reopened, err := Open(ctx, path)
	must(t, err)
	t.Cleanup(func() { must(t, reopened.Close()) })
	_, persisted, err := reopened.ExportSnapshot(ctx)
	must(t, err)
	if !reflect.DeepEqual(persisted, meta) {
		t.Fatalf("sync metadata after local-only writes = %+v, want %+v", persisted, meta)
	}
	gotSettings, err := reopened.LoadSettings(ctx)
	must(t, err)
	if gotSettings != settings {
		t.Fatalf("reopened settings = %+v, want %+v", gotSettings, settings)
	}
	entries, err = reopened.ListPendingUploads(ctx)
	must(t, err)
	if len(entries) != 1 || entries[0].State != UploadFailed || entries[0].LastError != "connection reset" ||
		entries[0].LocalSPBookPath != "retry.gz" || entries[0].LocalCoverPath != "cover.webp" || entries[0].CreatedAt != 1 {
		t.Fatalf("reopened queue = %+v", entries)
	}
	must(t, reopened.UpdatePendingUploadState(ctx, "b", UploadAssetsVerified, ""))
	must(t, reopened.DeletePendingUpload(ctx, "b"))
	entries, err = reopened.ListPendingUploads(ctx)
	must(t, err)
	if len(entries) != 0 {
		t.Fatalf("queue after delete = %+v", entries)
	}
}

func TestStoreGroupsAndBookShelf(t *testing.T) {
	s, ctx := openTestStore(t), t.Context()
	first, err := s.CreateGroup(ctx, "第一组")
	must(t, err)
	second, err := s.CreateGroup(ctx, "第二组")
	must(t, err)
	must(t, s.RenameGroup(ctx, first.ID, "改名"))
	must(t, s.ReorderGroups(ctx, []string{second.ID, first.ID}))
	groups, err := s.ListGroups(ctx)
	must(t, err)
	if len(groups) != 2 || groups[0].ID != second.ID || groups[1].Name != "改名" ||
		!groups[1].NameVer.After(first.NameVer) || !groups[1].SortVer.After(first.SortVer) {
		t.Fatalf("ordered groups = %+v", groups)
	}
	book, sp := importTestBook(t, s, "b")
	must(t, s.SetBookGroup(ctx, book.ID, first.ID))
	must(t, s.SetBookStarred(ctx, book.ID, true))
	before, err := s.GetBook(ctx, book.ID)
	must(t, err)
	book.Title = "再导入"
	must(t, s.UpsertImportedBook(ctx, book, sp))
	grouped, err := s.ListBooks(ctx, first.ID)
	must(t, err)
	if len(grouped) != 1 || grouped[0].Title != book.Title || grouped[0].GroupVer != before.GroupVer ||
		grouped[0].StarredVer != before.StarredVer || !grouped[0].IsStarred {
		t.Fatalf("reimported shelf book = %+v", grouped)
	}
	must(t, s.DissolveGroup(ctx, first.ID))
	groups, err = s.ListGroups(ctx)
	must(t, err)
	after, err := s.GetBook(ctx, book.ID)
	must(t, err)
	if len(groups) != 1 || groups[0].ID != second.ID || after.GroupID != "" ||
		!after.GroupVer.After(before.GroupVer) || after.StarredVer != before.StarredVer {
		t.Fatalf("groups after dissolve = %+v, book = %+v", groups, after)
	}
	data, _, err := s.ExportSnapshot(ctx)
	must(t, err)
	for _, group := range data.Groups {
		if group.ID == first.ID && group.Deleted && group.DeletedVer == after.GroupVer && group.DeletedAt > 0 {
			return
		}
	}
	t.Fatalf("group tombstone absent from export: %+v", data.Groups)
}

func TestStoreGroupDissolutionHidesConcurrentAssignment(t *testing.T) {
	left, right, ctx := openTestStore(t), openTestStore(t), t.Context()
	group, err := left.CreateGroup(ctx, "旧分组")
	must(t, err)
	book, _ := importTestBook(t, left, "b")
	shared, leftMeta, err := left.ExportSnapshot(ctx)
	must(t, err)
	must(t, right.ApplyMergedSnapshot(ctx, shared, 0, leftMeta.LamportCounter, 0, false))
	must(t, left.DissolveGroup(ctx, group.ID))
	must(t, right.SetBookGroup(ctx, book.ID, group.ID))
	local, localMeta, err := left.ExportSnapshot(ctx)
	must(t, err)
	remote, remoteMeta, err := right.ExportSnapshot(ctx)
	must(t, err)
	merged := booksync.MergeSnapshotData(local, remote)
	must(t, left.ApplyMergedSnapshot(ctx, merged, localMeta.DirtySeq, remoteMeta.LamportCounter, 0, false))
	got, err := left.GetBook(ctx, book.ID)
	must(t, err)
	byGroup, err := left.ListBooks(ctx, group.ID)
	must(t, err)
	export, _, err := left.ExportSnapshot(ctx)
	must(t, err)
	if got.GroupID != "" || len(byGroup) != 0 || export.Books[0].GroupID != group.ID {
		t.Fatalf("dissolved group assignment: visible=%+v byGroup=%+v export=%+v", got, byGroup, export.Books)
	}
}

func TestStoreBookContentAndFTS(t *testing.T) {
	s, ctx := openTestStore(t), t.Context()
	book, sp := importTestBook(t, s, "b")
	got, err := s.GetBook(ctx, book.ID)
	must(t, err)
	if got.LocalState != domain.LocalStateDownloaded || got.LocalContentHash != book.ContentHash || got.PoemCount != 2 ||
		got.RawFilename != book.RawFilename || got.RawSize != book.RawSize || got.DynastyOrGenre != book.DynastyOrGenre {
		t.Fatalf("book after import = %+v", got)
	}
	poems, err := s.ListPoems(ctx, book.ID)
	must(t, err)
	if len(poems) != 3 || poems[0].Kind != domain.EntryKindProse || !reflect.DeepEqual(poems[1], sp.Poems[1]) || poems[2].ID != "b-p2" {
		t.Fatalf("poems after import = %+v", poems)
	}
	poem, err := s.GetPoem(ctx, book.ID, "b-p1")
	must(t, err)
	if !reflect.DeepEqual(poem, sp.Poems[1]) {
		t.Fatalf("poem round trip = %+v", poem)
	}
	for _, query := range []string{"明月", `("forest":*`} {
		found, err := s.SearchPoems(ctx, query, 1)
		must(t, err)
		if len(found) != 1 {
			t.Fatalf("search %q = %+v", query, found)
		}
	}
	for _, query := range []string{"", `)("*`, "missing"} {
		found, err := s.SearchPoems(ctx, query, 10)
		must(t, err)
		if found == nil || len(found) != 0 {
			t.Fatalf("empty search %q = %+v", query, found)
		}
	}
	_, err = s.ToggleFavoritePoem(ctx, book.ID, poem.ID, poem.Title, poem.Author, "摘句", true)
	must(t, err)
	_, err = s.CreatePoemExcerptQuote(ctx, book.ID, poem.ID, poem.Title, book.Title, poem.Author, "摘句", "1-1")
	must(t, err)
	_, err = s.RecordPoemRead(ctx, book.ID, poem.ID, 0.3, book.PoemCount)
	must(t, err)
	_, before, err := s.ExportSnapshot(ctx)
	must(t, err)
	must(t, s.ClearBookCache(ctx, book.ID))
	got, err = s.GetBook(ctx, book.ID)
	must(t, err)
	poems, err = s.ListPoems(ctx, book.ID)
	must(t, err)
	found, err := s.SearchPoems(ctx, "明月", 10)
	must(t, err)
	favorites, err := s.ListFavorites(ctx, book.ID)
	must(t, err)
	quotes, err := s.ListQuotes(ctx, QuoteFilter{Kind: domain.QuoteKindExcerpt})
	must(t, err)
	progress, err := s.GetProgress(ctx, book.ID)
	must(t, err)
	if got.LocalState != domain.LocalStateRemote || got.LocalContentHash != "" || len(poems) != 0 || len(found) != 0 ||
		len(favorites) != 1 || len(quotes) != 1 || progress.LastPoemID != poem.ID {
		t.Fatalf("clear cache: book=%+v poems=%d hits=%d favorites=%d quotes=%d progress=%+v",
			got, len(poems), len(found), len(favorites), len(quotes), progress)
	}
	must(t, s.SaveBookContent(ctx, book.ID, book.ContentHash, sp))
	got, err = s.GetBook(ctx, book.ID)
	must(t, err)
	found, err = s.SearchPoems(ctx, "明月", 10)
	must(t, err)
	_, after, err := s.ExportSnapshot(ctx)
	must(t, err)
	if got.LocalState != domain.LocalStateDownloaded || len(found) != 1 || !reflect.DeepEqual(before, after) {
		t.Fatalf("redownload: book=%+v hits=%d sync metadata=%+v, before=%+v", got, len(found), after, before)
	}
}

func TestStoreFailedImportRollsBackContentAndVersion(t *testing.T) {
	s, ctx := openTestStore(t), t.Context()
	book, sp := importTestBook(t, s, "b")
	_, before, err := s.ExportSnapshot(ctx)
	must(t, err)
	broken := *sp
	broken.Poems = []domain.Poem{sp.Poems[0], sp.Poems[0]}
	book.ContentHash = "changed-hash"
	if err = s.UpsertImportedBook(ctx, book, &broken); err == nil {
		t.Fatal("duplicate poem ID accepted")
	}
	got, err := s.GetBook(ctx, book.ID)
	must(t, err)
	found, err := s.SearchPoems(ctx, "明月", 10)
	must(t, err)
	_, after, err := s.ExportSnapshot(ctx)
	must(t, err)
	if got.ContentHash == book.ContentHash || got.LocalState != domain.LocalStateDownloaded || len(found) != 1 || !reflect.DeepEqual(before, after) {
		t.Fatalf("failed import altered state: book=%+v hits=%d meta=%+v", got, len(found), after)
	}
}

func TestStoreFavoriteActiveAndExcerptAreIndependent(t *testing.T) {
	s, ctx := openTestStore(t), t.Context()
	importTestBook(t, s, "b")
	lit, err := s.ToggleFavoritePoem(ctx, "b", "b-p1", "山间", "山人", "默认节选", true)
	must(t, err)
	off, err := s.ToggleFavoritePoem(ctx, "b", "b-p1", "山间", "山人", "默认节选", false)
	must(t, err)
	must(t, s.UpdateFavoriteExcerpt(ctx, "b", "b-p1", "自选节选", "2-2", true))
	visible, err := s.ListFavorites(ctx, "")
	must(t, err)
	data, _, err := s.ExportSnapshot(ctx)
	must(t, err)
	if len(visible) != 0 || len(data.Favorites) != 1 {
		t.Fatalf("favorite toggle: visible=%+v snapshot=%+v", visible, data.Favorites)
	}
	updated := data.Favorites[0]
	if updated.Active || updated.ActiveVer != off.ActiveVer || !updated.ExcerptVer.After(lit.ExcerptVer) || updated.DeletedAt != off.DeletedAt {
		t.Fatalf("excerpt edit changed membership: %+v", updated)
	}
	relit, err := s.ToggleFavoritePoem(ctx, "b", "b-p1", "山间", "山人", "另一个默认节选", true)
	must(t, err)
	if relit.ID != lit.ID || !relit.Active || !relit.ActiveVer.After(updated.ActiveVer) || relit.Excerpt != "自选节选" ||
		relit.HighlightLines != "2-2" || !relit.IsCustomQuote || relit.ExcerptVer != updated.ExcerptVer || relit.DeletedAt != 0 {
		t.Fatalf("relit favorite = %+v", relit)
	}
}

func TestStoreQuoteKindsFacetsAndFTS(t *testing.T) {
	s, ctx := openTestStore(t), t.Context()
	book, sp := importTestBook(t, s, "b")
	one, err := s.CreatePoemExcerptQuote(ctx, book.ID, "b-p1", "山间", book.Title, book.Author, "明月照山间", "1-1")
	must(t, err)
	two, err := s.CreatePoemExcerptQuote(ctx, book.ID, "b-p1", "山间", book.Title, book.Author, "又见远山", "2-2")
	must(t, err)
	manual, err := s.CreateStandaloneQuote(ctx, "clouds", "A", "Z", "reeden")
	must(t, err)
	excerpts, err := s.ListQuotes(ctx, QuoteFilter{Kind: domain.QuoteKindExcerpt, Author: book.Author, SourceBook: book.Title})
	must(t, err)
	if one.ID == two.ID || len(excerpts) != 2 {
		t.Fatalf("multiple excerpts: IDs %q %q, quotes=%+v", one.ID, two.ID, excerpts)
	}
	for _, excerpt := range excerpts {
		if !excerpt.BookActive || excerpt.EffectiveKind != domain.QuoteKindExcerpt ||
			!strings.HasPrefix(excerpt.Deeplink, "stillpoets://poem/b/b-p1?lines=") {
			t.Fatalf("active excerpt = %+v", excerpt)
		}
	}
	must(t, s.UpdateQuote(ctx, manual.ID, "夜归人", "A", "Z"))
	old, err := s.ListQuotes(ctx, QuoteFilter{Query: "clouds"})
	must(t, err)
	current, err := s.ListQuotes(ctx, QuoteFilter{Query: `("归人"*`, Author: "A", Limit: 1})
	must(t, err)
	if len(old) != 0 || len(current) != 1 || current[0].Quote.ID != manual.ID || current[0].Quote.ImportSource != "reeden" {
		t.Fatalf("quote index after edit: old=%+v current=%+v", old, current)
	}
	must(t, s.DeleteBook(ctx, book.ID))
	if err := s.UpsertImportedBook(ctx, book, sp); err == nil {
		t.Fatal("reimport revived a deleted book ID")
	}
	excerpts, err = s.ListQuotes(ctx, QuoteFilter{Kind: domain.QuoteKindExcerpt})
	must(t, err)
	standalone, err := s.ListQuotes(ctx, QuoteFilter{Kind: domain.QuoteKindStandalone, SourceBook: book.Title})
	must(t, err)
	if len(excerpts) != 0 || len(standalone) != 2 {
		t.Fatalf("kind filters after delete: excerpts=%+v standalone=%+v", excerpts, standalone)
	}
	for _, item := range standalone {
		if item.BookActive || item.Quote.Kind != domain.QuoteKindExcerpt || item.EffectiveKind != domain.QuoteKindStandalone ||
			item.Deeplink != "stillpoets://quote/"+item.Quote.ID {
			t.Fatalf("degraded excerpt = %+v", item)
		}
	}
	facets, err := s.ListQuoteFacets(ctx)
	must(t, err)
	if !slices.Equal(facets.SourceBooks, []string{"Z", book.Title}) || !slices.Equal(facets.Authors, []string{"A", book.Author}) {
		t.Fatalf("quote facets = %+v", facets)
	}
	must(t, s.DeleteQuote(ctx, manual.ID))
	if err := s.UpdateQuote(ctx, manual.ID, "recall", "", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("edit tombstone error = %v", err)
	}
	current, err = s.ListQuotes(ctx, QuoteFilter{Query: "归人"})
	must(t, err)
	facets, err = s.ListQuoteFacets(ctx)
	must(t, err)
	data, _, err := s.ExportSnapshot(ctx)
	must(t, err)
	if len(current) != 0 || !slices.Equal(facets.Authors, []string{book.Author}) || len(data.Books) != 1 || !data.Books[0].Deleted {
		t.Fatalf("deleted quote/book visibility: hits=%+v facets=%+v books=%+v", current, facets, data.Books)
	}
	for _, quote := range data.Quotes {
		if quote.ID == manual.ID && quote.Deleted && quote.DeletedAt > 0 && !quote.DeletedVer.IsZero() {
			return
		}
	}
	t.Fatalf("quote tombstone absent from export: %+v", data.Quotes)
}

func TestStoreReadingProgressAndStats(t *testing.T) {
	s, ctx := openTestStore(t), t.Context()
	importTestBook(t, s, "older")
	importTestBook(t, s, "newer")
	initial, err := s.GetProgress(ctx, "older")
	must(t, err)
	if initial.Status != domain.ReadStatusUnread {
		t.Fatalf("initial progress = %+v", initial)
	}
	progress, err := s.RecordPoemRead(ctx, "older", "older-p2", 0.2, 2)
	must(t, err)
	if progress.Status != domain.ReadStatusReading || !slices.Equal(progress.ReadPoemIDs, []string{"older-p2"}) {
		t.Fatalf("first progress = %+v", progress)
	}
	finished, err := s.RecordPoemRead(ctx, "older", "older-p1", 0.4, 2)
	must(t, err)
	if finished.Status != domain.ReadStatusFinished || finished.FinishedAt == 0 || !finished.StatusVer.After(progress.StatusVer) ||
		!slices.Equal(finished.ReadPoemIDs, []string{"older-p1", "older-p2"}) {
		t.Fatalf("finished progress = %+v", finished)
	}
	reread, err := s.RecordPoemRead(ctx, "older", "older-p2", 0.6, 3)
	must(t, err)
	if reread.Status != domain.ReadStatusFinished || reread.StatusVer != finished.StatusVer || reread.FinishedAt != finished.FinishedAt ||
		!reread.PositionVer.After(finished.PositionVer) || !slices.Equal(reread.ReadPoemIDs, finished.ReadPoemIDs) {
		t.Fatalf("reread progress = %+v", reread)
	}
	_, err = s.RecordPoemRead(ctx, "newer", "newer-p1", 0.3, 2)
	must(t, err)
	continueItem, err := s.GetContinueReading(ctx)
	must(t, err)
	if continueItem == nil || continueItem.Book.ID != "newer" || continueItem.PoemTitle != "山间" {
		t.Fatalf("continue reading = %+v", continueItem)
	}
	_, err = s.ToggleFavoritePoem(ctx, "older", "older-p2", "深林", "", "quiet forest", true)
	must(t, err)
	_, err = s.CreateStandaloneQuote(ctx, "散句", "", "", "manual")
	must(t, err)
	must(t, s.DeleteBook(ctx, "newer"))
	continueItem, err = s.GetContinueReading(ctx)
	must(t, err)
	if continueItem == nil || continueItem.Book.ID != "older" || continueItem.Progress.LastPoemID != "older-p2" {
		t.Fatalf("continue reading after delete = %+v", continueItem)
	}
	stats, err := s.GetStatsOverview(ctx)
	must(t, err)
	if want := (StatsOverview{BookCount: 1, FinishedBookCount: 1, ReadPoemCount: 2, FavoriteCount: 1, QuoteCount: 1, OfflineBookCount: 1}); stats != want {
		t.Fatalf("stats = %+v, want %+v", stats, want)
	}
	data, dirty, err := s.ExportSnapshot(ctx)
	must(t, err)
	must(t, s.ApplyMergedSnapshot(ctx, data, dirty.DirtySeq, dirty.LamportCounter, 1, true))
	must(t, s.UpdateLocalScroll(ctx, "older", 0.75))
	_, clean, err := s.ExportSnapshot(ctx)
	must(t, err)
	all, err := s.ListProgress(ctx)
	must(t, err)
	if clean.Dirty || clean.DirtySeq != dirty.DirtySeq || clean.LamportCounter != dirty.LamportCounter ||
		len(all) != 1 || all[0].LocalScrollRatio != 0.75 || all[0].PositionVer != reread.PositionVer {
		t.Fatalf("local scroll: meta=%+v progress=%+v", clean, all)
	}
	reset, err := s.MarkBookFinished(ctx, "older", false)
	must(t, err)
	if reset.Status != domain.ReadStatusReading || reset.FinishedAt != 0 || !reset.StatusVer.After(finished.StatusVer) || reset.LocalScrollRatio != 0.75 {
		t.Fatalf("manual reset = %+v", reset)
	}
	manual, err := s.MarkBookFinished(ctx, "older", true)
	must(t, err)
	if manual.Status != domain.ReadStatusFinished || manual.FinishedAt == 0 || !manual.StatusVer.After(reset.StatusVer) {
		t.Fatalf("manual finish = %+v", manual)
	}
}

func TestStoreSyncMergePreservesLocalCacheAndProgress(t *testing.T) {
	left, right, ctx := openTestStore(t), openTestStore(t), t.Context()
	group, err := left.CreateGroup(ctx, "第一组")
	must(t, err)
	book, sp := importTestBook(t, left, "b")
	must(t, left.SetBookGroup(ctx, book.ID, group.ID))
	_, err = left.ToggleFavoritePoem(ctx, book.ID, "b-p1", "山间", book.Author, "默认", true)
	must(t, err)
	kept, err := left.CreateStandaloneQuote(ctx, "oldtext", "作者", "出处", "manual")
	must(t, err)
	removed, err := left.CreateStandaloneQuote(ctx, "removedtext", "作者", "出处", "manual")
	must(t, err)
	_, err = left.RecordPoemRead(ctx, book.ID, "b-p1", 0.6, 2)
	must(t, err)
	leftData, leftMeta, err := left.ExportSnapshot(ctx)
	must(t, err)
	if len(leftData.Groups) != 1 || len(leftData.Books) != 1 || len(leftData.Favorites) != 1 || len(leftData.Quotes) != 2 ||
		len(leftData.Progress) != 1 || leftData.Books[0].LocalContentHash != "" || leftData.Books[0].LocalState != "" ||
		leftData.Progress[0].LocalScrollRatio != 0 {
		t.Fatalf("local data leaked or synced data missing: %+v", leftData)
	}
	must(t, right.ApplyMergedSnapshot(ctx, leftData, 0, leftMeta.LamportCounter, 0, false))
	cloud, err := right.GetBook(ctx, book.ID)
	must(t, err)
	if cloud.LocalState != domain.LocalStateRemote {
		t.Fatalf("remote book = %+v", cloud)
	}
	must(t, right.DissolveGroup(ctx, group.ID))
	must(t, right.SetBookStarred(ctx, book.ID, true))
	must(t, right.UpdateFavoriteExcerpt(ctx, book.ID, "b-p1", "云端节选", "1-1", true))
	must(t, right.UpdateQuote(ctx, kept.ID, "新篇落在此处", "作者", "出处"))
	must(t, right.DeleteQuote(ctx, removed.ID))
	_, err = right.RecordPoemRead(ctx, book.ID, "b-p2", 0.1, 2)
	must(t, err)
	book.ContentHash = "newer-hash"
	must(t, right.UpsertImportedBook(ctx, book, sp))
	rightData, rightMeta, err := right.ExportSnapshot(ctx)
	must(t, err)
	merged := booksync.MergeSnapshotData(leftData, rightData)
	maxCounter := booksync.MaxCounterInData(max(leftMeta.LamportCounter, rightMeta.LamportCounter), merged) + 10
	must(t, left.UpdateLocalScroll(ctx, book.ID, 0.85))
	must(t, left.ApplyMergedSnapshot(ctx, merged, leftMeta.DirtySeq, maxCounter, 4, true))
	got, err := left.GetBook(ctx, book.ID)
	must(t, err)
	progress, err := left.GetProgress(ctx, book.ID)
	must(t, err)
	favorites, err := left.ListFavorites(ctx, book.ID)
	must(t, err)
	export, meta, err := left.ExportSnapshot(ctx)
	must(t, err)
	if got.LocalContentHash != "hash-b" || got.LocalState != domain.LocalStateStale || got.ContentHash != book.ContentHash ||
		!got.IsStarred || got.GroupID != "" || !export.Groups[0].Deleted || len(favorites) != 1 || favorites[0].Excerpt != "云端节选" {
		t.Fatalf("merged shelf: book=%+v groups=%+v favorites=%+v", got, export.Groups, favorites)
	}
	if progress.LocalScrollRatio != 0.85 || progress.LastPoemID != "b-p2" || progress.Status != domain.ReadStatusFinished ||
		!slices.Equal(progress.ReadPoemIDs, []string{"b-p1", "b-p2"}) {
		t.Fatalf("merged progress = %+v", progress)
	}
	if meta.Dirty || meta.DirtySeq != leftMeta.DirtySeq || meta.LocalRev != 4 || meta.LastSyncAt <= 0 || meta.LamportCounter != maxCounter {
		t.Fatalf("published sync metadata = %+v", meta)
	}
	for _, query := range []string{"oldtext", "removedtext"} {
		results, err := left.ListQuotes(ctx, QuoteFilter{Query: query})
		must(t, err)
		if len(results) != 0 {
			t.Fatalf("stale sync FTS hits for %q = %+v", query, results)
		}
	}
	current, err := left.ListQuotes(ctx, QuoteFilter{Query: "落在"})
	must(t, err)
	if len(current) != 1 || current[0].Quote.ID != kept.ID {
		t.Fatalf("merged FTS = %+v", current)
	}
	next, err := left.CreateGroup(ctx, "新分组")
	must(t, err)
	if next.NameVer.Counter != maxCounter+1 {
		t.Fatalf("next local version = %+v, want counter %d", next.NameVer, maxCounter+1)
	}
	data, dirty, err := left.ExportSnapshot(ctx)
	must(t, err)
	must(t, left.ApplyMergedSnapshot(ctx, data, dirty.DirtySeq, maxCounter, 0, false))
	_, unpublished, err := left.ExportSnapshot(ctx)
	must(t, err)
	if unpublished.LocalRev != dirty.LocalRev || unpublished.Dirty != dirty.Dirty || unpublished.DirtySeq != dirty.DirtySeq || unpublished.LamportCounter != dirty.LamportCounter || unpublished.LastSyncAt < dirty.LastSyncAt {
		t.Fatalf("pull-only sync changed publication metadata: got %+v, want %+v", unpublished, dirty)
	}
}

func TestStoreSyncApplyRetainsWritesAfterExport(t *testing.T) {
	s, ctx := openTestStore(t), t.Context()
	quote, err := s.CreateStandaloneQuote(ctx, "beforepublish", "", "", "manual")
	must(t, err)
	published, oldMeta, err := s.ExportSnapshot(ctx)
	must(t, err)
	must(t, s.UpdateQuote(ctx, quote.ID, "afterpublish", "", ""))
	added, err := s.CreateStandaloneQuote(ctx, "alsoadded", "", "", "manual")
	must(t, err)
	// The returned maximum may be higher than the counter of both local edits.
	must(t, s.ApplyMergedSnapshot(ctx, published, oldMeta.DirtySeq, oldMeta.LamportCounter+10, 1, true))
	data, dirty, err := s.ExportSnapshot(ctx)
	must(t, err)
	if !dirty.Dirty || dirty.DirtySeq != oldMeta.DirtySeq+2 || dirty.LocalRev != 1 || dirty.LastSyncAt <= 0 || len(data.Quotes) != 2 {
		t.Fatalf("apply with local writes: meta=%+v quotes=%+v", dirty, data.Quotes)
	}
	for text, id := range map[string]string{"afterpublish": quote.ID, "alsoadded": added.ID} {
		quotes, err := s.ListQuotes(ctx, QuoteFilter{Query: text})
		must(t, err)
		if len(quotes) != 1 || quotes[0].Quote.ID != id {
			t.Fatalf("new write with %q missing: %+v", text, quotes)
		}
	}
	must(t, s.ApplyMergedSnapshot(ctx, data, dirty.DirtySeq, dirty.LamportCounter, 2, true))
	_, clean, err := s.ExportSnapshot(ctx)
	must(t, err)
	if clean.Dirty || clean.LocalRev != 2 || clean.DirtySeq != dirty.DirtySeq {
		t.Fatalf("subsequent publication = %+v", clean)
	}
}

func TestStoreOpenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Open(ctx, ":memory:"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open with canceled context: %v", err)
	}
}

func TestRecordPoemReadRejectsCrossBookPoemID(t *testing.T) {
	s, ctx := openTestStore(t), t.Context()
	sp := &domain.SPBook{
		BookID: "b1",
		Title:  "诗集一",
		Poems: []domain.Poem{
			{ID: "p1", BookID: "b1", OrderIndex: 0, Kind: domain.EntryKindPoem, Title: "第一首"},
		},
	}
	must(t, s.UpsertImportedBook(ctx, domain.Book{
		ID: "b1", Title: "诗集一", ContentHash: "h1", RawFilename: "b1.epub", PoemCount: 1,
	}, sp))

	if _, err := s.RecordPoemRead(ctx, "b1", "foreign-poem-id", 0.2, 1); err == nil {
		t.Fatalf("expected cross-book poemID to be rejected")
	}
	if _, err := s.RecordPoemRead(ctx, "b1", "p1", 0.5, 1); err != nil {
		t.Fatalf("valid poemID failed: %v", err)
	}
}

func TestSaveBookContentRejectsStaleHash(t *testing.T) {
	s, ctx := openTestStore(t), t.Context()
	sp := &domain.SPBook{
		BookID: "b1",
		Title:  "诗集一",
		Poems:  []domain.Poem{{ID: "p1", BookID: "b1", Title: "第一首"}},
	}
	must(t, s.UpsertImportedBook(ctx, domain.Book{
		ID: "b1", Title: "诗集一", ContentHash: "hash-new", RawFilename: "b1.epub", PoemCount: 1,
	}, sp))
	if err := s.SaveBookContent(ctx, "b1", "hash-old", sp); err == nil {
		t.Fatalf("expected stale content hash to be rejected")
	}
}

func TestUpdateSettingsPatchAndKeepDirtyAndMaxCounterGuard(t *testing.T) {
	s, ctx := openTestStore(t), t.Context()
	_, err := s.UpdateSettings(ctx, func(st *domain.Settings) { st.ThemePreset = "journal" })
	must(t, err)
	st, err := s.UpdateSettings(ctx, func(st *domain.Settings) { st.FontSize = 22 })
	must(t, err)
	if st.ThemePreset != "journal" || st.FontSize != 22 {
		t.Fatalf("patch clobbered previous field: %+v", st)
	}

	// 验证 keepDirty=true 在 published=true 时保留 dirty=1，且拒绝超大 Lamport 计数
	data, meta, err := s.ExportSnapshot(ctx)
	must(t, err)
	if err := s.ApplyMergedSnapshotEx(ctx, data, meta.DirtySeq, ^uint64(0), 1, true, false, nil); err == nil {
		t.Fatalf("expected oversized lamport counter to fail")
	}
	must(t, s.ApplyMergedSnapshotEx(ctx, data, meta.DirtySeq, meta.LamportCounter, 1, true, true, nil))
	after, err := s.GetSyncMeta(ctx)
	must(t, err)
	if !after.Dirty || after.LocalRev != 1 {
		t.Fatalf("expected dirty=true and local_rev=1 with keepDirty, got %+v", after)
	}
}

func TestApplyMergedSnapshotExPersistsRemoteDeviceRevsAtomically(t *testing.T) {
	s, ctx := openTestStore(t), t.Context()
	meta, err := s.GetSyncMeta(ctx)
	must(t, err)
	if meta.KnownRemoteRevs != nil {
		t.Fatalf("fresh database has remote revs: %v", meta.KnownRemoteRevs)
	}
	initial := map[string]uint64{"dev-a": 2, "dev-deleted": 1}
	must(t, s.ApplyMergedSnapshotEx(ctx, booksync.SnapshotData{}, meta.DirtySeq, 2, 1, true, false, initial))
	meta, err = s.GetSyncMeta(ctx)
	must(t, err)
	if !maps.Equal(meta.KnownRemoteRevs, initial) {
		t.Fatalf("saved revs = %v, want %v", meta.KnownRemoteRevs, initial)
	}

	// 在合并数据写入后让代次记录语句失败，验证整个事务回滚。
	_, err = s.db.Execute(ctx, `CREATE TRIGGER fail_remote_revs BEFORE INSERT ON sync_state
		WHEN NEW.key = 'remote_device_revs' BEGIN SELECT RAISE(ABORT, 'remote revs test failure'); END`)
	must(t, err)
	changed := booksync.SnapshotData{Groups: []domain.BookGroup{{ID: "remote", Name: "远端分组"}}}
	replacement := map[string]uint64{"dev-a": 3}
	if err := s.ApplyMergedSnapshotEx(ctx, changed, meta.DirtySeq, 3, 2, true, false, replacement); err == nil || !strings.Contains(err.Error(), "remote revs test failure") {
		t.Fatalf("expected transaction failure, got %v", err)
	}
	dbData, dbMeta, err := s.ExportSnapshot(ctx)
	must(t, err)
	if len(dbData.Groups) != 0 || dbMeta.LocalRev != 1 || dbMeta.LamportCounter != 2 || !maps.Equal(dbMeta.KnownRemoteRevs, initial) {
		t.Fatalf("failed transaction left changes: groups=%v meta=%+v", dbData.Groups, dbMeta)
	}

	_, err = s.db.Execute(ctx, `DROP TRIGGER fail_remote_revs`)
	must(t, err)
	must(t, s.ApplyMergedSnapshotEx(ctx, changed, meta.DirtySeq, 3, 0, false, false, replacement))
	dbData, dbMeta, err = s.ExportSnapshot(ctx)
	must(t, err)
	if len(dbData.Groups) != 1 || dbMeta.LocalRev != 1 || !maps.Equal(dbMeta.KnownRemoteRevs, replacement) {
		t.Fatalf("expected merged data and replacement revs: groups=%v meta=%+v", dbData.Groups, dbMeta)
	}

	must(t, s.ResetWorkspaceSyncState(ctx))
	dbMeta, err = s.GetSyncMeta(ctx)
	must(t, err)
	if dbMeta.KnownRemoteRevs != nil || dbMeta.LocalRev != 0 || dbMeta.LastSyncAt != 0 || !dbMeta.Dirty {
		t.Fatalf("workspace reset left sync state: %+v", dbMeta)
	}
}

func TestRecordPoemPeekPreservesPositionAndContinueReading(t *testing.T) {
	s, ctx := openTestStore(t), t.Context()
	importTestBook(t, s, "older")
	importTestBook(t, s, "newer")

	// 1. 建立 older 的主线阅读进度（older-p1，滚动比例 0.45）
	mainline, err := s.RecordPoemRead(ctx, "older", "older-p1", 0.45, 2)
	must(t, err)
	if mainline.LastPoemID != "older-p1" || mainline.LocalScrollRatio != 0.45 {
		t.Fatalf("mainline progress = %+v", mainline)
	}

	// 2. 偶遇 older-p2：计入 ReadPoemIDs 并触发完读，但严格保留 LastPoemID / PositionVer / LocalScrollRatio
	peeked, changed, err := s.RecordPoemPeek(ctx, "older", "older-p2", 2)
	must(t, err)
	if !changed {
		t.Fatalf("expected first peek of unread poem to report changed=true")
	}
	if peeked.LastPoemID != "older-p1" || peeked.PositionVer != mainline.PositionVer || peeked.LocalScrollRatio != 0.45 {
		t.Fatalf("peek clobbered mainline position: got=%+v wantLast=%q wantVer=%+v", peeked, mainline.LastPoemID, mainline.PositionVer)
	}
	if !slices.Equal(peeked.ReadPoemIDs, []string{"older-p1", "older-p2"}) || peeked.Status != domain.ReadStatusFinished {
		t.Fatalf("peek did not update read set/status: %+v", peeked)
	}

	// 3. 再次偶遇已读过的 older-p2：无新增状态变化，不递增 Lamport 且不标脏
	data, metaBefore, err := s.ExportSnapshot(ctx)
	must(t, err)
	must(t, s.ApplyMergedSnapshot(ctx, data, metaBefore.DirtySeq, metaBefore.LamportCounter, 1, true))
	metaClean, err := s.GetSyncMeta(ctx)
	must(t, err)

	peekAgain, changedAgain, err := s.RecordPoemPeek(ctx, "older", "older-p2", 2)
	must(t, err)
	metaAfter, err := s.GetSyncMeta(ctx)
	must(t, err)
	if changedAgain || metaAfter.Dirty || metaAfter.LamportCounter != metaClean.LamportCounter || peekAgain.LastPoemID != "older-p1" {
		t.Fatalf("duplicate peek should be a no-op: changed=%v meta=%+v progress=%+v", changedAgain, metaAfter, peekAgain)
	}

	// 4. 偶遇一本从未读过的书 newer：计入已读集合但不写入 LastPoemID，不抢占首页「继续阅读」
	newerPeek, newerChanged, err := s.RecordPoemPeek(ctx, "newer", "newer-p2", 2)
	must(t, err)
	if !newerChanged || newerPeek.LastPoemID != "" || !newerPeek.PositionVer.IsZero() ||
		newerPeek.Status != domain.ReadStatusReading || !slices.Equal(newerPeek.ReadPoemIDs, []string{"newer-p2"}) {
		t.Fatalf("unread book peek = %+v (changed=%v)", newerPeek, newerChanged)
	}
	cont, err := s.GetContinueReading(ctx)
	must(t, err)
	if cont == nil || cont.Book.ID != "older" || cont.Progress.LastPoemID != "older-p1" {
		t.Fatalf("continue reading hijacked by peek: %+v", cont)
	}

	// 5. 跨书 poemID 依然被拒绝
	if _, _, err := s.RecordPoemPeek(ctx, "older", "newer-p1", 2); err == nil {
		t.Fatalf("expected cross-book poemID in RecordPoemPeek to be rejected")
	}
}

func TestSetBooksGroupBatch(t *testing.T) {
	s, ctx := openTestStore(t), t.Context()
	gModern, err := s.CreateGroup(ctx, "现代诗")
	must(t, err)
	gForeign, err := s.CreateGroup(ctx, "外国诗")
	must(t, err)

	importTestBook(t, s, "b1")
	importTestBook(t, s, "b2")
	importTestBook(t, s, "b3")

	// 批量归入现代诗（含重复 ID 与空白 ID 自动去重忽略）
	must(t, s.SetBooksGroup(ctx, []string{"b1", "b2", "b1", " "}, gModern.ID))
	inModern, err := s.ListBooks(ctx, gModern.ID)
	must(t, err)
	if len(inModern) != 2 {
		t.Fatalf("expected 2 books in modern group, got %d", len(inModern))
	}

	// 批量迁移 b2, b3 至外国诗
	must(t, s.SetBooksGroup(ctx, []string{"b2", "b3"}, gForeign.ID))
	inModern, err = s.ListBooks(ctx, gModern.ID)
	must(t, err)
	inForeign, err := s.ListBooks(ctx, gForeign.ID)
	must(t, err)
	if len(inModern) != 1 || inModern[0].ID != "b1" || len(inForeign) != 2 {
		t.Fatalf("unexpected group distribution: modern=%d foreign=%d", len(inModern), len(inForeign))
	}

	// 批量移回未分组
	must(t, s.SetBooksGroup(ctx, []string{"b2", "b3"}, ""))
	inForeign, err = s.ListBooks(ctx, gForeign.ID)
	must(t, err)
	if len(inForeign) != 0 {
		t.Fatalf("expected 0 books in foreign group after ungroup, got %d", len(inForeign))
	}

	// 无效分组 ID 拒绝整批写入
	if err := s.SetBooksGroup(ctx, []string{"b1"}, "non-existent-group"); err == nil {
		t.Fatalf("expected error for non-existent target group")
	}
}
