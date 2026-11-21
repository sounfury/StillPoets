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
	"stillpoets/internal/ui/layout"
	"stillpoets/internal/ui/theme"
)

func (a *AppUI) renderShelfPane(c *ui.Context, pal theme.Palette, isTablet bool) {
	ui.Column(c.Key("shelf-pane")).Fill().Children(func() {
		components.BrandHeader(c, pal, "StillPoets", true,
			func() {
				a.showSearchModal = true
			},
			func() {
				a.showAddModal = true
			},
		)
		components.ShelfSubTabs(c, pal, a.shelfSubTab, func(sub components.ShelfSubTab) {
			a.shelfSubTab = sub
			if sub == components.SubTabQuotes {
				go a.reloadQuotesOnly()
			}
		})

		ui.Scroll(c.Key("shelf-content-scroll")).Grow(1).FillWidth().Children(func() {
			if a.shelfSubTab == components.SubTabBooks {
				a.renderBooksSubTab(c, pal, isTablet)
			} else {
				a.renderQuotesSubTab(c, pal, isTablet)
			}
		})
	})
}

func (a *AppUI) renderBooksSubTab(c *ui.Context, pal theme.Palette, isTablet bool) {
	ui.Column(c.Key("books-subtab")).Padding(14, 18, 88, 18).Gap(18).FillWidth().Children(func() {
		// 手机单栏在首屏展示「今日重读」画册/刊物主视觉卡；平板左主栏聚焦继续阅读 + 书架网格（右栏已是实时整诗阅读区）
		if !isTablet {
			a.renderTodayRereadHero(c, pal)
		}

		// 继续阅读
		if a.continueItem != nil {
			a.renderContinueReading(c, pal, a.continueItem)
		}

		// 我的诗集 + 管理分组 + 横向分组直达条
		a.renderMyBooksSection(c, pal, isTablet)
	})
}

func (a *AppUI) renderTodayRereadHero(c *ui.Context, pal theme.Palette) {
	today := a.todayReread
	if today == nil {
		// 空书库引导卡
		ui.Column(c.Key("hero-empty")).
			FillWidth().
			Padding(20).
			Gap(12).
			Radius(6).
			Background(pal.HeroBg).
			Border(1, pal.Border).
			Children(func() {
				ui.Text(c, "今日重读  |  静候第一卷诗集").FontSize(12).TextColor(pal.TextSecondary)
				ui.Text(c, "不急着读完，\n给一句诗多一点时间。").
					FontSize(22).Bold().LineHeight(1.5).TextColor(pal.TextPrimary)
				ui.Row(c).Gap(10).Padding(6, 0, 0, 0).Children(func() {
					if ui.PrimaryButton(c, "导入 EPUB 诗集").Clicked() {
						a.triggerImportEPUB()
					}
					if ui.Button(c, "记一张散页").Clicked() {
						a.openNewQuoteModal()
					}
				})
			})
		return
	}

	if pal.HeroStyle == theme.HeroOpenPaper {
		// 刊物风（Editorial）：无底色块，左侧刊物蓝细竖条 + 开放纸面大字
		ui.Row(c.Key("hero-editorial")).FillWidth().Gap(14).Padding(6, 0, 8, 0).Children(func() {
			ui.Box(c).Width(3).FillHeight().Background(pal.HeroCorner).Radius(2)
			ui.Column(c).Grow(1).Gap(10).Children(func() {
				ui.Text(c, "今日重读  |  来自我的诗集").FontSize(12).TextColor(pal.TextMuted)
				ui.Text(c, today.Excerpt).
					FontSize(19).FontWeight(500).LineHeight(1.52).MaxLines(4).TextColor(pal.TextPrimary)
				ui.Column(c).Gap(2).Children(func() {
					ui.Text(c, today.Author).FontSize(13).Bold().MaxLines(1).TextColor(pal.TextPrimary)
					ui.Textf(c, "《%s》· %s", today.BookTitle, today.PoemTitle).FontSize(12).MaxLines(1).TextColor(pal.TextMuted)
				})
				a.renderHeroActions(c, pal, today)
			})
		})
		return
	}

	// 画册风 / 暗夜极简：鼠尾草绿矩形色块 + 右上赤陶几何角标
	hero := ui.Column(c.Key("hero-artbook")).
		FillWidth().
		Padding(20, 20, 18, 20).
		Gap(12).
		Radius(4).
		Clip().
		Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(r, pal.HeroBg, 4)
			// 右上几何赤陶弧块（对应 stillpoets-v3-shelf-fragments.png）
			corner := (&ui.Path{}).
				MoveTo(r.X+r.W*0.72, r.Y).
				LineTo(r.X+r.W, r.Y).
				LineTo(r.X+r.W, r.Y+74).
				LineTo(r.X+r.W*0.90, r.Y+74).
				QuadTo(r.X+r.W*0.76, r.Y+54, r.X+r.W*0.72, r.Y).
				Close()
			p.FillPath(corner, pal.HeroCorner)
		})
	hero.Children(func() {
		ui.Text(c, "今日重读  |  来自我的诗集").FontSize(12).TextColor(pal.TextSecondary)
		ui.Text(c, today.Excerpt).
			FontSize(19).FontWeight(500).LineHeight(1.5).MaxLines(4).TextColor(pal.TextPrimary).Padding(2, 24, 2, 0)
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, today.Author).FontSize(13).Bold().MaxLines(1).TextColor(pal.TextPrimary)
			ui.Textf(c, "《%s》· %s", today.BookTitle, today.PoemTitle).FontSize(12).MaxLines(1).TextColor(pal.TextSecondary)
		})
		a.renderHeroActions(c, pal, today)
	})
}

