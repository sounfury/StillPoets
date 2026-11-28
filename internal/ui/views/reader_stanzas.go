package views

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
	"stillpoets/internal/domain"
	"stillpoets/internal/ui/theme"
)

type poemStanzaRenderArgs struct {
	book       domain.Book
	poem       domain.Poem
	isCurrent  bool
	poetryFont string
	fontSize   float32
	fontWeight int
	lineHeight float32
}

func (a *AppUI) renderSelectionActionBar(c *ui.Context, pal theme.Palette, book domain.Book, poem domain.Poem, isTablet bool) {
	selectedCount := len(a.selectedLines)

	renderPill := func(key, label string, primary bool, onClick func()) {
		renderReaderPillButton(c, pal, key, label, primary, onClick)
	}

	renderSummary := func() {
		if selectedCount == 0 {
			ui.Text(c, "点按诗行勾选或取消（支持跨段多选）").
				FontSize(12).TextColor(pal.TextSecondary).MaxLines(1).Grow(1)
			return
		}
		ui.Textf(c, "已选 %d 行", selectedCount).
			FontSize(13).Bold().TextColor(pal.TextPrimary).MaxLines(1).Grow(1)
	}

	renderActions := func() {
		if selectedCount == 0 {
			return
		}
		renderPill("sel-act-quote", "留作散页", true, func() {
			a.saveSelectedLinesAsQuote(book, poem)
		})
		renderPill("sel-act-widget", "设为金句", false, func() {
			a.saveSelectedLinesAsWidgetExcerpt(book, poem)
		})
		renderPill("sel-act-share", "分享", false, func() {
			content, _ := a.collectSelectedText(poem)
			a.openSelectionShareCard(book, poem, content)
		})
		renderPill("sel-act-copy", "复制", false, func() {
			content, _ := a.collectSelectedText(poem)
			a.copyPoemText(c, content, fmt.Sprintf("已复制 %d 行诗句", selectedCount))
			a.clearPoemSelectionState()
		})
	}

	renderDone := func() {
		renderPill("sel-act-done", "完成", false, func() {
			a.clearPoemSelectionState()
		})
	}

	container := ui.Column(c.Key("selection-bar")).
		FillWidth().
		Padding(9, 20).
		Gap(8).
		Background(pal.HighlightBg).
		BorderColor(pal.Border).
		BorderWidth(1, 0, 1, 0).
		Transition(theme.BarRevealTransition())

	container.Children(func() {
		if isTablet {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				renderSummary()
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					renderActions()
					renderDone()
				})
			})
			return
		}

		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			renderSummary()
			renderDone()
		})
		if selectedCount > 0 {
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				renderActions()
			})
		}
	})
}

type poemContextFrame struct {
	opened bool
	slots  []poemLineSlot
}

type renderedStanza struct {
	index, start, end int
}

func (a *AppUI) openLineContext(c *ui.Context, poemID string, stanzaIdx, lineNum int, frame *poemContextFrame) {
	a.poemCtxTarget = &poemContextTarget{
		poemID:    poemID,
		stanzaIdx: stanzaIdx,
		lineStart: lineNum,
		lineEnd:   lineNum,
	}
	frame.opened = true
	c.AnimationFrame()
}

func (a *AppUI) renderPoemStanzas(c *ui.Context, pal theme.Palette, args poemStanzaRenderArgs) {
	var frame poemContextFrame
	handleInteracted := false

	stanzaCol := ui.Column(c.Key("poem-stanzas-"+args.poem.ID)).
		Gap(14).
		Padding(18, 18, 18, 18).
		FillWidth()
	stanzaCol.Children(func() {
		lineNum := 0
		for i, stanza := range args.poem.Stanzas {
			span := renderedStanza{index: i, start: lineNum + 1, end: lineNum + len(stanza.Lines)}
			a.renderPoemStanza(c, pal, args, span, stanza.Lines, &frame)
			lineNum = span.end
		}
		if args.isCurrent && !a.selectingLines && a.poemCtxTarget != nil && a.poemCtxTarget.poemID == args.poem.ID {
			handleInteracted = a.renderSelectionHandles(c, pal, selectionDragCtx{
				poem:      args.poem,
				stanzaCol: stanzaCol,
				slots:     frame.slots,
			})
		}
	})

	if !args.isCurrent || a.selectingLines || a.poemCtxTarget == nil || a.poemCtxTarget.poemID != args.poem.ID {
		return
	}
	if anchorSlot, ok := findLineSlot(frame.slots, a.poemCtxTarget.lineStart); ok {
		a.renderPoemContextPopover(c, pal, args.book, args.poem, selectionPopoverState{
			anchor:           anchorSlot.el,
			openedThisFrame:  frame.opened,
			handleInteracted: handleInteracted,
		})
	}
}

