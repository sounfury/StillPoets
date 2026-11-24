package views

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/mobile"

	"stillpoets/internal/app"
	"stillpoets/internal/domain"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/theme"
)

func (a *AppUI) renderModals(c *ui.Context, pal theme.Palette) {
	a.renderAddModal(c, pal)
	a.renderQuoteEditorModal(c, pal)
	a.renderQuoteDetailModal(c, pal)
	a.renderGroupManagerModal(c, pal)
	a.renderSearchModal(c, pal)
	a.renderTOCModal(c, pal)
	a.renderTypographyModal(c, pal)
}

func (a *AppUI) renderAddModal(c *ui.Context, pal theme.Palette) {
	components.AnimatedSheet(c, &a.showAddModal, pal, []float32{0.44}, "新增", func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "新增").Font(theme.FontSerif).FontSize(17).Bold().TextColor(pal.TextPrimary).Grow(1)
			closeBtn := ui.Box(c.Key("add-close")).Padding(4).Cursor(ui.CursorPointer)
			theme.ApplyPressScale(closeBtn, 0.90)
			closeBtn.Children(func() { ui.Icon(c, components.IconClose).Width(16).Height(16).TextColor(pal.TextMuted) })
			if closeBtn.Clicked() {
				a.showAddModal = false
			}
		})

		renderAction := func(key, title, desc string, onClick func()) {
			row := ui.Column(c.Key(key)).
				FillWidth().
				Padding(12, 14).
				Gap(3).
				Radius(10).
				Background(pal.Background).
				Border(1, pal.Border).
				Cursor(ui.CursorPointer)
			theme.ApplyPressScale(row, 0.98)
			row.Children(func() {
				ui.Text(c, title).FontSize(14).Bold().TextColor(pal.TextPrimary)
				ui.Text(c, desc).FontSize(12).TextColor(pal.TextMuted)
			})
			if row.Clicked() {
				onClick()
			}
		}

		renderAction("act-epub", "导入诗集（EPUB）", "解析本地 EPUB 诗集并自动备份至 WebDAV", func() {
			a.triggerImportEPUB()
		})
		renderAction("act-quote", "记一张散页", "随手记下一句独立散句与原书出处", func() {
			a.openNewQuoteModal()
		})
		renderAction("act-group", "新建 / 管理分组", "自定义书架分类与展示顺序", func() {
			a.showAddModal = false
			a.groupFormError = ""
			a.showGroupModal = true
		})
	})
}

func (a *AppUI) renderQuoteEditorModal(c *ui.Context, pal theme.Palette) {
	components.AnimatedDialog(c, "quote-editor", &a.showQuoteModal, pal, 420, 560, func() {
		title := "记一张散页"
		if a.editingQuoteID != "" {
			title = "编辑散页"
		}
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).Font(theme.FontSerif).FontSize(17).Bold().TextColor(pal.TextPrimary).Grow(1)
			closeBtn := ui.Box(c.Key("qe-close")).Padding(4).Cursor(ui.CursorPointer)
			theme.ApplyPressScale(closeBtn, 0.90)
			closeBtn.Children(func() { ui.Icon(c, components.IconClose).Width(16).Height(16).TextColor(pal.TextMuted) })
			if closeBtn.Clicked() {
				a.showQuoteModal = false
			}
		})

		ui.TextArea(c.Key("qe-content"), &a.quoteFormContent).
			Height(120).
			Placeholder("写下打动你的一句诗或读书摘录…").
			Label("句子正文")
		ui.TextInput(c.Key("qe-author"), &a.quoteFormAuthor).Placeholder("诗人 / 作者（可选）").Label("作者")
		ui.TextInput(c.Key("qe-source"), &a.quoteFormSource).Placeholder("原书出处（可选）").Label("原书出处")

		if a.quoteFormError != "" {
			ui.Text(c, a.quoteFormError).FontSize(12).TextColor(pal.Accent)
		}

		ui.Row(c).FillWidth().Gap(10).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				a.showQuoteModal = false
			}
			if ui.PrimaryButton(c, "保存").Clicked() {
				a.submitQuoteForm()
			}
		})
	})
}