func (a *AppUI) renderHeroActions(c *ui.Context, pal theme.Palette, today *app.TodayReread) {
	ui.Row(c.Key("hero-actions")).FillWidth().AlignItems(ui.Center).Padding(6, 0, 0, 0).Children(func() {
		readBtn := ui.Row(c.Key("hero-read")).
			Padding(8, 16).
			Radius(6).
			Background(pal.Accent).
			AlignItems(ui.Center).
			Cursor(ui.CursorPointer)
		readBtn.Children(func() {
			ui.Text(c, "读完整首 ↗").FontSize(13).Bold().TextColor(pal.AccentText)
		})
		if readBtn.Clicked() {
			a.openBook(today.BookID, today.PoemID, app.ParseLineRanges(today.HighlightLines), true)
		}

		ui.Spacer(c)

		saveBtn := ui.Row(c.Key("hero-save-quote")).
			Padding(8, 10).
			Gap(5).
			AlignItems(ui.Center).
			Radius(6).
			Cursor(ui.CursorPointer)
		saveBtn.Children(func() {
			ui.Icon(c, components.IconBookmark).Width(15).Height(15).TextColor(pal.TextPrimary)
			ui.Text(c, "留作散页").FontSize(13).TextColor(pal.TextPrimary)
		})
		if saveBtn.Clicked() {
			a.saveTodayRereadAsQuote(today)
		}
	})
}

func (a *AppUI) saveTodayRereadAsQuote(today *app.TodayReread) {
	if today == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		book, err := a.svc.GetBook(ctx, today.BookID)
		if err != nil {
			a.showTransientToast("留存失败：" + err.Error())
			return
		}
		poem := domain.Poem{
			ID:     today.PoemID,
			BookID: today.BookID,
			Title:  today.PoemTitle,
			Author: today.Author,
		}
		if _, err := a.svc.CreatePoemExcerptQuote(ctx, book, poem, today.Excerpt, today.HighlightLines); err != nil {
			a.showTransientToast("留存失败：" + err.Error())
			return
		}
		a.reloadSync(ctx)
		a.showTransientToast("已留作散页")
	}()
}

func (a *AppUI) renderContinueReading(c *ui.Context, pal theme.Palette, item *storage.ContinueReadingItem) {
	ratio := 0.0
	if item.Book.PoemCount > 0 {
		ratio = float64(item.Progress.ReadPoemCount()) / float64(item.Book.PoemCount)
		if ratio > 1 {
			ratio = 1
		}
	}
	subtitle := "上次读到书内诗篇"
	if item.PoemTitle != "" {
		subtitle = fmt.Sprintf("上次读到《%s》", item.PoemTitle)
	}

	ui.Column(c.Key("continue-section")).Gap(10).FillWidth().Children(func() {
		ui.Text(c, "继续阅读").FontSize(16).Bold().TextColor(pal.TextPrimary)

		card := ui.Row(c.Key("continue-card")).
			FillWidth().
			Padding(10, 4).
			Gap(12).
			AlignItems(ui.Center).
			Cursor(ui.CursorPointer)
		card.Children(func() {
			components.BookCoverThumbnail(c, "cont-thumb", item.Book.ID, pal)
			ui.Column(c).Grow(1).Gap(5).Children(func() {
				ui.Text(c, item.Book.Title).FontSize(15).Bold().TextColor(pal.TextPrimary).MaxLines(1)
				ui.Text(c, subtitle).FontSize(12).TextColor(pal.TextMuted).MaxLines(1)
				ui.Progress(c, ratio).Height(3).FillWidth().TextColor(pal.Accent).Background(pal.Border)
			})
			ui.Icon(c, components.IconArrowRight).Width(18).Height(18).TextColor(pal.TextSecondary)
		})
		if card.Clicked() {
			a.openBook(item.Book.ID, item.Progress.LastPoemID, nil, true)
		}
	})
}

