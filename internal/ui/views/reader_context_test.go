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

func dragHandleToText(t *testing.T, tester *ui.Tester, handleLabel, targetText string) {
	t.Helper()
	info, ok := tester.Access(handleLabel)
	if !ok {
		t.Fatalf("selection handle %q not found in accessibility tree", handleLabel)
	}
	targetRect, ok := tester.Find(targetText)
	if !ok {
		t.Fatalf("target line %q not found", targetText)
	}
	startX := info.Bounds.X + info.Bounds.W*0.5
	startY := info.Bounds.Y + info.Bounds.H*0.5
	endX := startX
	endY := targetRect.Y + targetRect.H*0.5

	tester.FingerDown(startX, startY)
	tester.Frame()
	const steps = 8
	for i := 1; i <= steps; i++ {
		f := float32(i) / float32(steps)
		tester.FingerMove(startX+(endX-startX)*f, startY+(endY-startY)*f)
		tester.Frame()
	}
	tester.FingerUp(endX, endY)
	tester.Frame()
}

func TestReaderLongPressDragHandlesCrossStanzaFlow(t *testing.T) {
	svc, appUI, book, _ := setupLongPressTestApp(t)

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

	// 1. 触屏长按第 1 行 -> 选中第 1 行并展示起止手柄与全功能悬浮菜单
	longPressText(t, tester, "第一段第一句秋风起")
	if appUI.poemCtxTarget == nil {
		t.Fatalf("expected long press inside swipe pager + scroll to open poem selection")
	}
	if appUI.poemCtxTarget.lineStart != 1 || appUI.poemCtxTarget.lineEnd != 1 {
		t.Fatalf("expected initial selection 1..1, got %+v", appUI.poemCtxTarget)
	}
	for _, label := range []string{"已选 1 行", "选本节", "全选", "勾选", "复制", "留散页", "设为金句", "分享"} {
		if !tester.HasText(label) {
			t.Fatalf("expected popover text %q to be visible, got texts=%v", label, tester.Texts())
		}
	}

	// 2. 拖动终点水滴手柄跨越段落至第 3 行 -> 选区无缝扩至 1..3，松手后菜单自动回到 3 行状态
	dragHandleToText(t, tester, "选区终点手柄", "第二段第一句灯火明")
	if appUI.poemCtxTarget == nil {
		t.Fatalf("selection should stay open while and after dragging handle")
	}
	if appUI.poemCtxTarget.lineStart != 1 || appUI.poemCtxTarget.lineEnd != 3 {
		t.Fatalf("expected dragged cross-stanza selection 1..3, got %+v", appUI.poemCtxTarget)
	}
	if !tester.HasText("已选 3 行") {
		t.Fatalf("expected updated count label 已选 3 行, got texts=%v", tester.Texts())
	}

	// 3. 点击「复制」-> 写入跨段 3 行并关闭气泡
	if err := tester.Click("复制"); err != nil {
		t.Fatalf("click 复制: %v", err)
	}
	tester.Frame()
	wantThreeLines := "第一段第一句秋风起\n第一段第二句落叶黄\n第二段第一句灯火明"
	copyMu.Lock()
	gotCopied := copiedText
	copyMu.Unlock()
	if gotCopied != wantThreeLines {
		t.Fatalf("copyTextFn got %q, want %q", gotCopied, wantThreeLines)
	}
	if got := tester.Clipboard(); got != wantThreeLines {
		t.Fatalf("ui clipboard got %q, want %q", got, wantThreeLines)
	}
	if appUI.poemCtxTarget != nil {
		t.Fatalf("expected popover to dismiss after copy")
	}

	// 4. 长按第 3 行 -> 点击「选本节」扩至 3..4 -> 点击「设为金句」直接生效
	longPressText(t, tester, "第二段第一句灯火明")
	if err := tester.Click("选本节"); err != nil {
		t.Fatalf("click 选本节: %v", err)
	}
	tester.Frame()
	if appUI.poemCtxTarget == nil || appUI.poemCtxTarget.lineStart != 3 || appUI.poemCtxTarget.lineEnd != 4 {
		t.Fatalf("expected stanza expansion 3..4, got %+v", appUI.poemCtxTarget)
	}
	if err := tester.Click("设为金句"); err != nil {
		t.Fatalf("click 设为金句: %v", err)
	}
	tester.Frame()

	wantStanza2 := "第二段第一句灯火明\n第二段第二句归舟晚"
	deadline := time.Now().Add(3 * time.Second)
	var favMatched bool
	for time.Now().Before(deadline) {
		favs, err := svc.ListFavorites(t.Context(), book.ID)
		if err == nil && len(favs) == 1 && favs[0].Excerpt == wantStanza2 && favs[0].HighlightLines == "3-4" && favs[0].IsCustomQuote {
			favMatched = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !favMatched {
		t.Fatalf("expected stanza 3..4 saved as widget custom excerpt")
	}

	// 5. 长按第 1 行 -> 点「选本节」-> 点「留散页」写入诗歌节选散页
	longPressText(t, tester, "第一段第二句落叶黄")
	if err := tester.Click("选本节"); err != nil {
		t.Fatalf("click 选本节: %v", err)
	}
	tester.Frame()
	if err := tester.Click("留散页"); err != nil {
		t.Fatalf("click 留散页: %v", err)
	}
	tester.Frame()

	wantStanza1 := "第一段第一句秋风起\n第一段第二句落叶黄"
	deadline = time.Now().Add(3 * time.Second)
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

	// 6. 长按第 2 行 -> 点击「全选」-> 点击「分享」直达卡片弹窗
	longPressText(t, tester, "第一段第二句落叶黄")
	if err := tester.Click("全选"); err != nil {
		t.Fatalf("click 全选: %v", err)
	}
	tester.Frame()
	if appUI.poemCtxTarget == nil || appUI.poemCtxTarget.lineStart != 1 || appUI.poemCtxTarget.lineEnd != 4 {
		t.Fatalf("expected select all 1..4, got %+v", appUI.poemCtxTarget)
	}
	if err := tester.Click("分享"); err != nil {
		t.Fatalf("click 分享: %v", err)
	}
	tester.Frame()
	wantAll := wantStanza1 + "\n" + wantStanza2
	if !appUI.showShareCardModal || appUI.shareCardData.Kind != sharecard.KindQuote || appUI.shareCardData.QuoteText != wantAll {
		t.Fatalf("unexpected share card modal state: open=%v kind=%v text=%q", appUI.showShareCardModal, appUI.shareCardData.Kind, appUI.shareCardData.QuoteText)
	}
	appUI.showShareCardModal = false
	tester.Frame()

	// 7. 桌面右键兼容 + 点击气泡外关闭 + Esc 关闭
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

func TestReaderLongPoemHandleDragBothDirectionsAndFlip(t *testing.T) {
	_, appUI, book, _ := setupLongPressTestApp(t)
	appUI.openBook(book.ID, "poem-long", nil, true)
	appUI.reloadSync(t.Context())

	tester := ui.NewTester(appUI.Render, 390, 844)
	tester.Frame()

	// 1. 不分节长诗长按第 5 行 -> 选中第 5 行
	longPressText(t, tester, "长诗第零五行灯塔初亮")
	if appUI.poemCtxTarget == nil || appUI.poemCtxTarget.lineStart != 5 || appUI.poemCtxTarget.lineEnd != 5 {
		t.Fatalf("expected initial selection 5..5, got %+v", appUI.poemCtxTarget)
	}

	// 2. 向下拖动终点手柄至第 8 行 -> 选区变为 5..8
	dragHandleToText(t, tester, "选区终点手柄", "长诗第零八行石阶生凉")
	if appUI.poemCtxTarget == nil || appUI.poemCtxTarget.lineStart != 5 || appUI.poemCtxTarget.lineEnd != 8 {
		t.Fatalf("expected expanded selection 5..8, got %+v", appUI.poemCtxTarget)
	}

	// 3. 向上拖动起点手柄至第 3 行 -> 选区变为 3..8
	dragHandleToText(t, tester, "选区起点手柄", "长诗第零三行远帆如豆")
	if appUI.poemCtxTarget == nil || appUI.poemCtxTarget.lineStart != 3 || appUI.poemCtxTarget.lineEnd != 8 {
		t.Fatalf("expected upward expanded selection 3..8, got %+v", appUI.poemCtxTarget)
	}

	// 4. 反向越过另一端：把终点手柄向上拖过第 3 行到第 2 行 -> 自动翻转为 2..3
	dragHandleToText(t, tester, "选区终点手柄", "长诗第零二行鸥鸟低飞")
	if appUI.poemCtxTarget == nil || appUI.poemCtxTarget.lineStart != 2 || appUI.poemCtxTarget.lineEnd != 3 {
		t.Fatalf("expected flipped selection 2..3, got %+v", appUI.poemCtxTarget)
	}

	if err := tester.Click("复制"); err != nil {
		t.Fatalf("click 复制 on dragged multi-line selection: %v", err)
	}
	tester.Frame()
	want := "长诗第零二行鸥鸟低飞\n长诗第零三行远帆如豆"
	if got := tester.Clipboard(); got != want {
		t.Fatalf("dragged multi-line copy got %q, want %q", got, want)
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
		t.Fatalf("expected tablet dark theme long press to open selection")
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
