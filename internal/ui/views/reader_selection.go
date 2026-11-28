package views

import (
	"fmt"
	"math"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/domain"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/theme"
)

type selectionDragKind uint8

const (
	selectionDragNone selectionDragKind = iota
	selectionDragStart
	selectionDragEnd
)

type poemContextTarget struct {
	poemID     string
	stanzaIdx  int
	lineStart  int // 1-based inclusive
	lineEnd    int // 1-based inclusive
	dragKind   selectionDragKind
	dragAnchor int
	dragTip    int
}

func (t *poemContextTarget) lineNumbers() []int {
	if t == nil || t.lineStart <= 0 || t.lineEnd < t.lineStart {
		return nil
	}
	nums := make([]int, 0, t.lineEnd-t.lineStart+1)
	for n := t.lineStart; n <= t.lineEnd; n++ {
		nums = append(nums, n)
	}
	return nums
}

func (t *poemContextTarget) containsLine(lineNum int) bool {
	return t != nil && lineNum >= t.lineStart && lineNum <= t.lineEnd
}

func (t *poemContextTarget) isDragging() bool {
	return t != nil && t.dragKind != selectionDragNone
}

type poemLineSlot struct {
	lineNum   int
	stanzaIdx int
	el        ui.Element
}

type selectionHandleSpec struct {
	key        string
	dragKind   selectionDragKind
	slot       poemLineSlot
	isStartCap bool
	anchorLine int
}

type selectionPopoverState struct {
	anchor           ui.Element
	openedThisFrame  bool
	handleInteracted bool
}

type selectionDragCtx struct {
	poem      domain.Poem
	stanzaCol ui.Element
	slots     []poemLineSlot
}

func selectionHighlightFill(pal theme.Palette) ui.Color {
	alpha := float32(0.18)
	if pal.IsDark() {
		alpha = 0.26
	}
	return ui.RGBA(pal.Accent.R, pal.Accent.G, pal.Accent.B, alpha)
}

func totalPoemLines(poem domain.Poem) int {
	total := 0
	for _, s := range poem.Stanzas {
		total += len(s.Lines)
	}
	return total
}

func stanzaRangeForLine(poem domain.Poem, lineNum int) (int, int, int) {
	cur := 0
	for idx, stanza := range poem.Stanzas {
		start := cur + 1
		end := cur + len(stanza.Lines)
		if lineNum >= start && lineNum <= end {
			return idx, start, end
		}
		cur = end
	}
	return 0, lineNum, lineNum
}

func findLineSlot(slots []poemLineSlot, lineNum int) (poemLineSlot, bool) {
	for _, s := range slots {
		if s.lineNum == lineNum {
			return s, true
		}
	}
	return poemLineSlot{}, false
}

func relativeRect(child, parent ui.Rect) ui.Rect {
	return ui.Rect{
		X: child.X - parent.X,
		Y: child.Y - parent.Y,
		W: child.W,
		H: child.H,
	}
}

func hitTestPoemLine(slots []poemLineSlot, colBounds ui.Rect, localY float32) int {
	if len(slots) == 0 {
		return 0
	}
	bestLine := slots[0].lineNum
	bestDist := float32(math.MaxFloat32)
	for _, slot := range slots {
		b := slot.el.LayoutBounds()
		if b.H <= 0 {
			continue
		}
		rel := relativeRect(b, colBounds)
		if localY >= rel.Y && localY <= rel.Y+rel.H {
			return slot.lineNum
		}
		center := rel.Y + rel.H*0.5
		dist := float32(math.Abs(float64(localY - center)))
		if dist < bestDist {
			bestDist = dist
			bestLine = slot.lineNum
		}
	}
	return bestLine
}

