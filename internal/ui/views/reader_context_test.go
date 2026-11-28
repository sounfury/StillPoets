package views

import (
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
	"stillpoets/internal/domain"
	"stillpoets/internal/storage"
	"stillpoets/internal/ui/sharecard"
)

func setupLongPressTestApp(t *testing.T) (*app.Service, *AppUI, domain.Book, *domain.SPBook) {
	t.Helper()
	ctx := t.Context()
	svc, err := app.Open(ctx, t.TempDir(), app.NewMemorySecretStore())
	if err != nil {
		t.Fatalf("app.Open: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })

	sp := &domain.SPBook{
		SchemaVersion: 1,
		BookID:        "book-ctx-1",
		Title:         "长按交互测试诗集",
		Author:        "测试诗人",
		SourceFormat:  "epub",
		Poems: []domain.Poem{
			{
				ID:         "poem-short",
				BookID:     "book-ctx-1",
				OrderIndex: 0,
				Kind:       domain.EntryKindPoem,
				Title:      "分节短诗",
				Author:     "测试诗人",
				Stanzas: []domain.Stanza{
					{Lines: []string{"第一段第一句秋风起", "第一段第二句落叶黄"}},
					{Lines: []string{"第二段第一句灯火明", "第二段第二句归舟晚"}},
				},
			},
			{
				ID:         "poem-long",
				BookID:     "book-ctx-1",
				OrderIndex: 1,
				Kind:       domain.EntryKindPoem,
				Title:      "不分节长诗",
				Author:     "测试诗人",
				Stanzas: []domain.Stanza{
					{Lines: []string{
						"长诗第零一行海面微澜",
						"长诗第零二行鸥鸟低飞",
						"长诗第零三行远帆如豆",
						"长诗第零四行云影徘徊",
						"长诗第零五行灯塔初亮",
						"长诗第零六行潮声渐起",
						"长诗第零七行晚风轻拂",
						"长诗第零八行石阶生凉",
						"长诗第零九行归人停步",
						"长诗第十零行夜色温柔",
					}},
				},
			},
		},
	}
	book := domain.Book{
		ID:          "book-ctx-1",
		Title:       "长按交互测试诗集",
		Author:      "测试诗人",
		ContentHash: "hash-ctx-1",
		RawFilename: "ctx.epub",
		PoemCount:   2,
	}
	if err := svc.Store().UpsertImportedBook(ctx, book, sp); err != nil {
		t.Fatalf("UpsertImportedBook: %v", err)
	}

	appUI := NewAppUI(svc, nil, nil)
	appUI.openBook(book.ID, "poem-short", nil, true)
	appUI.reloadSync(ctx)
	return svc, appUI, book, sp
}

func longPressText(t *testing.T, tester *ui.Tester, text string) ui.Rect {
	t.Helper()
	rect, ok := tester.Find(text)
	if !ok {
		t.Fatalf("text %q not found on screen; visible=%v", text, tester.Texts())
	}
	cx := rect.X + rect.W/2
	cy := rect.Y + rect.H/2
	tester.FingerDown(cx, cy)
	tester.Advance(650 * time.Millisecond)
	tester.FingerUp(cx, cy)
	tester.Frame()
	return rect
}

func TestReaderLongPressStanzaPopoverFlow(t *testing.T) {
	svc, appUI, _, _ := setupLongPressTestApp(t)

	var (
		copyMu     sync.Mutex
		copiedText string
	)
	appUI.SetCopyTextFunc(func(s string) {
		copyMu.Lock()
		copiedText = s
		copyMu.Unlock()
	})

	tester := ui.NewTester(appUI.Render, 390, 844)
	tester.Frame()

	// 1. 触屏长按分节短诗的第一段 -> 命中整节并弹出 4 项悬浮菜单
	longPressText(t, tester, "第一段第一句秋风起")
	if appUI.poemCtxTarget == nil {
		t.Fatalf("expected long press inside swipe pager + scroll to open poem context popover")
	}
	if appUI.poemCtxTarget.isWholeRow || appUI.poemCtxTarget.lineStart != 1 || appUI.poemCtxTarget.lineEnd != 2 {
		t.Fatalf("expected whole stanza 1..2 selected, got %+v", appUI.poemCtxTarget)
	}
	for _, label := range []string{"复制", "分享", "留散页", "勾选"} {
		if !tester.HasText(label) {
			t.Fatalf("expected popover action %q to be visible, got texts=%v", label, tester.Texts())
		}
	}

	// 2. 点击「复制」-> 写入整节文本并关闭气泡
	if err := tester.Click("复制"); err != nil {
		t.Fatalf("click 复制: %v", err)
	}
	tester.Frame()
	wantStanza1 := "第一段第一句秋风起\n第一段第二句落叶黄"
	copyMu.Lock()
	gotCopied := copiedText
	copyMu.Unlock()
	if gotCopied != wantStanza1 {
		t.Fatalf("copyTextFn got %q, want %q", gotCopied, wantStanza1)
	}
	if got := tester.Clipboard(); got != wantStanza1 {
		t.Fatalf("ui clipboard got %q, want %q", got, wantStanza1)
	}
	if appUI.poemCtxTarget != nil {
		t.Fatalf("expected popover to dismiss after copy")
	}

	// 3. 再次长按第二段 -> 点击「分享」-> 直达单句卡片弹窗
	longPressText(t, tester, "第二段第一句灯火明")
	if err := tester.Click("分享"); err != nil {
		t.Fatalf("click 分享: %v", err)
	}
	tester.Frame()
	wantStanza2 := "第二段第一句灯火明\n第二段第二句归舟晚"
	if !appUI.showShareCardModal || appUI.shareCardData.Kind != sharecard.KindQuote || appUI.shareCardData.QuoteText != wantStanza2 {
		t.Fatalf("unexpected share card modal state: open=%v kind=%v text=%q", appUI.showShareCardModal, appUI.shareCardData.Kind, appUI.shareCardData.QuoteText)
	}
	// 关闭分享弹窗
	appUI.showShareCardModal = false
	tester.Frame()

	// 4. 长按第一段 -> 点击「留散页」-> 写入诗歌节选散页并高亮
	longPressText(t, tester, "第一段第二句落叶黄")
	if err := tester.Click("留散页"); err != nil {
		t.Fatalf("click 留散页: %v", err)
	}
	tester.Frame()

	deadline := time.Now().Add(3 * time.Second)
	var savedQuotes []storage.QuoteView
	for time.Now().Before(deadline) {
		qs, err := svc.ListQuotes(t.Context(), storage.QuoteFilter{})
		if err == nil && len(qs) > 0 {
			savedQuotes = qs
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if len(savedQuotes) != 1 || savedQuotes[0].Quote.Content != wantStanza1 || savedQuotes[0].Quote.HighlightLines != "1-2" {
		t.Fatalf("expected stanza saved as excerpt quote, got %+v", savedQuotes)
	}

	// 5. 长按第一段 -> 点击「勾选」-> 自动预选第 1-2 行，再点第 3 行实现跨段多选
	longPressText(t, tester, "第一段第一句秋风起")
	if err := tester.Click("勾选"); err != nil {
		t.Fatalf("click 勾选: %v", err)
	}
	// 推进 BarRevealTransition（200ms 折叠展开动画）完成
	tester.Advance(300 * time.Millisecond)
	tester.Frame()
	if !appUI.selectingLines || !appUI.selectedLines[1] || !appUI.selectedLines[2] || len(appUI.selectedLines) != 2 {
		t.Fatalf("expected selection mode pre-populated with stanza lines 1..2, got selecting=%v lines=%v", appUI.selectingLines, appUI.selectedLines)
	}
	if err := tester.Click("第二段第一句灯火明"); err != nil {
		t.Fatalf("click line 3 to extend selection: %v", err)
	}
	tester.Frame()
	if !appUI.selectedLines[3] || len(appUI.selectedLines) != 3 {
		t.Fatalf("expected 3 lines selected across stanzas, got %v", appUI.selectedLines)
	}
	if err := tester.Click("复制"); err != nil {
		t.Fatalf("click selection bar 复制: %v", err)
	}
	tester.Frame()
	wantThreeLines := "第一段第一句秋风起\n第一段第二句落叶黄\n第二段第一句灯火明"
	if got := tester.Clipboard(); got != wantThreeLines {
		t.Fatalf("multi-line selection copy got %q, want %q", got, wantThreeLines)
	}
	if appUI.selectingLines {
		t.Fatalf("expected selectingLines to exit after copying multi-line selection")
	}

	// 6. 桌面右键兼容 + 点击气泡外关闭 + Esc 关闭
	rect, ok := tester.Find("第一段第一句秋风起")
	if !ok {
		t.Fatalf("line 1 not found")
	}
	tester.RightClickAt(rect.X+rect.W/2, rect.Y+rect.H/2)
	tester.Frame()
	if appUI.poemCtxTarget == nil {
		t.Fatalf("expected right click to open popover")
	}
	tester.ClickAt(20, 760)
	tester.Frame()
	if appUI.poemCtxTarget != nil {
		t.Fatalf("expected outside click to dismiss popover")
	}

	tester.RightClickAt(rect.X+rect.W/2, rect.Y+rect.H/2)
	tester.Frame()
	if appUI.poemCtxTarget == nil {
		t.Fatalf("expected right click to reopen popover")
	}
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if appUI.poemCtxTarget != nil {
		t.Fatalf("expected Escape to dismiss popover")
	}
}

func TestReaderLongPressLongStanzaFallsBackToSingleLine(t *testing.T) {
	_, appUI, book, _ := setupLongPressTestApp(t)
	appUI.openBook(book.ID, "poem-long", nil, true)
	appUI.reloadSync(t.Context())

	tester := ui.NewTester(appUI.Render, 390, 844)
	tester.Frame()

	// 不分节长诗（10 行 > 8 行阈值）长按第 5 行 -> 仅落到单行第 5 行
	longPressText(t, tester, "长诗第零五行灯塔初亮")
	if appUI.poemCtxTarget == nil {
		t.Fatalf("expected long press on long stanza line to open popover")
	}
	if !appUI.poemCtxTarget.isWholeRow || appUI.poemCtxTarget.lineStart != 5 || appUI.poemCtxTarget.lineEnd != 5 {
		t.Fatalf("expected single-line fallback at line 5, got %+v", appUI.poemCtxTarget)
	}

	if err := tester.Click("复制"); err != nil {
		t.Fatalf("click 复制 on single line: %v", err)
	}
	tester.Frame()
	if got := tester.Clipboard(); got != "长诗第零五行灯塔初亮" {
		t.Fatalf("single-line copy got %q, want %q", got, "长诗第零五行灯塔初亮")
	}
}

func TestReaderTopShareOpensFullPoemOnly(t *testing.T) {
	_, appUI, book, sp := setupLongPressTestApp(t)
	tester := ui.NewTester(appUI.Render, 1080, 720)
	tester.Frame()

	appUI.openPoemShareCard(book, sp.Poems[0])
	tester.Frame()
	if !appUI.showShareCardModal || appUI.shareCardData.Kind != sharecard.KindPoem {
		t.Fatalf("expected full poem share card, got open=%v kind=%v", appUI.showShareCardModal, appUI.shareCardData.Kind)
	}
	if tester.HasText("金句") {
		t.Fatalf("expected full poem share modal not to show confusing 金句 switch")
	}
}

func TestReaderLongPressTabletAndDarkTheme(t *testing.T) {
	_, appUI, _, _ := setupLongPressTestApp(t)
	appUI.settings.ThemePreset = "dark"

	tester := ui.NewTester(appUI.Render, 1080, 720)
	tester.Frame()

	longPressText(t, tester, "第一段第一句秋风起")
	if appUI.poemCtxTarget == nil {
		t.Fatalf("expected tablet dark theme long press to open popover")
	}
	if err := tester.Click("勾选"); err != nil {
		t.Fatalf("tablet click 勾选: %v", err)
	}
	tester.Advance(300 * time.Millisecond)
	tester.Frame()
	for _, label := range []string{"留作散页", "设为金句", "分享", "复制", "完成"} {
		if !tester.HasText(label) {
			t.Fatalf("expected tablet selection bar action %q, got texts=%v", label, tester.Texts())
		}
	}
	if img := tester.Image(); img == nil {
		t.Fatalf("tablet dark theme render returned nil image")
	}
}
