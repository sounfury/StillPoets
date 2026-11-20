package theme

import "github.com/egoist/mygo/ui"

// PresetID 标识 V1.0 内置的三套精选诗意主题（同时与原生小组件共享配色 Token）。
type PresetID string

const (
	PresetXuanPaper    PresetID = "xuan_paper"    // 水墨宣纸
	PresetDarkMinimal  PresetID = "dark_minimal"  // 暗黑极简
	PresetFrostedGlass PresetID = "frosted_glass" // 透明磨砂
)

// Palette 定义应用与小组件共享的语义配色。
type Palette struct {
	ID          PresetID
	Name        string
	Background  ui.Color
	Surface     ui.Color
	Border      ui.Color
	TextPrimary ui.Color
	TextMuted   ui.Color
	Accent      ui.Color // 印章朱红 / 高亮色
	BgHex       string
	TextHex     string
	AccentHex   string
}

// Presets 返回 V1.0 预设主题集合。
func Presets() []Palette {
	return []Palette{
		XuanPaper(),
		DarkMinimal(),
		FrostedGlass(),
	}
}

// XuanPaper 返回默认“水墨宣纸”主题。
func XuanPaper() Palette {
	return Palette{
		ID:          PresetXuanPaper,
		Name:        "水墨宣纸",
		Background:  ui.Hex("#F7F3E9"),
		Surface:     ui.Hex("#FFFDF8"),
		Border:      ui.Hex("#E6DEC8"),
		TextPrimary: ui.Hex("#2C2824"),
		TextMuted:   ui.Hex("#7A7063"),
		Accent:      ui.Hex("#9B3B2B"),
		BgHex:       "#F7F3E9",
		TextHex:     "#2C2824",
		AccentHex:   "#9B3B2B",
	}
}

// DarkMinimal 返回“暗黑极简”主题。
func DarkMinimal() Palette {
	return Palette{
		ID:          PresetDarkMinimal,
		Name:        "暗黑极简",
		Background:  ui.Hex("#151517"),
		Surface:     ui.Hex("#1F1F23"),
		Border:      ui.Hex("#2E2E34"),
		TextPrimary: ui.Hex("#ECE9E2"),
		TextMuted:   ui.Hex("#9A958C"),
		Accent:      ui.Hex("#C86D5B"),
		BgHex:       "#151517",
		TextHex:     "#ECE9E2",
		AccentHex:   "#C86D5B",
	}
}

// FrostedGlass 返回“磨砂素笺”主题。
func FrostedGlass() Palette {
	return Palette{
		ID:          PresetFrostedGlass,
		Name:        "磨砂素笺",
		Background:  ui.Hex("#EEF1F4"),
		Surface:     ui.RGBA(255, 255, 255, 0.78),
		Border:      ui.RGBA(120, 130, 145, 0.22),
		TextPrimary: ui.Hex("#1F242B"),
		TextMuted:   ui.Hex("#64707D"),
		Accent:      ui.Hex("#4B6B7C"),
		BgHex:       "#EEF1F4",
		TextHex:     "#1F242B",
		AccentHex:   "#4B6B7C",
	}
}
