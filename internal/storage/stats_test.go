package storage

import (
	"path/filepath"
	"testing"
	"time"

	"stillpoets/internal/domain"
	booksync "stillpoets/internal/sync"
)

func TestReadingEventsStatsDashboardAndAuthorProfile(t *testing.T) {
	ctx := t.Context()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	// 1. 导入一本个人诗集与一本多人合集（验证合集按单诗作者归属而非编者）
	bookHaizi := domain.Book{
		ID:          "bk-haizi",
		Title:       "海子诗全集",
		Author:      "海子",
		ContentHash: "hash-haizi",
		RawFilename: "haizi.epub",
		PoemCount:   2,
	}
	spHaizi := &domain.SPBook{
		SchemaVersion: 1,
		BookID:        "bk-haizi",
		Title:         "海子诗全集",
		Author:        "海子",
		Poems: []domain.Poem{
			{ID: "hz-1", BookID: "bk-haizi", OrderIndex: 0, Kind: domain.EntryKindPoem, Title: "日记", Author: "海子"},
			{ID: "hz-2", BookID: "bk-haizi", OrderIndex: 1, Kind: domain.EntryKindPoem, Title: "面朝大海，春暖花开", Author: "海子"},
		},
	}
	if err := store.UpsertImportedBook(ctx, bookHaizi, spHaizi); err != nil {
		t.Fatalf("UpsertImportedBook haizi: %v", err)
	}

	bookAnthology := domain.Book{
		ID:          "bk-anth",
		Title:       "现代诗选",
		Author:      "佚名",
		ContentHash: "hash-anth",
		RawFilename: "anth.epub",
		PoemCount:   2,
	}
	spAnthology := &domain.SPBook{
		SchemaVersion: 1,
		BookID:        "bk-anth",
		Title:         "现代诗选",
		Author:        "佚名",
		Poems: []domain.Poem{
			{ID: "an-1", BookID: "bk-anth", OrderIndex: 0, Kind: domain.EntryKindPoem, Title: "一代人", Author: "顾城"},
			{ID: "an-2", BookID: "bk-anth", OrderIndex: 1, Kind: domain.EntryKindPoem, Title: "秋日", Author: "里尔克"},
		},
	}
	if err := store.UpsertImportedBook(ctx, bookAnthology, spAnthology); err != nil {
		t.Fatalf("UpsertImportedBook anthology: %v", err)
	}

	// 读完海子诗集，验证完读墙
	if _, err := store.RecordPoemRead(ctx, "bk-haizi", "hz-1", 0.5, 2); err != nil {
		t.Fatalf("RecordPoemRead hz-1: %v", err)
	}
	if _, err := store.RecordPoemRead(ctx, "bk-haizi", "hz-2", 0.5, 2); err != nil {
		t.Fatalf("RecordPoemRead hz-2: %v", err)
	}
	// 精选与散页
	if _, err := store.ToggleFavoritePoem(ctx, FavoritePoemInput{BookID: "bk-haizi", PoemID: "hz-1", PoemTitle: "日记", Author: "海子", Excerpt: "姐姐，今夜我在德令哈", Active: true}); err != nil {
		t.Fatalf("ToggleFavoritePoem: %v", err)
	}
	if _, err := store.CreateStandaloneQuote(ctx, "黑夜给了我黑色的眼睛", "顾城", "一代人", "manual"); err != nil {
		t.Fatalf("CreateStandaloneQuote: %v", err)
	}

	// 2. 记录读诗事件（固定时区，覆盖同一天去重、跨天重读、不同小时段）
	loc := time.FixedZone("CST", 8*3600)
	day1 := time.Date(2026, 11, 20, 21, 30, 0, 0, loc)
	day2 := time.Date(2026, 11, 24, 22, 15, 0, 0, loc)
	day2Morning := time.Date(2026, 11, 24, 7, 45, 0, 0, loc)
	now := time.Date(2026, 11, 25, 10, 0, 0, 0, loc)

	ev1, ins1, err := store.RecordReadingEvent(ctx, RecordReadingEventInput{BookID: "bk-haizi", PoemID: "hz-1", PoemTitle: "日记", Author: "海子", ReadAt: day1})
	if err != nil || !ins1 || ev1.IsReread {
		t.Fatalf("expected initial insert on day1, got ev=%+v ins=%v err=%v", ev1, ins1, err)
	}
	// 同一天再读同一首诗 -> 去重不插入
	_, insDup, err := store.RecordReadingEvent(ctx, RecordReadingEventInput{BookID: "bk-haizi", PoemID: "hz-1", PoemTitle: "日记", Author: "海子", ReadAt: day1.Add(10 * time.Minute)})
	if err != nil || insDup {
		t.Fatalf("expected same-day dedup, got ins=%v err=%v", insDup, err)
	}
	// 跨天再次读同一首诗 -> 识别为重读
	evReread, insReread, err := store.RecordReadingEvent(ctx, RecordReadingEventInput{BookID: "bk-haizi", PoemID: "hz-1", ReadAt: day2})
	if err != nil || !insReread || !evReread.IsReread || evReread.PoemTitle != "日记" || evReread.Author != "海子" {
		t.Fatalf("expected cross-day reread with backfilled metadata, got ev=%+v ins=%v err=%v", evReread, insReread, err)
	}
	// 合集中的单首诗应归属顾城（而非书目作者「佚名」）
	evGucheng, insGC, err := store.RecordReadingEvent(ctx, RecordReadingEventInput{BookID: "bk-anth", PoemID: "an-1", ReadAt: day2Morning})
	if err != nil || !insGC || evGucheng.Author != "顾城" {
		t.Fatalf("expected anthology poem author 顾城, got ev=%+v ins=%v err=%v", evGucheng, insGC, err)
	}

	// 3. 聚合完整看板（全部窗口）
	dash, err := store.GetStatsDashboard(ctx, StatsQuery{
		Period:     StatsPeriodAll,
		Year:       0,
		AuthorMode: AuthorWeightCombined,
		Now:        now,
	})
	if err != nil {
		t.Fatalf("GetStatsDashboard: %v", err)
	}
	if dash.Overview.FinishedBookCount != 1 || len(dash.FinishedBooks) != 1 || dash.FinishedBooks[0].Book.ID != "bk-haizi" {
		t.Fatalf("unexpected finished books: overview=%+v finished=%+v", dash.Overview, dash.FinishedBooks)
	}
	if dash.Overview.ReadPoemCount != 3 { // hz-1, hz-2 (from progress) + an-1 (from event)
		t.Fatalf("expected 3 unique read poems in all window, got %d", dash.Overview.ReadPoemCount)
	}
	if dash.Heatmap.ActiveDays != 2 || dash.Heatmap.TotalEvents != 3 || dash.Heatmap.LatestActiveDate != "2026-11-24" {
		t.Fatalf("unexpected heatmap stats: activeDays=%d total=%d latest=%q",
			dash.Heatmap.ActiveDays, dash.Heatmap.TotalEvents, dash.Heatmap.LatestActiveDate)
	}
	// 入夜（21点、22点）2条，清晨（7点）1条
	if dash.Rhythm.TotalEvents != 3 || dash.Rhythm.Slots[3].Count != 2 || dash.Rhythm.Slots[0].Count != 1 {
		t.Fatalf("unexpected rhythm: %+v", dash.Rhythm)
	}
	if dash.Rhythm.Insight == "" {
		t.Fatalf("expected non-empty rhythm insight")
	}

	// 作者云：应包含海子与顾城，且不包含「佚名」
	authorMap := map[string]AuthorStat{}
	for _, a := range dash.Authors {
		authorMap[a.Author] = a
	}
	if _, hasAnon := authorMap["佚名"]; hasAnon {
		t.Fatalf("anonymous author should be excluded from poet cloud: %+v", dash.Authors)
	}
	if authorMap["海子"].ReadCount != 2 || authorMap["海子"].FavoriteCount != 1 {
		t.Fatalf("unexpected 海子 author stat: %+v", authorMap["海子"])
	}
	if authorMap["顾城"].ReadCount != 1 || authorMap["顾城"].QuoteCount != 1 {
		t.Fatalf("unexpected 顾城 author stat: %+v", authorMap["顾城"])
	}

	// 本周窗口（2026-11-25 为周三，本周从 2026-11-23 周一开始，只含 day2 的 2 条事件）
	weekDash, err := store.GetStatsDashboard(ctx, StatsQuery{
		Period:     StatsPeriodWeek,
		AuthorMode: AuthorWeightRead,
		Now:        now,
	})
	if err != nil {
		t.Fatalf("GetStatsDashboard week: %v", err)
	}
	if weekDash.Overview.ReadEventCount != 2 || weekDash.Overview.ActiveDays != 1 {
		t.Fatalf("unexpected week overview: %+v", weekDash.Overview)
	}

	// 4. 清理海子正文缓存，验证未下载状态下热力图按日下钻与诗人档案仍可完整展示
	if err := store.ClearBookCache(ctx, "bk-haizi"); err != nil {
		t.Fatalf("ClearBookCache: %v", err)
	}
	dayItems, err := store.ListReadingEventsByDate(ctx, "2026-11-24")
	if err != nil {
		t.Fatalf("ListReadingEventsByDate: %v", err)
	}
	if len(dayItems) != 2 {
		t.Fatalf("expected 2 events on 2026-11-24, got %d", len(dayItems))
	}
	if dayItems[0].Event.PoemTitle != "日记" || dayItems[0].BookTitle != "海子诗全集" ||
		dayItems[0].BookLocalState != domain.LocalStateRemote || !dayItems[0].IsFavorite || !dayItems[0].Event.IsReread {
		t.Fatalf("unexpected remote book drill-down item: %+v", dayItems[0])
	}

	// 5. 诗人档案下钻
	profile, err := store.GetAuthorProfile(ctx, "顾城")
	if err != nil {
		t.Fatalf("GetAuthorProfile: %v", err)
	}
	if profile.ReadPoemCount != 1 || profile.QuoteCount != 1 || len(profile.RecentReads) != 1 || len(profile.Books) != 1 {
		t.Fatalf("unexpected 顾城 profile: %+v", profile)
	}

	// 6. 验证 ReadingEvents 跨端快照导出与合并落库闭环
	snap, meta, err := store.ExportSnapshot(ctx)
	if err != nil {
		t.Fatalf("ExportSnapshot: %v", err)
	}
	if len(snap.ReadingEvents) != 3 {
		t.Fatalf("expected 3 exported reading events, got %d", len(snap.ReadingEvents))
	}

	store2, err := Open(ctx, filepath.Join(t.TempDir(), "stats_dev2.db"))
	if err != nil {
		t.Fatalf("Open dev2: %v", err)
	}
	defer store2.Close()

	_, meta2, err := store2.ExportSnapshot(ctx)
	if err != nil {
		t.Fatalf("ExportSnapshot dev2: %v", err)
	}
	merged := booksync.MergeSnapshotData(booksync.SnapshotData{}, snap)
	if err := store2.ApplyMergedSnapshot(ctx, merged, meta2.DirtySeq, meta.LamportCounter, 1, true); err != nil {
		t.Fatalf("ApplyMergedSnapshot dev2: %v", err)
	}
	dev2Day, err := store2.ListReadingEventsByDate(ctx, "2026-11-24")
	if err != nil || len(dev2Day) != 2 {
		t.Fatalf("dev2 expected 2 synced events on 2026-11-24, got %d err=%v", len(dev2Day), err)
	}
}
