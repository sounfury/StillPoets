package components

import (
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/mobile"

	"stillpoets/internal/ui/layout"
	"stillpoets/internal/ui/theme"
)

// NavTab 表示主导航三栏：书架 / 统计 / 我的。
type NavTab int

const (
	TabShelf NavTab = iota
	TabStats
	TabProfile
)

// ShelfSubTab 表示书架顶部的平级双入口：诗集 / 散页。
type ShelfSubTab int

const (
	SubTabBooks ShelfSubTab = iota
	SubTabQuotes
)

// BrandHeader 渲染顶部品牌行（StillPoets + 搜索 + 新增），全部新增操作收敛至顶部 "+"。
func BrandHeader(c *ui.Context, pal theme.Palette, title string, showActions bool, onSearch, onAdd func()) {
	titleFont := theme.FontSerif
	titleSize := float32(21)
	letterSp := float32(0.4)
	if title == "StillPoets" {
		titleFont = theme.FontBrand
		titleSize = 23
		letterSp = 0.25
	}
	ui.Row(c.Key("brand-header")).
		FillWidth().
		Padding(10, 20, 4, 20).
		AlignItems(ui.Center).
		Children(func() {
			ui.Text(c, title).
				Font(titleFont).
				FontSize(titleSize).
				Bold().
				LetterSpacing(letterSp).
				TextColor(pal.TextPrimary).
				Grow(1)
			if showActions {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					searchBtn := ui.Box(c.Key("btn-search")).Width(32).Height(32).Center().Radius(8).Cursor(ui.CursorPointer)
					theme.ApplyPressScale(searchBtn, 0.92)
					searchBtn.Children(func() {
						ui.Icon(c, IconSearch).Width(18).Height(18).TextColor(pal.TextPrimary)
					})
					if searchBtn.Clicked() && onSearch != nil {
						onSearch()
					}

					addBtn := ui.Box(c.Key("btn-add")).Width(32).Height(32).Center().Radius(8).Cursor(ui.CursorPointer)
					theme.ApplyPressScale(addBtn, 0.92)
					addBtn.Children(func() {
						ui.Icon(c, IconPlus).Width(19).Height(19).TextColor(pal.TextPrimary)
					})
					if addBtn.Clicked() && onAdd != nil {
						onAdd()
					}
				})
			}
		})
}

// ShelfSubTabs 渲染「诗集 / 散页」常驻顶切换（选中态下划线支持随手势进度实时平滑滑移）。
// slideProgress 取值 [0, 1]：0 对应「诗集」，1 对应「散页」。
func ShelfSubTabs(c *ui.Context, pal theme.Palette, active ShelfSubTab, slideProgress float32, onSelect func(ShelfSubTab)) {
	if slideProgress < 0 {
		slideProgress = 0
	} else if slideProgress > 1 {
		slideProgress = 1
	}

	row := ui.Row(c.Key("shelf-subtabs")).
		FillWidth().
		Padding(2, 20, 0, 20).
		AlignItems(ui.End)
	if pal.HeroStyle == theme.HeroOpenPaper {
		row.BorderColor(pal.Border).BorderWidth(0, 0, 1, 0)
	}
	row.Children(func() {
		const (
			itemW float32 = 44
			gapW  float32 = 22
			barW  float32 = 28
		)
		// 指示条随 slideProgress 在两个标签中心之间平滑滑移
		barX := (itemW-barW)/2 + slideProgress*(itemW+gapW)

		tabsWrap := ui.Column(c.Key("shelf-subtabs-wrap")).Gap(5)
		tabsWrap.Children(func() {
			ui.Row(c).Gap(gapW).AlignItems(ui.End).Children(func() {
				items := []struct {
					tab   ShelfSubTab
					label string
				}{
					{SubTabBooks, "诗集"},
					{SubTabQuotes, "散页"},
				}
				for _, item := range items {
					isActive := active == item.tab
					col := ui.Box(c.Key(item.label)).Width(itemW).Center().Padding(2, 0, 0, 0).Cursor(ui.CursorPointer)
					theme.ApplyPressScale(col, 0.94)
					col.Children(func() {
						txt := ui.Text(c, item.label).Font(theme.FontSans).FontSize(15).LetterSpacing(0.4)
						if isActive {
							txt.FontWeight(600).TextColor(pal.TextPrimary)
						} else {
							txt.TextColor(pal.TextMuted)
						}
					})
					if col.Clicked() && onSelect != nil {
						onSelect(item.tab)
					}
				}
			})

			// 跟手滑动的主题短下划线轨道
			ui.Box(c.Key("subtab-track")).
				Width(itemW*2 + gapW).
				Height(2).
				Draw(func(p *ui.Painter, r ui.Rect) {
					p.Fill(ui.Rect{X: r.X + barX, Y: r.Y, W: barW, H: r.H}, pal.Accent, 1)
				})
		})
	})
}

