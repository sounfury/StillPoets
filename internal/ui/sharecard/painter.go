package sharecard

import (
	"hash/fnv"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/storage"
	"stillpoets/internal/ui/theme"
)

// drawClassicalSeal 绘制一枚克制的矢量「朱文方印」（外框微圆角 + 内含抽象篆意几何刻纹），
// 不依赖系统印章字体，在 1x 与 3x 渲染下均保持绝对清晰。
func drawClassicalSeal(p *ui.Painter, r ui.Rect, clr ui.Color) {
	if p == nil || r.W <= 0 || r.H <= 0 {
		return
	}
	side := min(r.W, r.H)
	x := r.X + (r.W-side)*0.5
	y := r.Y + (r.H-side)*0.5
	box := ui.Rect{X: x, Y: y, W: side, H: side}

	// 外朱红框
	stroke := max(1.3, side*0.075)
	p.Stroke(box, clr, stroke, side*0.12)

	// 内部对称篆意几何刻纹（「静」「诗」意象抽象线条）
	in := side * 0.22
	iw := side - in*2
	ih := side - in*2
	ix := x + in
	iy := y + in
	lw := max(1.0, side*0.055)

	// 左侧竖笔 + 两短横
	p.Fill(ui.Rect{X: ix + iw*0.18, Y: iy, W: lw, H: ih}, clr, lw*0.3)
	p.Fill(ui.Rect{X: ix, Y: iy + ih*0.24, W: iw * 0.42, H: lw}, clr, lw*0.3)
	p.Fill(ui.Rect{X: ix, Y: iy + ih*0.64, W: iw * 0.42, H: lw}, clr, lw*0.3)

	// 右侧口框 + 上短横（诗意印纹）
	rx := ix + iw*0.54
	rw := iw * 0.46
	p.Fill(ui.Rect{X: rx, Y: iy + ih*0.08, W: rw, H: lw}, clr, lw*0.3)
	p.Fill(ui.Rect{X: rx + rw*0.42, Y: iy, W: lw, H: ih * 0.42}, clr, lw*0.3)
	p.Stroke(ui.Rect{X: rx + lw*0.4, Y: iy + ih*0.48, W: rw - lw*0.8, H: ih * 0.48}, clr, lw, lw*0.4)
}

// drawDoubleFrame 绘制古典线装书双线边框（外细框 + 内缩细框 + 四角书角小折纹）。
func drawDoubleFrame(p *ui.Painter, r ui.Rect, outerClr, innerClr ui.Color) {
	if p == nil || r.W < 24 || r.H < 24 {
		return
	}
	inset1 := float32(10)
	r1 := ui.Rect{X: r.X + inset1, Y: r.Y + inset1, W: r.W - inset1*2, H: r.H - inset1*2}
	p.Stroke(r1, outerClr, 1.1, 2)

	inset2 := float32(14.5)
	r2 := ui.Rect{X: r.X + inset2, Y: r.Y + inset2, W: r.W - inset2*2, H: r.H - inset2*2}
	p.Stroke(r2, innerClr, 0.75, 1)

	// 四角微距书角点缀
	corner := float32(5.5)
	corners := [][2]float32{
		{r2.X, r2.Y},
		{r2.X + r2.W - corner, r2.Y},
		{r2.X, r2.Y + r2.H - corner},
		{r2.X + r2.W - corner, r2.Y + r2.H - corner},
	}
	for _, pt := range corners {
		p.Stroke(ui.Rect{X: pt[0], Y: pt[1], W: corner, H: corner}, outerClr, 0.85, 0)
	}
}

// drawMiniBookCover 在卡片内渲染迷你实体书封（有原书封面图则直接渲染，否则生成稳定抽象几何书封 + 书脊折痕）。
func renderMiniBookCover(c *ui.Context, key, bookID string, coverImg ui.ImageSource, pal CardPalette, w, h float32) {
	shadowClr := ui.RGBA(28, 22, 16, 0.16)
	if pal.IsDark {
		shadowClr = ui.RGBA(0, 0, 0, 0.45)
	}
	wrap := ui.Box(c.Key(key)).
		Width(w).Height(h).
		Radius(2, 5, 5, 2).
		Shadow(0, 3, 8, -1, shadowClr)

	if coverImg != nil {
		wrap.Children(func() {
			ui.Box(c.Key(key+"-img")).
				Fill().
				Radius(2, 5, 5, 2).
				Clip().
				Border(0.75, pal.Border).
				DrawOver(func(p *ui.Painter, r ui.Rect) {
					drawMiniSpine(p, r, pal.IsDark)
				}).
				Children(func() {
					ui.Image(c, coverImg).Fill().Fit(ui.Cover)
				})
		})
		return
	}

	swatches := pal.CoverSwatches
	if len(swatches) == 0 {
		swatches = theme.Artbook().CoverSwatches
	}
	h32 := fnv.New32a()
	_, _ = h32.Write([]byte(bookID))
	sw := swatches[int(h32.Sum32())%len(swatches)]

	wrap.Children(func() {
		ui.Box(c.Key(key+"-art")).
			Fill().
			Radius(2, 5, 5, 2).
			Clip().
			Draw(func(p *ui.Painter, r ui.Rect) {
				p.Fill(r, sw.Bg, 0)
				path := (&ui.Path{}).
					MoveTo(r.X+r.W*0.16, r.Y+r.H).
					QuadTo(r.X+r.W*0.36, r.Y+r.H*0.26, r.X+r.W, r.Y+r.H*0.16).
					LineTo(r.X+r.W, r.Y+r.H).
					Close()
				p.FillPath(path, sw.Accent)
				theme.DrawPaperTexture(p, r, 0, sw.IsDarkSurface(), 0.048)
				drawMiniSpine(p, r, pal.IsDark)
			})
	})
}

