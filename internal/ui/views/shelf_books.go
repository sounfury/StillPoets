package views

import (
	"github.com/egoist/mygo/ui"

	"stillpoets/internal/domain"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/theme"
)

func (a *AppUI) renderMyBooksHeaderActions(c *ui.Context, pal theme.Palette, books []domain.Book) {
	if a.shelfOrganizing {
		if len(books) > 0 {
			selAllLabel := "全选"
			if a.allFilteredBooksSelected(books) {
				selAllLabel = "取消全选"
			}
			selAllBtn := ui.Box(c.Key("org-sel-all")).Padding(3, 6).Cursor(ui.CursorPointer)
			selAllBtn.Children(func() {
				ui.Text(c, selAllLabel).Font(theme.FontSans).FontSize(12).TextColor(pal.TextSecondary)
			})
			if selAllBtn.Clicked() {
				a.toggleSelectAllFilteredBooks(books)
			}
		}
		doneLabel := "完成"
		if a.shelfTargetGroupID != "" {
			doneLabel = "取消"
		}
		doneBtn := ui.Box(c.Key("org-done")).Padding(3, 6).Cursor(ui.CursorPointer)
		doneBtn.Children(func() {
			ui.Text(c, doneLabel).Font(theme.FontSans).FontSize(12).Bold().TextColor(pal.Accent)
		})
		if doneBtn.Clicked() {
			prevTarget := a.shelfTargetGroupID
			a.exitShelfOrganize()
			if prevTarget != "" {
				a.selectedGrp = prevTarget
			}
		}
	} else {
		if len(a.books) > 0 {
			orgBtn := ui.Box(c.Key("organize-books")).Padding(3, 6).Cursor(ui.CursorPointer)
			orgBtn.Children(func() {
				ui.Text(c, "整理").Font(theme.FontSans).FontSize(12).TextColor(pal.TextSecondary)
			})
			if orgBtn.Clicked() {
				a.enterShelfOrganize("")
			}
		}
		manageBtn := ui.Box(c.Key("manage-groups")).Padding(3, 6).Cursor(ui.CursorPointer)
		manageBtn.Children(func() {
			ui.Text(c, "管理分组").Font(theme.FontSans).FontSize(12).TextColor(pal.TextMuted)
		})
		if manageBtn.Clicked() {
			a.groupFormError = ""
			a.showGroupModal = true
		}
	}
}

func (a *AppUI) renderMyBooksHeader(c *ui.Context, pal theme.Palette, books []domain.Book) {
	// 标题行：我的诗集 + 右侧轻量「管理分组」
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
		title := "我的诗集"
		if a.shelfOrganizing {
			title = "整理诗集"
		}
		ui.Text(c, title).Font(theme.FontSerif).FontSize(16).Bold().LetterSpacing(0.3).TextColor(pal.TextPrimary).Grow(1)

		a.renderMyBooksHeaderActions(c, pal, books)
	})
}

func (a *AppUI) renderShelfTargetBanner(c *ui.Context, pal theme.Palette) {
	// 定向选入模式（从空分组点「从书架选入」进入）提示横条
	if a.shelfOrganizing && a.shelfTargetGroupID != "" {
		if targetName := a.groupNameByID(a.shelfTargetGroupID); targetName != "" {
			ui.Row(c.Key("org-target-banner")).
				FillWidth().
				Padding(8, 12).
				Radius(8).
				Background(pal.SurfaceMuted).
				Border(0.75, pal.Border).
				AlignItems(ui.Center).
				Children(func() {
					ui.Textf(c, "为「%s」挑选诗集 · 点选下方封面即可收入", targetName).
						Font(theme.FontNote).FontSize(12).TextColor(pal.TextSecondary).Grow(1)
				})
		}
	}
}

