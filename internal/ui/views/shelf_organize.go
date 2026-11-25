package views

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/domain"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/theme"
)

// --- 书架原地批量整理状态机 ---

func (a *AppUI) enterShelfOrganize(preselectBookID string) {
	a.shelfOrganizing = true
	a.shelfTargetGroupID = ""
	a.shelfSelectedBooks = map[string]bool{}
	if preselectBookID != "" {
		a.shelfSelectedBooks[preselectBookID] = true
	}
}

// enterShelfOrganizeForGroup 从某个分组（尤其是空分组）直接进入全书架「定向选入」模式：
// 切到全部书架网格看封面挑书，确认后批量收入该分组并自动返回该分组视图。
func (a *AppUI) enterShelfOrganizeForGroup(groupID string) {
	if groupID == "" {
		a.enterShelfOrganize("")
		return
	}
	a.shelfOrganizing = true
	a.shelfTargetGroupID = groupID
	a.selectedGrp = ""
	a.shelfSelectedBooks = map[string]bool{}
}

func (a *AppUI) exitShelfOrganize() {
	if !a.shelfOrganizing && len(a.shelfSelectedBooks) == 0 && a.shelfTargetGroupID == "" {
		return
	}
	a.shelfOrganizing = false
	a.shelfTargetGroupID = ""
	a.shelfSelectedBooks = map[string]bool{}
	a.showBatchGroupModal = false
	a.batchNewGroupName = ""
	a.batchGroupError = ""
}

func (a *AppUI) toggleShelfBookSelected(bookID string) {
	if bookID == "" {
		return
	}
	if a.shelfSelectedBooks == nil {
		a.shelfSelectedBooks = map[string]bool{}
	}
	if a.shelfSelectedBooks[bookID] {
		delete(a.shelfSelectedBooks, bookID)
	} else {
		a.shelfSelectedBooks[bookID] = true
	}
}

func (a *AppUI) selectedShelfBookIDs() []string {
	if len(a.shelfSelectedBooks) == 0 {
		return nil
	}
	valid := make(map[string]bool, len(a.books))
	for _, b := range a.books {
		valid[b.ID] = true
	}
	ids := make([]string, 0, len(a.shelfSelectedBooks))
	for _, b := range a.books {
		if a.shelfSelectedBooks[b.ID] && valid[b.ID] {
			ids = append(ids, b.ID)
		}
	}
	return ids
}

func (a *AppUI) allFilteredBooksSelected(books []domain.Book) bool {
	if len(books) == 0 {
		return false
	}
	for _, b := range books {
		if !a.shelfSelectedBooks[b.ID] {
			return false
		}
	}
	return true
}

func (a *AppUI) toggleSelectAllFilteredBooks(books []domain.Book) {
	if a.shelfSelectedBooks == nil {
		a.shelfSelectedBooks = map[string]bool{}
	}
	if a.allFilteredBooksSelected(books) {
		for _, b := range books {
			delete(a.shelfSelectedBooks, b.ID)
		}
		return
	}
	for _, b := range books {
		a.shelfSelectedBooks[b.ID] = true
	}
}

func (a *AppUI) openBookActionModal(bookID string) {
	if bookID == "" {
		return
	}
	a.actionBookID = bookID
	a.actionNewGroupName = ""
	a.actionGroupError = ""
	a.showBookActionModal = true
}

func (a *AppUI) groupNameByID(groupID string) string {
	if groupID == "" {
		return ""
	}
	for _, g := range a.groups {
		if g.ID == groupID {
			return g.Name
		}
	}
	return ""
}

func (a *AppUI) bookCountInGroup(groupID string) int {
	n := 0
	for _, b := range a.books {
		if b.GroupID == groupID {
			n++
		}
	}
	return n
}

func (a *AppUI) bookByID(bookID string) (domain.Book, bool) {
	for _, b := range a.books {
		if b.ID == bookID {
			return b, true
		}
	}
	return domain.Book{}, false
}

func (a *AppUI) applyBatchGroupMoveAsync(bookIDs []string, targetGroupID, targetGroupName string, switchToTarget bool) {
	if len(bookIDs) == 0 {
		return
	}
	count := len(bookIDs)
	a.exitShelfOrganize()
	if switchToTarget {
		a.selectedGrp = targetGroupID
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.svc.SetBooksGroup(ctx, bookIDs, targetGroupID); err != nil {
			a.showTransientToast("更新分组失败：" + err.Error())
			return
		}
		a.reloadSync(nil)
		if targetGroupID == "" {
			a.showTransientToast(fmt.Sprintf("已将 %d 本诗集移出分组", count))
		} else {
			a.showTransientToast(fmt.Sprintf("已将 %d 本诗集收入「%s」", count, targetGroupName))
		}
	}()
}

