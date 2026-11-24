package views

import (
	"context"
	"fmt"
	"math"
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

func (a *AppUI) syncShelfPager(scope string, width float32) int {
	page := int(a.shelfSubTab)
	if page < 0 || page > 1 {
		page = 0
	}
	if a.shelfPager.scopeKey != scope {
		a.shelfPager.scopeKey = scope
		a.shelfPager.width = width
		a.shelfPager.syncedPage = page
		a.shelfPager.offset = ui.Point{X: -float32(page) * width}
		a.shelfPager.progress = float32(page)
		a.shelfPager.epoch++
	} else if math.Abs(float64(a.shelfPager.width-width)) > 0.5 {
		a.shelfPager.width = width
		a.shelfPager.offset = ui.Point{X: -float32(page) * width}
		a.shelfPager.progress = float32(page)
		a.shelfPager.epoch++
	} else if a.shelfPager.syncedPage != page {
		a.shelfPager.syncedPage = page
		a.shelfPager.offset = ui.Point{X: -float32(page) * width}
	}
	return page
}

func (a *AppUI) attachShelfPagerDraggable(track ui.Element, width float32, curPage int) float32 {
	startX := -float32(curPage) * width
	// 适配平板左窄书架栏（348 DIP）：避免在 1000+ DIP 工作区中需要划过半屏才能切到散页
	threshold := min(width*0.28, 130)
	track.Draggable(&a.shelfPager.offset, ui.DraggableOptions{
		Axis:      ui.Horizontal,
		Bounds:    &ui.Rect{X: -width, W: width},
		Animation: theme.PageSpring,
		Snap: func(p ui.Point) ui.Point {
			to := curPage
			delta := p.X - startX
			switch {
			case delta < -threshold && curPage == 0:
				to = 1
			case delta > threshold && curPage == 1:
				to = 0
			}
			return ui.Point{X: -float32(to) * width}
		},
	})
	shownX := track.DragOffset().X
	a.shelfDragging = track.Dragging()
	prog := float32(0)
	if width > 0 {
		prog = max(0, min(1, -shownX/width))
	}
	a.shelfPager.progress = prog

	if !track.Dragging() {
		target := max(0, min(1, int(math.Round(float64(-a.shelfPager.offset.X/width)))))
		if target != curPage {
			a.shelfPager.syncedPage = target
			a.selectShelfSubTab(components.ShelfSubTab(target))
			// 让顶栏文字与下划线在下一帧同步更新
			if target == int(components.SubTabBooks) {
				a.shelfPager.progress = 0
			} else {
				a.shelfPager.progress = 1
			}
		}
	}
	return prog
}

// renderTabletShelfWorkspace 渲染平板端「诗集+阅读器双栏 ↔ 散页全宽瀑布流」可手势横滑工作区。
func (a *AppUI) renderTabletShelfWorkspace(c *ui.Context, pal theme.Palette) {
	viewport := ui.Box(c.Key("tablet-shelf-viewport")).Fill().Clip()
	width := viewport.Bounds().W
	if width <= 0 {
		winW, _ := c.Size()
		width = max(500, winW-layout.NavRailWidthDIP)
		c.AnimationFrame()
	}
	curPage := a.syncShelfPager("tablet", width)

	viewport.Children(func() {
		trackKey := fmt.Sprintf("tablet-shelf-track-%d", a.shelfPager.epoch)
		track := ui.Box(c.Key(trackKey)).
			Absolute().
			Left(0).Top(0).Bottom(0).
			Width(width * 2)

		a.attachShelfPagerDraggable(track, width, curPage)

		track.Children(func() {
			// Page 0：左书架（诗集）+ 右阅读器双栏工作区
			ui.Box(c.Key("tablet-page-books")).
				Absolute().
				Left(0).Top(0).Bottom(0).
				Width(width).
				Children(func() {
					layout.MasterDetail(c, layout.MasterPaneWidthDIP, pal.Border,
						func() {
							ui.Column(c.Key("tablet-books-col")).Fill().Children(func() {
								a.renderShelfHeaderAndTabs(c, pal)
								ui.Scroll(c.Key("tablet-books-scroll")).Grow(1).FillWidth().Children(func() {
									a.renderBooksSubTab(c, pal, true)
								})
							})
						},
						func() { a.renderReaderPane(c, pal, true) },
					)
				})

			// Page 1：全宽散页大瀑布流
			ui.Box(c.Key("tablet-page-quotes")).
				Absolute().
				Left(width).Top(0).Bottom(0).
				Width(width).
				Children(func() {
					ui.Column(c.Key("tablet-quotes-col")).Fill().Children(func() {
						a.renderShelfHeaderAndTabs(c, pal)
						ui.Scroll(c.Key("tablet-quotes-scroll")).TrackScroll(&a.tabletQuotesScroll).Grow(1).FillWidth().Children(func() {
							a.renderQuotesSubTab(c, pal, true)
						})
					})
				})
		})
	})
}

func (a *AppUI) renderShelfHeaderAndTabs(c *ui.Context, pal theme.Palette) {
	components.BrandHeader(c, pal, "StillPoets", true,
		func() {
			a.showSearchModal = true
		},
		func() {
			a.showAddModal = true
		},
	)
	components.ShelfSubTabs(c, pal, a.shelfSubTab, a.shelfPager.progress, a.selectShelfSubTab)
}

func (a *AppUI) renderShelfPane(c *ui.Context, pal theme.Palette, isTablet bool) {
	ui.Column(c.Key("shelf-pane")).Fill().Children(func() {
		a.renderShelfHeaderAndTabs(c, pal)

		viewport := ui.Box(c.Key("phone-shelf-viewport")).Grow(1).FillWidth().Clip()
		width := viewport.Bounds().W
		if width <= 0 {
			winW, _ := c.Size()
			width = max(320, winW)
			c.AnimationFrame()
		}
		curPage := a.syncShelfPager("phone", width)

		viewport.Children(func() {
			trackKey := fmt.Sprintf("phone-shelf-track-%d", a.shelfPager.epoch)
			track := ui.Box(c.Key(trackKey)).
				Absolute().
				Left(0).Top(0).Bottom(0).
				Width(width * 2)

			a.attachShelfPagerDraggable(track, width, curPage)

			track.Children(func() {
				// Page 0：诗集书架
				ui.Box(c.Key("phone-page-books")).
					Absolute().
					Left(0).Top(0).Bottom(0).
					Width(width).
					Children(func() {
						ui.Scroll(c.Key("shelf-books-scroll")).Fill().Children(func() {
							a.renderBooksSubTab(c, pal, isTablet)
						})
					})

				// Page 1：散页瀑布流
				ui.Box(c.Key("phone-page-quotes")).
					Absolute().
					Left(width).Top(0).Bottom(0).
					Width(width).
					Children(func() {
						ui.Scroll(c.Key("shelf-quotes-scroll")).TrackScroll(&a.phoneQuotesScroll).Fill().Children(func() {
							a.renderQuotesSubTab(c, pal, isTablet)
						})
					})
			})
		})
	})
}

func (a *AppUI) renderBooksSubTab(c *ui.Context, pal theme.Palette, isTablet bool) {
	bottomPad := float32(88)
	if !isTablet && a.useFloatingBottomNav() {
		bottomPad = 108
	}
	ui.Column(c.Key("books-subtab")).Padding(10, 18, bottomPad, 18).Gap(14).FillWidth().Children(func() {
		// 手机单栏在首屏展示「今日重读」画册/刊物主视觉卡；平板左主栏聚焦继续阅读 + 书架网格（右栏已是实时整诗阅读区）
		if !isTablet {
			a.renderTodayRereadHero(c, pal)
		}

		// 轻巧单行书签条：继续阅读
		if a.continueItem != nil {
			a.renderContinueReading(c, pal, a.continueItem)
		}

		// 我的诗集 + 管理分组 + 横向分组直达条
		a.renderMyBooksSection(c, pal, isTablet)
	})
}

func (a *AppUI) renderTodayRereadHero(c *ui.Context, pal theme.Palette) {
	today := a.todayReread
	poetryFont := theme.PoetryFont(a.settings.FontFamily)
	midnight := a.isMidnightCave()
	heroLabel := "今日重读"
	if midnight {
		heroLabel = dpsMidnightCaveLabel
	}

	if today == nil {
		// 空书库引导卡
		emptyCard := ui.Column(c.Key("hero-empty")).
			FillWidth().
			Padding(16, 18).
			Gap(10).
			Radius(6).
			Background(pal.HeroBg).
			Border(1, pal.Border).
			Clip()
		if pal.HeroStyle != theme.HeroOpenPaper {
			isDark := pal.IsDark()
			emptyCard.Draw(func(p *ui.Painter, r ui.Rect) {
				theme.DrawPaperTexture(p, r, 6, isDark, 0.045)
			})
		}
		emptyQuote := dpsEmptyHeroQuote
		if midnight {
			emptyQuote = dpsMidnightHeroQuote
		}
		emptyCard.Children(func() {
			ui.Text(c, heroLabel+"  ·  静候第一卷诗集").Font(theme.FontSans).FontSize(11).LetterSpacing(0.6).TextColor(pal.TextSecondary)
			ui.Text(c, emptyQuote).
				Font(poetryFont).FontSize(19).FontWeight(500).LineHeight(1.55).TextColor(pal.TextPrimary)
			ui.Row(c).Gap(10).Padding(4, 0, 0, 0).Children(func() {
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

	attribution := strings.TrimSpace(today.Author)
	src := fmt.Sprintf("《%s》", today.BookTitle)
	if strings.TrimSpace(today.PoemTitle) != "" {
		src += " · " + today.PoemTitle
	}
	if attribution != "" {
		attribution += "  " + src
	} else {
		attribution = src
	}

	if pal.HeroStyle == theme.HeroOpenPaper {
		// 刊物风（Editorial）：无底色块，左侧刊物蓝细竖条 + 开放纸面大字
		ui.Row(c.Key("hero-editorial")).FillWidth().Gap(12).Padding(4, 0, 4, 0).Children(func() {
			ui.Box(c).Width(2.5).FillHeight().Background(pal.HeroCorner).Radius(1.5)
			ui.Column(c).Grow(1).Gap(7).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, heroLabel).Font(theme.FontSans).FontSize(11).FontWeight(600).LetterSpacing(0.8).TextColor(pal.TextSecondary)
					ui.Text(c, "·").Font(theme.FontSans).FontSize(11).TextColor(pal.TextMuted)
					ui.Text(c, "来自我的诗集").Font(theme.FontNote).FontSize(11).TextColor(pal.TextMuted)
				})
				ui.Text(c, today.Excerpt).
					Font(poetryFont).FontSize(18).FontWeight(400).LineHeight(1.54).MaxLines(3).TextColor(pal.TextPrimary)
				ui.Text(c, attribution).Font(theme.FontNote).FontSize(12).MaxLines(1).TextColor(pal.TextMuted)
				a.renderHeroActions(c, pal, today)
			})
		})
		return
	}

	// 画册风 / 暗夜极简：鼠尾草绿棉纸色块 + 右上规整几何赤陶四分之一圆弧 + 纸张纤维肌理
	hero := ui.Column(c.Key("hero-artbook")).
		FillWidth().
		Padding(15, 18, 13, 18).
		Gap(8).
		Radius(4).
		Border(1, pal.Border.Alpha(0.55)).
		Clip().
		Draw(func(p *ui.Painter, r ui.Rect) {
			p.Clip(r, 4, func() {
				p.Fill(r, pal.HeroBg, 0)
				// 右上标准四分之一圆赤陶角标（圆心锚定右上顶点，边缘利落自然）
				arcR := min(float32(56), r.H*0.38)
				corner := (&ui.Path{}).Circle(r.X+r.W, r.Y, arcR)
				p.FillPath(corner, pal.HeroCorner)
				// 叠加 4.8% 纸张肌理与纤维，消除扁平色块电子感
				theme.DrawPaperTexture(p, r, 0, pal.IsDark(), 0.048)
			})
		})
	hero.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, heroLabel).Font(theme.FontSans).FontSize(11).FontWeight(600).LetterSpacing(0.8).TextColor(pal.TextSecondary)
			ui.Text(c, "·").Font(theme.FontSans).FontSize(11).TextColor(pal.TextSecondary)
			ui.Text(c, "来自我的诗集").Font(theme.FontNote).FontSize(11).TextColor(pal.TextSecondary)
		})
		ui.Text(c, today.Excerpt).
			Font(poetryFont).FontSize(17.5).FontWeight(400).LineHeight(1.52).MaxLines(3).TextColor(pal.TextPrimary).Padding(1, 22, 1, 0)
		ui.Text(c, attribution).Font(theme.FontNote).FontSize(12).MaxLines(1).TextColor(pal.TextSecondary)
		a.renderHeroActions(c, pal, today)
	})
}