// DefaultFloatingBottomNav 返回当前运行平台默认是否启用液态玻璃悬浮底栏（iOS/Cupertino 默认开启，其余默认关闭）。
func DefaultFloatingBottomNav() bool {
	return mobile.Current() == mobile.Cupertino
}

// FloatingBottomNavBar 渲染 iOS 风格的悬浮液态玻璃底栏（底部柔化滚边 + 悬浮胶囊玻璃 + 选中态微光胶囊）。
func FloatingBottomNavBar(c *ui.Context, pal theme.Palette, active NavTab, onSelect func(NavTab)) {
	_, _, safeBottom, _ := c.SafeArea()
	// 键盘弹出时自动隐藏悬浮底栏，避免遮挡输入区
	if safeBottom > 120 {
		return
	}

	// 底部柔化滚边，让滚动到悬浮胶囊下方的诗集/散页自然过渡并保留液态玻璃折射层次
	ui.Box(c.Key("bottom-nav-scroll-edge")).
		Absolute().
		Left(0).Right(0).
		Bottom(-safeBottom).
		Height(84 + safeBottom).
		PassThrough().
		Material(glass.ScrollEdge{Bottom: true, Background: pal.Background})

	isDark := pal.ID == theme.PresetDarkMinimal
	shadowClr := ui.RGBA(0, 0, 0, 0.09)
	activePillBg := pal.Accent.Alpha(0.12)
	inactiveClr := pal.TextSecondary
	if isDark {
		shadowClr = ui.RGBA(0, 0, 0, 0.30)
		activePillBg = ui.RGBA(255, 255, 255, 0.14)
		inactiveClr = pal.TextMuted
	}

	ui.Row(c.Key("bottom-nav-float-wrap")).
		Absolute().
		Left(0).Right(0).
		Bottom(8).
		Padding(0, 22).
		Justify(ui.Center).
		PassThrough().
		Children(func() {
			ui.Row(c.Key("bottom-nav")).
				FillWidth().
				MaxWidth(316).
				Height(60).
				Padding(5, 6).
				Gap(4).
				Radius(30).
				AlignItems(ui.Center).
				Material(glass.Glass{Style: glass.Regular, Interactive: true}).
				Shadow(0, 6, 20, 0, shadowClr).
				Role(ui.RoleTabList).
				Children(func() {
					for _, item := range navItems() {
						isActive := active == item.tab
						col := ui.Column(c.Key(item.label)).
							Grow(1).
							Basis(0).
							FillHeight().
							Center().
							Gap(2).
							Radius(25).
							Cursor(ui.CursorPointer).
							Transition(theme.ColorTransition()).
							Role(ui.RoleTab).
							Label(item.label).
							Checked(isActive)
						if isActive {
							col.Background(activePillBg)
						}
						theme.ApplyPressScale(col, 0.94)
						col.Children(func() {
							clr := inactiveClr
							if isActive {
								clr = pal.Accent
							}
							ui.Icon(c, item.icon).Width(20).Height(20).TextColor(clr)
							lbl := ui.Text(c, item.label).Font(theme.FontSans).FontSize(11).TextColor(clr)
							if isActive {
								lbl.Bold()
							}
						})
						if col.Clicked() && onSelect != nil {
							onSelect(item.tab)
						}
					}
				})
		})
}

// BottomNavBar 渲染手机端三栏底部导航（Android 经典平铺底栏：书架 / 统计 / 我的）。
func renderNavItemBody(c *ui.Context, pal theme.Palette, item navItem, isActive bool, iconSize float32, showDot bool) {
	clr := pal.TextMuted
	if isActive {
		if pal.HeroStyle == theme.HeroOpenPaper {
			clr = pal.Accent
		} else {
			clr = pal.TextPrimary
		}
	}
	ui.Icon(c, item.icon).Width(iconSize).Height(iconSize).TextColor(clr)
	lbl := ui.Text(c, item.label).Font(theme.FontSans).FontSize(11).TextColor(clr)
	if isActive {
		lbl.Bold()
	}
	dot := ui.Box(c.Key("dot")).Width(4).Height(4).Radius(2).Transition(theme.ColorTransition())
	if showDot {
		dot.Background(pal.Accent)
	} else {
		dot.Background(ui.Transparent)
	}
}

