package components

import (
	"hash/fnv"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/ui/theme"
)

// coverSeed 根据 bookID 稳定映射几何构图样式与色板索引，确保无封面诗集跨端展示一致。
func coverSeed(bookID string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(bookID))
	return h.Sum32()
}

// BookCoverThumbnail 渲染「继续阅读」里的迷你抽象几何缩略封（44x44）。
func BookCoverThumbnail(c *ui.Context, key, bookID string, pal theme.Palette) {
	sw := pal.CoverSwatches[int(coverSeed(bookID))%len(pal.CoverSwatches)]
	ui.Box(c.Key(key)).Width(44).Height(44).Radius(4).Clip().
		Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(r, sw.Bg, 4)
			path := (&ui.Path{}).
				MoveTo(r.X+r.W*0.18, r.Y+r.H).
				QuadTo(r.X+r.W*0.35, r.Y+r.H*0.28, r.X+r.W, r.Y+r.H*0.18).
				LineTo(r.X+r.W, r.Y+r.H).
				Close()
			p.FillPath(path, sw.Accent)
		})
}

// BookCoverCard 渲染现代独立出版风格的抽象几何书封（支持平板选中框与云端未下载角标）。
func BookCoverCard(c *ui.Context, key, bookID, title, author string, pal theme.Palette, selected, isRemote bool) bool {
	seed := coverSeed(bookID)
	sw := pal.CoverSwatches[int(seed)%len(pal.CoverSwatches)]
	pattern := int(seed/7) % 4

	outer := ui.Column(c.Key(key)).Gap(6).Cursor(ui.CursorPointer)
	theme.ApplyPressScale(outer, 0.965)
	outer.Children(func() {
		cover := ui.Box(c.Key("art")).
			FillWidth().
			AspectRatio(0.74).
			Radius(4).
			Clip().
			Draw(func(p *ui.Painter, r ui.Rect) {
				p.Fill(r, sw.Bg, 4)
				drawGeometricMotif(p, r, pattern, sw, pal)
			})
		if selected {
			cover.Border(2, pal.Accent)
		}
		cover.Children(func() {
			ui.Column(c).Fill().Padding(11, 10).Children(func() {
				ui.Row(c).AlignItems(ui.Start).Children(func() {
					ui.Column(c).Grow(1).Gap(3).Children(func() {
						ui.Text(c, title).FontSize(14).Bold().TextColor(sw.Text).MaxLines(2)
						if author != "" {
							ui.Text(c, author).FontSize(11).TextColor(sw.Muted).MaxLines(1)
						}
					})
					if isRemote {
						ui.Icon(c, IconCloud).Width(14).Height(14).TextColor(sw.Muted)
					}
				})
			})
		})

		// 刊物风在封面下方额外保留极简书名/作者落款，画册风仅在封面内排版
		if pal.HeroStyle == theme.HeroOpenPaper {
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, title).FontSize(13).Bold().TextColor(pal.TextPrimary).MaxLines(1)
				if author != "" {
					ui.Text(c, author).FontSize(11).TextColor(pal.TextMuted).MaxLines(1)
				}
			})
		}
	})
	return outer.Clicked()
}

func drawGeometricMotif(p *ui.Painter, r ui.Rect, pattern int, sw theme.CardSwatch, pal theme.Palette) {
	switch pattern {
	case 0:
		// 双曲线扫掠（参考《海子诗集》赤陶暖白构图）
		arc1 := (&ui.Path{}).
			MoveTo(r.X+r.W*0.08, r.Y+r.H).
			QuadTo(r.X+r.W*0.32, r.Y+r.H*0.42, r.X+r.W, r.Y+r.H*0.36).
			LineTo(r.X+r.W, r.Y+r.H).
			Close()
		p.FillPath(arc1, sw.Accent)
		arc2 := (&ui.Path{}).
			MoveTo(r.X+r.W*0.46, r.Y+r.H).
			QuadTo(r.X+r.W*0.62, r.Y+r.H*0.66, r.X+r.W, r.Y+r.H*0.62).
			LineTo(r.X+r.W, r.Y+r.H).
			Close()
		p.FillPath(arc2, sw.Bg)

	case 1:
		// 深拱形 + 几何角块（参考《恶之花》鼠尾草/墨黑构图）
		arch := (&ui.Path{}).
			MoveTo(r.X+r.W*0.32, r.Y+r.H).
			QuadTo(r.X+r.W*0.42, r.Y+r.H*0.42, r.X+r.W, r.Y+r.H*0.34).
			LineTo(r.X+r.W, r.Y+r.H).
			Close()
		p.FillPath(arch, sw.Accent)
		wedge := (&ui.Path{}).
			MoveTo(r.X, r.Y+r.H*0.72).
			QuadTo(r.X+r.W*0.36, r.Y+r.H*0.68, r.X+r.W*0.48, r.Y+r.H).
			LineTo(r.X, r.Y+r.H).
			Close()
		p.FillPath(wedge, pal.Surface)
		p.Fill(ui.Rect{X: r.X + r.W*0.72, Y: r.Y + r.H*0.78, W: r.W * 0.28, H: r.H * 0.22}, sw.Bg, 0)

	case 2:
		// 双叶弧线（参考《草叶集》墨蓝/橄榄植物抽象构图）
		leaf1 := (&ui.Path{}).
			MoveTo(r.X+r.W*0.26, r.Y+r.H).
			QuadTo(r.X+r.W*0.34, r.Y+r.H*0.56, r.X+r.W*0.86, r.Y+r.H*0.44).
			QuadTo(r.X+r.W*0.72, r.Y+r.H*0.82, r.X+r.W*0.26, r.Y+r.H).
			Close()
		p.FillPath(leaf1, sw.Accent)
		leaf2 := (&ui.Path{}).
			MoveTo(r.X+r.W*0.52, r.Y+r.H).
			QuadTo(r.X+r.W*0.72, r.Y+r.H*0.66, r.X+r.W, r.Y+r.H*0.68).
			LineTo(r.X+r.W, r.Y+r.H).
			Close()
		p.FillPath(leaf2, sw.Accent)

	default:
		// 沉稳斜切面（参考《叶芝诗集》暖赭/刊物酒红构图）
		diag := (&ui.Path{}).
			MoveTo(r.X+r.W*0.18, r.Y+r.H).
			QuadTo(r.X+r.W*0.48, r.Y+r.H*0.50, r.X+r.W, r.Y+r.H*0.48).
			LineTo(r.X+r.W, r.Y+r.H).
			Close()
		p.FillPath(diag, sw.Accent)
	}
}