func (a *AppUI) submitQuoteForm() {
	content := strings.TrimSpace(a.quoteFormContent)
	if content == "" {
		a.quoteFormError = "句子正文不能为空"
		return
	}
	editID := a.editingQuoteID
	author := strings.TrimSpace(a.quoteFormAuthor)
	source := strings.TrimSpace(a.quoteFormSource)
	a.showQuoteModal = false
	a.quoteFormError = ""

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var err error
		if editID == "" {
			_, err = a.svc.CreateStandaloneQuote(ctx, content, author, source)
		} else {
			err = a.svc.UpdateQuote(ctx, editID, content, author, source)
		}
		if err != nil {
			a.showTransientToast("保存失败：" + err.Error())
			return
		}
		a.reloadSync(ctx)
		a.showTransientToast("散页已保存")
	}()
}

func (a *AppUI) renderQuoteDetailModal(c *ui.Context, pal theme.Palette) {
	components.AnimatedDialog(c, "quote-detail", &a.showQuoteDetail, pal, 440, 560, func() {
		qv := a.detailQuote
		if qv == nil {
			return
		}
		tag := "独立散句"
		if qv.EffectiveKind == domain.QuoteKindExcerpt {
			tag = "诗歌节选"
		}
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, tag).Font(theme.FontMono).FontSize(11).LetterSpacing(0.6).TextColor(pal.Accent).Grow(1)
			closeBtn := ui.Box(c.Key("qd-close")).Padding(4).Cursor(ui.CursorPointer)
			theme.ApplyPressScale(closeBtn, 0.90)
			closeBtn.Children(func() { ui.Icon(c, components.IconClose).Width(16).Height(16).TextColor(pal.TextMuted) })
			if closeBtn.Clicked() {
				a.showQuoteDetail = false
			}
		})

		ui.Text(c, qv.Quote.Content).
			Font(theme.PoetryFont(a.settings.FontFamily)).
			FontSize(18.5).
			FontWeight(400).
			LineHeight(1.78).
			TextColor(pal.TextPrimary)

		metaParts := []string{}
		if qv.Quote.Author != "" {
			metaParts = append(metaParts, qv.Quote.Author)
		}
		if qv.Quote.SourceBook != "" {
			metaParts = append(metaParts, "《"+qv.Quote.SourceBook+"》")
		}
		if qv.Quote.PoemTitle != "" {
			metaParts = append(metaParts, qv.Quote.PoemTitle)
		}
		if len(metaParts) > 0 {
			ui.Text(c, strings.Join(metaParts, " · ")).Font(theme.FontNote).FontSize(13).TextColor(pal.TextMuted)
		}

		ui.Divider(c)

		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			if qv.EffectiveKind == domain.QuoteKindExcerpt {
				if ui.PrimaryButton(c, "读完整首 ↗").Clicked() {
					a.showQuoteDetail = false
					a.selectShelfSubTab(components.SubTabBooks)
					a.openBookWithSource(qv.Quote.BookID, qv.Quote.PoemID, app.ParseLineRanges(qv.Quote.HighlightLines), true, entryQuote)
				}
			}
			ui.Spacer(c)
			if ui.Button(c, "编辑").Clicked() {
				a.showQuoteDetail = false
				a.editingQuoteID = qv.Quote.ID
				a.quoteFormContent = qv.Quote.Content
				a.quoteFormAuthor = qv.Quote.Author
				a.quoteFormSource = qv.Quote.SourceBook
				a.quoteFormError = ""
				a.showQuoteModal = true
			}
			if ui.Button(c, "删除").Clicked() {
				id := qv.Quote.ID
				a.showQuoteDetail = false
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if err := a.svc.DeleteQuote(ctx, id); err != nil {
						a.showTransientToast("删除失败：" + err.Error())
						return
					}
					a.reloadSync(nil)
					a.showTransientToast("已删除散页")
				}()
			}
		})
	})
}

