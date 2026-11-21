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

// Version 是用于跨端确定性冲突裁决的唯一全序逻辑版本。
// 比较规则：先比较 Lamport Counter，再按 InstallID 字典序决胜，彻底摆脱设备墙钟漂移问题。
type Version struct {
	Counter   uint64 `json:"c"`
	InstallID string `json:"i"`
}

// IsZero 返回版本号是否为零值。
func (v Version) IsZero() bool {
	return v.Counter == 0 && v.InstallID == ""
}

// Compare 比较两个版本的全序关系：v > other 返回 1，相等返回 0，v < other 返回 -1。
func (v Version) Compare(other Version) int {
	if v.Counter > other.Counter {
		return 1
	}
	if v.Counter < other.Counter {
		return -1
	}
	return strings.Compare(v.InstallID, other.InstallID)
}

// After 返回 v 是否严格大于 other。
func (v Version) After(other Version) bool {
	return v.Compare(other) > 0
}

// MaxCounterOf 返回多个版本号与初始值中的最大 Lamport 计数器值。
func MaxCounterOf(base uint64, versions ...Version) uint64 {
	m := base
	for _, v := range versions {
		if v.Counter > m {
			m = v.Counter
		}
	}
	return m
}

// LocalBookState 表示某本诗集在当前设备上的本地正文缓存状态（仅本机使用，不参与同步）。
type LocalBookState string

const (
	LocalStateRemote      LocalBookState = "remote"      // 仅同步了元数据，正文在云端未下载
	LocalStateDownloading LocalBookState = "downloading" // 正在按需下载正文包
	LocalStateDownloaded  LocalBookState = "downloaded"  // 正文已落库，可离线阅读
	LocalStateStale       LocalBookState = "stale"       // 远端正文包哈希已更新，需重新拉取
)

// DeriveLocalBookState 由本机实际已下载的正文包哈希与合并后远端 ContentHash 动态推导本地状态。
// 该状态严格置于合并器之外，保证元数据合并满足交换律与结合律。
func DeriveLocalBookState(book Book, downloadedContentHash string) LocalBookState {
	if downloadedContentHash == "" {
		return LocalStateRemote
	}
	if book.ContentHash != "" && downloadedContentHash != book.ContentHash {
		return LocalStateStale
	}
	return LocalStateDownloaded
}

// BookReadStatus 表示诗集的整体阅读/完读状态。
type BookReadStatus string

const (
	ReadStatusUnread   BookReadStatus = "unread"
	ReadStatusReading  BookReadStatus = "reading"
	ReadStatusFinished BookReadStatus = "finished"
)

// BookGroup 表示用户自定义诗集组（支持新建空组、重命名、排序、解散）。
// 删除对同一 GroupID 永久生效（吸收状态）；组被解散后，原引用该组的诗集自动归入默认「全部诗集」。
type BookGroup struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	NameVer    Version `json:"nameVer"`
	SortOrder  int     `json:"sortOrder"`
	SortVer    Version `json:"sortVer"`
	CreatedAt  int64   `json:"createdAt"`
	Deleted    bool    `json:"deleted,omitempty"`
	DeletedAt  int64   `json:"deletedAt,omitempty"`
	DeletedVer Version `json:"deletedVer,omitempty"`
}

