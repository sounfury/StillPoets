package app

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"stillpoets/internal/domain"
	"stillpoets/internal/ui/theme"
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

func TestWidgetTimelineStableAcrossRebuildsAndSettingsUpdates(t *testing.T) {
	ctx := t.Context()
	svc, err := Open(ctx, t.TempDir(), NewMemorySecretStore())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer svc.Close()

	// 注入 30 首诗，确保超过 maxWidgetEntries(24)，验证截取集合与顺序稳定性
	poems := make([]domain.Poem, 0, 30)
	for i := 0; i < 30; i++ {
		poems = append(poems, domain.Poem{
			ID:         fmt.Sprintf("p-%02d", i),
			BookID:     "bk-stable",
			OrderIndex: i,
			Kind:       domain.EntryKindPoem,
			Title:      fmt.Sprintf("诗篇 %02d", i),
			Author:     "诗人",
			Stanzas:    []domain.Stanza{{Lines: []string{fmt.Sprintf("这是第 %02d 首诗的第一行", i)}}},
		})
	}
	book := domain.Book{
		ID:          "bk-stable",
		Title:       "稳定诗集",
		Author:      "诗人",
		ContentHash: "hash-stable",
		RawFilename: "stable.epub",
		PoemCount:   len(poems),
	}
	sp := &domain.SPBook{
		SchemaVersion: 1,
		BookID:        book.ID,
		Title:         book.Title,
		Author:        book.Author,
		SourceFormat:  "epub",
		Poems:         poems,
	}
	if err := svc.Store().UpsertImportedBook(ctx, book, sp); err != nil {
		t.Fatalf("UpsertImportedBook: %v", err)
	}

	callbackCount := 0
	svc.SetWidgetUpdatedCallback(func(timelinePath, fontsDir string) {
		callbackCount++
	})

	// 1. 首次构建写入快照并触发 1 次原生刷新
	tl1, err := svc.RebuildWidgetTimeline(ctx)
	if err != nil {
		t.Fatalf("first RebuildWidgetTimeline: %v", err)
	}
	if len(tl1.Entries) != maxWidgetEntries {
		t.Fatalf("entries len = %d, want %d", len(tl1.Entries), maxWidgetEntries)
	}
	if tl1.PoolRevision == "" {
		t.Fatalf("expected non-empty PoolRevision")
	}
	if callbackCount != 1 {
		t.Fatalf("expected 1 callback after initial build, got %d", callbackCount)
	}

	// 2. 模拟冷启动 / WebDAV 同步反复触发 RebuildWidgetTimeline：
	//    条目顺序、PoolRevision、GeneratedAt 必须 100% 保持不变，且不触发冗余原生刷新
	tl2, err := svc.RebuildWidgetTimeline(ctx)
	if err != nil {
		t.Fatalf("second RebuildWidgetTimeline: %v", err)
	}
	if !slices.Equal(tl1.Entries, tl2.Entries) {
		t.Fatalf("entries order changed across identical rebuild")
	}
	if tl2.PoolRevision != tl1.PoolRevision || tl2.GeneratedAt != tl1.GeneratedAt {
		t.Fatalf("expected identical poolRevision/generatedAt: got (%q,%d) want (%q,%d)",
			tl2.PoolRevision, tl2.GeneratedAt, tl1.PoolRevision, tl1.GeneratedAt)
	}
	if callbackCount != 1 {
		t.Fatalf("expected no redundant native callback when timeline unchanged, got %d", callbackCount)
	}

	// 3. 调整切换间隔（180 分钟）与主题配色：
	//    需要通知原生端更新配置，但条目顺序与 PoolRevision 保持不变（不洗牌、不归零手动偏移）
	if _, err := svc.UpdateSettings(ctx, func(st *domain.Settings) {
		st.WidgetRefreshMinutes = 180
		st.ThemePreset = string(theme.PresetEditorial)
		st.WidgetTheme.Preset = string(theme.PresetEditorial)
	}); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	tl3, err := svc.ReadWidgetTimeline()
	if err != nil {
		t.Fatalf("ReadWidgetTimeline: %v", err)
	}
	if tl3.RefreshIntervalMinutes != 180 {
		t.Fatalf("RefreshIntervalMinutes = %d, want 180", tl3.RefreshIntervalMinutes)
	}
	if tl3.PoolRevision != tl1.PoolRevision {
		t.Fatalf("PoolRevision changed on interval/theme edit: %q -> %q", tl1.PoolRevision, tl3.PoolRevision)
	}
	if !slices.Equal(tl1.Entries, tl3.Entries) {
		t.Fatalf("entries order unexpectedly reshuffled on interval/theme edit")
	}
	if callbackCount != 2 {
		t.Fatalf("expected callback on interval/theme update, got %d", callbackCount)
	}

	// 4. 用户显式点击「刷新小组件」调用 ReshuffleWidgetTimeline：
	//    必须递增 PoolRevision、换出新顺序并立刻通知原生端
	tl4, err := svc.ReshuffleWidgetTimeline(ctx)
	if err != nil {
		t.Fatalf("ReshuffleWidgetTimeline: %v", err)
	}
	if tl4.PoolRevision == tl3.PoolRevision {
		t.Fatalf("expected PoolRevision to change after ReshuffleWidgetTimeline")
	}
	if slices.Equal(tl3.Entries, tl4.Entries) {
		t.Fatalf("expected entries order to change after ReshuffleWidgetTimeline")
	}
	if callbackCount != 3 {
		t.Fatalf("expected callback after ReshuffleWidgetTimeline, got %d", callbackCount)
	}
}