func (a *AppUI) renderPoemStanza(c *ui.Context, pal theme.Palette, args poemStanzaRenderArgs, stanza renderedStanza, lines []string, frame *poemContextFrame) {
	stanzaBox := ui.Column(c.Key(fmt.Sprintf("stanza-%d", stanza.index))).
		Gap(4).
		FillWidth()

	lineTriggered := false
	stanzaBox.Children(func() {
		for i, line := range lines {
			lineNum := stanza.start + i
			isCtxLine := a.isContextLineSelected(args.isCurrent, args.poem.ID, lineNum)
			row := a.renderPoemLine(c, pal, args, lineNum, line, isCtxLine)
			frame.slots = append(frame.slots, poemLineSlot{
				lineNum:   lineNum,
				stanzaIdx: stanza.index,
				el:        row,
			})
			if a.canOpenPoemContext(args.isCurrent) && poemContextTriggered(row) {
				lineTriggered = true
				a.openLineContext(c, args.poem.ID, stanza.index, lineNum, frame)
			}
		}
	})
	if a.canOpenPoemContext(args.isCurrent) && !lineTriggered && len(lines) > 0 && poemContextTriggered(stanzaBox) {
		a.openLineContext(c, args.poem.ID, stanza.index, stanza.start, frame)
	}
}

func (a *AppUI) canOpenPoemContext(isCurrent bool) bool {
	return isCurrent && !a.selectingLines
}

func poemContextTriggered(el ui.Element) bool {
	return el.LongPressed() || el.RightClicked()
}

func (a *AppUI) renderPoemLine(c *ui.Context, pal theme.Palette, args poemStanzaRenderArgs, lineNum int, line string, isCtxLine bool) ui.Element {
	isHighlighted := args.isCurrent && a.isLineHighlighted(lineNum)
	isSelected := args.isCurrent && a.selectedLines[lineNum]
	row := ui.Box(c.Key(fmt.Sprintf("ln-%d", lineNum))).
		FillWidth().
		Padding(3, 6).
		Radius(4).
		Border(1, ui.Transparent)
	if isCtxLine {
		row.Background(selectionHighlightFill(pal))
	} else if isSelected {
		row.Background(pal.HighlightBg).Border(1, pal.Accent)
	} else if isHighlighted {
		row.Background(pal.HighlightBg)
	}
	if args.isCurrent && a.selectingLines {
		row.Cursor(ui.CursorPointer)
	}
	row.Children(func() {
		ui.Text(c, line).
			Font(args.poetryFont).
			FontSize(args.fontSize).
			FontWeight(args.fontWeight).
			LineHeight(args.lineHeight).
			TextColor(pal.TextPrimary)
	})
	if args.isCurrent && a.selectingLines && row.Clicked() {
		a.toggleSelectedLine(lineNum)
	}
	return row
}

func (a *AppUI) isContextLineSelected(isCurrent bool, poemID string, lineNum int) bool {
	return isCurrent && !a.selectingLines &&
		a.poemCtxTarget != nil &&
		a.poemCtxTarget.poemID == poemID &&
		a.poemCtxTarget.containsLine(lineNum)
}

func (a *AppUI) toggleSelectedLine(lineNum int) {
	if a.selectedLines[lineNum] {
		delete(a.selectedLines, lineNum)
		return
	}
	a.selectedLines[lineNum] = true
}

func (a *AppUI) enterLineSelectionMode(initialLines []int) {
	a.poemCtxTarget = nil
	a.selectingLines = true
	a.selectedLines = make(map[int]bool, len(initialLines))
	for _, n := range initialLines {
		if n > 0 {
			a.selectedLines[n] = true
		}
	}
}

