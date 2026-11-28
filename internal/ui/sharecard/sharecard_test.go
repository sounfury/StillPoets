package sharecard

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"stillpoets/internal/domain"
	"stillpoets/internal/storage"
	"stillpoets/internal/ui/theme"
)

func TestRenderPNGAllKindsAndDynamicHeight(t *testing.T) {
	now := time.Date(2026, 11, 28, 21, 30, 0, 0, time.Local)
	cfg := DefaultRenderConfig()
	opts := DefaultOptions(theme.PresetArtbook)

	// 1. 单句卡片
	quoteData := CardData{
		Kind:       KindQuote,
		Date:       now,
		QuoteText:  "那里，一切皆有序而美，\n奢华、安宁，\n与欢愉。",
		QuoteTag:   "诗歌节选",
		Author:     "波德莱尔",
		SourceBook: "恶之花",
		PoemTitle:  "邀游",
	}
	quotePNG, qw, qh, err := RenderPNG(quoteData, opts, cfg)
	if err != nil {
		t.Fatalf("RenderPNG quote failed: %v", err)
	}
	if qw != 1080 {
		t.Fatalf("quote png width = %d, want 1080", qw)
	}
	if qh < 600 || qh > 2400 {
		t.Fatalf("unexpected quote png height = %d", qh)
	}
	if _, err := png.DecodeConfig(bytes.NewReader(quotePNG)); err != nil {
		t.Fatalf("quote png invalid: %v", err)
	}
	maybeDumpCard(t, "1-quote-minimal-artbook.png", quotePNG)

	// 2. 长整诗卡片（验证动态高度随诗节自然撑开，明显高于短单句）
	poemData := CardData{
		Kind:         KindPoem,
		Date:         now,
		BookID:       "book-haizi",
		BookTitle:    "海子诗集",
		Author:       "海子",
		PoemTitle:    "日记",
		PoemSubtitle: "写给德令哈的夜",
		Epigraph:     "姐姐，今夜我在德令哈。",
		Stanzas: []domain.Stanza{
			{Lines: []string{"姐姐，今夜我在德令哈，夜色笼罩", "姐姐，我今夜只有戈壁"}},
			{Lines: []string{"草原尽头我两手空空", "悲痛时握不住一颗泪滴"}},
			{Lines: []string{"姐姐，今夜我在德令哈", "这是雨水中一座荒凉的城"}},
			{Lines: []string{"除了那些路过的和居住的", "德令哈……今夜", "这是唯一的，最后的，抒情。"}},
			{Lines: []string{"这是唯一的，最后的，草原。", "我把石头还给石头", "让胜利的胜利"}},
			{Lines: []string{"今夜青稞只属于她自己", "一切都在生长", "今夜我只有美丽的戈壁 空空"}},
			{Lines: []string{"姐姐，今夜我不关心人类，我只想你"}},
		},
		Colophon: "1988.7.25",
	}
	poemPNG, pw, ph, err := RenderPNG(poemData, Options{Template: TemplateSeal, Theme: ThemeXuanPaper, ShowWatermark: true}, cfg)
	if err != nil {
		t.Fatalf("RenderPNG poem failed: %v", err)
	}
	maybeDumpCard(t, "2-poem-seal-xuan.png", poemPNG)
	if pw != 1080 {
		t.Fatalf("poem png width = %d, want 1080", pw)
	}
	if ph <= qh {
		t.Fatalf("expected long poem height (%d) > short quote height (%d)", ph, qh)
	}

	// 3. 诗集精选合辑
	colData := CardData{
		Kind:          KindCollection,
		Date:          now,
		BookID:        "book-haizi",
		BookTitle:     "海子诗集",
		Author:        "海子",
		FavoriteCount: 3,
		CollectionItems: []CollectionItem{
			{PoemTitle: "日记", Author: "海子", Excerpt: "姐姐，今夜我在德令哈，夜色笼罩"},
			{PoemTitle: "面朝大海，春暖花开", Author: "海子", Excerpt: "从明天起，做一个幸福的人\n喂马、劈柴，周游世界"},
			{PoemTitle: "黑夜的献诗", Author: "海子", Excerpt: "黑夜从大地上升起\n遮住了光明的天空"},
		},
	}
	colPNG, cw, ch, err := RenderPNG(colData, Options{Template: TemplateEditorial, Theme: ThemeEditorial, ShowWatermark: true}, cfg)
	if err != nil {
		t.Fatalf("RenderPNG collection failed: %v", err)
	}
	maybeDumpCard(t, "3-collection-editorial.png", colPNG)
	if cw != 1080 || ch < 900 {
		t.Fatalf("unexpected collection dimensions: %dx%d", cw, ch)
	}

	// 4. 完读纪念卡
	finData := CardData{
		Kind:           KindFinished,
		Date:           now,
		BookID:         "book-baudelaire",
		BookTitle:      "恶之花",
		Author:         "波德莱尔",
		FinishedAt:     now.Unix(),
		ReadPoemCount:  42,
		TotalPoemCount: 42,
		FavoriteCount:  9,
		QuoteCount:     14,
		CollectionItems: []CollectionItem{
			{PoemTitle: "邀游", Excerpt: "那里，一切皆有序而美，\n奢华、安宁，与欢愉。"},
		},
	}
	finPNG, fw, fh, err := RenderPNG(finData, Options{Template: TemplateArtbook, Theme: ThemeDarkMinimal, ShowWatermark: true}, cfg)
	if err != nil {
		t.Fatalf("RenderPNG finished failed: %v", err)
	}
	maybeDumpCard(t, "4-finished-artbook-dark.png", finPNG)
	if fw != 1080 || fh < 800 {
		t.Fatalf("unexpected finished dimensions: %dx%d", fw, fh)
	}

	// 5. 周期读诗报告
	hmWeeks := make([]storage.HeatmapWeek, 20)
	for wi := range hmWeeks {
		for di := 0; di < 7; di++ {
			hmWeeks[wi].Days[di] = storage.HeatmapDay{
				Date:  "2026-11-01",
				Count: (wi + di) % 5,
				Level: (wi + di) % 5,
			}
		}
	}
	var hourly [24]int
	hourly[21], hourly[22], hourly[23], hourly[0] = 6, 12, 9, 4
	repData := CardData{
		Kind:              KindStatsReport,
		Date:              now,
		ReportPeriodLabel: "2026 年度读诗报告",
		ReportRangeNote:   "2026.01.01 — 2026.11.28",
		Overview: storage.WindowOverview{
			ReadPoemCount:     186,
			FinishedBookCount: 6,
			ActiveDays:        92,
			FavoriteCount:     34,
			QuoteCount:        48,
		},
		Heatmap: storage.HeatmapData{
			Weeks:       hmWeeks,
			ActiveDays:  92,
			TotalEvents: 214,
		},
		Rhythm: storage.ReadingRhythm{
			TotalEvents: 214,
			Hourly:      hourly,
			Insight:     "你偏爱在入夜至深夜读诗，21:00–01:00 贡献了 48% 的读诗时刻。",
		},
		TopAuthors: []storage.AuthorStat{
			{Author: "海子", ReadCount: 42, FavoriteCount: 11, QuoteCount: 8, Score: 91},
			{Author: "波德莱尔", ReadCount: 28, FavoriteCount: 7, QuoteCount: 5, Score: 59},
			{Author: "里尔克", ReadCount: 19, FavoriteCount: 5, QuoteCount: 4, Score: 42},
		},
		FinishedTitles: []string{"海子诗集", "恶之花", "草叶集"},
	}
	repPNG, rw, rh, err := RenderPNG(repData, Options{Template: TemplateCalendar, Theme: ThemeVintageKraft, ShowWatermark: false}, cfg)
	if err != nil {
		t.Fatalf("RenderPNG report failed: %v", err)
	}
	maybeDumpCard(t, "5-report-calendar-kraft.png", repPNG)
	if rw != 1080 || rh < 1200 {
		t.Fatalf("unexpected report dimensions: %dx%d", rw, rh)
	}
	t.Logf("rendered sizes: quote=%dx%d poem=%dx%d col=%dx%d fin=%dx%d report=%dx%d",
		qw, qh, pw, ph, cw, ch, fw, fh, rw, rh)
}

