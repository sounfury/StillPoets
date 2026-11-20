package domain

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// LocalBookState 表示某本诗集在当前设备上的本地正文缓存状态。
type LocalBookState string

const (
	LocalStateRemote      LocalBookState = "remote"      // 仅同步了元数据，正文在云端未下载
	LocalStateDownloading LocalBookState = "downloading" // 正在按需下载正文包
	LocalStateDownloaded  LocalBookState = "downloaded"  // 正文已落库，可离线阅读
	LocalStateStale       LocalBookState = "stale"       // 远端正文包哈希已更新，需重新拉取
)

// BookReadStatus 表示诗集的整体阅读/完读状态。
type BookReadStatus string

const (
	ReadStatusUnread   BookReadStatus = "unread"
	ReadStatusReading  BookReadStatus = "reading"
	ReadStatusFinished BookReadStatus = "finished"
)

// Book 表示入口 A（诗集体系）的元数据记录，不含诗篇正文。
type Book struct {
	ID             string         `json:"id"`
	Title          string         `json:"title"`
	Author         string         `json:"author"`
	DynastyOrGenre string         `json:"dynastyOrGenre,omitempty"`
	Category       string         `json:"category,omitempty"`
	IsStarred      bool           `json:"isStarred"`
	SortOrder      int            `json:"sortOrder"`
	CoverHash      string         `json:"coverHash,omitempty"`
	ContentHash    string         `json:"contentHash"`          // parsed.spbook.json.gz 的 SHA-256
	ContentSize    int64          `json:"contentSize"`          // parsed.spbook.json.gz 字节大小
	RawFilename    string         `json:"rawFilename"`          // 远端原始文件名，如 海子诗全集_a1b2c3.epub
	RawSize        int64          `json:"rawSize,omitempty"`    // 原始文件字节大小
	PoemCount      int            `json:"poemCount"`            // 总篇目数
	LocalState     LocalBookState `json:"localState,omitempty"` // 仅本地使用，不写入远端 meta/library.json
	CreatedAt      int64          `json:"createdAt"`
	UpdatedAt      int64          `json:"updatedAt"`
	DeletedAt      int64          `json:"deletedAt,omitempty"`
	DeviceID       string         `json:"deviceId,omitempty"`
}

// SPBook 是标准化解析后的诗集正文包（对应 books/<bookId>/parsed.spbook.json.gz）。
// 多端通过下载该标准化文件保证 BookID/PoemID/Stanza/Line 索引严格一致。
type SPBook struct {
	SchemaVersion int    `json:"schemaVersion"`
	BookID        string `json:"bookId"`
	Title         string `json:"title"`
	Author        string `json:"author"`
	SourceFormat  string `json:"sourceFormat"` // epub | md | json
	ParsedAt      int64  `json:"parsedAt"`
	Poems         []Poem `json:"poems"`
}

// Poem 表示诗集中的一首完整诗歌。
type Poem struct {
	ID         string   `json:"id"`
	BookID     string   `json:"bookId"`
	OrderIndex int      `json:"orderIndex"`
	Title      string   `json:"title"`
	Subtitle   string   `json:"subtitle,omitempty"`
	Author     string   `json:"author,omitempty"` // 多人合集时覆盖 Book.Author
	Stanzas    []Stanza `json:"stanzas"`
}

// Stanza 表示诗歌中的一个诗节，由有序诗行组成。
type Stanza struct {
	Lines []string `json:"lines"`
}

// FavoritePoem 表示诗集内“精选集”收藏记录（诗篇级小红心 ❤️）。
// 冗余轻量 Excerpt 与 PoemTitle，保证未下载诗集时小组件与预览列表也可正常展示。
type FavoritePoem struct {
	ID             string `json:"id"`
	BookID         string `json:"bookId"`
	PoemID         string `json:"poemId"`
	PoemTitle      string `json:"poemTitle"`
	Author         string `json:"author,omitempty"`
	Excerpt        string `json:"excerpt"`                  // 默认首节 2~4 行，或用户手动选定的本诗金句
	HighlightLines string `json:"highlightLines,omitempty"` // 高亮行范围，如 "4-4" 或 "2-5"
	IsCustomQuote  bool   `json:"isCustomQuote,omitempty"`  // 是否为用户手动划选的本诗金句
	CreatedAt      int64  `json:"createdAt"`
	UpdatedAt      int64  `json:"updatedAt"`
	DeletedAt      int64  `json:"deletedAt,omitempty"`
	DeviceID       string `json:"deviceId,omitempty"`
}