func drawMiniSpine(p *ui.Painter, r ui.Rect, isDark bool) {
	spineW := min(max(r.W*0.075, 4), 7.5)
	wrapAlpha := float32(0.18)
	grooveAlpha := float32(0.20)
	hiAlpha := float32(0.26)
	if isDark {
		wrapAlpha = 0.28
		grooveAlpha = 0.30
		hiAlpha = 0.14
	}
	p.FillGradient(
		ui.Rect{X: r.X, Y: r.Y, W: spineW, H: r.H},
		ui.LinearGradient{From: ui.RGBA(0, 0, 0, wrapAlpha), To: ui.RGBA(0, 0, 0, 0), Angle: 90},
		0,
	)
	gx := r.X + spineW
	p.Fill(ui.Rect{X: gx, Y: r.Y, W: 1.0, H: r.H}, ui.RGBA(0, 0, 0, grooveAlpha), 0)
	p.Fill(ui.Rect{X: gx + 1.0, Y: r.Y, W: 1.0, H: r.H}, ui.RGBA(255, 255, 255, hiAlpha), 0)
}

// drawMiniHeatmap 将读诗热力图缩略绘制到统计报告卡片中（取最近最多 28 周，自动计算单元格宽高铺满容器）。
func drawMiniHeatmap(p *ui.Painter, r ui.Rect, hm storage.HeatmapData, colors [5]ui.Color) {
	if p == nil || r.W <= 0 || r.H <= 0 || len(hm.Weeks) == 0 {
		return
	}
	weeks := hm.Weeks
	const maxCols = 28
	if len(weeks) > maxCols {
		weeks = weeks[len(weeks)-maxCols:]
	}
	cols := len(weeks)
	const rows = 7
	gap := float32(2.2)

	cellW := (r.W - gap*float32(cols-1)) / float32(cols)
	cellH := (r.H - gap*float32(rows-1)) / float32(rows)
	cell := min(cellW, cellH)
	if cell < 2 {
		cell = 2
	}
	totalW := cell*float32(cols) + gap*float32(cols-1)
	totalH := cell*float32(rows) + gap*float32(rows-1)
	ox := r.X + (r.W-totalW)*0.5
	oy := r.Y + (r.H-totalH)*0.5
	rad := min(cell*0.24, 2.2)

	for ci, wk := range weeks {
		cx := ox + float32(ci)*(cell+gap)
		for ri := 0; ri < rows; ri++ {
			day := wk.Days[ri]
			if day.OutOfRange {
				continue
			}
			cy := oy + float32(ri)*(cell+gap)
			lvl := day.Level
			if lvl < 0 {
				lvl = 0
			} else if lvl > 4 {
				lvl = 4
			}
			clr := colors[lvl]
			if day.InFuture {
				clr = ui.Color{R: colors[0].R, G: colors[0].G, B: colors[0].B, A: colors[0].A / 2}
			}
			p.Fill(ui.Rect{X: cx, Y: cy, W: cell, H: cell}, clr, rad)
		}
	}
}

// drawMiniHourlyBars 将 24 小时读诗频次绘制为精致的等宽迷你柱状图。
func drawMiniHourlyBars(p *ui.Painter, r ui.Rect, hourly [24]int, accent, track ui.Color) {
	if p == nil || r.W <= 0 || r.H <= 0 {
		return
	}
	maxVal := 0
	for _, v := range hourly {
		if v > maxVal {
			maxVal = v
		}
	}
	gap := float32(3)
	barW := (r.W - gap*23) / 24
	if barW < 2 {
		barW = 2
	}
	for i := 0; i < 24; i++ {
		bx := r.X + float32(i)*(barW+gap)
		// 底层轨道刻度
		p.Fill(ui.Rect{X: bx, Y: r.Y + r.H - 3, W: barW, H: 3}, track, 1.5)
		if maxVal == 0 || hourly[i] <= 0 {
			continue
		}
		ratio := float32(hourly[i]) / float32(maxVal)
		bh := max(4.5, ratio*(r.H-2))
		clr := accent
		if ratio < 0.45 {
			clr = ui.Color{R: accent.R, G: accent.G, B: accent.B, A: 145}
		}
		p.Fill(ui.Rect{X: bx, Y: r.Y + r.H - bh, W: barW, H: bh}, clr, min(barW*0.45, 2.5))
	}
}