func (a *AppUI) renderHeroActions(c *ui.Context, pal theme.Palette, today *app.TodayReread) {
	// 去工具化 CTA：将实心陶土红胶囊改为克制的「纯文字 + 细下划线」阅读入口
	ui.Row(c.Key("hero-actions")).FillWidth().AlignItems(ui.Center).Padding(3, 0, 0, 0).Children(func() {
		readBtn := ui.Row(c.Key("hero-read")).
			Padding(4, 2, 4, 0).
			AlignItems(ui.Center).
			Cursor(ui.CursorPointer)
		theme.ApplyPressScale(readBtn, 0.95)
		readBtn.Children(func() {
			ui.Text(c, "读完整首 ↗").
				Font(theme.FontSans).
				FontSize(12.5).
				FontWeight(600).
				LetterSpacing(0.3).
				TextColor(pal.Accent).
				Underline().
				UnderlineOffset(3).
				DecorationColor(pal.Accent.Alpha(0.65)).
				DecorationThickness(1)
		})
		if readBtn.Clicked() {
			a.openBookWithSource(today.BookID, today.PoemID, app.ParseLineRanges(today.HighlightLines), true, entryToday)
		}

		ui.Spacer(c)

		saveBtn := ui.Row(c.Key("hero-save-quote")).
			Padding(4, 2, 4, 6).
			Gap(4).
			AlignItems(ui.Center).
			Radius(4).
			Cursor(ui.CursorPointer)
		theme.ApplyPressScale(saveBtn, 0.95)
		saveBtn.Children(func() {
			ui.Icon(c, components.IconBookmark).Width(13.5).Height(13.5).TextColor(pal.TextSecondary)
			ui.Text(c, "留作散页").Font(theme.FontSans).FontSize(12).TextColor(pal.TextSecondary)
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
	pct := int(math.Round(ratio * 100))
	subtitle := "上次读到书内诗篇"
	if strings.TrimSpace(item.PoemTitle) != "" {
		subtitle = fmt.Sprintf("上次读到《%s》", item.PoemTitle)
	}

	// 内嵌迷你实体书封 + 右上「继续阅读 · 百分比」+ 极细进度条的一体化轻卡（省去外置区块标题）
	card := ui.Row(c.Key("continue-card")).
		FillWidth().
		Padding(11, 13).
		Gap(13).
		Radius(10).
		Background(pal.Surface).
		Border(1, pal.Border).
		AlignItems(ui.Center).
		Clip().
		Cursor(ui.CursorPointer)
	theme.ApplyPressScale(card, 0.985)
	if !pal.IsDark() {
		card.Shadow(0, 2, 7, -1, ui.RGBA(32, 26, 18, 0.045))
	}
	if grain := pal.PaperGrainOpacity(); grain > 0 {
		isDark := pal.IsDark()
		card.Draw(func(p *ui.Painter, r ui.Rect) {
			theme.DrawPaperTexture(p, r, 10, isDark, grain*0.85)
		})
	}
	card.Children(func() {
		components.BookCoverThumbnail(c, "cont-thumb", item.Book.ID, a.coverImageFor(item.Book), pal)

		ui.Column(c).Grow(1).Gap(4).Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, item.Book.Title).
					Font(theme.FontSerif).
					FontSize(14.5).
					Bold().
					TextColor(pal.TextPrimary).
					MaxLines(1).
					Grow(1)

				ui.Row(c).Gap(3).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "继续阅读").Font(theme.FontSans).FontSize(11).TextColor(pal.TextMuted)
					if item.Book.PoemCount > 0 {
						ui.Text(c, "·").Font(theme.FontSans).FontSize(11).TextColor(pal.TextMuted)
						ui.Textf(c, "%d%%", pct).Font(theme.FontMono).FontFeatures("tnum").FontSize(11).TextColor(pal.TextMuted)
					}
				})
			})

			ui.Text(c, subtitle).
				Font(theme.FontNote).
				FontSize(12).
				TextColor(pal.TextSecondary).
				MaxLines(1)

			ui.Box(c.Key("continue-progress")).
				FillWidth().
				Height(5).
				Draw(func(p *ui.Painter, r ui.Rect) {
					bar := ui.Rect{X: r.X, Y: r.Y + 2.5, W: r.W, H: 2.5}
					p.Fill(bar, pal.Border.Alpha(0.55), 1.25)
					if ratio > 0 {
						p.Fill(ui.Rect{X: bar.X, Y: bar.Y, W: bar.W * float32(ratio), H: bar.H}, pal.Accent, 1.25)
					}
				})
		})

		ui.Icon(c, components.IconChevronRight).Width(15).Height(15).TextColor(pal.TextMuted)
	})
	if card.Clicked() {
		a.openBook(item.Book.ID, item.Progress.LastPoemID, nil, true)
	}
}