// --- 书架底部原地整理操作条 ---

func (a *AppUI) renderShelfOrganizeBar(c *ui.Context, pal theme.Palette, isTablet bool) {
	if !a.shelfOrganizing {
		return
	}
	ids := a.selectedShelfBookIDs()
	count := len(ids)

	// 手机端启用液态玻璃悬浮底栏时，为底栏胶囊留出安全间距，避免操作栏与底栏重叠
	bottomPad := float32(10)
	if !isTablet && a.useFloatingBottomNav() {
		bottomPad = 86
	}

	ui.Column(c.Key("shelf-org-bar")).
		FillWidth().
		Background(pal.Surface).
		Children(func() {
			ui.Box(c).FillWidth().Height(1).Background(pal.Border)
			ui.Column(c).
				FillWidth().
				Padding(10, 14, bottomPad, 14).
				Gap(8).
				Children(func() {
					if a.shelfTargetGroupID != "" {
						targetName := a.groupNameByID(a.shelfTargetGroupID)
						if targetName == "" {
							a.exitShelfOrganize()
							return
						}
						ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
							label := fmt.Sprintf("已选 %d 本", count)
							if count == 0 {
								label = "点选诗集封面"
							}
							ui.Text(c, label).
								Font(theme.FontSans).FontFeatures("tnum").FontSize(12.5).TextColor(pal.TextSecondary).Grow(1)

							if ui.Button(c, "取消").Clicked() {
								prevTarget := a.shelfTargetGroupID
								a.exitShelfOrganize()
								a.selectedGrp = prevTarget
							}
							confirm := ui.PrimaryButton(c, fmt.Sprintf("收入「%s」", targetName)).Disabled(count == 0)
							if count > 0 && confirm.Clicked() {
								a.applyBatchGroupMoveAsync(ids, a.shelfTargetGroupID, targetName, true)
							}
						})
						return
					}

					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
						label := fmt.Sprintf("已选 %d 本", count)
						if count == 0 {
							label = "点选诗集封面"
						}
						ui.Text(c, label).
							Font(theme.FontSans).FontFeatures("tnum").FontSize(12.5).TextColor(pal.TextSecondary).Grow(1)

						if a.selectedGrp != "" {
							removeBtn := ui.Button(c, "移出本组").Disabled(count == 0)
							if count > 0 && removeBtn.Clicked() {
								a.applyBatchGroupMoveAsync(ids, "", "", false)
							}
						}

						moveBtn := ui.PrimaryButton(c, "移入分组").Disabled(count == 0)
						if count > 0 && moveBtn.Clicked() {
							a.batchNewGroupName = ""
							a.batchGroupError = ""
							a.showBatchGroupModal = true
						}
					})
				})
		})
}

// --- 弹窗入口汇总 ---

func (a *AppUI) renderShelfOrganizeModals(c *ui.Context, pal theme.Palette) {
	a.renderBatchGroupModal(c, pal)
	a.renderBookActionModal(c, pal)
}