func resolveSelectionHandleSpecs(target *poemContextTarget, slots []poemLineSlot) (selectionHandleSpec, selectionHandleSpec, bool) {
	startLine, endLine := target.lineStart, target.lineEnd
	startCapIsTop, endCapIsTop := true, false
	startAnchor, endAnchor := target.lineEnd, target.lineStart

	if target.dragKind == selectionDragStart {
		startLine = target.dragTip
		endLine = target.dragAnchor
		startCapIsTop = target.dragTip <= target.dragAnchor
		endCapIsTop = !startCapIsTop
		startAnchor = target.dragAnchor
		endAnchor = target.dragTip
	} else if target.dragKind == selectionDragEnd {
		startLine = target.dragAnchor
		endLine = target.dragTip
		endCapIsTop = target.dragTip < target.dragAnchor
		startCapIsTop = !endCapIsTop
		startAnchor = target.dragTip
		endAnchor = target.dragAnchor
	}

	sSlot, okS := findLineSlot(slots, startLine)
	eSlot, okE := findLineSlot(slots, endLine)
	if !okS || !okE {
		return selectionHandleSpec{}, selectionHandleSpec{}, false
	}
	return selectionHandleSpec{
		key:        "sel-handle-start",
		dragKind:   selectionDragStart,
		slot:       sSlot,
		isStartCap: startCapIsTop,
		anchorLine: startAnchor,
	}, selectionHandleSpec{
		key:        "sel-handle-end",
		dragKind:   selectionDragEnd,
		slot:       eSlot,
		isStartCap: endCapIsTop,
		anchorLine: endAnchor,
	}, true
}

func (a *AppUI) renderSelectionHandles(c *ui.Context, pal theme.Palette, dctx selectionDragCtx) bool {
	target := a.poemCtxTarget
	if target == nil || target.poemID != dctx.poem.ID {
		return false
	}
	colBounds := dctx.stanzaCol.LayoutBounds()
	if colBounds.W <= 0 || colBounds.H <= 0 {
		return false
	}
	startSpec, endSpec, ok := resolveSelectionHandleSpecs(target, dctx.slots)
	if !ok {
		return false
	}

	startActive := a.renderSelectionHandle(c, pal, dctx, startSpec)
	endActive := a.renderSelectionHandle(c, pal, dctx, endSpec)
	if !startActive && !endActive && target.isDragging() {
		target.dragKind = selectionDragNone
		c.AnimationFrame()
	}
	return startActive || endActive
}

func (a *AppUI) renderSelectionHandle(c *ui.Context, pal theme.Palette, dctx selectionDragCtx, spec selectionHandleSpec) bool {
	colBounds := dctx.stanzaCol.LayoutBounds()
	rowBounds := spec.slot.el.LayoutBounds()
	if rowBounds.W <= 0 || rowBounds.H <= 0 {
		return false
	}
	rel := relativeRect(rowBounds, colBounds)

	const hitW = float32(36)
	const capPad = float32(18)
	const barW = float32(2.4)
	const dotR = float32(5.5)

	hitH := max(float32(42), rel.H+capPad+6)
	left := rel.X - hitW*0.5
	top := rel.Y - capPad
	if !spec.isStartCap {
		left = rel.X + rel.W - hitW*0.5
		top = rel.Y - 6
	}

	accent := pal.Accent
	ringColor := pal.ReaderPaper
	lineH := rel.H
	isStartCap := spec.isStartCap

	handleLabel := "选区终点手柄"
	if spec.dragKind == selectionDragStart {
		handleLabel = "选区起点手柄"
	}
	handle := ui.Box(c.Key(spec.key)).
		Label(handleLabel).
		Absolute().
		Left(left).
		Top(top).
		Width(hitW).
		Height(hitH).
		Cursor(ui.CursorPointer)
	handle.Draw(func(p *ui.Painter, r ui.Rect) {
		cx := r.X + r.W*0.5
		barY := r.Y + capPad
		dotY := barY - dotR
		if !isStartCap {
			barY = r.Y + 6
			dotY = barY + lineH + dotR
		}
		p.Fill(ui.Rect{X: cx - barW*0.5, Y: barY, W: barW, H: lineH}, accent, barW*0.5)
		p.Fill(ui.Rect{X: cx - dotR - 1.5, Y: dotY - dotR - 1.5, W: (dotR + 1.5) * 2, H: (dotR + 1.5) * 2}, ringColor, dotR+1.5)
		p.Fill(ui.Rect{X: cx - dotR, Y: dotY - dotR, W: dotR * 2, H: dotR * 2}, accent, dotR)
	})

	_, _, dragged := handle.Dragged()
	px, py, _ := handle.PointerPosition()
	pressed := handle.Pressed() || handle.PressedInside()
	if dragged {
		a.applySelectionHandleDrag(c, dctx, handle, spec, ui.Point{X: px, Y: py})
		return true
	}
	return pressed
}

