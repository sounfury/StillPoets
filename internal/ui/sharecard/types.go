// Package sharecard 提供 StillPoets 高清诗歌分享卡片的所见即所得排版视图与 3x 离屏 PNG 导出引擎。
//
// 预览弹窗与离线导出复用同一套 ui.Widget 渲染树（RenderCard），
// 天然共享已注册的自定义 .ttf/.otf 字体、棉纸肌理与矢量绘制能力。
package sharecard

import (
	"time"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/domain"
	"stillpoets/internal/storage"
	"stillpoets/internal/ui/theme"
)

// CardKind 标识导出卡片的内容形态（对齐 PRD 第八章与 9.1 / 9.5）。
type CardKind string

const (
	KindQuote       CardKind = "quote"       // 单句卡片（散页独立句子 / 划选诗歌节选 / 本诗金句）
	KindPoem        CardKind = "poem"        // 整诗长图（完整诗题、副标题、题记、诗节与落款）
	KindCollection  CardKind = "collection"  // 诗集精选合辑长图
	KindFinished    CardKind = "finished"    // 完读纪念卡
	KindStatsReport CardKind = "statsreport" // 周期读诗报告长图
)

// Label 返回卡片类型的中文展示名。
func (k CardKind) Label() string {
	switch k {
	case KindPoem:
		return "整诗长图"
	case KindCollection:
		return "精选合辑"
	case KindFinished:
		return "完读纪念卡"
	case KindStatsReport:
		return "读诗报告"
	default:
		return "单句诗笺"
	}
}

// FileSlug 返回用于导出文件名的简短英文标识。
func (k CardKind) FileSlug() string {
	switch k {
	case KindPoem:
		return "poem"
	case KindCollection:
		return "collection"
	case KindFinished:
		return "finished"
	case KindStatsReport:
		return "report"
	default:
		return "quote"
	}
}

// TemplateID 标识 5 套分享卡片排版模板。
type TemplateID string

const (
	TemplateMinimal   TemplateID = "minimal"   // 极简纸笺（默认）
	TemplateSeal      TemplateID = "seal"      // 古典印章（双线书框 + 矢量朱文印）
	TemplateEditorial TemplateID = "editorial" // 刊物版式（刊头编号 + 左侧竖线引文）
	TemplateCalendar  TemplateID = "calendar"  // 日历诗笺（大号月/日/星期日历头）
	TemplateArtbook   TemplateID = "artbook"   // 画册留白（主题色外框衬底 + 内嵌棉纸卡）
)

// TemplateMeta 描述模板在选择器中的展示信息。
type TemplateMeta struct {
	ID   TemplateID
	Name string
}

// Templates 返回全部 5 套可选模板（按展示顺序）。
func Templates() []TemplateMeta {
	return []TemplateMeta{
		{ID: TemplateMinimal, Name: "极简纸笺"},
		{ID: TemplateSeal, Name: "古典印章"},
		{ID: TemplateEditorial, Name: "刊物版式"},
		{ID: TemplateCalendar, Name: "日历诗笺"},
		{ID: TemplateArtbook, Name: "画册留白"},
	}
}

// CardThemeID 标识 5 套卡片纸面配色（打通现有 3 套 App 主题 + 2 套传统纸笺色板）。
type CardThemeID string

const (
	ThemeArtbook      CardThemeID = "artbook"       // 画册暖白
	ThemeEditorial    CardThemeID = "editorial"     // 刊物纯白
	ThemeDarkMinimal  CardThemeID = "dark_minimal"  // 暗夜极简
	ThemeXuanPaper    CardThemeID = "xuan_paper"    // 宣纸素笺
	ThemeVintageKraft CardThemeID = "vintage_kraft" // 复古牛皮纸
)

// ThemeFromAppPreset 将当前 App 主题映射为对应的卡片纸面配色，保证视觉一致性。
func ThemeFromAppPreset(preset theme.PresetID) CardThemeID {
	switch preset {
	case theme.PresetEditorial:
		return ThemeEditorial
	case theme.PresetDarkMinimal:
		return ThemeDarkMinimal
	default:
		return ThemeArtbook
	}
}

// CollectionItem 表示诗集精选合辑长图中的单条精选诗篇摘要。
type CollectionItem struct {
	PoemTitle string
	Author    string
	Excerpt   string
}

// CardData 承载生成任意类型分享卡片所需的纯值数据（无数据库或服务层依赖）。
type CardData struct {
	Kind       CardKind
	Date       time.Time
	FontFamily string
	FontWeight int

	// 1. 单句卡片字段
	QuoteText string
	QuoteTag  string // 如「独立散句」「诗歌节选」「本诗金句」

	// 2. 通用出处 / 整诗字段
	BookID       string
	BookTitle    string
	Author       string
	SourceBook   string
	PoemTitle    string
	PoemSubtitle string
	PoemSection  string
	Epigraph     string
	Stanzas      []domain.Stanza
	Colophon     string

	// 3. 诗集精选合辑 & 完读纪念卡字段
	CoverImg        ui.ImageSource
	FinishedAt      int64
	ReadPoemCount   int
	TotalPoemCount  int
	FavoriteCount   int
	QuoteCount      int
	CollectionItems []CollectionItem

	// 4. 周期读诗报告字段
	ReportPeriodLabel string
	ReportRangeNote   string
	Overview          storage.WindowOverview
	Heatmap           storage.HeatmapData
	Rhythm            storage.ReadingRhythm
	TopAuthors        []storage.AuthorStat
	FinishedTitles    []string
}

// EffectiveDate 返回用于日历模板与报告落款的日期（零值时取当前时间）。
func (d CardData) EffectiveDate() time.Time {
	if !d.Date.IsZero() {
		return d.Date
	}
	return time.Now()
}

// EffectiveFontWeight 返回规范化后的诗歌正文字重。
func (d CardData) EffectiveFontWeight() int {
	if d.FontWeight >= 200 && d.FontWeight <= 800 {
		return d.FontWeight
	}
	return 400
}

// Options 描述用户在预览弹窗中实时调节的样式选项。
type Options struct {
	Template      TemplateID
	Theme         CardThemeID
	ShowWatermark bool
}

// DefaultOptions 返回默认导出样式（极简纸笺 + 继承当前 App 主题 + 显示极轻角标）。
func DefaultOptions(appPreset theme.PresetID) Options {
	return Options{
		Template:      TemplateMinimal,
		Theme:         ThemeFromAppPreset(appPreset),
		ShowWatermark: true,
	}
}

// RenderConfig 控制离屏 PNG 导出的逻辑宽、缩放倍率与安全高度上限。
type RenderConfig struct {
	WidthDIP     int     // 默认 360 DIP（3x 下为 1080px 高清宽）
	Scale        float32 // 默认 3.0
	MaxHeightDIP int     // 默认 6400 DIP（防止极端长文撑爆内存）
}

// DefaultRenderConfig 返回默认 1080px 宽（360 DIP × 3x）的高清导出配置。
func DefaultRenderConfig() RenderConfig {
	return RenderConfig{
		WidthDIP:     360,
		Scale:        3.0,
		MaxHeightDIP: 6400,
	}
}

// ExportResult 记录一次高清 PNG 文件导出的元信息。
type ExportResult struct {
	FilePath string
	WidthPx  int
	HeightPx int
	Bytes    int
}
