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

// drawBookSpineEffect 为实体书封绘制左侧书脊折痕（柔和包边阴影 + 压痕槽深线 + 纸棱高光）与纸边厚感。
func drawBookSpineEffect(p *ui.Painter, r ui.Rect, isDark bool) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	spineW := min(max(r.W*0.068, 4.5), 8.5)
	// 1. 最左侧圆弧书脊包边暗部
	wrapAlpha := float32(0.18)
	if isDark {
		wrapAlpha = 0.28
	}
	p.FillGradient(
		ui.Rect{X: r.X, Y: r.Y, W: spineW, H: r.H},
		ui.LinearGradient{
			From:  ui.RGBA(0, 0, 0, wrapAlpha),
			To:    ui.RGBA(0, 0, 0, 0),
			Angle: 90,
		},
		0,
	)

	// 2. 书脊压痕槽（1px 深压痕 + 紧邻 1px 柔和纸棱高光 + 极浅漫反射过渡）
	grooveX := r.X + spineW
	grooveAlpha := float32(0.19)
	hiAlpha := float32(0.28)
	if isDark {
		grooveAlpha = 0.30
		hiAlpha = 0.14
	}
	p.Fill(ui.Rect{X: grooveX, Y: r.Y, W: 1.1, H: r.H}, ui.RGBA(0, 0, 0, grooveAlpha), 0)
	p.Fill(ui.Rect{X: grooveX + 1.1, Y: r.Y, W: 1.1, H: r.H}, ui.RGBA(255, 255, 255, hiAlpha), 0)
	p.FillGradient(
		ui.Rect{X: grooveX + 2.2, Y: r.Y, W: 3.2, H: r.H},
		ui.LinearGradient{
			From:  ui.RGBA(255, 255, 255, hiAlpha*0.35),
			To:    ui.RGBA(255, 255, 255, 0),
			Angle: 90,
		},
		0,
	)

	// 3. 顶部与右切口极细纸面折光，强化纸卡立面厚度
	p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 0.8}, ui.RGBA(255, 255, 255, hiAlpha*0.65), 0)
	p.Fill(ui.Rect{X: r.X + r.W - 0.9, Y: r.Y, W: 0.9, H: r.H}, ui.RGBA(0, 0, 0, grooveAlpha*0.5), 0)
	p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - 1.0, W: r.W, H: 1.0}, ui.RGBA(0, 0, 0, grooveAlpha*0.65), 0)
}

// BookCoverThumbnail 渲染「继续阅读」与「统计」里的迷你书封缩略图（40x48 实体书比例）。
// 若传入了原书封面图 coverImg 则优先展示原书封面，否则渲染抽象几何色块封。
func BookCoverThumbnail(c *ui.Context, key, bookID string, coverImg ui.ImageSource, pal theme.Palette) {
	shadowClr := ui.RGBA(28, 22, 16, 0.15)
	if pal.IsDark() {
		shadowClr = ui.RGBA(0, 0, 0, 0.42)
	}
	wrap := ui.Box(c.Key(key)).
		Width(38).Height(48).
		Radius(2, 4, 4, 2).
		Shadow(0, 2, 5, -1, shadowClr)

	if coverImg != nil {
		wrap.Children(func() {
			ui.Box(c.Key("thumb-img")).
				Fill().
				Radius(2, 4, 4, 2).
				Clip().
				Border(1, pal.Border).
				DrawOver(func(p *ui.Painter, r ui.Rect) {
					drawBookSpineEffect(p, r, pal.IsDark())
				}).
				Children(func() {
					ui.Image(c, coverImg).Fill().Fit(ui.Cover)
				})
		})
		return
	}
	sw := pal.CoverSwatches[int(coverSeed(bookID))%len(pal.CoverSwatches)]
	wrap.Children(func() {
		ui.Box(c.Key("thumb-art")).
			Fill().
			Radius(2, 4, 4, 2).
			Clip().
			Draw(func(p *ui.Painter, r ui.Rect) {
				p.Fill(r, sw.Bg, 0)
				path := (&ui.Path{}).
					MoveTo(r.X+r.W*0.18, r.Y+r.H).
					QuadTo(r.X+r.W*0.35, r.Y+r.H*0.28, r.X+r.W, r.Y+r.H*0.18).
					LineTo(r.X+r.W, r.Y+r.H).
					Close()
				p.FillPath(path, sw.Accent)
				theme.DrawPaperTexture(p, r, 0, sw.IsDarkSurface(), 0.048)
				drawBookSpineEffect(p, r, pal.IsDark())
			})
	})
}

