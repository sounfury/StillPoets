package theme

import "github.com/egoist/mygo/ui"

// PresetID 标识 StillPoets 内置主题（同时与原生桌面小组件共享配色 Token）。
type PresetID string

const (
	PresetArtbook     PresetID = "artbook"      // 画册风（默认）：近白 + 鼠尾草绿 + 赤陶 + 墨蓝
	PresetEditorial   PresetID = "editorial"    // 刊物风：清亮纸白 + 克莱因刊物蓝 + 酒红 + 橄榄绿
	PresetDarkMinimal PresetID = "dark_minimal" // 暗夜极简：深炭黑 + 柔赤陶
)

// HeroStyle 决定「今日重读」在诗集首页的排版形态。
type HeroStyle uint8

const (
	HeroCard      HeroStyle = iota // 画册风：鼠尾草绿矩形色块 + 右上赤陶几何角标
	HeroOpenPaper                  // 刊物风：无底色块，左侧刊物蓝细竖条 + 开放纸面大字
)

// CardSwatch 定义散页纸卡或抽象书封的一组和谐配色。
type CardSwatch struct {
	Bg       ui.Color
	Accent   ui.Color
	Text     ui.Color
	Muted    ui.Color
	Border   ui.Color
	IsFilled bool // true 表示深/彩底色卡，false 表示暖白细线卡
}

// Palette 定义应用与小组件共享的语义配色 Token。
type Palette struct {
	ID            PresetID
	Name          string
	Subtitle      string
	HeroStyle     HeroStyle
	Background    ui.Color
	ReaderPaper   ui.Color
	Surface       ui.Color
	SurfaceMuted  ui.Color
	Border        ui.Color
	TextPrimary   ui.Color
	TextSecondary ui.Color
	TextMuted     ui.Color
	Accent        ui.Color
	AccentText    ui.Color
	HeroBg        ui.Color
	HeroCorner    ui.Color
	HighlightBg   ui.Color
	QuoteSwatches []CardSwatch
	CoverSwatches []CardSwatch

	// 供原生桌面小组件序列化使用的十六进制色值
	BgHex     string
	TextHex   string
	AccentHex string
}

// Presets 返回内置预设主题集合。
func Presets() []Palette {
	return []Palette{
		Artbook(),
		Editorial(),
		DarkMinimal(),
	}
}

// ByID 按 PresetID 返回对应配色（兼容旧版本 "xuan_paper" 映射至默认画册风）。
func ByID(id PresetID) Palette {
	switch id {
	case PresetEditorial:
		return Editorial()
	case PresetDarkMinimal:
		return DarkMinimal()
	default:
		return Artbook()
	}
}

// UITheme 将当前语义色板映射为 mygo/ui 内置控件主题（按钮、输入框、弹窗、进度条自动同步配色）。
func (p Palette) UITheme() *ui.Theme {
	var t *ui.Theme
	if p.ID == PresetDarkMinimal {
		t = ui.DarkTheme()
	} else {
		t = ui.LightTheme()
	}
	t.Background = p.Background
	t.Surface = p.Surface
	t.SurfaceHover = p.SurfaceMuted
	t.Border = p.Border
	t.Text = p.TextPrimary
	t.TextMuted = p.TextMuted
	t.Accent = p.Accent
	t.AccentText = p.AccentText
	t.InactiveAccent = p.Accent
	t.Radius = 8
	return t
}

