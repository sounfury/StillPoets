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

func (a *AppUI) renderPoemStanzas(c *ui.Context, pal theme.Palette, args poemStanzaRenderArgs) {
	book, poem, isCurrent := args.book, args.poem, args.isCurrent
	poetryFont, fontSize, fontWeight, lineHeight := args.poetryFont, args.fontSize, args.fontWeight, args.lineHeight
	lineNum := 0
	var (
		ctxAnchor       ui.Element
		hasCtxAnchor    bool
		openedThisFrame bool
	)

	openContextTarget := func(stanzaIdx, stanzaStart, stanzaEnd, lineCount, targetLine int, stanzaEl, lineEl ui.Element) {
		openedThisFrame = true
		if lineCount <= stanzaLongPressLineLimit {
			a.poemCtxTarget = &poemContextTarget{
				poemID:     poem.ID,
				stanzaIdx:  stanzaIdx,
				lineStart:  stanzaStart,
				lineEnd:    stanzaEnd,
				isWholeRow: false,
			}
			ctxAnchor = stanzaEl
			hasCtxAnchor = true
		} else {
			a.poemCtxTarget = &poemContextTarget{
				poemID:     poem.ID,
				stanzaIdx:  stanzaIdx,
				lineStart:  targetLine,
				lineEnd:    targetLine,
				isWholeRow: true,
			}
			ctxAnchor = lineEl
			hasCtxAnchor = true
		}
		c.AnimationFrame()
	}

	ui.Column(c).Gap(14).Padding(8, 0, 8, 0).FillWidth().Children(func() {
		for si, stanza := range poem.Stanzas {
			stanzaStart := lineNum + 1
			stanzaEnd := lineNum + len(stanza.Lines)
			isCtxStanza := a.isContextStanzaActive(isCurrent, poem.ID, si)

			stanzaBox := ui.Column(c.Key(fmt.Sprintf("stanza-%d", si))).
				Gap(4).
				FillWidth().
				Padding(4, 4).
				Radius(6).
				Border(1, ui.Transparent).
				Transition(theme.ColorTransition())
			if isCtxStanza {
				stanzaBox.Background(pal.HighlightBg).Border(1, pal.Accent)
			}

			var (
				lineTriggered bool
				firstLineElem ui.Element
				hasFirstLine  bool
			)
			stanzaBox.Children(func() {
				for _, line := range stanza.Lines {
					lineNum++
					curLine := lineNum
					isHighlighted := isCurrent && a.isLineHighlighted(curLine)
					isSelected := isCurrent && a.selectedLines[curLine]
					isCtxLine := a.isContextLineActive(isCurrent, poem.ID, curLine)

					lineRow := ui.Box(c.Key(fmt.Sprintf("ln-%d", curLine))).
						FillWidth().
						Padding(3, 8).
						Radius(4).
						Border(1, ui.Transparent).
						Transition(theme.ColorTransition())
					if isSelected || isCtxLine {
						lineRow.Background(pal.HighlightBg).Border(1, pal.Accent)
					} else if isHighlighted {
						lineRow.Background(pal.HighlightBg)
					}
					if isCurrent && a.selectingLines {
						lineRow.Cursor(ui.CursorPointer)
					}
					lineRow.Children(func() {
						ui.Text(c, line).
							Font(poetryFont).
							FontSize(fontSize).
							FontWeight(fontWeight).
							LineHeight(lineHeight).
							TextColor(pal.TextPrimary)
					})
					if !hasFirstLine {
						firstLineElem = lineRow
						hasFirstLine = true
					}

					if isCurrent && a.selectingLines && lineRow.Clicked() {
						a.toggleSelectedLine(curLine)
					}
					if isCurrent && !a.selectingLines && (lineRow.LongPressed() || lineRow.RightClicked()) {
						lineTriggered = true
						openContextTarget(si, stanzaStart, stanzaEnd, len(stanza.Lines), curLine, stanzaBox, lineRow)
					} else if isCtxLine && !openedThisFrame {
						ctxAnchor = lineRow
						hasCtxAnchor = true
					}
				}
			})

			if isCurrent && !a.selectingLines && !lineTriggered && hasFirstLine &&
				(stanzaBox.LongPressed() || stanzaBox.RightClicked()) {
				openContextTarget(si, stanzaStart, stanzaEnd, len(stanza.Lines), stanzaStart, stanzaBox, firstLineElem)
			} else if isCtxStanza && !openedThisFrame {
				ctxAnchor = stanzaBox
				hasCtxAnchor = true
			}
		}
	})

	if isCurrent && !a.selectingLines && a.poemCtxTarget != nil && a.poemCtxTarget.poemID == poem.ID && hasCtxAnchor {
		a.renderPoemContextPopover(c, pal, book, poem, ctxAnchor, openedThisFrame)
	}
}

func (a *AppUI) isContextStanzaActive(isCurrent bool, poemID string, stanzaIdx int) bool {
	return isCurrent && !a.selectingLines &&
		a.poemCtxTarget != nil &&
		a.poemCtxTarget.poemID == poemID &&
		!a.poemCtxTarget.isWholeRow &&
		a.poemCtxTarget.stanzaIdx == stanzaIdx
}