func (a *AppUI) renderMyBooksSection(c *ui.Context, pal theme.Palette, isTablet bool) {
	ui.Column(c.Key("my-books-section")).Gap(10).FillWidth().Children(func() {
		// 标题行：我的诗集 + 右侧轻量「管理分组」
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "我的诗集").Font(theme.FontSerif).FontSize(16).Bold().LetterSpacing(0.3).TextColor(pal.TextPrimary).Grow(1)
			manageBtn := ui.Box(c.Key("manage-groups")).Padding(3, 6).Cursor(ui.CursorPointer)
			manageBtn.Children(func() {
				ui.Text(c, "管理分组").Font(theme.FontSans).FontSize(12).TextColor(pal.TextMuted)
			})
			if manageBtn.Clicked() {
				a.groupFormError = ""
				a.showGroupModal = true
			}
		})

		// 横向直达分组切换条（不藏二级页）
		ui.ScrollHorizontal(c.Key("group-pills-scroll")).FillWidth().Height(28).Scrollbars(false).Children(func() {
			ui.Row(c).Gap(18).Padding(1, 0, 3, 0).Children(func() {
				renderGroupTab := func(id, label string) {
					active := a.selectedGrp == id
					col := ui.Column(c.Key("grp-"+id)).AlignItems(ui.Center).Gap(4).Cursor(ui.CursorPointer)
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
					ui.Text(c, msg).Font(theme.FontNote).FontSize(13).TextColor(pal.TextMuted)
					if ui.PrimaryButton(c, "+ 导入 EPUB 诗集").Clicked() {
						a.triggerImportEPUB()
					}
				})
			return
		}

		cols := layout.BookGridColumns(c)
		ui.Grid(c.Key("book-cover-grid")).Columns(cols).Gap(14).FillWidth().Children(func() {
			for _, b := range books {
				selected := isTablet && b.ID == a.activeBookID
				isRemote := b.LocalState != domain.LocalStateDownloaded
				if components.BookCoverCard(c, "bk-"+b.ID, b.ID, b.Title, b.Author, a.coverImageFor(b), pal, selected, isRemote, a.settings.UseOriginalCover) {
					a.openBook(b.ID, "", nil, !isTablet)
				}
			}
		})
	})
}