// Artbook 返回默认「画册风」主题（参考 stillpoets-v3-shelf-fragments.png）。
func Artbook() Palette {
	return Palette{
		ID:            PresetArtbook,
		Name:          "画册风",
		Subtitle:      "鼠尾草绿 · 赤陶 · 墨蓝",
		HeroStyle:     HeroCard,
		Background:    ui.Hex("#F4F4EF"),
		ReaderPaper:   ui.Hex("#F8F7F2"),
		Surface:       ui.Hex("#FAF9F5"),
		SurfaceMuted:  ui.Hex("#ECEBE4"),
		Border:        ui.Hex("#DFDDD5"),
		TextPrimary:   ui.Hex("#1A1A1A"),
		TextSecondary: ui.Hex("#4B4A45"),
		TextMuted:     ui.Hex("#7D7B74"),
		Accent:        ui.Hex("#BC5B46"),
		AccentText:    ui.Hex("#FFFFFF"),
		HeroBg:        ui.Hex("#CFD8C6"),
		HeroCorner:    ui.Hex("#BC5B46"),
		HighlightBg:   ui.Hex("#E3E9DE"),
		QuoteSwatches: []CardSwatch{
			{Bg: ui.Hex("#CFD8C6"), Accent: ui.Hex("#485741"), Text: ui.Hex("#1A1A1A"), Muted: ui.Hex("#4B5347"), Border: ui.Hex("#C2CCB8"), IsFilled: true},
			{Bg: ui.Hex("#BC5B46"), Accent: ui.Hex("#F4EAE3"), Text: ui.Hex("#FAF6F0"), Muted: ui.Hex("#F0D9D0"), Border: ui.Hex("#B0523D"), IsFilled: true},
			{Bg: ui.Hex("#334C68"), Accent: ui.Hex("#DCE5EE"), Text: ui.Hex("#F7F9FB"), Muted: ui.Hex("#C7D4E2"), Border: ui.Hex("#2B415A"), IsFilled: true},
			{Bg: ui.Hex("#FAF9F5"), Accent: ui.Hex("#334C68"), Text: ui.Hex("#1A1A1A"), Muted: ui.Hex("#7D7B74"), Border: ui.Hex("#DFDDD5"), IsFilled: false},
		},
		CoverSwatches: []CardSwatch{
			{Bg: ui.Hex("#BC5B46"), Accent: ui.Hex("#F2E8DC"), Text: ui.Hex("#FAF6F0"), Muted: ui.Hex("#F0D9D0"), Border: ui.Hex("#A84E3B"), IsFilled: true},
			{Bg: ui.Hex("#C9D3BE"), Accent: ui.Hex("#232524"), Text: ui.Hex("#1A1A1A"), Muted: ui.Hex("#464D42"), Border: ui.Hex("#BAC6AE"), IsFilled: true},
			{Bg: ui.Hex("#34557C"), Accent: ui.Hex("#EFECE3"), Text: ui.Hex("#FAF9F5"), Muted: ui.Hex("#CEDCEB"), Border: ui.Hex("#2B4869"), IsFilled: true},
			{Bg: ui.Hex("#C9A66B"), Accent: ui.Hex("#2A2A28"), Text: ui.Hex("#1E1D1A"), Muted: ui.Hex("#4F4738"), Border: ui.Hex("#B8955A"), IsFilled: true},
		},
		BgHex:     "#F4F4EF",
		TextHex:   "#1A1A1A",
		AccentHex: "#BC5B46",
	}
}

// Editorial 返回第二内置主题「刊物风」（参考 stillpoets-v3-editorial.png，中性清亮纸白底，无偏黄感）。
func Editorial() Palette {
	const paperHex = "#FFFFFF"
	paper := ui.Hex(paperHex)
	border := ui.Hex("#E5E5E5")
	return Palette{
		ID:            PresetEditorial,
		Name:          "刊物风",
		Subtitle:      "纸白 · 刊物蓝 · 酒红",
		HeroStyle:     HeroOpenPaper,
		Background:    paper,
		ReaderPaper:   paper,
		Surface:       paper,
		SurfaceMuted:  ui.Hex("#F2F2F2"),
		Border:        border,
		TextPrimary:   ui.Hex("#181818"),
		TextSecondary: ui.Hex("#4A4946"),
		TextMuted:     ui.Hex("#7B7A74"),
		Accent:        ui.Hex("#2345B5"),
		AccentText:    ui.Hex("#FFFFFF"),
		HeroBg:        paper,
		HeroCorner:    ui.Hex("#2345B5"),
		HighlightBg:   ui.Hex("#E8EEF9"),
		QuoteSwatches: []CardSwatch{
			{Bg: paper, Accent: ui.Hex("#2345B5"), Text: ui.Hex("#181818"), Muted: ui.Hex("#6F6E69"), Border: border, IsFilled: false},
			{Bg: ui.Hex("#752B36"), Accent: ui.Hex("#F3E5E7"), Text: ui.Hex("#FAF6F3"), Muted: ui.Hex("#E3C8CC"), Border: ui.Hex("#66242E"), IsFilled: true},
			{Bg: ui.Hex("#2345B5"), Accent: ui.Hex("#E3E9FA"), Text: ui.Hex("#F8FAFF"), Muted: ui.Hex("#C5D2F5"), Border: ui.Hex("#1C3A9E"), IsFilled: true},
			{Bg: ui.Hex("#FCFCFC"), Accent: ui.Hex("#2345B5"), Text: ui.Hex("#181818"), Muted: ui.Hex("#7B7A74"), Border: border, IsFilled: false},
		},
		CoverSwatches: []CardSwatch{
			{Bg: ui.Hex("#2345B5"), Accent: ui.Hex("#F2F1EC"), Text: ui.Hex("#FAFAF8"), Muted: ui.Hex("#CBD6F6"), Border: ui.Hex("#1D3999"), IsFilled: true},
			{Bg: ui.Hex("#752B36"), Accent: ui.Hex("#F2EFEA"), Text: ui.Hex("#FAF7F5"), Muted: ui.Hex("#E4C9CD"), Border: ui.Hex("#63222C"), IsFilled: true},
			{Bg: ui.Hex("#5B664E"), Accent: ui.Hex("#F2F1EC"), Text: ui.Hex("#F9F9F6"), Muted: ui.Hex("#D4DACD"), Border: ui.Hex("#4D5742"), IsFilled: true},
			{Bg: ui.Hex("#2C323B"), Accent: ui.Hex("#EFEFE9"), Text: ui.Hex("#F7F7F4"), Muted: ui.Hex("#C2C7CF"), Border: ui.Hex("#22272F"), IsFilled: true},
		},
		BgHex:     paperHex,
		TextHex:   "#181818",
		AccentHex: "#2345B5",
	}
}