func (a *AppUI) applySelectionHandleDrag(c *ui.Context, dctx selectionDragCtx, handle ui.Element, spec selectionHandleSpec, pt ui.Point) {
	target := a.poemCtxTarget
	if target == nil || target.poemID != dctx.poem.ID {
		return
	}
	if target.dragKind == selectionDragNone {
		target.dragKind = spec.dragKind
		target.dragAnchor = spec.anchorLine
		target.dragTip = spec.slot.lineNum
	}
	if target.dragKind != spec.dragKind {
		return
	}

	wx, wy := handle.LocalToWindow(pt.X, pt.Y)
	_, colY, ok := dctx.stanzaCol.WindowToLocal(wx, wy)
	if !ok {
		return
	}
	bias := float32(-8)
	if spec.isStartCap {
		bias = 8
	}
	hitLine := hitTestPoemLine(dctx.slots, dctx.stanzaCol.LayoutBounds(), colY+bias)
	if hitLine <= 0 {
		return
	}
	target.dragTip = hitLine
	target.lineStart = min(target.dragAnchor, hitLine)
	target.lineEnd = max(target.dragAnchor, hitLine)
	target.stanzaIdx, _, _ = stanzaRangeForLine(dctx.poem, target.lineStart)
	c.AnimationFrame()
}

func popoverPalette(pal theme.Palette) (bg, fg, muted, sep, border, shadow ui.Color) {
	if pal.IsDark() {
		return pal.Surface, pal.TextPrimary, pal.TextMuted, pal.Border, pal.Border, ui.RGBA(0, 0, 0, 0.50)
	}
	return ui.Hex("#1F1E1C"), ui.Hex("#FAF8F4"), ui.RGBA(250, 248, 244, 0.65),
		ui.RGBA(255, 255, 255, 0.14), ui.RGBA(255, 255, 255, 0.08), ui.RGBA(0, 0, 0, 0.28)
}

func (a *AppUI) renderPoemContextPopover(c *ui.Context, pal theme.Palette, book domain.Book, poem domain.Poem, state selectionPopoverState) {
	target := a.poemCtxTarget
	if target == nil || target.poemID != poem.ID || target.isDragging() {
		return
	}
	lineNums := target.lineNumbers()
	if len(lineNums) == 0 {
		a.poemCtxTarget = nil
		return
	}

	bg, fg, muted, sep, border, shadow := popoverPalette(pal)

	ui.Overlay(c, func() {
		popover := ui.Column(c.Key("poem-ctx-popover")).
			AttachTo(state.anchor, ui.AnchorTop, ui.AnchorBottom).
			Margin(16).
			Padding(7, 8, 6, 8).
			Gap(6).
			Radius(12).
			Background(bg).
			Border(0.75, border).
			Shadow(0, 10, 28, -2, shadow).
			Transition(theme.DialogPanelTransition())

		actionClicked := false
		popover.Children(func() {
			colors := popoverStyleColors{fg: fg, muted: muted, sep: sep}
			if a.renderSelectionPopoverHeader(c, poem, target, lineNums, colors) {
				actionClicked = true
			}
			ui.Box(c).FillWidth().Height(0.75).Background(sep)
			if a.renderSelectionPopoverActions(c, book, poem, lineNums, fg) {
				actionClicked = true
			}
		})

		if !actionClicked && !state.openedThisFrame && !state.handleInteracted &&
			(popover.PressedOutside() || popover.OverlayShortcut(0, ui.KeyEscape)) {
			a.poemCtxTarget = nil
		}
	})
}

