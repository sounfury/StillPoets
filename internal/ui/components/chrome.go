package components

import (
	"github.com/egoist/mygo/ui"

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
	ui.Row(c.Key("brand-header")).
		FillWidth().
		Padding(14, 20, 8, 20).
		AlignItems(ui.Center).
		Children(func() {
			ui.Text(c, title).FontSize(22).Bold().TextColor(pal.TextPrimary).Grow(1)
			if showActions {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					searchBtn := ui.Box(c.Key("btn-search")).Width(34).Height(34).Center().Radius(8).Cursor(ui.CursorPointer)
					searchBtn.Children(func() {
						ui.Icon(c, IconSearch).Width(19).Height(19).TextColor(pal.TextPrimary)
					})
					if searchBtn.Clicked() && onSearch != nil {
						onSearch()
					}

					addBtn := ui.Box(c.Key("btn-add")).Width(34).Height(34).Center().Radius(8).Cursor(ui.CursorPointer)
					addBtn.Children(func() {
						ui.Icon(c, IconPlus).Width(20).Height(20).TextColor(pal.TextPrimary)
					})
					if addBtn.Clicked() && onAdd != nil {
						onAdd()
					}
				})
			}
		})
}

// ShelfSubTabs 渲染「诗集 / 散页」常驻顶切换（选中态配主题短下划线）。
func ShelfSubTabs(c *ui.Context, pal theme.Palette, active ShelfSubTab, onSelect func(ShelfSubTab)) {
	row := ui.Row(c.Key("shelf-subtabs")).
		FillWidth().
		Padding(4, 20, 0, 20).
		Gap(26).
		AlignItems(ui.End)
	if pal.HeroStyle == theme.HeroOpenPaper {
		row.BorderColor(pal.Border).BorderWidth(0, 0, 1, 0)
	}
	row.Children(func() {
		items := []struct {
			tab   ShelfSubTab
			label string
		}{
			{SubTabBooks, "诗集"},
			{SubTabQuotes, "散页"},
		}
		for _, item := range items {
			isActive := active == item.tab
			col := ui.Column(c.Key(item.label)).AlignItems(ui.Center).Gap(6).Padding(4, 0, 0, 0).Cursor(ui.CursorPointer)
			col.Children(func() {
				txt := ui.Text(c, item.label).FontSize(16)
				if isActive {
					txt.Bold().TextColor(pal.TextPrimary)
				} else {
					txt.TextColor(pal.TextMuted)
				}
				line := ui.Box(c.Key("underline")).Width(32).Height(2.5).Radius(2)
				if isActive {
					line.Background(pal.Accent)
				} else {
					line.Background(ui.Transparent)
				}
			})
			if col.Clicked() && onSelect != nil {
				onSelect(item.tab)
			}
		}
	})
}

// BottomNavBar 渲染手机端三栏底部导航（书架 / 统计 / 我的）。
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
				col.Children(func() {
					clr := pal.TextMuted
					if isActive {
						if pal.HeroStyle == theme.HeroOpenPaper {
							clr = pal.Accent
						} else {
							clr = pal.TextPrimary
						}
					}
					ui.Icon(c, item.icon).Width(20).Height(20).TextColor(clr)
					lbl := ui.Text(c, item.label).FontSize(11).TextColor(clr)
					if isActive {
						lbl.Bold()
					}
					dot := ui.Box(c.Key("dot")).Width(4).Height(4).Radius(2)
					if isActive && pal.HeroStyle != theme.HeroOpenPaper {
						dot.Background(pal.Accent)
					} else {
						dot.Background(ui.Transparent)
					}
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
			ui.Text(c, "S").FontSize(26).Bold().Italic().TextColor(pal.TextPrimary).Padding(0, 0, 12, 0)
			for _, item := range navItems() {
				isActive := active == item.tab
				col := ui.Column(c.Key(item.label)).
					Width(56).
					Padding(8, 0).
					Center().
					Gap(4).
					Radius(8).
					Cursor(ui.CursorPointer)
				col.Children(func() {
					clr := pal.TextMuted
					if isActive {
						if pal.HeroStyle == theme.HeroOpenPaper {
							clr = pal.Accent
						} else {
							clr = pal.TextPrimary
						}
					}
					ui.Icon(c, item.icon).Width(21).Height(21).TextColor(clr)
					lbl := ui.Text(c, item.label).FontSize(11).TextColor(clr)
					if isActive {
						lbl.Bold()
					}
					dot := ui.Box(c.Key("dot")).Width(4).Height(4).Radius(2)
					if isActive {
						dot.Background(pal.Accent)
					} else {
						dot.Background(ui.Transparent)
					}
				})
				if col.Clicked() && onSelect != nil {
					onSelect(item.tab)
				}
			}
		})
}

// SyncToastPill 渲染瞬态云同步轻胶囊（同步成功约 2 秒后自动淡出，平时不占位）。
func SyncToastPill(c *ui.Context, pal theme.Palette, message string, onClick func()) {
	if message == "" {
		return
	}
	pill := ui.Row(c.Key("sync-toast-pill")).
		Padding(7, 14).
		Gap(7).
		AlignItems(ui.Center).
		Background(pal.Surface).
		Border(1, pal.Border).
		Radius(999).
		Cursor(ui.CursorPointer)
	pill.Children(func() {
		ui.Icon(c, IconCloudCheck).Width(15).Height(15).TextColor(pal.Accent)
		ui.Text(c, message).FontSize(12).TextColor(pal.TextSecondary)
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