func (a *AppUI) renderGroupManagerModal(c *ui.Context, pal theme.Palette) {
	components.AnimatedDialog(c, "groups", &a.showGroupModal, pal, 440, 540, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "管理诗集分组").Font(theme.FontSerif).FontSize(17).Bold().TextColor(pal.TextPrimary).Grow(1)
			closeBtn := ui.Box(c.Key("gm-close")).Padding(4).Cursor(ui.CursorPointer)
			theme.ApplyPressScale(closeBtn, 0.90)
			closeBtn.Children(func() { ui.Icon(c, components.IconClose).Width(16).Height(16).TextColor(pal.TextMuted) })
			if closeBtn.Clicked() {
				a.showGroupModal = false
				a.renamingGroupID = ""
			}
		})

		// 新建空组
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c.Key("gm-new-name"), &a.newGroupName).Placeholder("输入新分组名称（如：外国诗）").Grow(1)
			if ui.PrimaryButton(c, "新建").Clicked() {
				name := strings.TrimSpace(a.newGroupName)
				if name == "" {
					a.groupFormError = "分组名称不能为空"
				} else {
					a.newGroupName = ""
					a.groupFormError = ""
					go func() {
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						if _, err := a.svc.CreateGroup(ctx, name); err != nil {
							errMsg := err.Error()
							a.dispatchUI(func() {
								a.groupFormError = errMsg
							})
						}
						a.reloadSync(nil)
					}()
				}
			}
		})

		if a.groupFormError != "" {
			ui.Text(c, a.groupFormError).FontSize(12).TextColor(pal.Accent)
		}

		ui.Scroll(c.Key("gm-list")).Grow(1).FillWidth().Children(func() {
			ui.Column(c).Gap(8).FillWidth().Children(func() {
				if len(a.groups) == 0 {
					ui.Text(c, "暂无自定义分组；默认归入「全部」。").FontSize(13).TextColor(pal.TextMuted).Padding(12, 0)
					return
				}
				for i, g := range a.groups {
					i, g := i, g
					ui.Column(c.Key("gitem-"+g.ID)).
						FillWidth().
						Padding(10, 12).
						Gap(8).
						Radius(8).
						Background(pal.Background).
						Border(1, pal.Border).
						Children(func() {
							if a.renamingGroupID == g.ID {
								ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
									ui.TextInput(c.Key("gm-ren-"+g.ID), &a.renamingGroupTx).Grow(1)
									if ui.PrimaryButton(c, "确定").Clicked() {
										newName := strings.TrimSpace(a.renamingGroupTx)
										a.renamingGroupID = ""
										if newName != "" {
											go func() {
												ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
												defer cancel()
												_ = a.svc.RenameGroup(ctx, g.ID, newName)
												a.reloadSync(ctx)
												a.requestRepaint()
											}()
										}
									}
								})
							} else {
								ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
									ui.Text(c, g.Name).FontSize(14).Bold().TextColor(pal.TextPrimary).Grow(1)
									if i > 0 && ui.Button(c, "↑").Clicked() {
										a.moveGroupByID(g.ID, -1)
									}
									if i+1 < len(a.groups) && ui.Button(c, "↓").Clicked() {
										a.moveGroupByID(g.ID, +1)
									}
									if ui.Button(c, "重命名").Clicked() {
										a.renamingGroupID = g.ID
										a.renamingGroupTx = g.Name
									}
									if ui.Button(c, "解散").Clicked() {
										if a.selectedGrp == g.ID {
											a.selectedGrp = ""
										}
										go func() {
											ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
											defer cancel()
											_ = a.svc.DissolveGroup(ctx, g.ID)
											a.reloadSync(ctx)
											a.requestRepaint()
										}()
									}
								})
							}
						})
				}
			})
		})
	})
}

func (a *AppUI) moveGroupByID(groupID string, delta int) {
	ids := make([]string, len(a.groups))
	pos := -1
	for idx, g := range a.groups {
		ids[idx] = g.ID
		if g.ID == groupID {
			pos = idx
		}
	}
	target := pos + delta
	if pos < 0 || target < 0 || target >= len(ids) {
		return
	}
	ids[pos], ids[target] = ids[target], ids[pos]
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.svc.ReorderGroups(ctx, ids)
		a.reloadSync(nil)
	}()
}

