package views

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
	"stillpoets/internal/domain"
	"stillpoets/internal/storage"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/theme"
)

func TestStatsDashboardRenderAndInteractions(t *testing.T) {
	ctx := t.Context()
	svc, err := app.Open(ctx, t.TempDir(), app.NewMemorySecretStore())
	if err != nil {
		t.Fatalf("app.Open: %v", err)
	}
	defer svc.Close()

	seedBook := func(id, title, author string, poems []domain.Poem) domain.Book {
		sp := &domain.SPBook{
			SchemaVersion: 1,
			BookID:        id,
			Title:         title,
			Author:        author,
			SourceFormat:  "epub",
			Poems:         poems,
		}
		b := domain.Book{
			ID:          id,
			Title:       title,
			Author:      author,
			ContentHash: "hash-" + id,
			RawFilename: id + ".epub",
			PoemCount:   len(poems),
		}
		if err := svc.Store().UpsertImportedBook(ctx, b, sp); err != nil {
			t.Fatalf("UpsertImportedBook %s: %v", id, err)
		}
		return b
	}

	bHaizi := seedBook("bk-hz", "海子诗全集", "海子", []domain.Poem{
		{ID: "hz-1", BookID: "bk-hz", OrderIndex: 0, Kind: domain.EntryKindPoem, Title: "日记", Author: "海子", Stanzas: []domain.Stanza{{Lines: []string{"姐姐，今夜我在德令哈"}}}},
		{ID: "hz-2", BookID: "bk-hz", OrderIndex: 1, Kind: domain.EntryKindPoem, Title: "面朝大海，春暖花开", Author: "海子", Stanzas: []domain.Stanza{{Lines: []string{"从明天起，做一个幸福的人"}}}},
	})
	bRilke := seedBook("bk-rk", "里尔克诗选", "里尔克", []domain.Poem{
		{ID: "rk-1", BookID: "bk-rk", OrderIndex: 0, Kind: domain.EntryKindPoem, Title: "秋日", Author: "里尔克", Stanzas: []domain.Stanza{{Lines: []string{"主啊，是时候了。"}}}},
		{ID: "rk-2", BookID: "bk-rk", OrderIndex: 1, Kind: domain.EntryKindPoem, Title: "豹", Author: "里尔克", Stanzas: []domain.Stanza{{Lines: []string{"它的目光被那走不完的铁栏缠得这般疲倦"}}}},
	})
	bAnth := seedBook("bk-an", "现代诗选", "佚名", []domain.Poem{
		{ID: "an-1", BookID: "bk-an", OrderIndex: 0, Kind: domain.EntryKindPoem, Title: "一代人", Author: "顾城", Stanzas: []domain.Stanza{{Lines: []string{"黑夜给了我黑色的眼睛"}}}},
		{ID: "an-2", BookID: "bk-an", OrderIndex: 1, Kind: domain.EntryKindPoem, Title: "错误", Author: "郑愁予", Stanzas: []domain.Stanza{{Lines: []string{"我打江南走过"}}}},
		{ID: "an-3", BookID: "bk-an", OrderIndex: 2, Kind: domain.EntryKindPoem, Title: "雨巷", Author: "戴望舒", Stanzas: []domain.Stanza{{Lines: []string{"撑着油纸伞"}}}},
	})

	// 完读 2 本，1 本在读
	_, _ = svc.RecordPoemRead(ctx, bHaizi.ID, "hz-1", 0, 2)
	_, _ = svc.RecordPoemRead(ctx, bHaizi.ID, "hz-2", 0, 2)
	_, _ = svc.RecordPoemRead(ctx, bRilke.ID, "rk-1", 0, 2)
	_, _ = svc.MarkBookFinished(ctx, bRilke.ID, true)
	_, _ = svc.RecordPoemRead(ctx, bAnth.ID, "an-1", 0, 3)

	// 精选与散页
	_, _ = svc.ToggleFavoritePoem(ctx, bHaizi, domain.Poem{ID: "hz-1", BookID: bHaizi.ID, Title: "日记", Author: "海子"}, true)
	_, _ = svc.ToggleFavoritePoem(ctx, bRilke, domain.Poem{ID: "rk-1", BookID: bRilke.ID, Title: "秋日", Author: "里尔克"}, true)
	_, _ = svc.CreateStandaloneQuote(ctx, "黑夜给了我黑色的眼睛，我却用它寻找光明。", "顾城", "一代人")
	_, _ = svc.CreateStandaloneQuote(ctx, "有何胜利可言，挺住意味一切。", "里尔克", "给青年诗人的信")

	// 注入近 3 周多时段读诗事件，点亮热力图与昼夜节律
	now := time.Now()
	events := []struct {
		bookID, poemID, title, author string
		daysAgo, hour                 int
		reread                        bool
	}{
		{"bk-hz", "hz-1", "日记", "海子", 0, 21, false},
		{"bk-hz", "hz-2", "面朝大海，春暖花开", "海子", 0, 22, false},
		{"bk-rk", "rk-1", "秋日", "里尔克", 0, 23, false},
		{"bk-hz", "hz-1", "日记", "海子", 1, 20, true},
		{"bk-an", "an-1", "一代人", "顾城", 2, 7, false},
		{"bk-an", "an-2", "错误", "郑愁予", 4, 15, false},
		{"bk-rk", "rk-2", "豹", "里尔克", 6, 22, false},
		{"bk-an", "an-3", "雨巷", "戴望舒", 11, 10, false},
		{"bk-hz", "hz-2", "面朝大海，春暖花开", "海子", 14, 0, true},
	}
	for _, ev := range events {
		ts := time.Date(now.Year(), now.Month(), now.Day()-ev.daysAgo, ev.hour, 18, 0, 0, now.Location())
		if _, _, err := svc.Store().RecordReadingEvent(ctx, storage.RecordReadingEventInput{
			BookID:        ev.bookID,
			PoemID:        ev.poemID,
			PoemTitle:     ev.title,
			Author:        ev.author,
			ReadAt:        ts,
			WasReadBefore: ev.reread,
		}); err != nil {
			t.Fatalf("RecordReadingEvent: %v", err)
		}
	}

	appUI := NewAppUI(svc, nil, nil)
	appUI.activeTab = components.TabStats
	appUI.reloadSync(ctx)

	if appUI.statsDashboard.Overview.FinishedBookCount != 2 {
		t.Fatalf("expected 2 finished books, got %d", appUI.statsDashboard.Overview.FinishedBookCount)
	}
	if appUI.statsDashboard.Heatmap.ActiveDays < 5 {
		t.Fatalf("expected >=5 active heatmap days, got %d", appUI.statsDashboard.Heatmap.ActiveDays)
	}
	if len(appUI.statsDashboard.Authors) < 4 {
		t.Fatalf("expected >=4 poets in cloud, got %d", len(appUI.statsDashboard.Authors))
	}
	if appUI.statsSelectedDate == "" || len(appUI.statsDayEvents) == 0 {
		t.Fatalf("expected default selected active date with events, got date=%q len=%d", appUI.statsSelectedDate, len(appUI.statsDayEvents))
	}

	// 1. 手机端 · 画册风（五时段环图 + 热力图 + 当日明细）
	appUI.settings.ThemePreset = string(theme.PresetArtbook)
	imgPhoneArt := ui.Render(appUI.Render, 390, 844, 2.0)
	if imgPhoneArt == nil {
		t.Fatalf("artbook phone stats render returned nil")
	}
	maybeSavePNG(t, imgPhoneArt, "stats-artbook-phone-top")

	// 2. 平板端 · 画册风（宽屏完整纵向预览，一次性核验总览/热力图/五时段环图/完读墙/诗人云/进度列表）
	imgTabletArt := ui.Render(appUI.Render, 1024, 1680, 2.0)
	if imgTabletArt == nil {
		t.Fatalf("artbook tablet stats render returned nil")
	}
	maybeSavePNG(t, imgTabletArt, "stats-artbook-tablet")

	// 2.5 验证时间窗口切换与作者云权重切换
	appUI.setStatsPeriod(storage.StatsPeriodWeek)
	appUI.setStatsAuthorMode(storage.AuthorWeightRead)
	appUI.reloadSync(ctx)
	if appUI.statsDashboard.Period != storage.StatsPeriodWeek || appUI.statsDashboard.AuthorMode != storage.AuthorWeightRead {
		t.Fatalf("expected week/read filter applied, got period=%q mode=%q",
			appUI.statsDashboard.Period, appUI.statsDashboard.AuthorMode)
	}
	appUI.setStatsPeriod(storage.StatsPeriodAll)
	appUI.setStatsAuthorMode(storage.AuthorWeightCombined)
	appUI.reloadSync(ctx)

	// 3. 24 小时柱状图 + 刊物风
	appUI.statsRhythm24h = true
	appUI.settings.ThemePreset = string(theme.PresetEditorial)
	imgEditorial24 := ui.Render(appUI.Render, 1024, 1500, 2.0)
	if imgEditorial24 == nil {
		t.Fatalf("editorial 24h stats render returned nil")
	}
	maybeSavePNG(t, imgEditorial24, "stats-editorial-tablet-24h")

	// 4. 暗夜极简 · 手机端
	appUI.statsRhythm24h = false
	appUI.settings.ThemePreset = string(theme.PresetDarkMinimal)
	imgDark := ui.Render(appUI.Render, 390, 844, 2.0)
	if imgDark == nil {
		t.Fatalf("dark minimal phone stats render returned nil")
	}
	maybeSavePNG(t, imgDark, "stats-dark-phone")

	// 5. 完读回望卡弹窗渲染
	appUI.settings.ThemePreset = string(theme.PresetArtbook)
	appUI.openFinishedBookDetail(appUI.statsDashboard.FinishedBooks[0])
	imgFinModal := ui.Render(appUI.Render, 390, 844, 2.0)
	if imgFinModal == nil {
		t.Fatalf("finished book modal render returned nil")
	}
	maybeSavePNG(t, imgFinModal, "stats-finished-modal")
	appUI.showFinishedModal = false

	// 6. 诗人档案卡下钻渲染
	prof, err := svc.GetAuthorProfile(ctx, "海子")
	if err != nil {
		t.Fatalf("GetAuthorProfile 海子: %v", err)
	}
	appUI.detailAuthorName = "海子"
	appUI.detailAuthorProfile = &prof
	appUI.detailAuthorLoading = false
	appUI.showAuthorModal = true
	imgAuthorModal := ui.Render(appUI.Render, 390, 844, 2.0)
	if imgAuthorModal == nil {
		t.Fatalf("author profile modal render returned nil")
	}
	maybeSavePNG(t, imgAuthorModal, "stats-author-modal")
	appUI.showAuthorModal = false

	// 7. 验证从统计页下钻跳诗进入偶遇模式（entryStats），不破坏主线续读位置
	appUI.jumpToPoemFromStats(bAnth.ID, "an-2", nil, true)
	if appUI.activeTab != components.TabShelf || appUI.readerEntrySource != entryStats || !appUI.readerMobile {
		t.Fatalf("unexpected jumpToPoemFromStats state: tab=%v src=%q mobile=%v",
			appUI.activeTab, appUI.readerEntrySource, appUI.readerMobile)
	}

	// 8. 验证有效读诗停留追踪器：快速划过不落库，显式收藏/留散页（force=true）立即落库
	appUI.startPoemDwell(bAnth.ID, domain.Poem{ID: "an-3", BookID: bAnth.ID, Kind: domain.EntryKindPoem, Title: "雨巷", Author: "戴望舒"}, bAnth.Author, false)
	appUI.flushCurrentPoemDwell(false) // 刚进入 < 1.5s，应过滤为噪声
	todayStr := time.Now().Format("2006-01-02")
	dayBefore, _ := svc.ListReadingEventsByDate(ctx, todayStr)
	for _, it := range dayBefore {
		if it.Event.PoemID == "an-3" {
			t.Fatalf("expected quick glance <1.5s to be filtered out, but an-3 was recorded")
		}
	}

	appUI.startPoemDwell(bAnth.ID, domain.Poem{ID: "an-3", BookID: bAnth.ID, Kind: domain.EntryKindPoem, Title: "雨巷", Author: "戴望舒"}, bAnth.Author, false)
	appUI.flushCurrentPoemDwell(true) // 强制确认（模拟点亮 ❤️ / 划句留散页）
	deadline := time.Now().Add(2 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		dayAfter, _ := svc.ListReadingEventsByDate(ctx, todayStr)
		for _, it := range dayAfter {
			if it.Event.PoemID == "an-3" {
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !found {
		t.Fatalf("expected force-flushed reading event for an-3 to be persisted")
	}
}
