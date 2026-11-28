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
	// 原地整理书架时锁定横向切页，防止手指点选封面时误划到散页
	if a.shelfOrganizing {
		track.Left(startX)
		a.shelfDragging = false
		a.shelfPager.progress = float32(curPage)
		return float32(curPage)
	}
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
								a.renderShelfOrganizeBar(c, pal, true)
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
						ui.Column(c.Key("phone-books-col")).Fill().Children(func() {
							ui.Scroll(c.Key("shelf-books-scroll")).Grow(1).FillWidth().Children(func() {
								a.renderBooksSubTab(c, pal, isTablet)
							})
							a.renderShelfOrganizeBar(c, pal, isTablet)
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
	if a.shelfOrganizing {
		// 整理模式下底部操作条占据实心高度，滚区自身仅保留轻量呼吸留白
		bottomPad = 24
	}
	ui.Column(c.Key("books-subtab")).Padding(10, 18, bottomPad, 18).Gap(14).FillWidth().Children(func() {
		// 整理模式下收起顶部「今日重读」与「继续阅读」，让视线聚焦书架封面网格
		if !a.shelfOrganizing {
			// 手机单栏在首屏展示「今日重读」画册/刊物主视觉卡；平板左主栏聚焦继续阅读 + 书架网格（右栏已是实时整诗阅读区）
			if !isTablet {
				a.renderTodayRereadHero(c, pal)
			}

			// 轻巧单行书签条：继续阅读
			if a.continueItem != nil {
				a.renderContinueReading(c, pal, a.continueItem)
			}
		}

		// 我的诗集 + 整理/管理分组 + 横向分组直达条
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
		books := a.filteredBooks()
		a.renderMyBooksHeader(c, pal, books)

		a.renderShelfTargetBanner(c, pal)

		a.renderShelfGroupTabs(c, pal)

		if len(books) == 0 {
			a.renderEmptyShelfGroup(c, pal)
			return
		}

		cols := layout.BookGridColumns(c)
		ui.Grid(c.Key("book-cover-grid")).Columns(cols).Gap(14).FillWidth().Children(func() {
			for _, b := range books {
				a.renderShelfBook(c, pal, b, isTablet)
			}
		})
	})
}