// BookCoverCard 渲染诗集封面卡：开启原书封面且有图时展示 EPUB 原封面，否则呈现抽象几何色块封面。
// 封面带左侧书脊压痕高光/阴影、棉纸肌理与底部柔和接触投影，营造立在书架上的实体书物感。
func BookCoverCard(c *ui.Context, key, bookID, title, author string, coverImg ui.ImageSource, pal theme.Palette, selected, isRemote, forceCaptionBelow bool) bool {
	seed := coverSeed(bookID)
	sw := pal.CoverSwatches[int(seed)%len(pal.CoverSwatches)]
	pattern := int(seed/7) % 4
	isDark := pal.IsDark()

	// 外层柔和环境阴影 + 底部紧贴接触投影
	dropShadow := ui.RGBA(28, 22, 16, 0.16)
	contactShadow := ui.RGBA(24, 18, 12, 0.22)
	if isDark {
		dropShadow = ui.RGBA(0, 0, 0, 0.48)
		contactShadow = ui.RGBA(0, 0, 0, 0.62)
	}

	outer := ui.Column(c.Key(key)).Gap(6).Cursor(ui.CursorPointer)
	theme.ApplyPressScale(outer, 0.965)
	outer.Children(func() {
		// 实体书外框（不裁剪阴影，并在底部绘制柔和接触投影）
		bookShell := ui.Box(c.Key("book-shell")).
			FillWidth().
			AspectRatio(0.73).
			Radius(2, 5, 5, 2).
			Shadow(0, 5, 12, -2, dropShadow).
			Draw(func(p *ui.Painter, r ui.Rect) {
				// 紧贴书底的窄接触阴影，让书本像稳稳立在书架上
				base := ui.Rect{X: r.X + 3, Y: r.Y + r.H - 2, W: max(0, r.W-6), H: 3.5}
				p.Shadow(base, 2, 0, 2.5, 4.5, 0, contactShadow)
			})

		bookShell.Children(func() {
			if coverImg != nil {
				cover := ui.Box(c.Key("art-img")).
					Fill().
					Radius(2, 5, 5, 2).
					Clip().
					Background(pal.SurfaceMuted).
					DrawOver(func(p *ui.Painter, r ui.Rect) {
						drawBookSpineEffect(p, r, isDark)
					})
				if selected {
					cover.Border(2, pal.Accent)
				} else {
					cover.Border(0.75, pal.Border)
				}
				cover.Children(func() {
					ui.Image(c, coverImg).Fill().Fit(ui.Cover)
					if isRemote {
						ui.Box(c.Key("remote-badge")).
							Absolute().
							Top(6).Right(6).
							Padding(4).
							Radius(999).
							Background(ui.RGBA(0, 0, 0, 0.45)).
							Children(func() {
								ui.Icon(c, IconCloud).Width(12).Height(12).TextColor(ui.RGB(255, 255, 255))
							})
					}
				})
			} else {
				cover := ui.Box(c.Key("art")).
					Fill().
					Radius(2, 5, 5, 2).
					Clip().
					Draw(func(p *ui.Painter, r ui.Rect) {
						p.Fill(r, sw.Bg, 0)
						drawGeometricMotif(p, r, pattern, sw, pal)
						theme.DrawPaperTexture(p, r, 0, sw.IsDarkSurface(), 0.05)
						drawBookSpineEffect(p, r, isDark)
					})
				if selected {
					cover.Border(2, pal.Accent)
				}
				cover.Children(func() {
					// 左内边距略宽（14 DIP）以避开书脊压痕线
					ui.Column(c).Fill().Padding(11, 10, 10, 14).Children(func() {
						ui.Row(c).AlignItems(ui.Start).Children(func() {
							ui.Column(c).Grow(1).Gap(3).Children(func() {
								ui.Text(c, title).
									Font(theme.FontSerif).
									FontSize(13.5).
									Bold().
									LineHeight(1.3).
									TextColor(sw.Text).
									MaxLines(2)
								if author != "" {
									ui.Text(c, author).
										Font(theme.FontNote).
										FontSize(11).
										TextColor(sw.Muted).
										MaxLines(1)
								}
							})
							if isRemote {
								ui.Icon(c, IconCloud).Width(13).Height(13).TextColor(sw.Muted)
							}
						})
					})
				})
			}
		})

		// 刊物风或开启原书封面模式时，在封面下方统一排布极简书名/作者落款
		if pal.HeroStyle == theme.HeroOpenPaper || forceCaptionBelow || coverImg != nil {
			ui.Column(c).Gap(2).Padding(2, 0, 0, 1).Children(func() {
				ui.Text(c, title).Font(theme.FontSerif).FontSize(12.5).Bold().TextColor(pal.TextPrimary).MaxLines(1)
				if author != "" {
					ui.Text(c, author).Font(theme.FontNote).FontSize(11).TextColor(pal.TextMuted).MaxLines(1)
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