// Book 表示入口 A（诗集体系）的元数据记录，不含诗篇正文。
// 可独立编辑字段（GroupID / IsStarred / SortOrder）与内容描述字段各自带逻辑版本，
// 避免多端无冲突修改互相覆盖；删除对同一 BookID 永久生效（吸收状态）。
type Book struct {
	ID string `json:"id"`

	// 导入内容描述组（由 ContentVer 统一控制）
	Title          string  `json:"title"`
	Author         string  `json:"author"`
	DynastyOrGenre string  `json:"dynastyOrGenre,omitempty"`
	CoverHash      string  `json:"coverHash,omitempty"`
	ContentHash    string  `json:"contentHash"`       // parsed.spbook.json.gz 的 SHA-256
	ContentSize    int64   `json:"contentSize"`       // parsed.spbook.json.gz 字节大小
	RawFilename    string  `json:"rawFilename"`       // 远端原始文件名，如 海子诗全集_a1b2c3.epub
	RawSize        int64   `json:"rawSize,omitempty"` // 原始文件字节大小
	PoemCount      int     `json:"poemCount"`         // 总篇目数
	ContentVer     Version `json:"contentVer"`

	// 可独立编辑的书架属性
	GroupID    string  `json:"groupId,omitempty"` // 空串表示默认「全部诗集」组
	GroupVer   Version `json:"groupVer,omitempty"`
	IsStarred  bool    `json:"isStarred"`
	StarredVer Version `json:"starredVer,omitempty"`
	SortOrder  int     `json:"sortOrder"`
	SortVer    Version `json:"sortVer,omitempty"`

	// 生命周期与永久软删除墓碑
	CreatedAt  int64   `json:"createdAt"`
	Deleted    bool    `json:"deleted,omitempty"`
	DeletedAt  int64   `json:"deletedAt,omitempty"`
	DeletedVer Version `json:"deletedVer,omitempty"`

	// 仅本地使用，显式排除在线上快照 JSON 之外
	LocalState       LocalBookState `json:"-"`
	LocalContentHash string         `json:"-"`
}

// EntryKind 区分诗集目录中的正式诗篇与非诗附文（译序、导读、后记、年表等）。
type EntryKind string

const (
	EntryKindPoem  EntryKind = "poem"  // 正式诗篇：计入 PoemCount、完读进度与小组件候选池
	EntryKindProse EntryKind = "prose" // 附文散文：保留阅读与目录，默认不计入完读与小组件池
)

// Note 表示归属到单篇诗或附文的注释（脚注、尾注、诗末注等）。
type Note struct {
	ID    string `json:"id"`
	Label string `json:"label,omitempty"` // 如 "[1]"、"①" 或 "注"
	Body  string `json:"body"`
}

// ImageRef 预留正文插图元数据引用（V1.0 仅记录占位，不打包图片或渲染；V1.1 启用）。
type ImageRef struct {
	Src     string `json:"src"`
	Alt     string `json:"alt,omitempty"`
	Caption string `json:"caption,omitempty"`
	Width   int    `json:"width,omitempty"`
	Height  int    `json:"height,omitempty"`
}

// SPBook 是标准化解析后的诗集正文包（对应 books/<bookId>/parsed.spbook.json.gz）。
// 多端通过下载该标准化文件保证 BookID/PoemID/Stanza/Line 索引严格一致。
type SPBook struct {
	SchemaVersion int    `json:"schemaVersion"`
	BookID        string `json:"bookId"`
	Title         string `json:"title"`
	Author        string `json:"author"`
	Translator    string `json:"translator,omitempty"`
	SourceFormat  string `json:"sourceFormat"` // epub | md | json
	ParsedAt      int64  `json:"parsedAt"`
	Poems         []Poem `json:"poems"`
}

// CountPoems 返回该诗集中正式诗篇（Kind == poem）的数量，排除译序/后记等附文。
func (b SPBook) CountPoems() int {
	n := 0
	for _, p := range b.Poems {
		if p.IsPoem() {
			n++
		}
	}
	return n
}

// Poem 表示诗集中的一篇独立条目（正式诗篇或译序/后记等附文）。
type Poem struct {
	ID         string     `json:"id"`
	BookID     string     `json:"bookId"`
	OrderIndex int        `json:"orderIndex"`
	Kind       EntryKind  `json:"kind,omitempty"`     // poem | prose（空值兼容视为 poem）
	Section    string     `json:"section,omitempty"`  // 所属分辑/卷名，如 "第一辑 · 抒情诗"
	Title      string     `json:"title"`
	Subtitle   string     `json:"subtitle,omitempty"`
	Author     string     `json:"author,omitempty"`   // 多人合集时覆盖 Book.Author
	Epigraph   string     `json:"epigraph,omitempty"` // 诗前题记/题辞，不混入首节
	Stanzas    []Stanza   `json:"stanzas"`            // poem 为诗节/诗行；prose 为散文段落
	Colophon   string     `json:"colophon,omitempty"` // 诗末创作日期/地点落款，不进小组件抽选
	Notes      []Note     `json:"notes,omitempty"`    // 内联到本篇的脚注/尾注/诗末注
	Images     []ImageRef `json:"images,omitempty"`   // V1.0 仅预留记录，暂不渲染
}

