package views

import (
	"image/color"
	"testing"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
	"stillpoets/internal/domain"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/theme"
)

func TestVisualPolishSyntheticRenderAllViews(t *testing.T) {
	ctx := t.Context()
	svc, err := app.Open(ctx, t.TempDir(), app.NewMemorySecretStore())
	if err != nil {
		t.Fatalf("app.Open: %v", err)
	}
	defer svc.Close()

	grpModern, _ := svc.CreateGroup(ctx, "现代诗")
	grpForeign, _ := svc.CreateGroup(ctx, "外国诗")
	_, _ = svc.CreateGroup(ctx, "枕边重读")

	seedBook := func(id, title, author, groupID string, poems []domain.Poem) domain.Book {
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
			GroupID:     groupID,
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

	b1 := seedBook("book-haizi", "海子诗集", "海子", grpModern.ID, []domain.Poem{
		{
			ID: "haizi-1", BookID: "book-haizi", OrderIndex: 0, Kind: domain.EntryKindPoem,
			Title: "日记", Author: "海子",
			Epigraph: "姐姐，今夜我在德令哈。",
			Stanzas: []domain.Stanza{
				{Lines: []string{"姐姐，今夜我在德令哈，夜色笼罩", "姐姐，我今夜只有戈壁"}},
				{Lines: []string{"草原尽头我两手空空", "悲痛时握不住一颗泪滴"}},
			},
			Colophon: "1988.7.25",
		},
		{
			ID: "haizi-2", BookID: "book-haizi", OrderIndex: 1, Kind: domain.EntryKindPoem,
			Title: "面朝大海，春暖花开", Author: "海子",
			Stanzas: []domain.Stanza{
				{Lines: []string{"从明天起，做一个幸福的人", "喂马、劈柴，周游世界"}},
			},
		},
		{
			ID: "haizi-3", BookID: "book-haizi", OrderIndex: 2, Kind: domain.EntryKindPoem,
			Title: "黑夜的献诗", Author: "海子",
			Stanzas: []domain.Stanza{
				{Lines: []string{"黑夜从大地上升起", "遮住了光明的天空"}},
			},
		},
	})
	b2 := seedBook("book-baudelaire", "恶之花", "波德莱尔", grpForeign.ID, []domain.Poem{
		{
			ID: "baud-1", BookID: "book-baudelaire", OrderIndex: 0, Kind: domain.EntryKindPoem,
			Title: "邀游", Author: "波德莱尔",
			Stanzas: []domain.Stanza{
				{Lines: []string{"那里，一切皆有序而美，", "奢华、安宁，", "与欢愉。"}},
				{Lines: []string{"打磨得发亮的家具，", "岁月的幽光将它们擦亮，", "装点着我们的房间。"}},
			},
		},
	})
	b3 := seedBook("book-whitman", "草叶集", "惠特曼", grpForeign.ID, []domain.Poem{
		{
			ID: "whit-1", BookID: "book-whitman", OrderIndex: 0, Kind: domain.EntryKindPoem,
			Title: "自我之歌", Author: "惠特曼",
			Stanzas: []domain.Stanza{
				{Lines: []string{"我以我本来的样子存在，", "这便足够。"}},
			},
		},
	})

	_, _ = svc.RecordPoemRead(ctx, b1.ID, "haizi-1", 0.4, 3)

	// 创建 6 条混合散页，验证 80% 纸白/浅米留白 + 20% 彩色点缀视觉节奏
	_, _ = svc.CreatePoemExcerptQuote(ctx, b2, domain.Poem{ID: "baud-1", BookID: b2.ID, Title: "邀游", Author: "波德莱尔"}, "那里，一切皆有序而美，\n奢华、安宁，\n与欢愉。", "1-3")
	_, _ = svc.CreatePoemExcerptQuote(ctx, b3, domain.Poem{ID: "whit-1", BookID: b3.ID, Title: "自我之歌", Author: "惠特曼"}, "我以我本来的样子存在，\n这便足够。", "1-2")
	_, _ = svc.CreatePoemExcerptQuote(ctx, b1, domain.Poem{ID: "haizi-1", BookID: b1.ID, Title: "日记", Author: "海子"}, "姐姐，今夜我在德令哈，\n夜色笼罩。", "1-2")
	_, _ = svc.CreateStandaloneQuote(ctx, "不急着读完，\n给一句诗多一点时间。", "随记", "我的笔记")
	_, _ = svc.CreatePoemExcerptQuote(ctx, b1, domain.Poem{ID: "haizi-2", BookID: b1.ID, Title: "面朝大海，春暖花开", Author: "海子"}, "从明天起，\n做一个幸福的人。", "1-2")
	_, _ = svc.CreatePoemExcerptQuote(ctx, b1, domain.Poem{ID: "haizi-3", BookID: b1.ID, Title: "黑夜的献诗", Author: "海子"}, "黑夜从大地上升起，\n遮住了光明的天空。", "1-2")

	appUI := NewAppUI(svc, nil, nil)
	appUI.todayReread = &app.TodayReread{
		BookID:         b2.ID,
		BookTitle:      b2.Title,
		PoemID:         "baud-1",
		PoemTitle:      "邀游",
		Author:         "波德莱尔",
		Excerpt:        "那里，一切皆有序而美，\n奢华、安宁，\n与欢愉。",
		HighlightLines: "1-3",
	}
	appUI.openBook(b1.ID, "haizi-1", nil, false)
	appUI.reloadSync(ctx)
	// 固定今日重读为波德莱尔便于对比预览图
	appUI.todayReread = &app.TodayReread{
		BookID:         b2.ID,
		BookTitle:      b2.Title,
		PoemID:         "baud-1",
		PoemTitle:      "邀游",
		Author:         "波德莱尔",
		Excerpt:        "那里，一切皆有序而美，\n奢华、安宁，\n与欢愉。",
		HighlightLines: "1-3",
	}

	// 1. 画册风 · 手机诗集页
	appUI.settings.ThemePreset = string(theme.PresetArtbook)
	appUI.activeTab = components.TabShelf
	appUI.shelfSubTab = components.SubTabBooks
	appUI.readerMobile = false
	imgBooks := ui.Render(appUI.Render, 390, 844, 2.0)
	if imgBooks == nil {
		t.Fatalf("artbook phone books render returned nil")
	}
	maybeSavePNG(t, imgBooks, "polish-artbook-phone-books")

	// 2. 画册风 · 手机散页页
	appUI.shelfSubTab = components.SubTabQuotes
	imgQuotes := ui.Render(appUI.Render, 390, 844, 2.0)
	if imgQuotes == nil {
		t.Fatalf("artbook phone quotes render returned nil")
	}
	maybeSavePNG(t, imgQuotes, "polish-artbook-phone-quotes")

	// 3. 画册风 · 手机散页上滑后（验证导语栏平滑淡出）
	appUI.phoneQuotesScroll.Y = 40
	imgQuotesScrolled := ui.Render(appUI.Render, 390, 844, 2.0)
	if imgQuotesScrolled == nil {
		t.Fatalf("artbook phone quotes scrolled render returned nil")
	}
	maybeSavePNG(t, imgQuotesScrolled, "polish-artbook-phone-quotes-scrolled")
	appUI.phoneQuotesScroll.Y = 0

	// 4. 画册风 · 手机阅读器
	appUI.shelfSubTab = components.SubTabBooks
	appUI.readerMobile = true
	imgReader := ui.Render(appUI.Render, 390, 844, 2.0)
	if imgReader == nil {
		t.Fatalf("artbook phone reader render returned nil")
	}
	maybeSavePNG(t, imgReader, "polish-artbook-phone-reader")
	appUI.readerMobile = false

	// 5. 刊物风 · 验证纯白纸底未被肌理污染
	appUI.settings.ThemePreset = string(theme.PresetEditorial)
	appUI.shelfSubTab = components.SubTabBooks
	imgEditorial := ui.Render(appUI.Render, 390, 844, 2.0)
	if imgEditorial == nil {
		t.Fatalf("editorial phone books render returned nil")
	}
	if px := imgEditorial.RGBAAt(16, 16); px != (color.RGBA{255, 255, 255, 255}) {
		t.Fatalf("editorial top margin pixel = %v, want pure #FFFFFF", px)
	}
	maybeSavePNG(t, imgEditorial, "polish-editorial-phone-books")

	// 6. 暗夜极简 · 手机诗集 + 散页
	appUI.settings.ThemePreset = string(theme.PresetDarkMinimal)
	appUI.shelfSubTab = components.SubTabBooks
	imgDarkBooks := ui.Render(appUI.Render, 390, 844, 2.0)
	if imgDarkBooks == nil {
		t.Fatalf("dark phone books render returned nil")
	}
	maybeSavePNG(t, imgDarkBooks, "polish-dark-phone-books")

	// 7. 画册风 · 平板双栏
	appUI.settings.ThemePreset = string(theme.PresetArtbook)
	appUI.shelfSubTab = components.SubTabBooks
	imgTablet := ui.Render(appUI.Render, 1080, 720, 2.0)
	if imgTablet == nil {
		t.Fatalf("artbook tablet render returned nil")
	}
	maybeSavePNG(t, imgTablet, "polish-artbook-tablet")
}
