package theme

import (
	"image"
	"image/color"
	"math"
	"sync"

	"github.com/egoist/mygo/ui"
)

const paperTileSize = 224

var (
	paperTileOnce sync.Once
	paperTileBmp  *ui.Bitmap
)

// paperTextureBitmap 程序化生成一张可无缝平铺的 224x224 棉纸肌理贴图（全白 + 变化 Alpha）。
// 仅在首次绘制时构建一次，绘制阶段通过 ImageOptions.Tint 统一控制暖墨/暖白着色与 3%~5% 透明度。
func paperTextureBitmap() *ui.Bitmap {
	paperTileOnce.Do(func() {
		rng := uint32(0x5A3C91E7)
		nextF := func() float32 {
			rng ^= rng << 13
			rng ^= rng >> 17
			rng ^= rng << 5
			return float32(rng&0xFFFF) / 65535.0
		}
		var alpha paperTileAlpha
		fillPaperGrain(&alpha, nextF)
		addPaperFibers(&alpha, nextF)
		paperTileBmp = ui.NewBitmap(paperTileImage(&alpha))
	})
	return paperTileBmp
}

type paperTileAlpha [paperTileSize * paperTileSize]float32

func fillPaperGrain(alphaBuf *paperTileAlpha, nextF func() float32) {
	const n = paperTileSize
	// 低频网格用于双线性平滑的柔和纸张云纹。
	const grid = 28
	const cell = n / grid
	var coarse [grid][grid]float32
	for gy := 0; gy < grid; gy++ {
		for gx := 0; gx < grid; gx++ {
			coarse[gy][gx] = nextF()
		}
	}

	// 写入基础纸张颗粒（柔和云纹 + 细腻纸齿 + 极轻水平帘纹）
	for y := 0; y < n; y++ {
		gy0 := y / cell
		gy1 := (gy0 + 1) % grid
		fy := float32(y%cell) / float32(cell)
		// smoothstep 让纸面起伏更柔和
		sy := fy * fy * (3 - 2*fy)

		// 极微弱的横纹（手工纸帘纹感）
		laid := float32(0.5+0.5*math.Sin(float64(y)*math.Pi/3.0)) * 0.14

		for x := 0; x < n; x++ {
			gx0 := x / cell
			gx1 := (gx0 + 1) % grid
			fx := float32(x%cell) / float32(cell)
			sx := fx * fx * (3 - 2*fx)

			c00 := coarse[gy0][gx0]
			c10 := coarse[gy0][gx1]
			c01 := coarse[gy1][gx0]
			c11 := coarse[gy1][gx1]
			low := (c00*(1-sx)+c10*sx)*(1-sy) + (c01*(1-sx)+c11*sx)*sy

			// 细腻纸齿（轻度均值化，避免刺眼像素点）
			fine := (nextF() + nextF()) * 0.5
			v := low*0.42 + fine*0.44 + laid
			// 拉开轻微对比，使微弱透明度下仍能感知柔和质感
			v = float32(math.Pow(float64(v), 1.35))
			alphaBuf[y*n+x] = v * 0.72
		}
	}
}

// 叠加 24 根无缝回绕的极细棉纸纤维（长度 5~11px，柔和不抢戏）。
func addPaperFibers(alphaBuf *paperTileAlpha, nextF func() float32) {
	for range 24 {
		addPaperFiber(alphaBuf, nextF)
	}
}

func addPaperFiber(alphaBuf *paperTileAlpha, nextF func() float32) {
	const n = paperTileSize
	x0 := nextF() * float32(n)
	y0 := nextF() * float32(n)
	angle := nextF() * 2 * math.Pi
	length := 5.0 + nextF()*6.5
	curve := (nextF() - 0.5) * 0.6
	strength := 0.14 + nextF()*0.18
	steps := int(length * 2.2)
	for s := 0; s <= steps; s++ {
		t := float32(s) / float32(max(1, steps))
		a := float64(angle + curve*t)
		px := x0 + float32(math.Cos(a))*length*t
		py := y0 + float32(math.Sin(a))*length*t
		// 两端渐隐，纤维更自然
		taper := float32(math.Sin(float64(t) * math.Pi))
		w := strength * taper

		ix := int(math.Floor(float64(px)))
		iy := int(math.Floor(float64(py)))
		fx := px - float32(ix)
		fy := py - float32(iy)

		// 2x2 抗锯齿回绕写入
		addPx := func(x, y int, weight float32) {
			wx := ((x % n) + n) % n
			wy := ((y % n) + n) % n
			idx := wy*n + wx
			alphaBuf[idx] = min(1.0, alphaBuf[idx]+weight)
		}
		addPx(ix, iy, w*(1-fx)*(1-fy))
		addPx(ix+1, iy, w*fx*(1-fy))
		addPx(ix, iy+1, w*(1-fx)*fy)
		addPx(ix+1, iy+1, w*fx*fy)
	}
}

func paperTileImage(alphaBuf *paperTileAlpha) *image.NRGBA {
	const n = paperTileSize
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			a := alphaBuf[y*n+x]
			if a < 0 {
				a = 0
			} else if a > 1 {
				a = 1
			}
			img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: uint8(a * 255)})
		}
	}
	return img
}

// DrawPaperTexture 在矩形 r 内叠加一层极淡（通常 3%~5%）的无缝纸张肌理与纤维纹理。
// isDarkSurface 为 true 时使用极淡暖白纤维高光，否则使用极淡暖墨肌理。
func DrawPaperTexture(p *ui.Painter, r ui.Rect, radius float32, isDarkSurface bool, opacity float32) {
	if p == nil || opacity <= 0 || r.W <= 0 || r.H <= 0 {
		return
	}
	bmp := paperTextureBitmap()
	if bmp == nil {
		return
	}

	tint := ui.RGBA(46, 38, 28, opacity)
	if isDarkSurface {
		tint = ui.RGBA(248, 244, 236, opacity)
	}

	vis := p.Visible()
	drawTiles := func() { drawPaperTiles(p, r, vis, bmp, tint) }

	if radius > 0 {
		p.Clip(r, radius, drawTiles)
	} else {
		p.Clip(r, 0, drawTiles)
	}
}

func drawPaperTiles(p *ui.Painter, r, vis ui.Rect, bmp *ui.Bitmap, tint ui.Color) {
	tile := float32(paperTileSize)
	// 对齐到窗口坐标网格，使滚动或相邻卡片不产生奇怪的相位跳变
	startX := float32(math.Floor(float64(r.X/tile))) * tile
	startY := float32(math.Floor(float64(r.Y/tile))) * tile
	endX := r.X + r.W
	endY := r.Y + r.H

	opts := ui.ImageOptions{Tint: tint}
	for y := startY; y < endY; y += tile {
		if vis.H > 0 && (y+tile < vis.Y || y > vis.Y+vis.H) {
			continue
		}
		for x := startX; x < endX; x += tile {
			if vis.W > 0 && (x+tile < vis.X || x > vis.X+vis.W) {
				continue
			}
			p.ImageRegion(bmp, ui.Rect{}, ui.Rect{X: x, Y: y, W: tile, H: tile}, opts)
		}
	}
}
