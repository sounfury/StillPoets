package theme

import "strings"

// 语义字体栈：针对现代诗阅读与出版物排版区分 5 类字形角色，
// 兼顾 iOS (CoreText cascadeList)、Android (Typeface 通用族回退) 与 Linux (Pango/Fontconfig)。
const (
	// FontBrand 用于顶部品牌字标「StillPoets」与平板左侧「S」徽标（经典出版衬线）。
	FontBrand = "Baskerville, Georgia, Palatino, Noto Serif CJK SC, DejaVu Serif, serif"

	// FontSerif 用于诗句正文、书封书名、区块标题与统计主数字（人文宋体 / 文学衬线）。
	FontSerif = "Georgia, Songti SC, STSong, Hiragino Mincho ProN, Noto Serif CJK SC, DejaVu Serif, serif"

	// FontNote 用于诗人署名、副标题、题记、落款与书籍出处（温和楷韵 / 衬线注记）。
	FontNote = "Kaiti SC, STKaiti, Georgia, Songti SC, Hiragino Mincho ProN, Noto Serif CJK SC, serif"

	// FontSans 用于导航、Tab、筛选胶囊、按钮与设置说明（清晰克制的界面无衬线）。
	FontSans = "system-ui, PingFang SC, Noto Sans CJK SC, DejaVu Sans, sans-serif"

	// FontMono 用于页码计数、进度百分比、角落微标与注释序号（等宽版次细标）。
	FontMono = "SF Mono, Menlo, Noto Sans Mono CJK SC, DejaVu Sans Mono, monospace"
)

// PoetryFont 返回诗歌与摘录正文的字体栈；若用户已选择自定义导入字体则优先使用，并回退至文学衬线体。
func PoetryFont(userFamily string) string {
	f := strings.TrimSpace(userFamily)
	if f == "" {
		return FontSerif
	}
	return f + ", " + FontSerif
}
