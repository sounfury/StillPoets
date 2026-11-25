package sync

import (
	"reflect"
	"testing"

	"stillpoets/internal/domain"
)

func v(c uint64, id string) domain.Version {
	return domain.Version{Counter: c, InstallID: id}
}

func TestMergeBooksFieldLevelAndPermanentDelete(t *testing.T) {
	// 场景 1：设备 A 改分组，设备 B 标星并调整排序 -> 字段级合并互不覆盖
	devA := []domain.Book{
		{
			ID:          "b1",
			Title:       "海子诗全集",
			ContentHash: "hash_v1",
			ContentVer:  v(1, "dev-a"),
			GroupID:     "grp_modern",
			GroupVer:    v(5, "dev-a"),
			IsStarred:   false,
			StarredVer:  v(1, "dev-a"),
			SortOrder:   10,
			SortVer:     v(1, "dev-a"),
			CreatedAt:   100,
		},
		{
			ID:         "b_del",
			Title:      "待删诗集",
			ContentVer: v(1, "dev-a"),
			Deleted:    true,
			DeletedAt:  200,
			DeletedVer: v(2, "dev-a"),
		},
	}
	devB := []domain.Book{
		{
			ID:          "b1",
			Title:       "海子诗全集（修订）",
			ContentHash: "hash_v2",
			ContentVer:  v(3, "dev-b"),
			GroupID:     "",
			GroupVer:    v(1, "dev-a"),
			IsStarred:   true,
			StarredVer:  v(4, "dev-b"),
			SortOrder:   1,
			SortVer:     v(4, "dev-b"),
			CreatedAt:   120,
		},
		{
			// B 离线修改了已被 A 删除的书且计数器更高 -> 永久删除仍胜出（不复活）
			ID:         "b_del",
			Title:      "待删诗集",
			ContentVer: v(1, "dev-a"),
			IsStarred:  true,
			StarredVer: v(99, "dev-b"),
			Deleted:    false,
		},
	}

	ab := MergeBooks(devA, devB)
	ba := MergeBooks(devB, devA)
	if !reflect.DeepEqual(ab, ba) {
		t.Fatalf("MergeBooks violated commutativity:\nab=%+v\nba=%+v", ab, ba)
	}

	byID := map[string]domain.Book{}
	for _, b := range ab {
		byID[b.ID] = b
	}

	b1 := byID["b1"]
	if b1.Title != "海子诗全集（修订）" || b1.ContentHash != "hash_v2" {
		t.Fatalf("b1 content not updated: %+v", b1)
	}
	if b1.GroupID != "grp_modern" || !b1.IsStarred || b1.SortOrder != 1 {
		t.Fatalf("b1 field-level merge lost edit: group=%q starred=%v sort=%d", b1.GroupID, b1.IsStarred, b1.SortOrder)
	}
	if b1.LocalState != "" {
		t.Fatalf("MergeBooks must not leak LocalState, got %q", b1.LocalState)
	}

	// 本地缓存状态在合并器之外由本地已下载哈希推导
	if st := domain.DeriveLocalBookState(b1, "hash_v1"); st != domain.LocalStateStale {
		t.Fatalf("DeriveLocalBookState(hash_v1) = %q, want stale", st)
	}
	if st := domain.DeriveLocalBookState(b1, "hash_v2"); st != domain.LocalStateDownloaded {
		t.Fatalf("DeriveLocalBookState(hash_v2) = %q, want downloaded", st)
	}
	if st := domain.DeriveLocalBookState(b1, ""); st != domain.LocalStateRemote {
		t.Fatalf("DeriveLocalBookState('') = %q, want remote", st)
	}

	if !byID["b_del"].Deleted {
		t.Fatalf("deleted book resurrected after offline edit: %+v", byID["b_del"])
	}
}

func TestMergeGroupsCreateRenameSortDissolve(t *testing.T) {
	devA := []domain.BookGroup{
		{
			ID:        "g1",
			Name:      "现代诗",
			NameVer:   v(5, "dev-a"),
			SortOrder: 2,
			SortVer:   v(1, "dev-a"),
			CreatedAt: 100,
		},
		{
			ID:         "g2",
			Name:       "旧分组",
			NameVer:    v(1, "dev-a"),
			Deleted:    true,
			DeletedAt:  300,
			DeletedVer: v(4, "dev-a"),
		},
	}
	devB := []domain.BookGroup{
		{
			ID:        "g1",
			Name:      "新诗",
			NameVer:   v(2, "dev-b"),
			SortOrder: 0,
			SortVer:   v(6, "dev-b"),
			CreatedAt: 100,
		},
		{
			// 离线重命名已解散分组不能让分组复活
			ID:      "g2",
			Name:    "旧分组改名",
			NameVer: v(20, "dev-b"),
			Deleted: false,
		},
	}

	ab := MergeGroups(devA, devB)
	ba := MergeGroups(devB, devA)
	if !reflect.DeepEqual(ab, ba) {
		t.Fatalf("MergeGroups violated commutativity")
	}
	byID := map[string]domain.BookGroup{}
	for _, g := range ab {
		byID[g.ID] = g
	}
	if byID["g1"].Name != "现代诗" || byID["g1"].SortOrder != 0 {
		t.Fatalf("g1 field-level merge unexpected: %+v", byID["g1"])
	}
	if !byID["g2"].Deleted {
		t.Fatalf("dissolved group resurrected: %+v", byID["g2"])
	}
}