func (a *AppUI) copyPoemText(c *ui.Context, text string, toast string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	a.mu.Lock()
	copyFn := a.copyTextFn
	a.mu.Unlock()
	if copyFn != nil {
		copyFn(text)
	}
	if c != nil {
		c.WriteClipboard(text)
	}
	if toast == "" {
		toast = "已复制诗句"
	}
	a.showTransientToastOnUI(toast)
}

func (a *AppUI) collectLinesText(poem domain.Poem, lineNums []int) (string, string) {
	if len(lineNums) == 0 {
		return "", ""
	}
	nums := slices.Clone(lineNums)
	slices.Sort(nums)
	nums = slices.Compact(nums)
	wanted := make(map[int]bool, len(nums))
	for _, n := range nums {
		wanted[n] = true
	}

	var lines []string
	cur := 0
	for _, stanza := range poem.Stanzas {
		for _, line := range stanza.Lines {
			cur++
			if wanted[cur] {
				lines = append(lines, strings.TrimSpace(line))
			}
		}
	}
	return strings.Join(lines, "\n"), app.FormatLineRanges(nums)
}

func (a *AppUI) collectSelectedText(poem domain.Poem) (string, string) {
	if len(a.selectedLines) == 0 {
		return "", ""
	}
	nums := make([]int, 0, len(a.selectedLines))
	for k, ok := range a.selectedLines {
		if ok {
			nums = append(nums, k)
		}
	}
	return a.collectLinesText(poem, nums)
}

func (a *AppUI) commitLineSelectionAction(poem domain.Poem, lineNums []int, okMsg, failPrefix string, fn func(ctx context.Context, content, lineSpec string) error) {
	content, lineSpec := a.collectLinesText(poem, lineNums)
	if content == "" {
		return
	}
	a.flushCurrentPoemDwell(true)
	a.clearPoemSelectionState()
	a.highlightRanges = app.ParseLineRanges(lineSpec)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := fn(ctx, content, lineSpec); err != nil {
			a.showTransientToast(failPrefix + err.Error())
			return
		}
		a.reloadSync(ctx)
		a.showTransientToast(okMsg)
	}()
}

func (a *AppUI) saveLineNumbersAsQuote(book domain.Book, poem domain.Poem, lineNums []int) {
	a.commitLineSelectionAction(poem, lineNums, "已留作诗歌节选散页", "留存散页失败：", func(ctx context.Context, content, lineSpec string) error {
		_, err := a.svc.CreatePoemExcerptQuote(ctx, book, poem, content, lineSpec)
		return err
	})
}

func (a *AppUI) saveSelectedLinesAsQuote(book domain.Book, poem domain.Poem) {
	a.saveLineNumbersAsQuote(book, poem, a.selectedLineList())
}

func (a *AppUI) saveLineNumbersAsWidgetExcerpt(book domain.Book, poem domain.Poem, lineNums []int) {
	a.commitLineSelectionAction(poem, lineNums, "已设为本诗小组件金句", "设置金句失败：", func(ctx context.Context, content, lineSpec string) error {
		return a.svc.SetFavoriteCustomExcerpt(ctx, book, poem, content, lineSpec)
	})
}

func (a *AppUI) saveSelectedLinesAsWidgetExcerpt(book domain.Book, poem domain.Poem) {
	a.saveLineNumbersAsWidgetExcerpt(book, poem, a.selectedLineList())
}

func (a *AppUI) selectedLineList() []int {
	nums := make([]int, 0, len(a.selectedLines))
	for k, ok := range a.selectedLines {
		if ok {
			nums = append(nums, k)
		}
	}
	return nums
}

func (a *AppUI) toggleCurrentPoemFavorite(book domain.Book, poem domain.Poem, active bool) {
	if active {
		a.flushCurrentPoemDwell(true)
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := a.svc.ToggleFavoritePoem(ctx, book, poem, active); err != nil {
			a.showTransientToast("操作失败：" + err.Error())
			return
		}
		a.reloadSync(ctx)
		if active {
			a.showTransientToast("已加入诗篇精选")
		} else {
			a.showTransientToast("已取消精选")
		}
	}()
}