func (a *AppUI) renderSearchModal(c *ui.Context, pal theme.Palette) {
	components.AnimatedDialog(c, "search", &a.showSearchModal, pal, 480, 560, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "全局检索").FontSize(17).Bold().TextColor(pal.TextPrimary).Grow(1)
			closeBtn := ui.Box(c.Key("srch-close")).Padding(4).Cursor(ui.CursorPointer)
			theme.ApplyPressScale(closeBtn, 0.90)
			closeBtn.Children(func() { ui.Icon(c, components.IconClose).Width(16).Height(16).TextColor(pal.TextMuted) })
			if closeBtn.Clicked() {
				a.showSearchModal = false
			}
		})

		if ui.SearchField(c.Key("global-search-input"), &a.searchQuery).Placeholder("搜已缓存诗句、诗题或散页…").Changed() {
			q := a.searchQuery
			a.mu.Lock()
			a.searchSeq++
			reqSeq := a.searchSeq
			a.mu.Unlock()
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				res, err := a.svc.Search(ctx, q, 20)
				if err != nil {
					return
				}
				a.dispatchUI(func() {
					a.mu.Lock()
					match := a.searchSeq == reqSeq
					a.mu.Unlock()
					if match {
						a.searchResults = res
					}
				})
			}()
		}

		ui.Scroll(c.Key("search-results-scroll")).Grow(1).FillWidth().Children(func() {
			ui.Column(c).Gap(10).FillWidth().Children(func() {
				if strings.TrimSpace(a.searchQuery) == "" {
					ui.Text(c, "支持中文全文检索（SQLite FTS5），同时搜索已离线诗集与所有散页。").
						FontSize(12).TextColor(pal.TextMuted).Padding(8, 0)
					return
				}
				if len(a.searchResults.Poems) == 0 && len(a.searchResults.Quotes) == 0 {
					ui.Text(c, "未找到匹配内容").FontSize(13).TextColor(pal.TextMuted).Padding(8, 0)
					return
				}
				if len(a.searchResults.Poems) > 0 {
					ui.Textf(c, "命中诗篇（%d）", len(a.searchResults.Poems)).FontSize(12).Bold().TextColor(pal.TextSecondary)
					for _, p := range a.searchResults.Poems {
						p := p
						row := ui.Column(c.Key("srch-p-"+p.ID)).
							FillWidth().
							Padding(10, 12).
							Gap(3).
							Radius(6).
							Background(pal.Background).
							Cursor(ui.CursorPointer)
						theme.ApplyPressScale(row, 0.985)
						row.Children(func() {
							ui.Text(c, p.Title).FontSize(14).Bold().TextColor(pal.TextPrimary)
							if p.Author != "" {
								ui.Text(c, p.Author).FontSize(12).TextColor(pal.TextMuted)
							}
						})
						if row.Clicked() {
							a.showSearchModal = false
							a.activeTab = components.TabShelf
							a.selectShelfSubTab(components.SubTabBooks)
							a.openBookWithSource(p.BookID, p.ID, nil, true, entrySearch)
						}
					}
				}
				if len(a.searchResults.Quotes) > 0 {
					ui.Textf(c, "命中散页（%d）", len(a.searchResults.Quotes)).FontSize(12).Bold().TextColor(pal.TextSecondary).Padding(6, 0, 0, 0)
					for _, qv := range a.searchResults.Quotes {
						qv := qv
						row := ui.Column(c.Key("srch-q-"+qv.Quote.ID)).
							FillWidth().
							Padding(10, 12).
							Gap(3).
							Radius(6).
							Background(pal.Background).
							Cursor(ui.CursorPointer)
						theme.ApplyPressScale(row, 0.985)
						row.Children(func() {
							ui.Text(c, qv.Quote.Content).FontSize(13).TextColor(pal.TextPrimary).MaxLines(2)
							ui.Textf(c, "%s · %s", qv.Quote.Author, qv.Quote.SourceBook).FontSize(11).TextColor(pal.TextMuted)
						})
						if row.Clicked() {
							a.showSearchModal = false
							cp := qv
							a.detailQuote = &cp
							a.showQuoteDetail = true
						}
					}
				}
			})
		})
	})
}