func TestMergeFavoritesUnfavoriteNotResurrectedByExcerptEdit(t *testing.T) {
	// A、B 离线对同一首诗操作，各自有不同本地 ID
	devA := []domain.FavoritePoem{
		{
			ID:             "fav_a",
			BookID:         "b1",
			PoemID:         "p1",
			PoemTitle:      "面朝大海，春暖花开",
			Active:         false, // A 取消了收藏
			ActiveVer:      v(5, "dev-a"),
			DeletedAt:      500,
			Excerpt:        "旧节选",
			ExcerptVer:     v(2, "dev-a"),
			HighlightLines: "1-2",
		},
	}
	devB := []domain.FavoritePoem{
		{
			ID:             "fav_b",
			BookID:         "b1",
			PoemID:         "p1",
			PoemTitle:      "面朝大海，春暖花开",
			Active:         true,
			ActiveVer:      v(3, "dev-b"), // 旧的点亮版本
			Excerpt:        "我有一所房子，面朝大海，春暖花开",
			HighlightLines: "4-4",
			IsCustomQuote:  true,
			ExcerptVer:     v(10, "dev-b"), // 节选版本更高，但不能恢复 Active
		},
	}

	ab := MergeFavorites(devA, devB)
	ba := MergeFavorites(devB, devA)
	if !reflect.DeepEqual(ab, ba) {
		t.Fatalf("MergeFavorites violated commutativity")
	}
	if len(ab) != 1 {
		t.Fatalf("expected 1 favorite by (bookId, poemId), got %d", len(ab))
	}
	got := ab[0]
	if got.Active {
		t.Fatalf("excerpt edit resurrected unfavorited poem: %+v", got)
	}
	if got.Excerpt != "我有一所房子，面朝大海，春暖花开" || !got.IsCustomQuote {
		t.Fatalf("newer excerpt not preserved: %+v", got)
	}

	// 显式重新点亮（更高 ActiveVer）可以恢复收藏
	relike := []domain.FavoritePoem{
		{
			ID:        "fav_c",
			BookID:    "b1",
			PoemID:    "p1",
			Active:    true,
			ActiveVer: v(11, "dev-b"),
		},
	}
	res := MergeFavorites(ab, relike)
	if !res[0].Active {
		t.Fatalf("explicit relike failed to restore favorite")
	}
}

func TestMergeProgressNoStatusRegressionAndReadPoemIDsUnion(t *testing.T) {
	// 设备 A 已读完（30 首并标 finished），设备 B 离线翻看第 1 首和第 4 首
	devA := []domain.ReadingProgress{
		{
			BookID:      "b1",
			LastPoemID:  "p3",
			PositionVer: v(3, "dev-a"),
			Status:      domain.ReadStatusFinished,
			FinishedAt:  1000,
			StatusVer:   v(5, "dev-a"),
			ReadPoemIDs: []string{"p1", "p2", "p3"},
		},
	}
	devB := []domain.ReadingProgress{
		{
			BookID:           "b1",
			LastPoemID:       "p1",
			PositionVer:      v(9, "dev-b"), // 续读位置更新，但未改动完读标记
			Status:           domain.ReadStatusReading,
			StatusVer:        v(1, "dev-b"),
			ReadPoemIDs:      []string{"p4", "p1"},
			LocalScrollRatio: 0.75,
		},
	}

	ab := MergeProgress(devA, devB)
	ba := MergeProgress(devB, devA)
	if !reflect.DeepEqual(ab, ba) {
		t.Fatalf("MergeProgress violated commutativity")
	}
	p := ab[0]
	if p.Status != domain.ReadStatusFinished || p.FinishedAt != 1000 {
		t.Fatalf("finished status regressed: %+v", p)
	}
	if p.LastPoemID != "p1" {
		t.Fatalf("LastPoemID = %q, want p1", p.LastPoemID)
	}
	wantIDs := []string{"p1", "p2", "p3", "p4"}
	if !reflect.DeepEqual(p.ReadPoemIDs, wantIDs) || p.ReadPoemCount() != 4 {
		t.Fatalf("ReadPoemIDs = %v, want %v", p.ReadPoemIDs, wantIDs)
	}
	if p.LocalScrollRatio != 0 {
		t.Fatalf("LocalScrollRatio should not be carried in merged progress")
	}
}

