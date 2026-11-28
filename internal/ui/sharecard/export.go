package sharecard

import (
	"bytes"
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

const (
	minExportHeightDIP = 180
	maxExportKeepFiles = 12
)

// RenderPNG 以两阶段无头软件渲染方式，将卡片精确渲染为无损高清 PNG 字节流：
//  1. 测量阶段：以固定逻辑宽度 + 充足垂直画布布局卡片，读取卡片根容器的真实排版高度；
//  2. 渲染阶段：将画布精确裁剪至实测高度，按 cfg.Scale（默认 3.0，即 1080px 宽）光栅化并编码 PNG。
func RenderPNG(data CardData, opts Options, cfg RenderConfig) ([]byte, int, int, error) {
	if cfg.WidthDIP <= 0 {
		cfg.WidthDIP = DefaultRenderConfig().WidthDIP
	}
	if cfg.Scale <= 0 {
		cfg.Scale = DefaultRenderConfig().Scale
	}
	if cfg.MaxHeightDIP <= 0 {
		cfg.MaxHeightDIP = DefaultRenderConfig().MaxHeightDIP
	}

	widthDIP := cfg.WidthDIP
	var measuredH float32

	measureView := func(c *ui.Context) {
		root := ui.Column(c.Key("sc-export-stage")).FillWidth().AlignItems(ui.Start)
		root.Children(func() {
			wrap := ui.Box(c.Key("sc-export-measure-wrap")).Width(float32(widthDIP))
			wrap.Children(func() {
				RenderCard(c, data, opts, float32(widthDIP))
			})
			if b := wrap.Bounds(); b.H > 0 {
				measuredH = b.H
			}
		})
	}

	// 1. 测量阶段（1x，快速完成一次布局计算）
	tt := ui.NewTester(measureView, widthDIP, cfg.MaxHeightDIP)
	// 第 1 帧由 NewTester 完成首次布局，第 2 帧读出稳定 Bounds().H
	tt.Frame()

	targetH := int(math.Ceil(float64(measuredH)))
	if targetH < minExportHeightDIP {
		targetH = minExportHeightDIP
	}
	if targetH > cfg.MaxHeightDIP {
		targetH = cfg.MaxHeightDIP
	}

	// 2. 高清渲染阶段（精确尺寸 × 3x）
	tt.SetSize(widthDIP, targetH)
	tt.SetScale(cfg.Scale)
	tt.Frame()

	img := tt.Image()
	if img == nil {
		return nil, 0, 0, fmt.Errorf("sharecard: offscreen render returned nil image")
	}

	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, 0, 0, fmt.Errorf("sharecard: encode png: %w", err)
	}
	return buf.Bytes(), img.Bounds().Dx(), img.Bounds().Dy(), nil
}

// ExportPNGFile 将卡片渲染并原子落盘至 exportDir（建议为 dataDir/exports，受 Android FileProvider 覆盖），
// 同时自动清理早于最近 12 张的历史临时卡片，防止本地空间膨胀。
func ExportPNGFile(exportDir string, data CardData, opts Options, cfg RenderConfig) (ExportResult, error) {
	raw, w, h, err := RenderPNG(data, opts, cfg)
	if err != nil {
		return ExportResult{}, err
	}
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		return ExportResult{}, fmt.Errorf("sharecard: mkdir export dir: %w", err)
	}

	stamp := time.Now().Format("20060102_150405")
	name := fmt.Sprintf("stillpoets_%s_%s.png", data.Kind.FileSlug(), stamp)
	dst := filepath.Join(exportDir, name)
	// 若同秒多次导出，追加毫秒避免覆盖正在被系统分享读取的文件
	if _, statErr := os.Stat(dst); statErr == nil {
		name = fmt.Sprintf("stillpoets_%s_%s_%03d.png", data.Kind.FileSlug(), stamp, time.Now().Nanosecond()/1e6)
		dst = filepath.Join(exportDir, name)
	}

	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return ExportResult{}, fmt.Errorf("sharecard: write temp png: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return ExportResult{}, fmt.Errorf("sharecard: commit png: %w", err)
	}

	pruneOldExports(exportDir, maxExportKeepFiles)
	return ExportResult{
		FilePath: dst,
		WidthPx:  w,
		HeightPx: h,
		Bytes:    len(raw),
	}, nil
}

func pruneOldExports(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= keep {
		return
	}
	type fileItem struct {
		name    string
		modTime time.Time
	}
	items := make([]fileItem, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "stillpoets_") || !strings.HasSuffix(e.Name(), ".png") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, fileItem{name: e.Name(), modTime: info.ModTime()})
	}
	if len(items) <= keep {
		return
	}
	slices.SortFunc(items, func(a, b fileItem) int {
		if a.modTime.After(b.modTime) {
			return -1
		}
		if a.modTime.Before(b.modTime) {
			return 1
		}
		return strings.Compare(b.name, a.name)
	})
	for _, stale := range items[keep:] {
		_ = os.Remove(filepath.Join(dir, stale.name))
	}
}