// --- 散页子标签页 ---

func (a *AppUI) renderQuotesSubTab(c *ui.Context, pal theme.Palette, isTablet bool) {
	bottomPad := float32(88)
	if !isTablet && a.useFloatingBottomNav() {
		bottomPad = 108
	}
	scrollY := a.phoneQuotesScroll.Y
	if isTablet {
		scrollY = a.tabletQuotesScroll.Y
	}
	// 上滑时自然淡出顶部轻量导语，把首屏与阅读空间彻底让给诗歌卡片
	introOpacity := float32(1.0)
	if scrollY > 0 {
		introOpacity = max(0, 1-scrollY/36)
	}

	ui.Column(c.Key("quotes-subtab")).Padding(8, 18, bottomPad, 18).Gap(10).FillWidth().Children(func() {
		// 弱化的单行导语 + 右侧极简「记一张」入口（随向上滑动自然收起/淡出）
		ui.Row(c.Key("quotes-intro-bar")).
			FillWidth().
			AlignItems(ui.Center).
			Opacity(introOpacity).
			Children(func() {
				ui.Row(c).Grow(1).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "留住的句子").Font(theme.FontSerif).FontSize(14).FontWeight(600).TextColor(pal.TextSecondary)
					ui.Text(c, "·  片段，也有来处").Font(theme.FontNote).FontSize(12).Italic().TextColor(pal.TextMuted).MaxLines(1)
				})
				addQuoteBtn := ui.Row(c.Key("quick-add-quote")).
					Padding(4, 2, 4, 8).
					Gap(4).
					Radius(6).
					AlignItems(ui.Center).
					Cursor(ui.CursorPointer)
				theme.ApplyPressScale(addQuoteBtn, 0.94)
				addQuoteBtn.Children(func() {
					ui.Icon(c, components.IconBookmark).Width(13.5).Height(13.5).TextColor(pal.Accent)
					ui.Text(c, "记一张").Font(theme.FontSans).FontSize(12).FontWeight(500).TextColor(pal.Accent)
				})
				if addQuoteBtn.Clicked() {
					a.openNewQuoteModal()
				}
			})

		// V1.0 类型筛选条：全部 / 诗歌节选 / 独立散句（紧凑克制）
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
				btn := ui.Column(c.Key("qk-"+k.label)).AlignItems(ui.Center).Gap(3).Padding(2, 0).Cursor(ui.CursorPointer)
				btn.Children(func() {
					t := ui.Text(c, k.label).Font(theme.FontSans).FontSize(13)
					if active {
						t.FontWeight(600).TextColor(pal.TextPrimary)
					} else {
						t.TextColor(pal.TextMuted)
					}
					dot := ui.Box(c).Width(14).Height(1.5).Radius(1).Transition(theme.ColorTransition())
					if active {
						dot.Background(pal.Accent)
					} else {
						dot.Background(ui.Transparent)
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
			ui.ScrollHorizontal(c.Key("quote-facets-scroll")).FillWidth().Height(28).Scrollbars(false).Children(func() {
				ui.Row(c).Gap(7).Padding(1, 0).Children(func() {
					renderChip := func(key, label string, active bool, onClick func()) {
						bg := pal.Surface
						fg := pal.TextSecondary
						bd := pal.Border
						if active {
							bg = pal.Accent.Alpha(0.11)
							fg = pal.Accent
							bd = pal.Accent.Alpha(0.45)
						}
						chip := ui.Box(c.Key(key)).
							Padding(4, 10).
							Radius(999).
							Background(bg).
							Border(1, bd).
							Cursor(ui.CursorPointer)
						chip.Children(func() {
							t := ui.Text(c, label).Font(theme.FontSans).FontSize(11.5).TextColor(fg)
							if active {
								t.FontWeight(600)
							}
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
					ui.Text(c, "还没有留住的散页").Font(theme.FontSerif).FontSize(15).Bold().TextColor(pal.TextPrimary)
					ui.Text(c, "在读诗时划选诗行点「留作散页」，或直接手动记下一句打动你的话。").
						Font(theme.FontSans).FontSize(13).TextColor(pal.TextMuted)
					if ui.PrimaryButton(c, "+ 记一张散页").Clicked() {
						a.openNewQuoteModal()
					}
					if a.isMidnightCave() {
						ui.Text(c, dpsMidnightQuietLine).
							Font(theme.FontNote).FontSize(12).Italic().TextColor(pal.TextMuted).Padding(6, 0, 0, 0)
					}
				})
			return
		}

		cols := layout.MasonryColumns(c)
		buckets := make([][]storage.QuoteView, cols)
		for i, qv := range a.quotes {
			buckets[i%cols] = append(buckets[i%cols], qv)
		}

		ui.Row(c.Key("quotes-masonry")).FillWidth().Gap(12).Padding(2, 0, 0, 0).AlignItems(ui.Start).Children(func() {
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
	isStandalone := qv.EffectiveKind == domain.QuoteKindStandalone
	sw := pal.QuoteSwatchFor(idx, isStandalone)
	poetryFont := theme.PoetryFont(a.settings.FontFamily)

	// 约 80% 保持纸白/浅米细线卡 + 纸张微肌理；仅约 20% 彩色点缀；整张可点击查看详情/跳读原诗
	card := ui.Column(c.Key("qcard-"+qv.Quote.ID)).
		FillWidth().
		Padding(18, 15, 14, 15).
		Gap(13).
		Radius(4).
		Background(sw.Bg).
		Border(1, sw.Border).
		Clip().
		Cursor(ui.CursorPointer)
	theme.ApplyPressScale(card, 0.975)

	// 纸白卡加极轻的纸片落影，彩色卡叠 4.8% 纸张肌理
	if !sw.IsFilled && !pal.IsDark() {
		card.Shadow(0, 2, 7, -1, ui.RGBA(32, 26, 18, 0.055))
	}
	grainOpacity := float32(0.036)
	if sw.IsFilled {
		grainOpacity = 0.048
	}
	if pal.ID == theme.PresetEditorial && !sw.IsFilled {
		grainOpacity = 0
	}
	if grainOpacity > 0 {
		darkSurf := sw.IsDarkSurface()
		card.Draw(func(p *ui.Painter, r ui.Rect) {
			theme.DrawPaperTexture(p, r, 4, darkSurf, grainOpacity)
		})
	}

	card.Children(func() {
		// 信息减法：诗句直接作为视觉核心，拉大行距与内边距让断行有韵律感
		ui.Text(c, qv.Quote.Content).
			Font(poetryFont).
			FontSize(14.5).
			FontWeight(400).
			LineHeight(1.82).
			TextColor(sw.Text)

		// 极细短韵律线，代替笨重满宽分隔线
		ui.Box(c).Width(20).Height(1).Background(sw.Accent.Alpha(0.55))

		// 极简底部出处 + 极小角落标记：首行排「作者 + 右角微标」，次行全宽排出处，避免双列下出处被挤断行
		author := strings.TrimSpace(qv.Quote.Author)
		sourceParts := []string{}
		if qv.Quote.SourceBook != "" {
			sourceParts = append(sourceParts, "《"+qv.Quote.SourceBook+"》")
		}
		if qv.Quote.PoemTitle != "" && qv.Quote.PoemTitle != qv.Quote.SourceBook {
			sourceParts = append(sourceParts, qv.Quote.PoemTitle)
		}
		sourceText := strings.Join(sourceParts, " · ")

		cornerMark := "节选 ↗"
		if isStandalone {
			cornerMark = "散句"
		}

		ui.Column(c).FillWidth().Gap(3).Children(func() {
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				lead := author
				leadClr := sw.Text.Alpha(0.82)
				if lead == "" {
					lead = "随手记下"
					leadClr = sw.Muted
				}
				ui.Text(c, lead).Font(theme.FontNote).FontSize(12).TextColor(leadClr).MaxLines(1).Grow(1)
				ui.Text(c, cornerMark).
					Font(theme.FontMono).
					FontSize(9.5).
					TextColor(sw.Muted.Alpha(0.75))
			})
			if sourceText != "" {
				ui.Text(c, sourceText).Font(theme.FontNote).FontSize(11).TextColor(sw.Muted).MaxLines(2)
			}
		})
	})

	if card.Clicked() {
		cp := qv
		a.detailQuote = &cp
		a.showQuoteDetail = true
	}
}

func (a *AppUI) reloadQuotesOnly() {
	a.mu.Lock()
	a.quoteFilterSeq++
	reqSeq := a.quoteFilterSeq
	f := storage.QuoteFilter{
		Kind:       a.quoteKind,
		SourceBook: a.quoteBook,
		Author:     a.quoteAuthor,
	}
	a.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	quotes, err := a.svc.ListQuotes(ctx, f)
	if err != nil {
		return
	}
	facets, facetsErr := a.svc.ListQuoteFacets(ctx)
	a.dispatchUI(func() {
		a.mu.Lock()
		match := a.quoteFilterSeq == reqSeq
		a.mu.Unlock()
		if !match {
			return
		}
		a.quotes = quotes
		if facetsErr == nil {
			a.quoteFacets = facets
		}
	})
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