func TestThreeWayAssociativityCommutativityIdempotencyWithSameCounterTie(t *testing.T) {
	// A、B、C 在相同 Lamport Counter=5 下离线修改同一条句子，通过 InstallID 全序决胜
	snapA := SnapshotData{
		Quotes: []domain.Quote{
			{ID: "q1", Content: "版本A", BodyVer: v(5, "dev-a"), IsFavorite: false, FavoriteVer: v(5, "dev-a")},
		},
	}
	snapB := SnapshotData{
		Quotes: []domain.Quote{
			{ID: "q1", Content: "版本B", BodyVer: v(5, "dev-b"), IsFavorite: true, FavoriteVer: v(5, "dev-b")},
		},
	}
	snapC := SnapshotData{
		Quotes: []domain.Quote{
			{ID: "q1", Content: "版本C", BodyVer: v(5, "dev-c"), IsFavorite: false, FavoriteVer: v(4, "dev-c")},
		},
	}

	// 验证任意合并顺序结果完全一致
	ab_c := MergeSnapshotData(MergeSnapshotData(snapA, snapB), snapC)
	a_bc := MergeSnapshotData(snapA, MergeSnapshotData(snapB, snapC))
	cb_a := MergeSnapshotData(MergeSnapshotData(snapC, snapB), snapA)
	idem := MergeSnapshotData(ab_c, ab_c)

	if !reflect.DeepEqual(ab_c, a_bc) || !reflect.DeepEqual(ab_c, cb_a) || !reflect.DeepEqual(ab_c, idem) {
		t.Fatalf("3-way merge violated algebraic laws:\nab_c=%+v\na_bc=%+v\ncb_a=%+v", ab_c, a_bc, cb_a)
	}
	if ab_c.Quotes[0].Content != "版本C" || !ab_c.Quotes[0].IsFavorite {
		t.Fatalf("tie-breaker result unexpected: %+v", ab_c.Quotes[0])
	}
}

func TestSPBookEncodeDecodeRoundTripAndValidation(t *testing.T) {
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
	decoded, err := DecodeSPBook(compressed, hash, "b1")
	if err != nil {
		t.Fatalf("DecodeSPBook failed: %v", err)
	}
	if decoded.Title != orig.Title || len(decoded.Poems) != 1 || decoded.Poems[0].Title != "面朝大海，春暖花开" {
		t.Fatalf("decoded mismatch: %+v", decoded)
	}
	if _, err := DecodeSPBook(compressed, "bad_hash", "b1"); err == nil {
		t.Fatalf("expected hash mismatch error")
	}
	if _, err := DecodeSPBook(compressed, hash, "other_book"); err == nil {
		t.Fatalf("expected bookId mismatch error")
	}
}

func TestMergeReadingEventsDedupAndAlgebraicLaws(t *testing.T) {
	a := []domain.ReadingEvent{
		{ID: "ev-a1", BookID: "b1", PoemID: "p1", PoemTitle: "", Author: "", ReadDate: "2026-11-20", ReadHour: 9, ReadAt: 1000, IsReread: false, DeviceID: "dev-a"},
		{ID: "ev-a2", BookID: "b1", PoemID: "p2", PoemTitle: "面朝大海", Author: "海子", ReadDate: "2026-11-21", ReadHour: 22, ReadAt: 2000, IsReread: false, DeviceID: "dev-a"},
	}
	b := []domain.ReadingEvent{
		{ID: "ev-b1", BookID: "b1", PoemID: "p1", PoemTitle: "日记", Author: "", ReadDate: "2026-11-20", ReadHour: 21, ReadAt: 1500, IsReread: true, DeviceID: "dev-b"},
	}
	c := []domain.ReadingEvent{
		{ID: "ev-c1", BookID: "b1", PoemID: "p1", PoemTitle: "日记（补）", Author: "海子", ReadDate: "2026-11-20", ReadHour: 23, ReadAt: 1800, IsReread: true, DeviceID: "dev-c"},
	}

	ab_c := MergeReadingEvents(MergeReadingEvents(a, b), c)
	a_bc := MergeReadingEvents(a, MergeReadingEvents(b, c))
	cb_a := MergeReadingEvents(MergeReadingEvents(c, b), a)
	idem := MergeReadingEvents(ab_c, ab_c)

	if !reflect.DeepEqual(ab_c, a_bc) || !reflect.DeepEqual(ab_c, cb_a) || !reflect.DeepEqual(ab_c, idem) {
		t.Fatalf("MergeReadingEvents violated algebraic laws:\nab_c=%+v\na_bc=%+v\ncb_a=%+v", ab_c, a_bc, cb_a)
	}
	if len(ab_c) != 2 {
		t.Fatalf("expected 2 merged events, got %d: %+v", len(ab_c), ab_c)
	}
	if ab_c[0].ID != "ev-a1" || ab_c[0].PoemTitle != "日记" || ab_c[0].Author != "海子" || ab_c[0].IsReread {
		t.Fatalf("unexpected merged first event: %+v", ab_c[0])
	}
}
