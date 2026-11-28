package views

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
	"stillpoets/internal/domain"
)

func setupLargeBookForTOCTest(t *testing.T) (*app.Service, *AppUI, domain.Book) {
	t.Helper()
	ctx := t.Context()
	svc, err := app.Open(ctx, t.TempDir(), app.NewMemorySecretStore())
	if err != nil {
		t.Fatalf("app.Open: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })

	poems := make([]domain.Poem, 0, 105)
	for i := 0; i < 105; i++ {
		title := fmt.Sprintf("第%03d首诗", i+1)
		lines := []string{fmt.Sprintf("这是第 %d 首的普通诗行", i+1)}
		switch i + 1 {
		case 93:
			title = "日记"
			lines = []string{"姐姐，今夜我在德令哈，夜色笼罩", "姐姐，我今夜只有戈壁"}
		case 98:
			title = "面朝大海，春暖花开"
			lines = []string{"从明天起，做一个幸福的人", "喂马、劈柴，周游世界"}
		case 99:
			title = "四姐妹"
			lines = []string{"荒凉的山冈上站着四姐妹"}
		case 105:
			title = "春天，十个海子"
			lines = []string{"春天，十个海子全都复活", "在光明的景色中"}
		}
		poems = append(poems, domain.Poem{
			ID:         fmt.Sprintf("haizi-%03d", i+1),
			BookID:     "book-haizi-105",
			OrderIndex: i,
			Kind:       domain.EntryKindPoem,
			Title:      title,
			Author:     "海子",
			Stanzas:    []domain.Stanza{{Lines: lines}},
		})
	}

	sp := &domain.SPBook{
		SchemaVersion: 1,
		BookID:        "book-haizi-105",
		Title:         "海子的诗",
		Author:        "海子",
		SourceFormat:  "epub",
		Poems:         poems,
	}
	book := domain.Book{
		ID:          "book-haizi-105",
		Title:       "海子的诗",
		Author:      "海子",
		ContentHash: "hash-haizi-105",
		RawFilename: "haizi-105.epub",
		PoemCount:   105,
	}
	if err := svc.Store().UpsertImportedBook(ctx, book, sp); err != nil {
		t.Fatalf("UpsertImportedBook: %v", err)
	}

	appUI := NewAppUI(svc, nil, nil)
	appUI.openBook(book.ID, "haizi-001", nil, true)
	appUI.reloadSync(ctx)
	return svc, appUI, book
}

func TestTOCScrollReachesLastPoem105OnPhone(t *testing.T) {
	_, appUI, _ := setupLargeBookForTOCTest(t)

	const winW, winH = 390, 844
	tester := ui.NewTester(appUI.Render, winW, winH)
	tester.Frame()

	// 打开目录抽屉并完整跑完 mobile.Sheet 两帧启动 + 弹簧升起动画
	appUI.showTOCModal = true
	tester.Frame()
	tester.Frame()
	tester.Advance(800 * time.Millisecond)
	tester.Frame()

	// 1. 模拟真机手指在列表区域向上滑，确认 mobile.Sheet 不会抢占列表纵向滚动
	tester.FingerDown(winW/2, winH*0.78)
	for step := 1; step <= 8; step++ {
		tester.FingerMove(winW/2, winH*0.78-float32(step)*40)
		tester.Advance(16 * time.Millisecond)
	}
	tester.FingerUp(winW/2, winH*0.78-320)
	tester.Advance(300 * time.Millisecond)
	tester.Frame()
	if appUI.tocScroll.Y <= 0 {
		t.Fatalf("expected touch swipe up inside TOC sheet to scroll list, got tocScroll=%+v", appUI.tocScroll)
	}

	// 2. 继续滚动到最底部，验证第 105 首完全落在可视窗口内且可点击
	for i := 0; i < 20; i++ {
		tester.Scroll(winW/2, winH*0.65, 0, 300)
		tester.Frame()
	}
	t.Logf("after scroll: tocScroll=%+v", appUI.tocScroll)

	rect, ok := tester.Find("春天，十个海子")
	if !ok {
		t.Fatalf("expected last poem #105 to be rendered after scrolling TOC to bottom; scrollY=%.1f", appUI.tocScroll.Y)
	}
	if rect.Y < 0 || rect.Y+rect.H > float32(winH) {
		t.Fatalf("last poem #105 clipped outside viewport (winH=%d): rect=%+v", winH, rect)
	}

	// 验证可以直接点击第 105 首并跳入阅读器
	if err := tester.Click("春天，十个海子"); err != nil {
		t.Fatalf("click last poem #105: %v", err)
	}
	tester.Frame()
	if appUI.showTOCModal {
		t.Fatalf("expected TOC modal to close after clicking poem #105")
	}
	if appUI.activePoemIdx != 104 {
		t.Fatalf("expected activePoemIdx=104 (poem #105), got %d", appUI.activePoemIdx)
	}
}

func TestTOCSearchByTitleAndFullTextWithHighlight(t *testing.T) {
	_, appUI, _ := setupLargeBookForTOCTest(t)

	tester := ui.NewTester(appUI.Render, 390, 844)
	tester.Frame()

	appUI.showTOCModal = true
	tester.Frame()
	tester.Frame()
	tester.Advance(800 * time.Millisecond)
	tester.Frame()

	// 1. 按诗题搜索
	appUI.tocQuery = "面朝大海"
	tester.Frame()
	if !tester.HasText("面朝大海，春暖花开") {
		t.Fatalf("expected title search to find 面朝大海，春暖花开, got texts=%v", tester.Texts())
	}
	if tester.HasText("四姐妹") {
		t.Fatalf("expected non-matching poem 四姐妹 to be filtered out")
	}

	// 2. 按正文全文搜索（支持跨行多关键词），并验证展示命中句摘要 + 点击跳入高亮命中行
	appUI.tocQuery = "德令哈 戈壁"
	tester.Frame()
	if !tester.HasText("日记") || !tester.HasText("姐姐，今夜我在德令哈，夜色笼罩") {
		t.Fatalf("expected full-text search to find 日记 with matched line snippet, got texts=%v", tester.Texts())
	}
	if err := tester.Click("日记"); err != nil {
		t.Fatalf("click matched poem 日记: %v", err)
	}
	tester.Frame()
	if appUI.activePoemIdx != 92 {
		t.Fatalf("expected activePoemIdx=92 (poem #93 日记), got %d", appUI.activePoemIdx)
	}
	if len(appUI.highlightRanges) != 1 || appUI.highlightRanges[0].Start != 1 || appUI.highlightRanges[0].End != 1 {
		t.Fatalf("expected matched line 1 highlighted, got %+v", appUI.highlightRanges)
	}
}

func TestGlobalSearchShowsBookTitleSnippetAndHighlightsLine(t *testing.T) {
	svc, appUI, _ := setupLargeBookForTOCTest(t)

	res, err := svc.Search(t.Context(), "德令哈", 20)
	if err != nil {
		t.Fatalf("svc.Search: %v", err)
	}
	if len(res.Poems) != 1 || res.Poems[0].Title != "日记" {
		t.Fatalf("expected FTS search to hit 日记, got %+v", res.Poems)
	}

	appUI.showSearchModal = true
	appUI.searchQuery = "德令哈"
	appUI.searchResults = res

	tester := ui.NewTester(appUI.Render, 390, 844)
	tester.Frame()
	tester.Advance(350 * time.Millisecond)
	tester.Frame()

	if !tester.HasText("《海子的诗》 · 海子") || !tester.HasText("姐姐，今夜我在德令哈，夜色笼罩") {
		t.Fatalf("expected global search card to show book title and matched snippet, got texts=%v", tester.Texts())
	}
	if err := tester.Click("姐姐，今夜我在德令哈，夜色笼罩"); err != nil {
		t.Fatalf("click global search hit: %v", err)
	}
	tester.Frame()
	if len(appUI.highlightRanges) != 1 || appUI.highlightRanges[0].Start != 1 {
		t.Fatalf("expected global search jump to highlight matched line 1, got %+v", appUI.highlightRanges)
	}
}
