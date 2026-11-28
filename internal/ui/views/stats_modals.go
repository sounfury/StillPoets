package views

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
	"stillpoets/internal/domain"
	"stillpoets/internal/storage"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/theme"
)

func (a *AppUI) renderStatsModals(c *ui.Context, pal theme.Palette) {
	a.renderFinishedBookModal(c, pal)
	a.renderAuthorProfileModal(c, pal)
}

func (a *AppUI) renderFinishedBookModal(c *ui.Context, pal theme.Palette) {
	item := a.detailFinishedBook
	if item == nil {
		a.showFinishedModal = false
		return
	}
	b := item.Book
	prog := item.Progress

	components.AnimatedDialog(c, "finished-book-detail", &a.showFinishedModal, pal, 440, 600, func() {
		// 顶部书封 + 书名 + 完读日期
		ui.Row(c).FillWidth().Gap(14).AlignItems(ui.Start).Children(func() {
			components.BookCoverThumbnail(c, "fin-dlg-cov-"+b.ID, b.ID, a.coverImageFor(b), pal)
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					ui.Text(c, b.Title).Font(theme.FontSerif).FontSize(17).Bold().TextColor(pal.TextPrimary).Grow(1).MaxLines(2)
					closeBtn := ui.Box(c.Key("fin-dlg-close")).Padding(4).Cursor(ui.CursorPointer)
					theme.ApplyPressScale(closeBtn, 0.90)
					closeBtn.Children(func() {
						ui.Icon(c, components.IconClose).Width(16).Height(16).TextColor(pal.TextMuted)
					})
					if closeBtn.Clicked() {
						a.showFinishedModal = false
					}
				})
				if b.Author != "" {
					ui.Text(c, b.Author).Font(theme.FontNote).FontSize(12.5).TextColor(pal.TextSecondary)
				}
				ui.Text(c, formatFinishedFullDate(prog.FinishedAt)).
					Font(theme.FontMono).FontFeatures("tnum").FontSize(11).TextColor(pal.Accent)
			})
		})

		// 三项回望指标
		ui.Row(c.Key("fin-dlg-metrics")).FillWidth().Gap(8).Children(func() {
			renderChip := func(key, label, val string) {
				ui.Column(c.Key(key)).Grow(1).Padding(9, 10).Gap(2).Radius(7).Background(pal.Background).Border(0.75, pal.Border).Children(func() {
					ui.Text(c, label).Font(theme.FontSans).FontSize(10.5).TextColor(pal.TextMuted)
					ui.Text(c, val).Font(theme.FontSerif).FontFeatures("tnum").FontSize(14).Bold().TextColor(pal.TextPrimary)
				})
			}
			renderChip("fm-read", "已读诗篇", fmt.Sprintf("%d / %d", prog.ReadPoemCount(), b.PoemCount))
			renderChip("fm-fav", "诗篇精选", fmt.Sprintf("%d 首", item.FavoriteCount))
			renderChip("fm-qt", "留存散页", fmt.Sprintf("%d 张", item.QuoteCount))
		})

		// 本书精选诗篇快速回看
		if len(item.TopFavorites) > 0 {
			ui.Text(c, "本书精选").Font(theme.FontSerif).FontSize(13.5).Bold().TextColor(pal.TextSecondary)
			ui.Scroll(c.Key("fin-dlg-favs-scroll")).FillWidth().MaxHeight(230).Children(func() {
				ui.Column(c).FillWidth().Gap(7).Children(func() {
					for i, fav := range item.TopFavorites {
						fav := fav
						row := ui.Column(c.Key(fmt.Sprintf("fin-fav-%d-%s", i, fav.PoemID))).
							FillWidth().
							Padding(9, 11).
							Gap(3).
							Radius(7).
							Background(pal.Background).
							Border(0.75, pal.Border).
							Cursor(ui.CursorPointer)
						theme.ApplyPressScale(row, 0.985)
						row.Children(func() {
							ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
								ui.Icon(c, components.IconHeartFilled).Width(11.5).Height(11.5).TextColor(pal.Accent)
								title := fav.PoemTitle
								if title == "" {
									title = "精选诗篇"
								}
								ui.Text(c, title).Font(theme.FontSerif).FontSize(13).Bold().TextColor(pal.TextPrimary).Grow(1).MaxLines(1)
								ui.Icon(c, components.IconChevronRight).Width(12).Height(12).TextColor(pal.TextMuted)
							})
							if ex := strings.TrimSpace(fav.Excerpt); ex != "" {
								ui.Text(c, ex).Font(theme.FontNote).FontSize(11.5).LineHeight(1.5).MaxLines(2).TextColor(pal.TextSecondary)
							}
						})
						if row.Clicked() {
							a.jumpToPoemFromStats(b.ID, fav.PoemID, app.ParseLineRanges(fav.HighlightLines), !a.lastIsTablet)
						}
					}
				})
			})
		}

		ui.Divider(c)

		// 底部操作：完读纪念卡 / 精选合辑 + 改为重读中 / 重读诗集
		a.renderFinishedBookModalActions(c, item)
	})
}