func (a *AppUI) renderMyBooksSection(c *ui.Context, pal theme.Palette, isTablet bool) {
	ui.Column(c.Key("my-books-section")).Gap(12).FillWidth().Children(func() {
		// 标题行：我的诗集 + 右侧轻量「管理分组」
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "我的诗集").FontSize(16).Bold().TextColor(pal.TextPrimary).Grow(1)
			manageBtn := ui.Box(c.Key("manage-groups")).Padding(4, 6).Cursor(ui.CursorPointer)
			manageBtn.Children(func() {
				ui.Text(c, "管理分组").FontSize(12).TextColor(pal.TextMuted)
			})
			if manageBtn.Clicked() {
				a.groupFormError = ""
				a.showGroupModal = true
			}
		})

		// 横向直达分组切换条（不藏二级页）
		ui.ScrollHorizontal(c.Key("group-pills-scroll")).FillWidth().Height(30).Scrollbars(false).Children(func() {
			ui.Row(c).Gap(18).Padding(2, 0, 4, 0).Children(func() {
				renderGroupTab := func(id, label string) {
					active := a.selectedGrp == id
					col := ui.Column(c.Key("grp-"+id)).AlignItems(ui.Center).Gap(4).Cursor(ui.CursorPointer)
					col.Children(func() {
						txt := ui.Text(c, label).FontSize(13)
						if active {
							txt.Bold().TextColor(pal.TextPrimary)
						} else {
							txt.TextColor(pal.TextMuted)
						}
						bar := ui.Box(c).Width(24).Height(2).Radius(1)
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

				renderGroupTab("", "全部")
				for _, g := range a.groups {
					renderGroupTab(g.ID, g.Name)
				}
			})
		})

		books := a.filteredBooks()
		if len(books) == 0 {
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
					ui.Text(c, msg).FontSize(13).TextColor(pal.TextMuted)
					if ui.PrimaryButton(c, "+ 导入 EPUB 诗集").Clicked() {
						a.triggerImportEPUB()
					}
				})
			return
		}

		cols := layout.BookGridColumns(c)
		ui.Grid(c.Key("book-cover-grid")).Columns(cols).Gap(12).FillWidth().Children(func() {
			for _, b := range books {
				selected := isTablet && b.ID == a.activeBookID
				isRemote := b.LocalState != domain.LocalStateDownloaded
				if components.BookCoverCard(c, "bk-"+b.ID, b.ID, b.Title, b.Author, pal, selected, isRemote) {
					a.openBook(b.ID, "", nil, !isTablet)
				}
			}
		})
	})
}

// --- 散页子标签页 ---

