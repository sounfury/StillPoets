package sharecard

import (
	"github.com/egoist/mygo/ui"

	"stillpoets/internal/ui/theme"
)

// CardPalette 定义分享卡片的纸面、墨色、边框、外框与热力图色阶。
type CardPalette struct {
	ID            CardThemeID
	Name          string
	SwatchDot     ui.Color // 供主题选择器展示的代表色点
	OuterBg       ui.Color // 画册模板外框衬底色
	OuterCorner   ui.Color // 画册模板右上装饰色块
	PaperBg       ui.Color // 卡片主体纸面色
	Surface       ui.Color // 卡片内嵌子块/指标卡底色
	Border        ui.Color // 细分割线与外框线
	BorderStrong  ui.Color // 双线框外线/强调细线
	TextPrimary   ui.Color
	TextSecondary ui.Color
	TextMuted     ui.Color
	Accent        ui.Color
	AccentSoft    ui.Color
	SealColor     ui.Color    // 古典朱文印章主色
	HeatmapColors [5]ui.Color // 0..4 级热力图色阶
	CoverSwatches []theme.CardSwatch
	IsDark        bool
	GrainOpacity  float32
}

// AllThemes 返回全部 5 套卡片纸面主题（3 套 App 原生主题 + 宣纸素笺 + 复古牛皮纸）。
func AllThemes() []CardPalette {
	return []CardPalette{
		PaletteByID(ThemeArtbook),
		PaletteByID(ThemeEditorial),
		PaletteByID(ThemeDarkMinimal),
		PaletteByID(ThemeXuanPaper),
		PaletteByID(ThemeVintageKraft),
	}
}

