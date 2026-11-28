package views

import (
	"fmt"
	"math"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/domain"
	"stillpoets/internal/ui/theme"
)

func (a *AppUI) renderEmptyReader(c *ui.Context, pal theme.Palette) {
	ui.Column(c).Fill().Center().Gap(18).Padding(36).Children(func() {
		ui.Text(c, "StillPoets").Font(theme.FontBrand).FontSize(26).Bold().TextColor(pal.TextPrimary)
		ui.Column(c.Key("reader-empty-thoreau")).
			MaxWidth(360).
			Padding(18, 22).
			Gap(10).
			Radius(8).
			Background(pal.Surface).
			Border(1, pal.Border).
			Children(func() {
				ui.Text(c, dpsThoreauOpening).
					Font(theme.PoetryFont(a.settings.FontFamily)).
					FontSize(15).
					LineHeight(1.85).
					TextColor(pal.TextPrimary)
				attr := "—— " + dpsThoreauAttribution
				if a.isMidnightCave() {
					attr += " · " + dpsMidnightCaveLabel
				}
				ui.Text(c, attr).
					Font(theme.FontNote).
					FontSize(11.5).
					Italic().
					TextColor(pal.TextMuted)
			})
		ui.Text(c, "在左侧选择一本诗集，或点右上角 + 导入新的 EPUB。").
			Font(theme.FontNote).FontSize(13).TextColor(pal.TextMuted)
	})
}

func (a *AppUI) renderReaderError(c *ui.Context, pal theme.Palette, book domain.Book, current domain.Poem) {
	// 若加载本诗正文或切书出现错误，展示明确错误条并提供重试入口（P1-7）
	if a.readerError != "" {
		ui.Row(c.Key("reader-error-bar")).FillWidth().Padding(8, 20).Gap(10).AlignItems(ui.Center).Background(pal.HighlightBg).Children(func() {
			ui.Text(c, a.readerError).FontSize(12).TextColor(pal.Accent).Grow(1)
			if ui.Button(c, "重试").Clicked() {
				a.openBookWithSource(book.ID, current.ID, a.highlightRanges, a.readerMobile, a.readerEntrySource)
			}
		})
	}
}

func (a *AppUI) syncPoemPager(bookID string, width float32, idx int) {
	// 换书或容器宽度变化时，重新锚定偏移并递增 epoch，避免跨书/旋屏长距离飞跃
	if a.poemPager.scopeKey != bookID {
		a.poemPager.scopeKey = bookID
		a.poemPager.width = width
		a.poemPager.syncedPage = idx
		a.poemPager.offset = ui.Point{X: -float32(idx) * width}
		a.poemPager.epoch++
	} else if math.Abs(float64(a.poemPager.width-width)) > 0.5 {
		a.poemPager.width = width
		a.poemPager.offset = ui.Point{X: -float32(idx) * width}
		a.poemPager.epoch++
	} else if a.poemPager.syncedPage != idx {
		// 外部改了目标诗篇：相邻一首平滑滑过去，目录跨多首则轻淡入直达
		delta := idx - a.poemPager.syncedPage
		if delta < 0 {
			delta = -delta
		}
		if delta > 1 {
			a.poemPager.epoch++
		}
		a.poemPager.syncedPage = idx
		a.poemPager.offset = ui.Point{X: -float32(idx) * width}
	}
}

func (a *AppUI) renderPoemScroll(c *ui.Context, pal theme.Palette, book domain.Book, poem domain.Poem, isCurrent bool) {
	ui.Scroll(c.Key("poem-scroll-" + poem.ID)).Fill().Children(func() {
		ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(28, 28, 48, 28).Children(func() {
			a.renderPoemContent(c, pal, book, poem, isCurrent)
		})
	})
}

func (a *AppUI) renderPoemSwipeTrack(c *ui.Context, pal theme.Palette, book domain.Book, poems []domain.Poem, width float32, idx int) {
	total := len(poems)
	trackKey := fmt.Sprintf("poem-track-%s-%d", book.ID, a.poemPager.epoch)
	track := ui.Box(c.Key(trackKey)).
		Absolute().
		Left(0).Top(0).Bottom(0).
		Width(width * float32(total)).
		Transition(theme.PoemJumpTransition())

	curIdx := idx
	startX := -float32(curIdx) * width
	threshold := min(width*0.26, 140)
	track.Draggable(&a.poemPager.offset, ui.DraggableOptions{
		Axis:      ui.Horizontal,
		Bounds:    &ui.Rect{X: -float32(total-1) * width, W: float32(total-1) * width},
		Animation: theme.PageSpring,
		Snap: func(p ui.Point) ui.Point {
			// 方向性翻页阈值（兼顾平板宽栏轻划手感与短距离防误触回弹），单次最多切一首
			to := curIdx
			delta := p.X - startX
			switch {
			case delta < -threshold && curIdx+1 < total:
				to = curIdx + 1
			case delta > threshold && curIdx > 0:
				to = curIdx - 1
			}
			return ui.Point{X: -float32(to) * width}
		},
	})

	shownX := track.DragOffset().X
	at := max(0, min(total-1, int(math.Round(float64(-shownX/width)))))
	lo, hi := max(0, at-1), min(total-1, at+1)

	// O(1) 窗口化渲染：仅构建当前页及其紧邻前后两首诗，无论诗集百首还是千首都恒定开销
	track.Children(func() {
		for i := lo; i <= hi; i++ {
			poem := poems[i]
			isCurrent := i == a.activePoemIdx
			ui.Box(c.Key("poem-page-" + poem.ID)).
				Absolute().
				Left(float32(i) * width).
				Top(0).Bottom(0).
				Width(width).
				Children(func() {
					a.renderPoemScroll(c, pal, book, poem, isCurrent)
				})
		}
	})

	if track.Dragging() && a.poemCtxTarget != nil {
		a.poemCtxTarget = nil
	}

	// 松手落点确定后立即同步顶栏标题、收藏态、页码与已读进度，正文继续平滑完成弹簧落位
	if !track.Dragging() {
		target := max(0, min(total-1, int(math.Round(float64(-a.poemPager.offset.X/width)))))
		if target != a.activePoemIdx {
			a.poemPager.syncedPage = target
			a.commitPoemIndex(book.ID, target)
			c.AnimationFrame()
		}
	}
}