func (a *AppUI) renderTOCModal(c *ui.Context, pal theme.Palette) {
	components.AnimatedSheet(c, &a.showTOCModal, pal, []float32{0.66, 0.92}, "诗集目录", func() {
		book, ok := a.activeBook()
		if !ok {
			return
		}
		prog := a.progressMap[book.ID]
		isFinished := prog.Status == domain.ReadStatusFinished

		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, book.Title).FontSize(17).Bold().TextColor(pal.TextPrimary)
				ui.Textf(c, "%s · 共 %d 篇", book.Author, len(a.activePoems)).FontSize(12).TextColor(pal.TextMuted)
			})
			closeBtn := ui.Box(c.Key("toc-close")).Padding(4).Cursor(ui.CursorPointer)
			theme.ApplyPressScale(closeBtn, 0.90)
			closeBtn.Children(func() { ui.Icon(c, components.IconClose).Width(16).Height(16).TextColor(pal.TextMuted) })
			if closeBtn.Clicked() {
				a.showTOCModal = false
			}
		})

		// 书本快捷管理行（完读 / 移入分组 / 清理缓存 / 删除）
		ui.Row(c.Key("toc-book-actions")).Gap(8).Wrap().Children(func() {
			finLabel := "标记已完读"
			if isFinished {
				finLabel = "已完读 ✓"
			}
			if ui.Button(c, finLabel).Clicked() {
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					_, _ = a.svc.MarkBookFinished(ctx, book.ID, !isFinished)
					a.reloadSync(ctx)
				}()
			}
			if ui.Button(c, "清理离线正文").Clicked() {
				a.showTOCModal = false
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
			if ui.Button(c, "删除诗集").Clicked() {
				a.showTOCModal = false
				a.mu.Lock()
				a.openSeq++
				a.activeBookID = ""
				a.mu.Unlock()
				a.readerMobile = false
				a.activePoems = nil
				a.readerEntrySource = entryMainline
				a.peekBannerHidden = false
				a.pendingPoemID = ""
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

		// 移入分组切换
		if len(a.groups) > 0 {
			ui.Row(c.Key("toc-group-assign")).Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "分组：").FontSize(12).TextColor(pal.TextMuted)
				renderAssign := func(gid, label string) {
					active := book.GroupID == gid
					bg, fg, bd := pal.Background, pal.TextSecondary, pal.Border
					if active {
						bg, fg, bd = pal.Accent, pal.AccentText, pal.Accent
					}
					chip := ui.Box(c.Key("asg-"+gid)).Padding(4, 9).Radius(999).Background(bg).Border(1, bd).Cursor(ui.CursorPointer).
						Transition(theme.ColorTransition())
					chip.Children(func() { ui.Text(c, label).FontSize(11).TextColor(fg) })
					if chip.Clicked() {
						go func() {
							ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							defer cancel()
							_ = a.svc.SetBookGroup(ctx, book.ID, gid)
							a.reloadSync(ctx)
						}()
					}
				}
				renderAssign("", "未分组")
				for _, g := range a.groups {
					renderAssign(g.ID, g.Name)
				}
			})
		}

		ui.Divider(c)

		// 诗集目录列表
		ui.Scroll(c.Key("toc-list-scroll")).Grow(1).FillWidth().Children(func() {
			ui.Column(c).Gap(4).FillWidth().Children(func() {
				for i, p := range a.activePoems {
					i, p := i, p
					isCurrent := i == a.activePoemIdx
					isMainline := prog.LastPoemID != "" && p.ID == prog.LastPoemID
					_, isFav := a.activeFavorites[p.ID]
					bg := ui.Transparent
					if isCurrent {
						bg = pal.HighlightBg
					}
					row := ui.Row(c.Key(fmt.Sprintf("toc-item-%d", i))).
						FillWidth().
						Padding(8, 10).
						Gap(8).
						Radius(6).
						Background(bg).
						AlignItems(ui.Center).
						Cursor(ui.CursorPointer)
					row.Children(func() {
						ui.Textf(c, "%d.", i+1).FontSize(12).TextColor(pal.TextMuted).Width(28)
						titleEl := ui.Text(c, p.Title).FontSize(13).MaxLines(1).Grow(1)
						if isCurrent {
							titleEl.Bold().TextColor(pal.Accent)
						} else {
							titleEl.TextColor(pal.TextPrimary)
						}
						if isMainline {
							ui.Box(c).Padding(2, 6).Radius(999).Border(1, pal.Accent).Children(func() {
								ui.Text(c, "续读").FontSize(10).TextColor(pal.Accent)
							})
						}
						if !p.IsPoem() {
							ui.Text(c, "附文").FontSize(10).TextColor(pal.TextMuted)
						}
						if isFav {
							ui.Icon(c, components.IconHeartFilled).Width(13).Height(13).TextColor(pal.Accent)
						}
					})
					if row.Clicked() {
						a.showTOCModal = false
						if a.readerEntrySource.isPeek() && isMainline {
							a.returnToMainlineProgress(book.ID)
						} else {
							a.navigatePoem(book.ID, i)
						}
					}
				}
			})
		})
	})
}