func (a *AppUI) isContextLineActive(isCurrent bool, poemID string, lineNum int) bool {
	return isCurrent && !a.selectingLines &&
		a.poemCtxTarget != nil &&
		a.poemCtxTarget.poemID == poemID &&
		a.poemCtxTarget.isWholeRow &&
		a.poemCtxTarget.lineStart == lineNum
}

func (a *AppUI) toggleSelectedLine(lineNum int) {
	if a.selectedLines[lineNum] {
		delete(a.selectedLines, lineNum)
		return
	}
	a.selectedLines[lineNum] = true
}

func (a *AppUI) renderPoemContextPopover(c *ui.Context, pal theme.Palette, book domain.Book, poem domain.Poem, anchor ui.Element, openedThisFrame bool) {
	target := a.poemCtxTarget
	if target == nil || target.poemID != poem.ID {
		return
	}
	lineNums := target.lineNumbers()
	if len(lineNums) == 0 {
		a.poemCtxTarget = nil
		return
	}

	// 浅色纸面用深墨胶囊以保持视觉聚焦，暗夜模式用微亮卡片面配细边框
	bg := ui.Hex("#1F1E1C")
	fg := ui.Hex("#FAF8F4")
	sep := ui.RGBA(255, 255, 255, 0.16)
	border := ui.RGBA(255, 255, 255, 0.08)
	shadow := ui.RGBA(0, 0, 0, 0.28)
	if pal.IsDark() {
		bg = pal.Surface
		fg = pal.TextPrimary
		sep = pal.Border
		border = pal.Border
		shadow = ui.RGBA(0, 0, 0, 0.50)
	}

	ui.Overlay(c, func() {
		popover := ui.Row(c.Key("poem-ctx-popover")).
			AttachTo(anchor, ui.AnchorTop, ui.AnchorBottom).
			Margin(8).
			Padding(4, 5).
			Gap(2).
			Radius(10).
			AlignItems(ui.Center).
			Background(bg).
			Border(0.75, border).
			Shadow(0, 8, 24, -2, shadow).
			Transition(theme.DialogPanelTransition())

		actionClicked := false
		renderItem := func(key, label string, onClick func()) {
			btn := ui.Box(c.Key(key)).
				Padding(7, 11).
				Radius(7).
				Cursor(ui.CursorPointer)
			theme.ApplyPressScale(btn, 0.94)
			btn.Children(func() {
				ui.Text(c, label).
					Font(theme.FontSans).
					FontSize(12.5).
					FontWeight(500).
					TextColor(fg)
			})
			if btn.Clicked() {
				actionClicked = true
				onClick()
			}
		}
		renderSep := func() {
			ui.Box(c).Width(1).Height(14).Background(sep)
		}

		popover.Children(func() {
			renderItem("poem-ctx-copy", "复制", func() {
				content, _ := a.collectLinesText(poem, lineNums)
				a.poemCtxTarget = nil
				a.copyPoemText(c, content, "已复制诗句")
			})
			renderSep()
			renderItem("poem-ctx-share", "分享", func() {
				content, _ := a.collectLinesText(poem, lineNums)
				a.openSelectionShareCard(book, poem, content)
			})
			renderSep()
			renderItem("poem-ctx-quote", "留散页", func() {
				a.saveLineNumbersAsQuote(book, poem, lineNums)
			})
			renderSep()
			renderItem("poem-ctx-select", "勾选", func() {
				a.enterLineSelectionMode(lineNums)
			})
		})

		if !actionClicked && !openedThisFrame && (popover.PressedOutside() || popover.OverlayShortcut(0, ui.KeyEscape)) {
			a.poemCtxTarget = nil
		}
	})
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

func (a *AppUI) saveLineNumbersAsQuote(book domain.Book, poem domain.Poem, lineNums []int) {
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
		if _, err := a.svc.CreatePoemExcerptQuote(ctx, book, poem, content, lineSpec); err != nil {
			a.showTransientToast("留存散页失败：" + err.Error())
			return
		}
		a.reloadSync(ctx)
		a.showTransientToast("已留作诗歌节选散页")
	}()
}

func (a *AppUI) saveSelectedLinesAsQuote(book domain.Book, poem domain.Poem) {
	nums := make([]int, 0, len(a.selectedLines))
	for k, ok := range a.selectedLines {
		if ok {
			nums = append(nums, k)
		}
	}
	a.saveLineNumbersAsQuote(book, poem, nums)
}

func (a *AppUI) saveSelectedLinesAsWidgetExcerpt(book domain.Book, poem domain.Poem) {
	content, lineSpec := a.collectSelectedText(poem)
	if content == "" {
		return
	}
	a.flushCurrentPoemDwell(true)
	a.clearPoemSelectionState()
	a.highlightRanges = app.ParseLineRanges(lineSpec)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.svc.SetFavoriteCustomExcerpt(ctx, book, poem, content, lineSpec); err != nil {
			a.showTransientToast("设置金句失败：" + err.Error())
			return
		}
		a.reloadSync(ctx)
		a.showTransientToast("已设为本诗小组件金句")
	}()
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
