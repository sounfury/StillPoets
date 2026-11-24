package views

import "time"

// 《死亡诗社》（Dead Poets Society）默认内置台词与轻量彩蛋文案，
// 统一集中管理，避免散落硬编码；所有彩蛋纯属本机 UI 表现，不写入数据库或 WebDAV 同步。
const (
	// 短空间默认句：设置主页预览条、App 内小组件预览及双端 2×2 / 4×2 空小组件
	dpsShortDefaultQuote = "诗歌、美丽、浪漫、爱情，才是我们活着的意义。"
	dpsShortDefaultMeta  = "《死亡诗社》 · 导入诗集或记散页后自动轮播"

	// 空书库「今日重读」引导卡（2 行）
	dpsEmptyHeroQuote = "及时行乐，孩子们，\n让你的生命超越凡俗。"

	// 核心长空间句：梭罗《瓦尔登湖》死亡诗社开场誓词（关于页扉页 / 字体标本卡 / 4×4 大号空小组件 / 平板空阅读器）
	dpsThoreauOpening = "我步入丛林，\n因为我希望生活得有意义，\n我希望活得深刻，\n吸取生命中所有的精华，\n把非生命的一切都击溃，\n以免当我生命终结，\n发现自己从没有活过。"
	dpsThoreauOpeningEN = "I went to the woods because I wanted to live deliberately.\nI wanted to live deep and suck out all the marrow of life,\nto put to rout all that was not life,\nand not, when I had come to die,\ndiscover that I had not lived."
	dpsThoreauAttribution = "梭罗《瓦尔登湖》 · 《死亡诗社》开场白"

	// 午夜洞穴彩蛋（00:00–04:00）
	dpsMidnightCaveLabel = "午夜洞穴"
	dpsMidnightHeroQuote = "惟有在梦中，\n人们才是真正自由的。"
	dpsMidnightQuietLine = "“惟有在梦中，人们才是真正自由的。”"

	// 排版预览轮换末句极淡落款
	dpsWeltonFootprint = "Welton Academy · 1959"
)

type dpsSpecimen struct {
	ZH         string
	EN         string
	ShowOrigin bool
}

var dpsInlineSpecimens = []dpsSpecimen{
	{
		ZH: dpsShortDefaultQuote,
	},
	{
		ZH: "诗歌、美丽、浪漫、爱情，这些才是我们活着的意义。",
		EN: "Poetry, beauty, romance, love — these are what we stay alive for.",
	},
	{
		ZH: "及时行乐，孩子们，让你的生命超越凡俗。",
		EN: "Carpe diem, seize the day, boys, make your lives extraordinary.",
	},
	{
		ZH: "惟有在梦中，人们才是真正自由的。",
		EN: "Only in their dreams can men be truly free.",
	},
	{
		ZH: "不管别人怎么说，文学和语言的确能改变世界。",
		EN: "No matter what anybody tells you, words and ideas can change the world.",
	},
	{
		ZH: "哦，让生活从此变成一首欢乐的诗。",
		EN: "Oh, to have life henceforth the poem of new joys.",
	},
	{
		ZH: "你们用不着表演，完全为你自己。",
		EN: "You don't have to perform. Just make it for yourself.",
	},
	{
		ZH: "要敢于逆流而上。",
		EN: "Swim against the stream.",
	},
	{
		ZH:         "船长，我的船长！",
		EN:         "Oh, captain, my captain!",
		ShowOrigin: true,
	},
}

var dpsFontsSpecimens = []dpsSpecimen{
	{
		ZH: dpsThoreauOpening,
	},
	{
		ZH: dpsThoreauOpening,
		EN: dpsThoreauOpeningEN,
	},
	{
		ZH: "医学、法律、商业、工程，这些都是崇高的追求，足以支撑人的一生。\n但诗歌、美丽、浪漫、爱情，这些才是我们活着的意义。",
		EN: "Medicine, law, business, engineering — these are noble pursuits and necessary to sustain life. But poetry, beauty, romance, love — these are what we stay alive for.",
	},
	{
		ZH: "及时采撷你的花蕾，旧时光一去不回；\n今天尚在微笑的花朵，明天变得风中枯萎。",
		EN: "Gather ye rosebuds while ye may, old time is still a-flying;\nAnd this same flower that smiles today, tomorrow will be dying.",
	},
	{
		ZH: "惟有在梦中，人们才是真正自由的。",
		EN: "Only in their dreams can men be truly free.",
	},
	{
		ZH: "学会自己思考，学会欣赏文学和语言。\n不管别人怎么说，文学和语言的确能改变世界。",
		EN: "Learn to think for yourselves again. No matter what anybody tells you, words and ideas can change the world.",
	},
	{
		ZH: "哦，让生活从此变成一首欢乐的诗。",
		EN: "Oh, to have life henceforth the poem of new joys.",
	},
	{
		ZH:         "你们用不着表演，完全为你自己。\n要敢于逆流而上。",
		EN:         "You don't have to perform. Just make it for yourself.\nSwim against the stream.",
		ShowOrigin: true,
	},
}

func inlinePreviewSpecimen(idx int) dpsSpecimen {
	n := len(dpsInlineSpecimens)
	return dpsInlineSpecimens[((idx%n)+n)%n]
}

func fontsPreviewSpecimen(idx int) dpsSpecimen {
	n := len(dpsFontsSpecimens)
	return dpsFontsSpecimens[((idx%n)+n)%n]
}

func isMidnightCaveTime(t time.Time) bool {
	h := t.Local().Hour()
	return h >= 0 && h < 4
}

func (a *AppUI) isMidnightCave() bool {
	if a.nowFn != nil {
		return isMidnightCaveTime(a.nowFn())
	}
	return isMidnightCaveTime(time.Now())
}