func (a *AppUI) renderQuotesSubTab(c *ui.Context, pal theme.Palette, isTablet bool) {
	ui.Column(c.Key("quotes-subtab")).Padding(14, 18, 88, 18).Gap(14).FillWidth().Children(func() {
		// 顶部标题与副标题
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				ui.Text(c, "留住的句子").FontSize(24).Bold().TextColor(pal.TextPrimary)
				ui.Text(c, "片段，也有来处。").FontSize(13).TextColor(pal.TextMuted)
			})
			addQuoteBtn := ui.Row(c.Key("quick-add-quote")).
				Padding(7, 12).
				Gap(5).
				Radius(8).
				Background(pal.Surface).
				Border(1, pal.Border).
				AlignItems(ui.Center).
				Cursor(ui.CursorPointer)
			addQuoteBtn.Children(func() {
				ui.Icon(c, components.IconBookmark).Width(15).Height(15).TextColor(pal.TextPrimary)
				ui.Text(c, "记一张").FontSize(13).TextColor(pal.TextPrimary)
			})
			if addQuoteBtn.Clicked() {
				a.openNewQuoteModal()
			}
		})

		// V1.0 类型筛选条：全部 / 诗歌节选 / 独立散句
		ui.Row(c.Key("quote-kind-tabs")).Gap(18).AlignItems(ui.Center).Children(func() {
			kinds := []struct {
				kind  domain.QuoteKind
				label string
			}{
				{"", "全部"},
				{domain.QuoteKindExcerpt, "诗歌节选"},
				{domain.QuoteKindStandalone, "独立散句"},
			}
			for _, k := range kinds {
				active := a.quoteKind == k.kind
				btn := ui.Box(c.Key("qk-"+k.label)).Padding(4, 2).Cursor(ui.CursorPointer)
				btn.Children(func() {
					t := ui.Text(c, k.label).FontSize(14)
					if active {
						t.Bold().TextColor(pal.TextPrimary)
					} else {
						t.TextColor(pal.TextMuted)
					}
				})
				if btn.Clicked() {
					a.quoteKind = k.kind
					go a.reloadQuotesOnly()
				}
			}
		})

		// 按「原书出处 / 作者」筛选（V1.0 不按收藏筛选）
		if len(a.quoteFacets.SourceBooks) > 0 || len(a.quoteFacets.Authors) > 0 {
			ui.ScrollHorizontal(c.Key("quote-facets-scroll")).FillWidth().Height(34).Scrollbars(false).Children(func() {
				ui.Row(c).Gap(8).Padding(2, 0).Children(func() {
					renderChip := func(key, label string, active bool, onClick func()) {
						bg := pal.Surface
						fg := pal.TextSecondary
						bd := pal.Border
						if active {
							bg = pal.Accent
							fg = pal.AccentText
							bd = pal.Accent
						}
						chip := ui.Box(c.Key(key)).
							Padding(5, 10).
							Radius(999).
							Background(bg).
							Border(1, bd).
							Cursor(ui.CursorPointer)
						chip.Children(func() {
							ui.Text(c, label).FontSize(12).TextColor(fg)
						})
						if chip.Clicked() {
							onClick()
						}
					}

					renderChip("facet-all", "全部出处", a.quoteBook == "" && a.quoteAuthor == "", func() {
						a.quoteBook = ""
						a.quoteAuthor = ""
						go a.reloadQuotesOnly()
					})
					for _, sb := range a.quoteFacets.SourceBooks {
						sb := sb
						renderChip("sb-"+sb, "《"+sb+"》", a.quoteBook == sb, func() {
							if a.quoteBook == sb {
								a.quoteBook = ""
							} else {
								a.quoteBook = sb
								a.quoteAuthor = ""
							}
							go a.reloadQuotesOnly()
						})
					}
					for _, au := range a.quoteFacets.Authors {
						au := au
						renderChip("au-"+au, au, a.quoteAuthor == au, func() {
							if a.quoteAuthor == au {
								a.quoteAuthor = ""
							} else {
								a.quoteAuthor = au
								a.quoteBook = ""
							}
							go a.reloadQuotesOnly()
						})
					}
				})
			})
		}

		// 散页瀑布流纸卡（手机 2 列，平板 3~4 列）
		if len(a.quotes) == 0 {
			ui.Column(c.Key("quotes-empty")).
				FillWidth().
				Padding(32, 18).
				Gap(10).
				AlignItems(ui.Center).
				Radius(8).
				Background(pal.Surface).
				Border(1, pal.Border).
				Children(func() {
					ui.Text(c, "还没有留住的散页").FontSize(15).Bold().TextColor(pal.TextPrimary)
					ui.Text(c, "在读诗时划选诗行点「留作散页」，或直接手动记下一句打动你的话。").
						FontSize(13).TextColor(pal.TextMuted)
					if ui.PrimaryButton(c, "+ 记一张散页").Clicked() {
						a.openNewQuoteModal()
					}
				})
			return
		}

		cols := layout.MasonryColumns(c)
		buckets := make([][]storage.QuoteView, cols)
		for i, qv := range a.quotes {
			buckets[i%cols] = append(buckets[i%cols], qv)
		}

		ui.Row(c.Key("quotes-masonry")).FillWidth().Gap(12).AlignItems(ui.Start).Children(func() {
			for colIdx := 0; colIdx < cols; colIdx++ {
				ui.Column(c.Key(fmt.Sprintf("qcol-%d", colIdx))).Grow(1).Gap(12).Children(func() {
					for rowIdx, qv := range buckets[colIdx] {
						globalIdx := rowIdx*cols + colIdx
						a.renderQuoteCard(c, pal, qv, globalIdx)
					}
				})
			}
		})
	})
}