type popoverStyleColors struct {
	fg    ui.Color
	muted ui.Color
	sep   ui.Color
}

func (a *AppUI) renderSelectionPopoverHeader(c *ui.Context, poem domain.Poem, target *poemContextTarget, lineNums []int, colors popoverStyleColors) bool {
	totalLines := totalPoemLines(poem)
	_, stanzaStart, stanzaEnd := stanzaRangeForLine(poem, target.lineStart)
	canExpandStanza := stanzaEnd > stanzaStart && (target.lineStart != stanzaStart || target.lineEnd != stanzaEnd)
	canSelectAll := totalLines > 1 && (target.lineStart != 1 || target.lineEnd != totalLines)
	clicked := false

	renderHeaderChip := func(key, label string, onClick func()) {
		chip := ui.Box(c.Key(key)).Padding(3, 8).Radius(6).Background(colors.sep).Cursor(ui.CursorPointer)
		theme.ApplyPressScale(chip, 0.94)
		chip.Children(func() {
			ui.Text(c, label).Font(theme.FontSans).FontSize(11).FontWeight(500).TextColor(colors.fg)
		})
		if chip.Clicked() {
			clicked = true
			onClick()
		}
	}

	ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Padding(0, 4).Children(func() {
		ui.Textf(c, "已选 %d 行", len(lineNums)).Font(theme.FontSans).FontSize(11).TextColor(colors.muted).Grow(1)
		if canExpandStanza {
			renderHeaderChip("poem-ctx-stanza", "选本节", func() {
				target.lineStart, target.lineEnd = stanzaStart, stanzaEnd
				c.AnimationFrame()
			})
		}
		if canSelectAll {
			renderHeaderChip("poem-ctx-all", "全选", func() {
				target.lineStart, target.lineEnd = 1, totalLines
				c.AnimationFrame()
			})
		}
		renderHeaderChip("poem-ctx-select", "勾选", func() {
			a.enterLineSelectionMode(lineNums)
		})
	})
	return clicked
}

func (a *AppUI) renderSelectionPopoverActions(c *ui.Context, book domain.Book, poem domain.Poem, lineNums []int, fg ui.Color) bool {
	clicked := false
	renderAction := func(key string, icon *ui.SVG, label string, onClick func()) {
		btn := ui.Column(c.Key(key)).
			MinWidth(60).
			Padding(6, 8).
			Gap(4).
			Radius(8).
			AlignItems(ui.Center).
			Cursor(ui.CursorPointer)
		theme.ApplyPressScale(btn, 0.93)
		btn.Children(func() {
			ui.Icon(c, icon).Width(16).Height(16).TextColor(fg)
			ui.Text(c, label).Font(theme.FontSans).FontSize(11.5).FontWeight(500).TextColor(fg)
		})
		if btn.Clicked() {
			clicked = true
			onClick()
		}
	}

	ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
		renderAction("poem-ctx-copy", components.IconCopy, "复制", func() {
			content, _ := a.collectLinesText(poem, lineNums)
			a.poemCtxTarget = nil
			a.copyPoemText(c, content, fmt.Sprintf("已复制 %d 行诗句", len(lineNums)))
		})
		renderAction("poem-ctx-quote", components.IconBookmark, "留散页", func() {
			a.saveLineNumbersAsQuote(book, poem, lineNums)
		})
		renderAction("poem-ctx-widget", components.IconStar, "设为金句", func() {
			a.saveLineNumbersAsWidgetExcerpt(book, poem, lineNums)
		})
		renderAction("poem-ctx-share", components.IconShareCard, "分享", func() {
			content, _ := a.collectLinesText(poem, lineNums)
			a.openSelectionShareCard(book, poem, content)
		})
	})
	return clicked
}