// EffectiveKind 返回归一化后的条目类型（空值默认为 poem）。
func (p Poem) EffectiveKind() EntryKind {
	if p.Kind == "" {
		return EntryKindPoem
	}
	return p.Kind
}

// IsPoem 返回该条目是否为正式诗篇（参与完读进度、诗篇计数与小组件抽选）。
func (p Poem) IsPoem() bool {
	return p.EffectiveKind() == EntryKindPoem
}

// Stanza 表示诗歌中的一个诗节，由有序诗行组成。
type Stanza struct {
	Lines []string `json:"lines"`
}

// FavoritePoem 表示诗集内“精选集”收藏记录（诗篇级小红心 ❤️），跨端合并主键为 (BookID, PoemID)。
// 收藏开关（Active / ActiveVer）与节选内容（Excerpt / HighlightLines / IsCustomQuote / ExcerptVer）独立版本化：
// 编辑节选不会把已取消的收藏重新复活，只有显式重新点亮 ❤️ 才会更新 ActiveVer。
type FavoritePoem struct {
	ID        string `json:"id,omitempty"` // 辅助展示/本地标识，落库须按 (book_id, poem_id) upsert
	BookID    string `json:"bookId"`
	PoemID    string `json:"poemId"`
	PoemTitle string `json:"poemTitle"`
	Author    string `json:"author,omitempty"`

	// 精选成员状态（仅由显式点亮 / 取消 ❤️ 更新）
	Active    bool    `json:"active"`
	ActiveVer Version `json:"activeVer"`
	DeletedAt int64   `json:"deletedAt,omitempty"`

	// 本诗金句 / 节选内容组（独立版本，不改变 Active）
	Excerpt        string  `json:"excerpt"`                  // 默认首节 2~4 行，或用户手动选定的本诗金句
	HighlightLines string  `json:"highlightLines,omitempty"` // 高亮行范围，如 "4-4" 或 "2-5"
	IsCustomQuote  bool    `json:"isCustomQuote,omitempty"`  // 是否为用户手动划选的本诗金句
	ExcerptVer     Version `json:"excerptVer,omitempty"`

	CreatedAt int64 `json:"createdAt"`
}

// QuoteKind 区分散页中的两类记录：来自诗集的诗歌节选 与 独立散句（手动记一张 / 外部摘录）。
type QuoteKind string

const (
	QuoteKindExcerpt    QuoteKind = "excerpt"    // 诗歌节选：关联诗集/诗篇，同一首诗可多次划选生成多张
	QuoteKindStandalone QuoteKind = "standalone" // 独立散句：手动记一张、Reeden 等外部导入或已删诗集降级后的摘录
)

// Quote 表示「散页」中的一条记录（诗歌节选或独立散句）。
// 删除对同一 QuoteID 永久生效（吸收状态），普通编辑不会复活已删除散页。
// 若关联诗集被删除，散页文字保留，运行时通过 EffectiveKind(bookActive=false) 自动降级为独立散句。
type Quote struct {
	ID             string    `json:"id"`
	Kind           QuoteKind `json:"kind,omitempty"`           // excerpt | standalone
	Content        string    `json:"content"`
	Author         string    `json:"author,omitempty"`
	SourceBook     string    `json:"sourceBook,omitempty"`     // 原书出处（诗集名或外部书名）
	BookID         string    `json:"bookId,omitempty"`         // 诗歌节选所属诗集 ID
	PoemID         string    `json:"poemId,omitempty"`         // 诗歌节选所属诗篇 ID
	PoemTitle      string    `json:"poemTitle,omitempty"`      // 冗余诗题，未下载/降级时保留出处可读性
	HighlightLines string    `json:"highlightLines,omitempty"` // 划选行范围（如 "4-6"）
	ImportSource   string    `json:"importSource,omitempty"`   // manual | reader | reeden | weread | json | txt
	Tags           []string  `json:"tags,omitempty"`
	BodyVer        Version   `json:"bodyVer"`

	IsFavorite  bool    `json:"isFavorite"`
	FavoriteVer Version `json:"favoriteVer,omitempty"`

	CreatedAt  int64   `json:"createdAt"`
	Deleted    bool    `json:"deleted,omitempty"`
	DeletedAt  int64   `json:"deletedAt,omitempty"`
	DeletedVer Version `json:"deletedVer,omitempty"`
}