// renderBatchGroupModal 渲染单层极简「移入分组」选择弹窗，支持一键新建分组并直接移入。
func (a *AppUI) renderBatchGroupModal(c *ui.Context, pal theme.Palette) {
	ids := a.selectedShelfBookIDs()
	if len(ids) == 0 {
		a.showBatchGroupModal = false
		return
	}

	components.AnimatedDialog(c, "batch-group-pick", &a.showBatchGroupModal, pal, 400, 520, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Textf(c, "将 %d 本诗集移入分组", len(ids)).
				Font(theme.FontSerif).FontSize(16.5).Bold().TextColor(pal.TextPrimary).Grow(1)
			closeBtn := ui.Box(c.Key("bg-close")).Padding(4).Cursor(ui.CursorPointer)
			theme.ApplyPressScale(closeBtn, 0.90)
			closeBtn.Children(func() {
				ui.Icon(c, components.IconClose).Width(16).Height(16).TextColor(pal.TextMuted)
			})
			if closeBtn.Clicked() {
				a.showBatchGroupModal = false
			}
		})

		ui.Scroll(c.Key("bg-list")).FillWidth().MaxHeight(280).Children(func() {
			ui.Column(c).FillWidth().Gap(6).Children(func() {
				renderOption := func(key, title, sub string, targetID, targetName string) {
					row := ui.Row(c.Key(key)).
						FillWidth().
						Padding(10, 12).
						Gap(8).
						Radius(8).
						Background(pal.Background).
						Border(0.75, pal.Border).
						AlignItems(ui.Center).
						Cursor(ui.CursorPointer)
					theme.ApplyPressScale(row, 0.985)
					row.Children(func() {
						ui.Text(c, title).Font(theme.FontSerif).FontSize(14).Bold().TextColor(pal.TextPrimary).Grow(1).MaxLines(1)
						if sub != "" {
							ui.Text(c, sub).Font(theme.FontMono).FontFeatures("tnum").FontSize(11).TextColor(pal.TextMuted)
						}
						ui.Icon(c, components.IconChevronRight).Width(13).Height(13).TextColor(pal.TextMuted)
					})
					if row.Clicked() {
						a.showBatchGroupModal = false
						a.applyBatchGroupMoveAsync(ids, targetID, targetName, false)
					}
				}

				renderOption("bg-opt-none", "未分组", fmt.Sprintf("%d 本", a.bookCountInGroup("")), "", "")
				for _, g := range a.groups {
					g := g
					renderOption("bg-opt-"+g.ID, g.Name, fmt.Sprintf("%d 本", a.bookCountInGroup(g.ID)), g.ID, g.Name)
				}
			})
		})

		ui.Divider(c)

		// 一步到位：新建分组并直接把选中诗集移入
		ui.Column(c.Key("bg-create-box")).FillWidth().Gap(6).Children(func() {
			ui.Text(c, "新建分组并移入").Font(theme.FontSans).FontSize(12).TextColor(pal.TextSecondary)
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.TextInput(c.Key("bg-new-name"), &a.batchNewGroupName).Placeholder("如：现代诗、枕边重读").Grow(1)
				if ui.PrimaryButton(c, "新建并移入").Clicked() {
					name := strings.TrimSpace(a.batchNewGroupName)
					if name == "" {
						a.batchGroupError = "请输入分组名称"
					} else {
						a.batchGroupError = ""
						a.batchNewGroupName = ""
						a.showBatchGroupModal = false
						a.exitShelfOrganize()
						moveIDs := append([]string(nil), ids...)
						go func() {
							ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							defer cancel()
							grp, err := a.svc.CreateGroup(ctx, name)
							if err != nil {
								a.showTransientToast("新建分组失败：" + err.Error())
								return
							}
							if err := a.svc.SetBooksGroup(ctx, moveIDs, grp.ID); err != nil {
								a.showTransientToast("移入分组失败：" + err.Error())
								return
							}
							a.reloadSync(nil)
							a.showTransientToast(fmt.Sprintf("已新建「%s」并收入 %d 本诗集", grp.Name, len(moveIDs)))
						}()
					}
				}
			})
			if a.batchGroupError != "" {
				ui.Text(c, a.batchGroupError).FontSize(11.5).TextColor(pal.Accent)
			}
		})
	})
}