func (a *AppUI) renderTypographyModal(c *ui.Context, pal theme.Palette) {
	components.AnimatedSheet(c, &a.showTypoModal, pal, []float32{0.62, 0.85}, "排版与外观", func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "排版与外观").FontSize(17).Bold().TextColor(pal.TextPrimary).Grow(1)
			closeBtn := ui.Box(c.Key("typo-close")).Padding(4).Cursor(ui.CursorPointer)
			theme.ApplyPressScale(closeBtn, 0.90)
			closeBtn.Children(func() { ui.Icon(c, components.IconClose).Width(16).Height(16).TextColor(pal.TextMuted) })
			if closeBtn.Clicked() {
				a.showTypoModal = false
			}
		})

		ui.Scroll(c.Key("typo-modal-scroll")).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(12).Padding(0, 0, 8, 0).Children(func() {
				ui.Row(c.Key("typo-themes")).FillWidth().Gap(8).Children(func() {
					for _, preset := range theme.Presets() {
						active := pal.ID == preset.ID
						btn := ui.Box(c.Key("tp-" + string(preset.ID))).
							Grow(1).
							Padding(8).
							Center().
							Radius(8).
							Background(preset.Background).
							Cursor(ui.CursorPointer).
							Transition(theme.ColorTransition())
						theme.ApplyPressScale(btn, 0.95)
						if active {
							btn.Border(2, pal.Accent)
						} else {
							btn.Border(1, preset.Border)
						}
						btn.Children(func() {
							ui.Text(c, preset.Name).FontSize(12).Bold().TextColor(preset.TextPrimary)
						})
						if btn.Clicked() {
							a.applyThemePreset(preset)
						}
					}
				})

				ui.Row(c.Key("typo-follow-dark")).FillWidth().AlignItems(ui.Center).Children(func() {
					followDark := a.settings.ShouldFollowSystemDark()
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						ui.Text(c, "跟随系统深色模式").FontSize(13).TextColor(pal.TextSecondary)
						hint := "深色模式自动切换为「暗夜极简」"
						if followDark && a.systemDark {
							hint = "当前已随系统切换为「暗夜极简」"
						} else if !followDark {
							hint = "已关闭自动切换"
						}
						ui.Text(c, hint).FontSize(11).TextColor(pal.TextMuted)
					})
					if mobile.Switch(c.Key("sw-typo-follow-dark"), &followDark).Changed() {
						a.toggleFollowSystemDark(followDark)
					}
				})

				ui.Column(c).Gap(6).Children(func() {
					ui.Textf(c, "字号：%.0f pt", a.fontSizeSlider).FontSize(13).TextColor(pal.TextSecondary)
					if ui.Slider(c.Key("typo-fs"), &a.fontSizeSlider, 14, 28).Changed() {
						a.saveTypography(float32(a.fontSizeSlider), float32(a.lineHeightSlider))
					}
				})

				ui.Column(c).Gap(6).Children(func() {
					ui.Textf(c, "行距：%.2f", a.lineHeightSlider).FontSize(13).TextColor(pal.TextSecondary)
					if ui.Slider(c.Key("typo-lh"), &a.lineHeightSlider, 1.4, 2.4).Changed() {
						a.saveTypography(float32(a.fontSizeSlider), float32(a.lineHeightSlider))
					}
				})

				// 正文字重四档快选
				ui.Column(c).FillWidth().Gap(6).Children(func() {
					curWeight := a.settings.EffectiveFontWeight()
					ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
						ui.Text(c, "正文字重").FontSize(13).TextColor(pal.TextSecondary).Grow(1)
						ui.Text(c, formatFontWeightLabel(curWeight)).Font(theme.FontMono).FontSize(11).TextColor(pal.Accent)
					})
					a.renderFontWeightSegmented(c, pal, "typo-fw")
				})

				// 字体快选 + 导入
				ui.Column(c).FillWidth().Gap(8).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
						ui.Text(c, "阅读字体").FontSize(13).TextColor(pal.TextSecondary).Grow(1)
						if ui.Button(c, "+ 导入字体").Clicked() {
							a.triggerImportFont()
						}
					})
					ui.Row(c.Key("typo-font-chips")).Gap(8).Wrap().Children(func() {
						renderChip := func(family, label string) {
							active := a.settings.FontFamily == family
							bg, fg, bd := pal.Background, pal.TextSecondary, pal.Border
							if active {
								bg, fg, bd = pal.Accent, pal.AccentText, pal.Accent
							}
							chip := ui.Box(c.Key("tf-"+label)).Padding(5, 10).Radius(999).Background(bg).Border(1, bd).Cursor(ui.CursorPointer).
								Transition(theme.ColorTransition())
							theme.ApplyPressScale(chip, 0.96)
							chip.Children(func() {
								el := ui.Text(c, label).FontSize(12).TextColor(fg)
								if family != "" {
									el.Font(family)
								}
							})
							if chip.Clicked() {
								a.applyFontFamily(family)
							}
						}
						renderChip("", "系统默认")
						for _, lf := range a.localFonts {
							renderChip(lf.Family, lf.Family)
						}
					})
				})
			})
		})
	})
}