// PaletteByID 按 ID 返回对应的卡片纸面色板。
func PaletteByID(id CardThemeID) CardPalette {
	switch id {
	case ThemeEditorial:
		ed := theme.Editorial()
		return CardPalette{
			ID:            ThemeEditorial,
			Name:          "刊物纯白",
			SwatchDot:     ed.Accent,
			OuterBg:       ui.Hex("#ECEEF3"),
			OuterCorner:   ed.Accent,
			PaperBg:       ui.Hex("#FFFFFF"),
			Surface:       ui.Hex("#F7F8FA"),
			Border:        ui.Hex("#E3E4E8"),
			BorderStrong:  ui.Hex("#181818"),
			TextPrimary:   ed.TextPrimary,
			TextSecondary: ed.TextSecondary,
			TextMuted:     ed.TextMuted,
			Accent:        ed.Accent,
			AccentSoft:    ui.Hex("#E8EEF9"),
			SealColor:     ui.Hex("#752B36"),
			HeatmapColors: [5]ui.Color{
				ui.Hex("#EFEFEF"),
				ui.Hex("#D6DFF6"),
				ui.Hex("#99B0E6"),
				ui.Hex("#5677CE"),
				ui.Hex("#2345B5"),
			},
			CoverSwatches: ed.CoverSwatches,
			IsDark:        false,
			GrainOpacity:  0,
		}

	case ThemeDarkMinimal:
		dk := theme.DarkMinimal()
		return CardPalette{
			ID:            ThemeDarkMinimal,
			Name:          "暗夜极简",
			SwatchDot:     ui.Hex("#24262B"),
			OuterBg:       ui.Hex("#111214"),
			OuterCorner:   dk.Accent,
			PaperBg:       ui.Hex("#1A1B1E"),
			Surface:       ui.Hex("#222429"),
			Border:        ui.Hex("#303239"),
			BorderStrong:  ui.Hex("#4B4E57"),
			TextPrimary:   dk.TextPrimary,
			TextSecondary: dk.TextSecondary,
			TextMuted:     dk.TextMuted,
			Accent:        dk.Accent,
			AccentSoft:    ui.Hex("#2C2422"),
			SealColor:     ui.Hex("#C8644F"),
			HeatmapColors: [5]ui.Color{
				ui.Hex("#27282E"),
				ui.Hex("#4B312B"),
				ui.Hex("#774438"),
				ui.Hex("#A55A49"),
				ui.Hex("#D0735E"),
			},
			CoverSwatches: dk.CoverSwatches,
			IsDark:        true,
			GrainOpacity:  0.038,
		}

	case ThemeXuanPaper:
		art := theme.Artbook()
		return CardPalette{
			ID:            ThemeXuanPaper,
			Name:          "宣纸素笺",
			SwatchDot:     ui.Hex("#F3E9D2"),
			OuterBg:       ui.Hex("#E5DAC4"),
			OuterCorner:   ui.Hex("#A83B2C"),
			PaperBg:       ui.Hex("#F7F1E3"),
			Surface:       ui.Hex("#F0E8D6"),
			Border:        ui.Hex("#DDD1BA"),
			BorderStrong:  ui.Hex("#8B7B65"),
			TextPrimary:   ui.Hex("#261F19"),
			TextSecondary: ui.Hex("#54473B"),
			TextMuted:     ui.Hex("#877867"),
			Accent:        ui.Hex("#A83B2C"),
			AccentSoft:    ui.Hex("#EFE0D5"),
			SealColor:     ui.Hex("#A83B2C"),
			HeatmapColors: [5]ui.Color{
				ui.Hex("#EAE0CC"),
				ui.Hex("#DFC4B7"),
				ui.Hex("#CF9686"),
				ui.Hex("#BC6756"),
				ui.Hex("#A83B2C"),
			},
			CoverSwatches: art.CoverSwatches,
			IsDark:        false,
			GrainOpacity:  0.055,
		}

	case ThemeVintageKraft:
		art := theme.Artbook()
		return CardPalette{
			ID:            ThemeVintageKraft,
			Name:          "复古牛皮",
			SwatchDot:     ui.Hex("#D5C2A3"),
			OuterBg:       ui.Hex("#CBB592"),
			OuterCorner:   ui.Hex("#7E3F24"),
			PaperBg:       ui.Hex("#E9DEC9"),
			Surface:       ui.Hex("#E0D2B8"),
			Border:        ui.Hex("#CBBAA0"),
			BorderStrong:  ui.Hex("#6B533B"),
			TextPrimary:   ui.Hex("#2B2118"),
			TextSecondary: ui.Hex("#524130"),
			TextMuted:     ui.Hex("#7D6954"),
			Accent:        ui.Hex("#8C4B2D"),
			AccentSoft:    ui.Hex("#DECBB2"),
			SealColor:     ui.Hex("#8C3B28"),
			HeatmapColors: [5]ui.Color{
				ui.Hex("#DDD0B9"),
				ui.Hex("#CDB497"),
				ui.Hex("#B8916E"),
				ui.Hex("#A16D4A"),
				ui.Hex("#8C4B2D"),
			},
			CoverSwatches: art.CoverSwatches,
			IsDark:        false,
			GrainOpacity:  0.062,
		}

	default: // ThemeArtbook
		art := theme.Artbook()
		return CardPalette{
			ID:            ThemeArtbook,
			Name:          "画册暖白",
			SwatchDot:     ui.Hex("#CFD8C6"),
			OuterBg:       art.HeroBg,
			OuterCorner:   art.HeroCorner,
			PaperBg:       art.ReaderPaper,
			Surface:       ui.Hex("#F1F0E9"),
			Border:        art.Border,
			BorderStrong:  ui.Hex("#B9B5A8"),
			TextPrimary:   art.TextPrimary,
			TextSecondary: art.TextSecondary,
			TextMuted:     art.TextMuted,
			Accent:        art.Accent,
			AccentSoft:    art.HighlightBg,
			SealColor:     ui.Hex("#B24A38"),
			HeatmapColors: [5]ui.Color{
				ui.Hex("#E9E8E1"),
				ui.Hex("#D4DFCD"),
				ui.Hex("#A9BF9E"),
				ui.Hex("#7D9C73"),
				ui.Hex("#4F6D4A"),
			},
			CoverSwatches: art.CoverSwatches,
			IsDark:        false,
			GrainOpacity:  0.044,
		}
	}
}