// renderBookActionModal 渲染长按/右键书封唤出的单本快捷操作弹窗：
// 单层直达所属分组（含未下载的云端诗集），同时提供完读、清缓存、删除与「批量整理书架」入口。
func (a *AppUI) renderBookActionModal(c *ui.Context, pal theme.Palette) {
	book, ok := a.bookByID(a.actionBookID)
	if !ok {
		a.showBookActionModal = false
		return
	}
	prog := a.progressMap[book.ID]
	isFinished := prog.Status == domain.ReadStatusFinished
	isDownloaded := book.LocalState == domain.LocalStateDownloaded

	components.AnimatedDialog(c, "book-quick-actions", &a.showBookActionModal, pal, 420, 560, func() {
		// 顶部书封缩略图 + 书名/作者 + 离线状态
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Start).Children(func() {
			components.BookCoverThumbnail(c, "ba-thumb-"+book.ID, book.ID, a.coverImageFor(book), pal)
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					ui.Text(c, book.Title).Font(theme.FontSerif).FontSize(16.5).Bold().TextColor(pal.TextPrimary).Grow(1).MaxLines(2)
					closeBtn := ui.Box(c.Key("ba-close")).Padding(4).Cursor(ui.CursorPointer)
					theme.ApplyPressScale(closeBtn, 0.90)
					closeBtn.Children(func() {
						ui.Icon(c, components.IconClose).Width(16).Height(16).TextColor(pal.TextMuted)
					})
					if closeBtn.Clicked() {
						a.showBookActionModal = false
					}
				})
				meta := strings.TrimSpace(book.Author)
				if book.PoemCount > 0 {
					if meta != "" {
						meta += " · "
					}
					meta += fmt.Sprintf("%d 首", book.PoemCount)
				}
				if !isDownloaded {
					if meta != "" {
						meta += " · "
					}
					meta += "云端按需下载"
				}
				if meta != "" {
					ui.Text(c, meta).Font(theme.FontNote).FontSize(12).TextColor(pal.TextMuted).MaxLines(1)
				}
			})
		})

		ui.Divider(c)

		// 所属分组（单层胶囊直达，点一下立即归类）
		ui.Column(c.Key("ba-groups")).FillWidth().Gap(8).Children(func() {
			ui.Text(c, "所属分组").Font(theme.FontSerif).FontSize(13.5).Bold().TextColor(pal.TextSecondary)
			ui.Row(c.Key("ba-group-chips")).FillWidth().Gap(7).Wrap().Children(func() {
				renderChip := func(gid, label string) {
					active := book.GroupID == gid
					bg, fg, bd := pal.Background, pal.TextSecondary, pal.Border
					if active {
						bg, fg, bd = pal.Accent, pal.AccentText, pal.Accent
					}
					chip := ui.Box(c.Key("ba-grp-"+gid)).
						Padding(5, 11).
						Radius(999).
						Background(bg).
						Border(1, bd).
						Cursor(ui.CursorPointer).
						Transition(theme.ColorTransition())
					theme.ApplyPressScale(chip, 0.95)
					chip.Children(func() {
						txt := ui.Text(c, label).Font(theme.FontSans).FontSize(12).TextColor(fg)
						if active {
							txt.Bold()
						}
					})
					if chip.Clicked() && !active {
						targetID, targetLabel := gid, label
						go func() {
							ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							defer cancel()
							if err := a.svc.SetBookGroup(ctx, book.ID, targetID); err != nil {
								a.showTransientToast("更新分组失败：" + err.Error())
								return
							}
							a.reloadSync(nil)
							if targetID == "" {
								a.showTransientToast(fmt.Sprintf("已将《%s》移回未分组", book.Title))
							} else {
								a.showTransientToast(fmt.Sprintf("已将《%s》收入「%s」", book.Title, targetLabel))
							}
						}()
					}
				}
				renderChip("", "未分组")
				for _, g := range a.groups {
					renderChip(g.ID, g.Name)
				}
			})

			// 快捷新建分组并直接归入本书
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Padding(2, 0, 0, 0).Children(func() {
				ui.TextInput(c.Key("ba-new-grp"), &a.actionNewGroupName).Placeholder("+ 新建分组并移入本书").Grow(1)
				if ui.Button(c, "新建").Clicked() {
					name := strings.TrimSpace(a.actionNewGroupName)
					if name == "" {
						a.actionGroupError = "分组名称不能为空"
					} else {
						a.actionGroupError = ""
						a.actionNewGroupName = ""
						go func() {
							ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							defer cancel()
							grp, err := a.svc.CreateGroup(ctx, name)
							if err != nil {
								a.showTransientToast("新建分组失败：" + err.Error())
								return
							}
							if err := a.svc.SetBookGroup(ctx, book.ID, grp.ID); err != nil {
								a.showTransientToast("移入分组失败：" + err.Error())
								return
							}
							a.reloadSync(nil)
							a.showTransientToast(fmt.Sprintf("已新建「%s」并收入《%s》", grp.Name, book.Title))
						}()
					}
				}
			})
			if a.actionGroupError != "" {
				ui.Text(c, a.actionGroupError).FontSize(11.5).TextColor(pal.Accent)
			}
		})

		ui.Divider(c)

		// 常用管理操作（标为完读 / 清理缓存 / 删除 / 批量整理）
		ui.Row(c.Key("ba-actions")).FillWidth().Gap(8).Wrap().Children(func() {
			finLabel := "标为已完读"
			if isFinished {
				finLabel = "已完读 ✓"
			}
			if ui.Button(c, finLabel).Clicked() {
				a.toggleBookFinishedAsync(book.ID, book.Title, !isFinished)
			}
			if isDownloaded && ui.Button(c, "清理离线正文").Clicked() {
				a.showBookActionModal = false
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if err := a.svc.ClearBookCache(ctx, book.ID); err != nil {
						a.showTransientToast("无法清理：" + err.Error())
						return
					}
					a.reloadSync(nil)
					a.showTransientToast("已清理本书离线缓存")
				}()
			}
			if ui.Button(c, "批量整理书架").Clicked() {
				a.showBookActionModal = false
				a.enterShelfOrganize(book.ID)
			}
			if ui.Button(c, "删除诗集").Clicked() {
				a.showBookActionModal = false
				if a.activeBookID == book.ID {
					a.mu.Lock()
					a.openSeq++
					a.activeBookID = ""
					a.mu.Unlock()
					a.readerMobile = false
					a.activePoems = nil
					a.readerEntrySource = entryMainline
					a.peekBannerHidden = false
					a.pendingPoemID = ""
				}
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if err := a.svc.DeleteBook(ctx, book.ID); err != nil {
						a.showTransientToast("删除诗集失败：" + err.Error())
						return
					}
					a.reloadSync(nil)
					a.showTransientToast("已删除诗集（相关诗歌节选已保留文字）")
				}()
			}
		})
	})
}