func (a *AppUI) renderQuoteCard(c *ui.Context, pal theme.Palette, qv storage.QuoteView, idx int) {
	swatches := pal.QuoteSwatches
	var sw theme.CardSwatch
	if qv.EffectiveKind == domain.QuoteKindStandalone {
		// 独立散句优先使用沉静暖白细线卡（与原型右下角一致）
		sw = swatches[len(swatches)-1]
	} else {
		sw = swatches[idx%len(swatches)]
	}

	tagLabel := "诗歌节选"
	if qv.EffectiveKind == domain.QuoteKindStandalone {
		tagLabel = "独立散句"
	}

	card := ui.Column(c.Key("qcard-"+qv.Quote.ID)).
		FillWidth().
		Padding(16, 15).
		Gap(10).
		Radius(4).
		Background(sw.Bg).
		Border(1, sw.Border).
		Cursor(ui.CursorPointer)
	card.Children(func() {
		ui.Text(c, tagLabel).FontSize(11).TextColor(sw.Muted)

		// 非满底卡片在顶部配一根极细的主题强调线
		if !sw.IsFilled {
			ui.Box(c).FillWidth().Height(1.5).Background(sw.Accent)
		}

		ui.Text(c, qv.Quote.Content).
			FontSize(15).FontWeight(500).LineHeight(1.58).TextColor(sw.Text)

		if strings.TrimSpace(qv.Quote.Author) != "" {
			ui.Text(c, qv.Quote.Author).FontSize(12).MaxLines(1).TextColor(sw.Muted)
		}

		// 细分隔线
		ui.Box(c).FillWidth().Height(1).Background(sw.Border)

		// 出处行
		sourceParts := []string{}
		if qv.Quote.SourceBook != "" {
			sourceParts = append(sourceParts, qv.Quote.SourceBook)
		}
		if qv.Quote.PoemTitle != "" && qv.Quote.PoemTitle != qv.Quote.SourceBook {
			sourceParts = append(sourceParts, qv.Quote.PoemTitle)
		}
		sourceText := strings.Join(sourceParts, " · ")
		if sourceText == "" {
			sourceText = "随手记下"
		}
		ui.Text(c, sourceText).FontSize(12).TextColor(sw.Muted).MaxLines(2)

		// 仅「诗歌节选（且原诗集仍存在）」提供 读完整首 ↗
		if qv.EffectiveKind == domain.QuoteKindExcerpt {
			jump := ui.Box(c.Key("jump-"+qv.Quote.ID)).Padding(2, 0).Cursor(ui.CursorPointer)
			jump.Children(func() {
				ui.Text(c, "读完整首 ↗").FontSize(12).Bold().TextColor(sw.Text)
			})
			if jump.Clicked() {
				a.shelfSubTab = components.SubTabBooks
				a.openBook(qv.Quote.BookID, qv.Quote.PoemID, app.ParseLineRanges(qv.Quote.HighlightLines), true)
				return
			}
		}
	})

	if card.Clicked() {
		cp := qv
		a.detailQuote = &cp
		a.showQuoteDetail = true
	}
}

func (a *AppUI) reloadQuotesOnly() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.mu.Lock()
	f := storage.QuoteFilter{
		Kind:       a.quoteKind,
		SourceBook: a.quoteBook,
		Author:     a.quoteAuthor,
	}
	a.mu.Unlock()
	quotes, err := a.svc.ListQuotes(ctx, f)
	if err != nil {
		return
	}
	facets, _ := a.svc.ListQuoteFacets(ctx)
	a.mu.Lock()
	a.quotes = quotes
	a.quoteFacets = facets
	a.mu.Unlock()
	a.requestRepaint()
}

func (a *AppUI) openNewQuoteModal() {
	a.editingQuoteID = ""
	a.quoteFormContent = ""
	a.quoteFormAuthor = ""
	a.quoteFormSource = ""
	a.quoteFormError = ""
	a.showAddModal = false
	a.showQuoteModal = true
}