func BottomNavBar(c *ui.Context, pal theme.Palette, active NavTab, onSelect func(NavTab)) {
	ui.Row(c.Key("bottom-nav")).
		FillWidth().
		Height(62).
		Background(pal.Background).
		BorderColor(pal.Border).
		BorderWidth(1, 0, 0, 0).
		AlignItems(ui.Center).
		Children(func() {
			items := navItems()
			for _, item := range items {
				isActive := active == item.tab
				col := ui.Column(c.Key(item.label)).
					Grow(1).
					FillHeight().
					Center().
					Gap(3).
					Cursor(ui.CursorPointer)
				theme.ApplyPressScale(col, 0.93)
				col.Children(func() {
					renderNavItemBody(c, pal, item, isActive, 20, isActive && pal.HeroStyle != theme.HeroOpenPaper)
				})
				if col.Clicked() && onSelect != nil {
					onSelect(item.tab)
				}
			}
		})
}

// TabletNavRail 渲染 Android 平板横屏最左侧窄导航轨（S 字标 + 书架 / 统计 / 我的）。
func TabletNavRail(c *ui.Context, pal theme.Palette, active NavTab, onSelect func(NavTab)) {
	ui.Column(c.Key("tablet-nav-rail")).
		Width(layout.NavRailWidthDIP).
		Shrink(0).
		FillHeight().
		Background(pal.Background).
		BorderColor(pal.Border).
		BorderWidth(0, 1, 0, 0).
		Padding(18, 0).
		AlignItems(ui.Center).
		Gap(22).
		Children(func() {
			ui.Text(c, "S").Font(theme.FontBrand).FontSize(27).Bold().Italic().TextColor(pal.TextPrimary).Padding(0, 0, 12, 0)
			for _, item := range navItems() {
				isActive := active == item.tab
				col := ui.Column(c.Key(item.label)).
					Width(56).
					Padding(8, 0).
					Center().
					Gap(4).
					Radius(8).
					Cursor(ui.CursorPointer)
				theme.ApplyPressScale(col, 0.93)
				col.Children(func() {
					renderNavItemBody(c, pal, item, isActive, 21, isActive)
				})
				if col.Clicked() && onSelect != nil {
					onSelect(item.tab)
				}
			}
		})
}

// ToastKind 表示底部瞬态轻胶囊的视觉图标类型。
type ToastKind int

const (
	ToastInfo ToastKind = iota
	ToastLoading
	ToastWarning
)

// SyncToastPill 渲染瞬态云同步轻胶囊（同步中带原生旋转等待动画，同步成功约 2 秒后自动淡出，平时不占位）。
func SyncToastPill(c *ui.Context, pal theme.Palette, kind ToastKind, message string, onClick func()) {
	if message == "" {
		return
	}
	pill := ui.Row(c.Key("sync-toast-pill")).
		Padding(8, 15).
		Gap(7).
		AlignItems(ui.Center).
		Background(pal.Surface).
		Border(1, pal.Border).
		Radius(999).
		Shadow(0, 6, 18, 0, ui.RGBA(0, 0, 0, 0.14)).
		Cursor(ui.CursorPointer)
	theme.ApplyPressScale(pill, 0.96)
	pill.Children(func() {
		switch kind {
		case ToastLoading:
			ui.Spinner(c.Key("toast-spinner")).FontSize(14).TextColor(pal.Accent).Label(message)
		case ToastWarning:
			ui.Icon(c, IconAlertCircle).Width(15).Height(15).TextColor(pal.Accent)
		default:
			ui.Icon(c, IconCloudCheck).Width(15).Height(15).TextColor(pal.Accent)
		}
		ui.Text(c, message).Font(theme.FontSans).FontSize(12).TextColor(pal.TextSecondary)
	})
	if pill.Clicked() && onClick != nil {
		onClick()
	}
}

type navItem struct {
	tab   NavTab
	label string
	icon  *ui.SVG
}

func navItems() []navItem {
	return []navItem{
		{TabShelf, "书架", IconShelf},
		{TabStats, "统计", IconStats},
		{TabProfile, "我的", IconProfile},
	}
}