func TestRenderAllTemplateThemeCombinations(t *testing.T) {
	data := CardData{
		Kind:       KindQuote,
		Date:       time.Date(2026, 11, 28, 9, 0, 0, 0, time.Local),
		QuoteText:  "黑夜给了我黑色的眼睛，\n我却用它寻找光明。",
		Author:     "顾城",
		SourceBook: "一代人",
	}
	// 用 1x 快速遍历全部 5 模板 × 5 纸面主题 = 25 组组合，确保无 panic 且尺寸合法
	cfg := RenderConfig{WidthDIP: 360, Scale: 1.0, MaxHeightDIP: 3200}
	for _, tpl := range Templates() {
		for _, th := range AllThemes() {
			raw, w, h, err := RenderPNG(data, Options{
				Template:      tpl.ID,
				Theme:         th.ID,
				ShowWatermark: true,
			}, cfg)
			if err != nil {
				t.Fatalf("template=%s theme=%s failed: %v", tpl.ID, th.ID, err)
			}
			if len(raw) == 0 || w != 360 || h < minExportHeightDIP {
				t.Fatalf("template=%s theme=%s unexpected output: bytes=%d %dx%d", tpl.ID, th.ID, len(raw), w, h)
			}
		}
	}
}

func TestExportPNGFileAndPrune(t *testing.T) {
	dir := t.TempDir()
	// 预置 14 个旧导出文件，验证导出后自动保留最近 12 个
	for i := 0; i < 14; i++ {
		p := filepath.Join(dir, "stillpoets_quote_20250101_0000"+string(rune('a'+i))+".png")
		if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
			t.Fatalf("seed stale file: %v", err)
		}
		mt := time.Now().Add(-time.Duration(20-i) * time.Minute)
		_ = os.Chtimes(p, mt, mt)
	}

	res, err := ExportPNGFile(dir, CardData{
		Kind:      KindQuote,
		QuoteText: "测试落盘与自动清理",
		Author:    "StillPoets",
	}, DefaultOptions(theme.PresetArtbook), RenderConfig{WidthDIP: 360, Scale: 2.0})
	if err != nil {
		t.Fatalf("ExportPNGFile failed: %v", err)
	}
	if _, err := os.Stat(res.FilePath); err != nil {
		t.Fatalf("exported file missing: %v", err)
	}
	if res.WidthPx != 720 || res.Bytes <= 0 {
		t.Fatalf("unexpected export result: %+v", res)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != maxExportKeepFiles {
		t.Fatalf("export dir entries = %d, want pruned to %d", len(entries), maxExportKeepFiles)
	}
}

func maybeDumpCard(t *testing.T, name string, raw []byte) {
	t.Helper()
	dir := os.Getenv("STILLPOETS_DUMP_CARDS")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir dump dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
		t.Fatalf("write dump card %s: %v", name, err)
	}
}