func (a *AppUI) renderFinishedBookModalActions(c *ui.Context, item *storage.FinishedBookItem) {
	b := item.Book
	ui.Column(c.Key("fin-dlg-actions")).FillWidth().Gap(8).Children(func() {
		ui.Row(c.Key("fin-dlg-card-row")).FillWidth().Gap(8).Children(func() {
			if ui.Button(c, "完读纪念卡").Grow(1).Clicked() {
				a.openFinishedShareCard(*item)
			}
			if item.FavoriteCount > 0 {
				if ui.Button(c, "精选合辑卡").Grow(1).Clicked() {
					a.openBookCollectionShareCardAsync(b, item.TopFavorites)
				}
			}
		})
		ui.Row(c.Key("fin-dlg-nav-row")).FillWidth().Gap(10).Justify(ui.End).Children(func() {
			if ui.Button(c, "改为重读中").Clicked() {
				a.showFinishedModal = false
				a.toggleBookFinishedAsync(b.ID, b.Title, false)
			}
			if ui.PrimaryButton(c, "重读诗集").Clicked() {
				a.showFinishedModal = false
				a.activeTab = components.TabShelf
				a.shelfSubTab = components.SubTabBooks
				a.shelfPager.scopeKey = ""
				a.openBook(b.ID, "", nil, !a.lastIsTablet)
			}
		})
	})
}

func (a *AppUI) openBookCollectionShareCardAsync(book domain.Book, fallback []domain.FavoritePoem) {
	fallbackCopy := append([]domain.FavoritePoem(nil), fallback...)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		favs, err := a.svc.ListFavorites(ctx, book.ID)
		if err != nil || len(favs) == 0 {
			favs = fallbackCopy
		}
		a.dispatchUI(func() {
			a.openCollectionShareCard(book, favs)
		})
	}()
}