func (a *AppUI) renderShelfGroupTabs(c *ui.Context, pal theme.Palette) {
	// 横向直达分组切换条（定向选入模式下暂时隐藏，锁定在全书架挑书）
	if !(a.shelfOrganizing && a.shelfTargetGroupID != "") {
		ui.ScrollHorizontal(c.Key("group-pills-scroll")).FillWidth().Height(28).Scrollbars(false).Children(func() {
			ui.Row(c).Gap(18).Padding(1, 0, 3, 0).Children(func() {

				a.renderShelfGroupTab(c, pal, "", "全部")
				for _, g := range a.groups {
					a.renderShelfGroupTab(c, pal, g.ID, g.Name)
				}
			})
		})
	}
}

func (a *AppUI) renderEmptyShelfGroup(c *ui.Context, pal theme.Palette) {
	ui.Column(c.Key("empty-books")).
		FillWidth().
		Padding(26, 16).
		Gap(10).
		AlignItems(ui.Center).
		Radius(8).
		Background(pal.Surface).
		Border(1, pal.Border).
		Children(func() {
			msg := "这个分组里还没有诗集"
			if a.selectedGrp == "" {
				msg = "书架空空如也，导入一本 EPUB 诗集开始阅读吧"
			}
			ui.Text(c, msg).Font(theme.FontNote).FontSize(13).TextColor(pal.TextMuted)
			ui.Row(c).Gap(10).Wrap().Center().Children(func() {
				if a.selectedGrp != "" && len(a.books) > 0 {
					if ui.PrimaryButton(c, "从书架选入").Clicked() {
						a.enterShelfOrganizeForGroup(a.selectedGrp)
					}
					if ui.Button(c, "+ 导入 EPUB 诗集").Clicked() {
						a.triggerImportEPUB()
					}
				} else {
					if ui.PrimaryButton(c, "+ 导入 EPUB 诗集").Clicked() {
						a.triggerImportEPUB()
					}
				}
			})
		})
}

func (a *AppUI) renderShelfBook(c *ui.Context, pal theme.Palette, b domain.Book, isTablet bool) {
	selected := isTablet && b.ID == a.activeBookID && !a.shelfOrganizing
	isRemote := b.LocalState != domain.LocalStateDownloaded
	orgOpts := components.BookCoverOrganizeOptions{
		Organizing: a.shelfOrganizing,
		Checked:    a.shelfSelectedBooks[b.ID],
	}
	if a.shelfOrganizing && a.selectedGrp == "" {
		orgOpts.GroupBadge = a.groupNameByID(b.GroupID)
	}
	act := components.BookCoverShelfCard(c, pal, components.BookCoverProps{
		Key:               "bk-" + b.ID,
		BookID:            b.ID,
		Title:             b.Title,
		Author:            b.Author,
		CoverImg:          a.coverImageFor(b),
		Selected:          selected,
		IsRemote:          isRemote,
		ForceCaptionBelow: a.settings.UseOriginalCover,
		Organize:          orgOpts,
	})
	if a.shelfOrganizing {
		if act.Clicked || act.LongPressed {
			a.toggleShelfBookSelected(b.ID)
		}
	} else {
		if act.LongPressed {
			a.openBookActionModal(b.ID)
		} else if act.Clicked {
			a.openBook(b.ID, "", nil, !isTablet)
		}
	}
}

func (a *AppUI) renderShelfGroupTab(c *ui.Context, pal theme.Palette, id, label string) {
	active := a.selectedGrp == id
	col := ui.Column(c.Key("grp-" + id)).AlignItems(ui.Center).Gap(4).Cursor(ui.CursorPointer)
	col.Children(func() {
		txt := ui.Text(c, label).Font(theme.FontSans).FontSize(13)
		if active {
			txt.FontWeight(600).TextColor(pal.TextPrimary)
		} else {
			txt.TextColor(pal.TextMuted)
		}
		bar := ui.Box(c).Width(20).Height(1.5).Radius(1).Transition(theme.ColorTransition())
		if active {
			bar.Background(pal.Accent)
		} else {
			bar.Background(ui.Transparent)
		}
	})
	if col.Clicked() {
		a.selectedGrp = id
	}
}