// EffectiveKind 结合关联诗集是否仍存在（未删除）推导散页的有效类型。
// 当所属诗集已删除时，保留文字并自动降级为 QuoteKindStandalone。
func (q Quote) EffectiveKind(bookActive bool) QuoteKind {
	hasOrigin := q.BookID != "" && q.PoemID != ""
	if hasOrigin && bookActive && (q.Kind == QuoteKindExcerpt || q.Kind == "") {
		return QuoteKindExcerpt
	}
	return QuoteKindStandalone
}

// WidgetDeeplink 返回该散页在小组件中的点击跳转协议：
// 有效诗歌节选跳回原诗并定位高亮；独立散句（含已降级节选）仅打开单句卡片。
func (q Quote) WidgetDeeplink(bookActive bool) string {
	if q.EffectiveKind(bookActive) == QuoteKindExcerpt {
		if q.HighlightLines != "" {
			return fmt.Sprintf("stillpoets://poem/%s/%s?lines=%s&highlight=1", q.BookID, q.PoemID, q.HighlightLines)
		}
		return fmt.Sprintf("stillpoets://poem/%s/%s?highlight=1", q.BookID, q.PoemID)
	}
	return fmt.Sprintf("stillpoets://quote/%s", q.ID)
}

// Annotation 表示单诗微批注或诗集整体长书评（V1.1 启用，Phase 0 先预留结构）。
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

// ReadingProgress 表示单本诗集的跨端阅读进度、已读篇目集合与完读标记。
// 拆分三段独立合并：
// 1. 续读位置（LastPoemID + PositionVer）
// 2. 完读状态（Status + FinishedAt + StatusVer，日常翻诗不覆盖完读状态）
// 3. 已读诗篇集合（ReadPoemIDs，按并集去重，跨端精确计算已读篇数）
// 滚动位置 LocalScrollRatio 仅保留在本机（json:"-"）。
type ReadingProgress struct {
	BookID string `json:"bookId"`

	LastPoemID  string  `json:"lastPoemId,omitempty"`
	PositionVer Version `json:"positionVer,omitempty"`

	Status     BookReadStatus `json:"status"`
	FinishedAt int64          `json:"finishedAt,omitempty"`
	StatusVer  Version        `json:"statusVer,omitempty"`

	ReadPoemIDs []string `json:"readPoemIds,omitempty"`

	// 仅本机使用：同一首诗的滚动比例受屏幕/字号影响，不跨端同步
	LocalScrollRatio float64 `json:"-"`
}

// ReadPoemCount 返回跨端去重后的已读诗篇总数。
func (p ReadingProgress) ReadPoemCount() int {
	return len(p.ReadPoemIDs)
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
	WidgetModeQuotesRandom   WidgetSourceMode = "quotes_random"   // 散页池随机（诗歌节选跳回原诗，独立散句不跳原诗）
)

// WidgetThemeConfig 定义小组件视觉主题，与原生 WidgetKit / Glance 共享。
type WidgetThemeConfig struct {
	Preset      string `json:"preset"`               // xuan_paper | dark_minimal | frosted_glass
	FontFamily  string `json:"fontFamily,omitempty"` // 内置或已注册的自定义字体名
	FontFile    string `json:"fontFile,omitempty"`   // 本地字体文件名
	BgColor     string `json:"bgColor"`
	TextColor   string `json:"textColor"`
	AccentColor string `json:"accentColor"`
}

// Settings 保存本机应用外观与小组件偏好（V1.0 仅本机存储，不跨端同步；严禁包含 WebDAV 密码）。
type Settings struct {
	ThemePreset  string            `json:"themePreset"`
	FontFamily   string            `json:"fontFamily"`
	FontSize     float32           `json:"fontSize"`
	LineHeight   float32           `json:"lineHeight"`
	WidgetMode   WidgetSourceMode  `json:"widgetMode"`
	WidgetBookID string            `json:"widgetBookId,omitempty"`
	WidgetTheme  WidgetThemeConfig `json:"widgetTheme"`
	UpdatedAt    int64             `json:"updatedAt"`
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

// NewInstallID 生成不随系统备份克隆的随机安装 ID（仅包含小写十六进制字符，安全用于文件名）。
func NewInstallID() string {
	var buf [12]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
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