// DarkMinimal 返回「暗夜极简」主题，适合夜间沉浸读诗。
func DarkMinimal() Palette {
	return Palette{
		ID:            PresetDarkMinimal,
		Name:          "暗夜极简",
		Subtitle:      "深炭 · 柔赤陶",
		HeroStyle:     HeroCard,
		Background:    ui.Hex("#151517"),
		ReaderPaper:   ui.Hex("#18181B"),
		Surface:       ui.Hex("#1F2024"),
		SurfaceMuted:  ui.Hex("#27282D"),
		Border:        ui.Hex("#2F3036"),
		TextPrimary:   ui.Hex("#ECE9E2"),
		TextSecondary: ui.Hex("#BCB8AE"),
		TextMuted:     ui.Hex("#8E8A81"),
		Accent:        ui.Hex("#D0735E"),
		AccentText:    ui.Hex("#FFFFFF"),
		HeroBg:        ui.Hex("#232B24"),
		HeroCorner:    ui.Hex("#B85F4B"),
		HighlightBg:   ui.Hex("#29332A"),
		QuoteSwatches: []CardSwatch{
			{Bg: ui.Hex("#242D26"), Accent: ui.Hex("#A8BA9F"), Text: ui.Hex("#ECE9E2"), Muted: ui.Hex("#A5AD9F"), Border: ui.Hex("#323D34"), IsFilled: true},
			{Bg: ui.Hex("#6B362C"), Accent: ui.Hex("#F1D7CE"), Text: ui.Hex("#F7EFEA"), Muted: ui.Hex("#DABAB0"), Border: ui.Hex("#7D4135"), IsFilled: true},
			{Bg: ui.Hex("#233345"), Accent: ui.Hex("#C5D4E4"), Text: ui.Hex("#EEF3F8"), Muted: ui.Hex("#A8BACE"), Border: ui.Hex("#2E4259"), IsFilled: true},
			{Bg: ui.Hex("#1F2024"), Accent: ui.Hex("#D0735E"), Text: ui.Hex("#ECE9E2"), Muted: ui.Hex("#9B978E"), Border: ui.Hex("#32333A"), IsFilled: false},
		},
		CoverSwatches: []CardSwatch{
			{Bg: ui.Hex("#7D3F32"), Accent: ui.Hex("#DFD5C8"), Text: ui.Hex("#F7F3EE"), Muted: ui.Hex("#DFC4BB"), Border: ui.Hex("#8E4A3C"), IsFilled: true},
			{Bg: ui.Hex("#344035"), Accent: ui.Hex("#191A19"), Text: ui.Hex("#ECEAE4"), Muted: ui.Hex("#B4BEB3"), Border: ui.Hex("#425043"), IsFilled: true},
			{Bg: ui.Hex("#283E57"), Accent: ui.Hex("#DEDAD0"), Text: ui.Hex("#F2F4F7"), Muted: ui.Hex("#B8C8D9"), Border: ui.Hex("#344E6B"), IsFilled: true},
			{Bg: ui.Hex("#78623F"), Accent: ui.Hex("#1E1D1B"), Text: ui.Hex("#F5F1E9"), Muted: ui.Hex("#D8C9B0"), Border: ui.Hex("#8A724B"), IsFilled: true},
		},
		BgHex:     "#151517",
		TextHex:   "#ECE9E2",
		AccentHex: "#D0735E",
	}
}