// Quote 表示入口 B 的独立句子/摘录记录（散装段落，不归属于诗集）。
type Quote struct {
	ID           string   `json:"id"`
	Content      string   `json:"content"`
	Author       string   `json:"author,omitempty"`
	SourceBook   string   `json:"sourceBook,omitempty"`
	ImportSource string   `json:"importSource,omitempty"` // reeden | weread | json | txt | manual
	Tags         []string `json:"tags,omitempty"`
	IsFavorite   bool     `json:"isFavorite"`
	CreatedAt    int64    `json:"createdAt"`
	UpdatedAt    int64    `json:"updatedAt"`
	DeletedAt    int64    `json:"deletedAt,omitempty"`
	DeviceID     string   `json:"deviceId,omitempty"`
}

// Annotation 表示单诗微批注或诗集整体长书评（V1.1 启用，Phase 0 先固化契约）。
type Annotation struct {
	ID         string `json:"id"`
	TargetType string `json:"targetType"` // poem | book
	BookID     string `json:"bookId"`
	PoemID     string `json:"poemId,omitempty"`
	Body       string `json:"body"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
	DeletedAt  int64  `json:"deletedAt,omitempty"`
	DeviceID   string `json:"deviceId,omitempty"`
}

// ReadingProgress 表示单本诗集的阅读进度与完读标记。
type ReadingProgress struct {
	BookID        string         `json:"bookId"`
	LastPoemID    string         `json:"lastPoemId,omitempty"`
	ScrollRatio   float64        `json:"scrollRatio"`
	ReadPoemCount int            `json:"readPoemCount"`
	Status        BookReadStatus `json:"status"`
	FinishedAt    int64          `json:"finishedAt,omitempty"`
	UpdatedAt     int64          `json:"updatedAt"`
	DeviceID      string         `json:"deviceId,omitempty"`
}

// ReadingEvent 表示按“某诗集的某首诗”记录的有效读诗事件（V1.1 统计热力图/时段占比/作者云使用）。
type ReadingEvent struct {
	ID        string `json:"id"`
	BookID    string `json:"bookId"`
	PoemID    string `json:"poemId"`
	PoemTitle string `json:"poemTitle"`
	Author    string `json:"author,omitempty"`
	ReadDate  string `json:"readDate"` // 本地日期 YYYY-MM-DD
	ReadHour  int    `json:"readHour"` // 本地小时 0-23
	ReadAt    int64  `json:"readAt"`   // UTC 毫秒时间戳
	IsReread  bool   `json:"isReread,omitempty"`
	DeviceID  string `json:"deviceId,omitempty"`
}

// WidgetSourceMode 定义桌面小组件的内容抽选模式。
type WidgetSourceMode string

const (
	WidgetModeGlobalRandom   WidgetSourceMode = "global_random"   // 全局所有诗集随机
	WidgetModeBookRandom     WidgetSourceMode = "book_random"     // 固定某诗集随机
	WidgetModeBookSequential WidgetSourceMode = "book_sequential" // 固定某诗集顺序
	WidgetModeBookFavorites  WidgetSourceMode = "book_favorites"  // 固定某诗集精选池 ❤️ 随机
	WidgetModeQuotesRandom   WidgetSourceMode = "quotes_random"   // 入口 B 独立句子库随机
)

// WidgetThemeConfig 定义小组件视觉主题，与原生 WidgetKit / Glance 共享。
type WidgetThemeConfig struct {
	Preset      string `json:"preset"`               // xuan_paper | dark_minimal | frosted_glass
	FontFamily  string `json:"fontFamily,omitempty"` // 内置或已注册的自定义字体名
	FontFile    string `json:"fontFile,omitempty"`   // 对应 assets/fonts/ 下的字体文件名
	BgColor     string `json:"bgColor"`
	TextColor   string `json:"textColor"`
	AccentColor string `json:"accentColor"`
}

// Settings 保存跨端同步的非敏感应用偏好（严禁包含 WebDAV 密码）。
type Settings struct {
	ThemePreset  string            `json:"themePreset"`
	FontFamily   string            `json:"fontFamily"`
	FontSize     float32           `json:"fontSize"`
	LineHeight   float32           `json:"lineHeight"`
	WidgetMode   WidgetSourceMode  `json:"widgetMode"`
	WidgetBookID string            `json:"widgetBookId,omitempty"`
	WidgetTheme  WidgetThemeConfig `json:"widgetTheme"`
	UpdatedAt    int64             `json:"updatedAt"`
	DeviceID     string            `json:"deviceId,omitempty"`
}

// WidgetTimeline 是主 App 预编译写入共享容器、供原生小组件直接消费的快照协议。
type WidgetTimeline struct {
	GeneratedAt int64             `json:"generatedAt"`
	Theme       WidgetThemeConfig `json:"theme"`
	Entries     []WidgetEntry     `json:"entries"`
}

// WidgetEntry 表示单条小组件展示条目。
type WidgetEntry struct {
	EntryType   string `json:"entryType"` // poem | quote
	DisplayText string `json:"displayText"`
	PoemTitle   string `json:"poemTitle,omitempty"`
	Author      string `json:"author,omitempty"`
	SourceTitle string `json:"sourceTitle,omitempty"` // 诗集名或独立句子的原书出处
	Deeplink    string `json:"deeplink"`              // stillpoets://poem/<bookId>/<poemId>?lines=4-4
}

// NowMillis 返回当前 UTC 毫秒时间戳。
func NowMillis() int64 {
	return time.Now().UTC().UnixMilli()
}

// NewID 生成时间有序且全局唯一的轻量实体 ID（毫秒时间戳十六进制 + 随机后缀）。
func NewID() string {
	var buf [6]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("%012x%s", time.Now().UTC().UnixMilli(), hex.EncodeToString(buf[:]))
}

// NewShortRandID 生成 6 位十六进制随机短 ID，用于原始文件名防重名后缀。
func NewShortRandID() string {
	var buf [3]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

// BuildRawFilename 清洗原始导入文件名并拼接短随机 ID，既保留可读性又防止同名文件覆盖。
// 例如："海子诗全集.epub" -> "海子诗全集_a1b2c3.epub"。
func BuildRawFilename(originalName, shortID string) string {
	trimmed := strings.TrimSpace(originalName)
	var base, ext, stem string
	if trimmed != "" {
		base = filepath.Base(trimmed)
		if base != "." && base != ".." && base != string(filepath.Separator) {
			rawExt := filepath.Ext(base)
			ext = strings.ToLower(rawExt)
			stem = strings.TrimSuffix(base, rawExt)
		}
	}

	// 清洗跨平台与 WebDAV 非法字符，保留中文、字母、数字、连字符与空格
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_",
		"?", "_", "\"", "_", "<", "_", ">", "_",
		"|", "_", "\n", " ", "\r", " ", "\t", " ",
	)
	stem = strings.TrimSpace(replacer.Replace(stem))
	stem = strings.Trim(stem, ".")
	if stem == "" {
		stem = "poems"
	}

	// 截断过长文件名（按 rune 计数避免截断 UTF-8 多字节中文字符）
	const maxStemRunes = 48
	if utf8.RuneCountInString(stem) > maxStemRunes {
		runes := []rune(stem)
		stem = strings.TrimSpace(string(runes[:maxStemRunes]))
	}

	if shortID == "" {
		shortID = NewShortRandID()
	}
	if ext == "" || ext == "." {
		ext = ".epub"
	}
	return fmt.Sprintf("%s_%s%s", stem, shortID, ext)
}