func (a *AppUI) renderAuthorProfileModal(c *ui.Context, pal theme.Palette) {
	components.AnimatedDialog(c, "author-profile", &a.showAuthorModal, pal, 480, 620, func() {
		prof := a.detailAuthorProfile

		// 标题栏
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				name := a.detailAuthorName
				if prof != nil && prof.Author != "" {
					name = prof.Author
				}
				ui.Text(c, name).Font(theme.FontSerif).FontSize(20).Bold().TextColor(pal.TextPrimary)
				if prof != nil {
					ui.Textf(c, "读过 %d 首 · %d 部诗集 · 精选 %d · 散页 %d",
						prof.ReadPoemCount, prof.BookCount, prof.FavoriteCount, prof.QuoteCount).
						Font(theme.FontMono).FontFeatures("tnum").FontSize(11).TextColor(pal.TextMuted)
				}
			})
			closeBtn := ui.Box(c.Key("au-dlg-close")).Padding(4).Cursor(ui.CursorPointer)
			theme.ApplyPressScale(closeBtn, 0.90)
			closeBtn.Children(func() {
				ui.Icon(c, components.IconClose).Width(16).Height(16).TextColor(pal.TextMuted)
			})
			if closeBtn.Clicked() {
				a.showAuthorModal = false
			}
		})

		ui.Divider(c)

		if a.detailAuthorLoading {
			ui.Row(c).FillWidth().Padding(24, 0).Center().Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "正在整理诗人档案…").FontSize(13).TextColor(pal.TextMuted)
			})
			return
		}
		if prof == nil {
			ui.Text(c, "暂无该诗人的更多阅读记录。").Font(theme.FontNote).FontSize(13).TextColor(pal.TextMuted).Padding(12, 0)
			return
		}

		ui.Scroll(c.Key("au-profile-scroll")).FillWidth().MaxHeight(460).Children(func() {
			ui.Column(c).FillWidth().Gap(16).Padding(4, 0, 12, 0).Children(func() {
				// 1. 分布诗集
				if len(prof.Books) > 0 {
					ui.Column(c.Key("au-books")).FillWidth().Gap(8).Children(func() {
						ui.Text(c, "分布诗集").Font(theme.FontSerif).FontSize(13.5).Bold().TextColor(pal.TextSecondary)
						for _, bk := range prof.Books {
							bk := bk
							row := ui.Row(c.Key("au-bk-"+bk.Book.ID)).
								FillWidth().
								Padding(9, 11).
								Gap(10).
								Radius(8).
								Background(pal.Background).
								Border(0.75, pal.Border).
								AlignItems(ui.Center).
								Cursor(ui.CursorPointer)
							theme.ApplyPressScale(row, 0.985)
							row.Children(func() {
								components.BookCoverThumbnail(c, "au-th-"+bk.Book.ID, bk.Book.ID, a.coverImageFor(bk.Book), pal)
								ui.Column(c).Grow(1).Gap(2).Children(func() {
									ui.Text(c, bk.Book.Title).Font(theme.FontSerif).FontSize(13.5).Bold().TextColor(pal.TextPrimary).MaxLines(1)
									ui.Textf(c, "读过 %d 首 · 精选 %d 首", bk.ReadCount, bk.FavoriteCount).
										Font(theme.FontMono).FontFeatures("tnum").FontSize(11).TextColor(pal.TextMuted)
								})
								ui.Icon(c, components.IconChevronRight).Width(13).Height(13).TextColor(pal.TextMuted)
							})
							if row.Clicked() {
								a.showAuthorModal = false
								a.activeTab = components.TabShelf
								a.shelfSubTab = components.SubTabBooks
								a.shelfPager.scopeKey = ""
								a.openBook(bk.Book.ID, "", nil, !a.lastIsTablet)
							}
						}
					})
				}

				// 2. 精选诗篇 ❤️
				if len(prof.Favorites) > 0 {
					ui.Column(c.Key("au-favs")).FillWidth().Gap(8).Children(func() {
						ui.Text(c, "精选诗篇").Font(theme.FontSerif).FontSize(13.5).Bold().TextColor(pal.TextSecondary)
						for i, fav := range prof.Favorites {
							fav := fav
							card := ui.Column(c.Key(fmt.Sprintf("au-fav-%d-%s", i, fav.PoemID))).
								FillWidth().
								Padding(10, 12).
								Gap(4).
								Radius(8).
								Background(pal.Background).
								Border(0.75, pal.Border).
								Cursor(ui.CursorPointer)
							theme.ApplyPressScale(card, 0.985)
							card.Children(func() {
								ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
									ui.Icon(c, components.IconHeartFilled).Width(12).Height(12).TextColor(pal.Accent)
									title := fav.PoemTitle
									if title == "" {
										title = "无题"
									}
									ui.Text(c, title).Font(theme.FontSerif).FontSize(13.5).Bold().TextColor(pal.TextPrimary).Grow(1).MaxLines(1)
									if fav.BookLocalState != domain.LocalStateDownloaded {
										ui.Icon(c, components.IconCloud).Width(12).Height(12).TextColor(pal.TextMuted)
									}
									ui.Text(c, fmt.Sprintf("《%s》", fav.BookTitle)).Font(theme.FontNote).FontSize(11).TextColor(pal.TextMuted).MaxLines(1)
								})
								if ex := strings.TrimSpace(fav.Excerpt); ex != "" {
									ui.Text(c, ex).Font(theme.FontNote).FontSize(12).LineHeight(1.55).MaxLines(2).TextColor(pal.TextSecondary)
								}
							})
							if card.Clicked() {
								a.jumpToPoemFromStats(fav.BookID, fav.PoemID, app.ParseLineRanges(fav.HighlightLines), !a.lastIsTablet)
							}
						}
					})
				}

				// 3. 留存散页（诗歌节选与独立散句）
				if len(prof.Quotes) > 0 {
					ui.Column(c.Key("au-quotes")).FillWidth().Gap(8).Children(func() {
						ui.Text(c, "留存散页").Font(theme.FontSerif).FontSize(13.5).Bold().TextColor(pal.TextSecondary)
						for i, qv := range prof.Quotes {
							qv := qv
							card := ui.Column(c.Key(fmt.Sprintf("au-qt-%d-%s", i, qv.Quote.ID))).
								FillWidth().
								Padding(10, 12).
								Gap(5).
								Radius(8).
								Background(pal.Background).
								Border(0.75, pal.Border).
								Cursor(ui.CursorPointer)
							theme.ApplyPressScale(card, 0.985)
							card.Children(func() {
								ui.Text(c, qv.Quote.Content).Font(theme.FontSerif).FontSize(13).LineHeight(1.55).MaxLines(3).TextColor(pal.TextPrimary)
								src := strings.TrimSpace(qv.Quote.SourceBook)
								if qv.Quote.PoemTitle != "" {
									if src != "" {
										src = fmt.Sprintf("《%s》· %s", src, qv.Quote.PoemTitle)
									} else {
										src = qv.Quote.PoemTitle
									}
								} else if src != "" {
									src = fmt.Sprintf("《%s》", src)
								} else {
									src = "独立散句"
								}
								ui.Text(c, src).Font(theme.FontNote).FontSize(11).TextColor(pal.TextMuted)
							})
							if card.Clicked() {
								if qv.EffectiveKind == domain.QuoteKindExcerpt {
									a.jumpToPoemFromStats(qv.Quote.BookID, qv.Quote.PoemID, app.ParseLineRanges(qv.Quote.HighlightLines), !a.lastIsTablet)
								} else {
									cp := qv
									a.showAuthorModal = false
									a.detailQuote = &cp
									a.showQuoteDetail = true
								}
							}
						}
					})
				}

				// 4. 近期读过
				if len(prof.RecentReads) > 0 {
					ui.Column(c.Key("au-recent")).FillWidth().Gap(6).Children(func() {
						ui.Text(c, "近期读过").Font(theme.FontSerif).FontSize(13.5).Bold().TextColor(pal.TextSecondary)
						for i, it := range prof.RecentReads {
							it := it
							row := ui.Row(c.Key(fmt.Sprintf("au-rd-%d-%s", i, it.Event.ID))).
								FillWidth().
								Padding(8, 11).
								Gap(8).
								Radius(7).
								Background(pal.Background).
								Border(0.75, pal.Border).
								AlignItems(ui.Center).
								Cursor(ui.CursorPointer)
							theme.ApplyPressScale(row, 0.985)
							row.Children(func() {
								ui.Column(c).Grow(1).Gap(2).Children(func() {
									title := it.Event.PoemTitle
									if title == "" {
										title = "无题"
									}
									ui.Text(c, title).Font(theme.FontSerif).FontSize(13).Bold().TextColor(pal.TextPrimary).MaxLines(1)
									ui.Text(c, fmt.Sprintf("《%s》", it.BookTitle)).Font(theme.FontNote).FontSize(11).TextColor(pal.TextMuted).MaxLines(1)
								})
								ui.Text(c, formatCNDate(it.Event.ReadDate)).Font(theme.FontMono).FontFeatures("tnum").FontSize(10.5).TextColor(pal.TextMuted)
								ui.Icon(c, components.IconChevronRight).Width(12).Height(12).TextColor(pal.TextMuted)
							})
							if row.Clicked() {
								a.jumpToPoemFromStats(it.Event.BookID, it.Event.PoemID, nil, !a.lastIsTablet)
							}
						}
					})
				}
			})
		})
	})
}
