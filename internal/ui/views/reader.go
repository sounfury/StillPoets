package views

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
	"stillpoets/internal/domain"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/layout"
	"stillpoets/internal/ui/theme"
)

func (a *AppUI) renderReaderPane(c *ui.Context, pal theme.Palette, isTablet bool) {
	book, hasBook := a.activeBook()

	pane := ui.Column(c.Key("reader-pane")).Fill().Background(pal.ReaderPaper)
	if grain := pal.PaperGrainOpacity(); grain > 0 {
		isDark := pal.IsDark()
		pane.Draw(func(p *ui.Painter, r ui.Rect) {
			theme.DrawPaperTexture(p, r, 0, isDark, grain*0.85)
		})
	}
	pane.Children(func() {
		if !hasBook {
			ui.Column(c).Fill().Center().Gap(18).Padding(36).Children(func() {
				ui.Text(c, "StillPoets").Font(theme.FontBrand).FontSize(26).Bold().TextColor(pal.TextPrimary)
				ui.Column(c.Key("reader-empty-thoreau")).
					MaxWidth(360).
					Padding(18, 22).
					Gap(10).
					Radius(8).
					Background(pal.Surface).
					Border(1, pal.Border).
					Children(func() {
						ui.Text(c, dpsThoreauOpening).
							Font(theme.PoetryFont(a.settings.FontFamily)).
							FontSize(15).
							LineHeight(1.85).
							TextColor(pal.TextPrimary)
						attr := "—— " + dpsThoreauAttribution
						if a.isMidnightCave() {
							attr += " · " + dpsMidnightCaveLabel
						}
						ui.Text(c, attr).
							Font(theme.FontNote).
							FontSize(11.5).
							Italic().
							TextColor(pal.TextMuted)
					})
				ui.Text(c, "在左侧选择一本诗集，或点右上角 + 导入新的 EPUB。").
					Font(theme.FontNote).FontSize(13).TextColor(pal.TextMuted)
			})
			return
		}

		// 云端未下载 / 下载中状态
		if book.LocalState != domain.LocalStateDownloaded {
			a.renderRemoteBookState(c, pal, book, isTablet)
			return
		}

		poems := a.activePoems
		idx := a.activePoemIdx
		var current domain.Poem
		hasPoem := len(poems) > 0 && idx >= 0 && idx < len(poems)
		if hasPoem {
			current = poems[idx]
		} else if len(poems) > 0 {
			idx = max(0, min(idx, len(poems)-1))
			a.activePoemIdx = idx
			current = poems[idx]
			hasPoem = true
		}
		_, isFav := a.activeFavorites[current.ID]

		// 1. 阅读器顶部工具栏（对应 stillpoets-v3-tablet.png 右侧顶栏）
		a.renderReaderHeader(c, pal, book, current, isFav, isTablet)

		// 若加载本诗正文或切书出现错误，展示明确错误条并提供重试入口（P1-7）
		if a.readerError != "" {
			ui.Row(c.Key("reader-error-bar")).FillWidth().Padding(8, 20).Gap(10).AlignItems(ui.Center).Background(pal.HighlightBg).Children(func() {
				ui.Text(c, a.readerError).FontSize(12).TextColor(pal.Accent).Grow(1)
				if ui.Button(c, "重试").Clicked() {
					a.openBookWithSource(book.ID, current.ID, a.highlightRanges, a.readerMobile, a.readerEntrySource)
				}
			})
		}

		// 1.5 若处于「偶遇浏览」模式（小组件/今日重读/散页/检索直达），展示克制的进度保护提示条
		if a.readerEntrySource.isPeek() && !a.peekBannerHidden && hasPoem {
			a.renderPeekProgressBanner(c, pal, book, current, isTablet)
		}

		// 2. 若处于「划选诗行」模式，展示平滑展开的轻量操作条（留作散页 / 设为本诗金句）
		if a.selectingLines && hasPoem {
			a.renderSelectionActionBar(c, pal, book, current)
		}

		// 3. 沉浸式整诗阅读主体（支持跟手左右滑动切换上一首/下一首诗，与长诗纵向滚动及划选模式无冲突）
		a.renderPoemSwipePager(c, pal, book, poems, isTablet)

		// 4. 底部翻诗栏（上一首 / 第 N / M 首 / 下一首）
		if hasPoem {
			a.renderReaderFooter(c, pal, book, poems)
		}
	})
}

func (a *AppUI) renderPoemSwipePager(c *ui.Context, pal theme.Palette, book domain.Book, poems []domain.Poem, isTablet bool) {
	viewport := ui.Box(c.Key("reader-swipe-viewport")).Grow(1).FillWidth().Clip()
	total := len(poems)
	if total == 0 {
		viewport.Children(func() {
			ui.Column(c).Fill().Center().Padding(28).Children(func() {
				ui.Text(c, "该诗集暂无可展示的篇目").FontSize(14).TextColor(pal.TextMuted)
			})
		})
		return
	}

	idx := max(0, min(a.activePoemIdx, total-1))
	a.activePoemIdx = idx

	width := viewport.Bounds().W
	if width <= 0 {
		winW, _ := c.Size()
		if isTablet {
			width = max(320, winW-layout.NavRailWidthDIP-layout.MasterPaneWidthDIP)
		} else {
			width = max(320, winW)
		}
		c.AnimationFrame()
	}

	// 换书或容器宽度变化时，重新锚定偏移并递增 epoch，避免跨书/旋屏长距离飞跃
	if a.poemPager.scopeKey != book.ID {
		a.poemPager.scopeKey = book.ID
		a.poemPager.width = width
		a.poemPager.syncedPage = idx
		a.poemPager.offset = ui.Point{X: -float32(idx) * width}
		a.poemPager.epoch++
	} else if math.Abs(float64(a.poemPager.width-width)) > 0.5 {
		a.poemPager.width = width
		a.poemPager.offset = ui.Point{X: -float32(idx) * width}
		a.poemPager.epoch++
	} else if a.poemPager.syncedPage != idx {
		// 外部改了目标诗篇：相邻一首平滑滑过去，目录跨多首则轻淡入直达
		delta := idx - a.poemPager.syncedPage
		if delta < 0 {
			delta = -delta
		}
		if delta > 1 {
			a.poemPager.epoch++
		}
		a.poemPager.syncedPage = idx
		a.poemPager.offset = ui.Point{X: -float32(idx) * width}
	}

	viewport.Children(func() {
		// 平板外层「诗集 ↔ 散页」拖拽进行中时，暂停内层阅读器横向 Draggable，
		// 避免右栏随画布平移经过初始触点时触发 MyGo draggableAlong 抢占外层手势。
		if isTablet && a.shelfDragging {
			poem := poems[idx]
			ui.Scroll(c.Key("poem-scroll-" + poem.ID)).Fill().Children(func() {
				ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(28, 28, 48, 28).Children(func() {
					a.renderPoemContent(c, pal, book, poem, true)
				})
			})
			return
		}

		// 划选诗行模式下锁定横向滑切，防止点按/微拖选行误翻页；
		// 同时在平板双栏模式下拦截右栏横向手势，避免误触发外层「诗集/散页」切换。
		if a.selectingLines {
			lockOffset := ui.Point{}
			locked := ui.Box(c.Key("poem-selecting-lock")).Fill().Draggable(&lockOffset, ui.DraggableOptions{
				Axis:   ui.Horizontal,
				Bounds: &ui.Rect{X: 0, W: 0},
				Rigid:  true,
			})
			locked.Children(func() {
				poem := poems[idx]
				ui.Scroll(c.Key("poem-scroll-" + poem.ID)).Fill().Children(func() {
					ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(28, 28, 48, 28).Children(func() {
						a.renderPoemContent(c, pal, book, poem, true)
					})
				})
			})
			return
		}

		trackKey := fmt.Sprintf("poem-track-%s-%d", book.ID, a.poemPager.epoch)
		track := ui.Box(c.Key(trackKey)).
			Absolute().
			Left(0).Top(0).Bottom(0).
			Width(width * float32(total)).
			Transition(theme.PoemJumpTransition())

		curIdx := idx
		startX := -float32(curIdx) * width
		threshold := min(width*0.26, 140)
		track.Draggable(&a.poemPager.offset, ui.DraggableOptions{
			Axis:      ui.Horizontal,
			Bounds:    &ui.Rect{X: -float32(total-1) * width, W: float32(total-1) * width},
			Animation: theme.PageSpring,
			Snap: func(p ui.Point) ui.Point {
				// 方向性翻页阈值（兼顾平板宽栏轻划手感与短距离防误触回弹），单次最多切一首
				to := curIdx
				delta := p.X - startX
				switch {
				case delta < -threshold && curIdx+1 < total:
					to = curIdx + 1
				case delta > threshold && curIdx > 0:
					to = curIdx - 1
				}
				return ui.Point{X: -float32(to) * width}
			},
		})

		shownX := track.DragOffset().X
		at := max(0, min(total-1, int(math.Round(float64(-shownX/width)))))
		lo, hi := max(0, at-1), min(total-1, at+1)

		// O(1) 窗口化渲染：仅构建当前页及其紧邻前后两首诗，无论诗集百首还是千首都恒定开销
		track.Children(func() {
			for i := lo; i <= hi; i++ {
				poem := poems[i]
				isCurrent := i == a.activePoemIdx
				ui.Box(c.Key("poem-page-" + poem.ID)).
					Absolute().
					Left(float32(i) * width).
					Top(0).Bottom(0).
					Width(width).
					Children(func() {
						ui.Scroll(c.Key("poem-scroll-" + poem.ID)).Fill().Children(func() {
							ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(28, 28, 48, 28).Children(func() {
								a.renderPoemContent(c, pal, book, poem, isCurrent)
							})
						})
					})
			}
		})

		// 松手落点确定后立即同步顶栏标题、收藏态、页码与已读进度，正文继续平滑完成弹簧落位
		if !track.Dragging() {
			target := max(0, min(total-1, int(math.Round(float64(-a.poemPager.offset.X/width)))))
			if target != a.activePoemIdx {
				a.poemPager.syncedPage = target
				a.commitPoemIndex(book.ID, target)
				c.AnimationFrame()
			}
		}
	})
}

func (a *AppUI) renderRemoteBookState(c *ui.Context, pal theme.Palette, book domain.Book, isTablet bool) {
	if !isTablet {
		ui.Row(c).FillWidth().Padding(14, 18).AlignItems(ui.Center).Children(func() {
			back := ui.Row(c.Key("remote-back")).Gap(4).AlignItems(ui.Center).Cursor(ui.CursorPointer)
			back.Children(func() {
				ui.Icon(c, components.IconBack).Width(18).Height(18).TextColor(pal.TextPrimary)
				ui.Text(c, "书架").FontSize(14).TextColor(pal.TextPrimary)
			})
			if back.Clicked() {
				a.mu.Lock()
				a.openSeq++
				a.mu.Unlock()
				a.readerMobile = false
			}
		})
	}

	a.mu.Lock()
	isDownloading := a.downloadingBooks[book.ID]
	a.mu.Unlock()

	ui.Column(c).Grow(1).FillWidth().Center().Gap(14).Padding(32).Children(func() {
		ui.Icon(c, components.IconCloud).Width(36).Height(36).TextColor(pal.TextMuted)
		ui.Text(c, book.Title).FontSize(22).Bold().TextColor(pal.TextPrimary)
		ui.Textf(c, "%s · %d 首", book.Author, book.PoemCount).FontSize(13).TextColor(pal.TextMuted)
		if isDownloading {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "正在按需下载诗集正文…").FontSize(13).TextColor(pal.TextSecondary)
			})
		} else {
			if a.readerError != "" {
				ui.Text(c, a.readerError).FontSize(12).TextColor(pal.Accent)
			}
			if ui.PrimaryButton(c, "按需下载并开始阅读").Clicked() {
				a.mu.Lock()
				a.openSeq++
				reqSeq := a.openSeq
				a.mu.Unlock()
				a.downloadBookAsync(book.ID, a.pendingPoemID, a.highlightRanges, reqSeq, a.readerEntrySource)
			}
		}
	})
}

func (a *AppUI) renderReaderHeader(c *ui.Context, pal theme.Palette, book domain.Book, poem domain.Poem, isFav bool, isTablet bool) {
	headerText := fmt.Sprintf("《%s》", book.Title)
	if poem.Section != "" {
		headerText += " · " + poem.Section
	} else if book.Author != "" {
		headerText += " · " + book.Author
	}

	ui.Row(c.Key("reader-header")).
		FillWidth().
		Padding(14, 24, 12, 24).
		AlignItems(ui.Center).
		Gap(10).
		Children(func() {
			if !isTablet {
				backBtn := ui.Box(c.Key("reader-back")).Width(32).Height(32).Center().Radius(6).Cursor(ui.CursorPointer)
				theme.ApplyPressScale(backBtn, 0.90)
				backBtn.Children(func() {
					ui.Icon(c, components.IconBack).Width(20).Height(20).TextColor(pal.TextPrimary)
				})
				if backBtn.Clicked() {
					a.flushCurrentPoemDwell(false)
					a.readerMobile = false
					a.selectingLines = false
					a.selectedLines = map[int]bool{}
				}
			}

			ui.Text(c, headerText).Font(theme.FontNote).FontSize(13).TextColor(pal.TextMuted).MaxLines(1).Grow(1)

			// 目录与书本管理
			tocBtn := ui.Box(c.Key("reader-toc")).Width(34).Height(34).Center().Radius(6).Cursor(ui.CursorPointer)
			theme.ApplyPressScale(tocBtn, 0.90)
			tocBtn.Children(func() {
				ui.Icon(c, components.IconList).Width(19).Height(19).TextColor(pal.TextPrimary)
			})
			if tocBtn.Clicked() {
				a.showTOCModal = true
			}

			// 整诗 ❤️ 精选
			if poem.ID != "" {
				favBtn := ui.Box(c.Key("reader-fav")).Width(34).Height(34).Center().Radius(6).Cursor(ui.CursorPointer)
				theme.ApplyPressScale(favBtn, 0.88)
				favBtn.Children(func() {
					if isFav {
						ui.Icon(c, components.IconHeartFilled).Width(19).Height(19).TextColor(pal.Accent)
					} else {
						ui.Icon(c, components.IconHeart).Width(19).Height(19).TextColor(pal.TextPrimary)
					}
				})
				if favBtn.Clicked() {
					a.toggleCurrentPoemFavorite(book, poem, !isFav)
				}

				// 🔖 划选留作散页 / 设为本诗金句
				bmBtn := ui.Box(c.Key("reader-excerpt")).Width(34).Height(34).Center().Radius(6).Cursor(ui.CursorPointer).
					Transition(theme.ColorTransition())
				theme.ApplyPressScale(bmBtn, 0.90)
				if a.selectingLines {
					bmBtn.Background(pal.HighlightBg)
				}
				bmBtn.Children(func() {
					clr := pal.TextPrimary
					if a.selectingLines {
						clr = pal.Accent
					}
					ui.Icon(c, components.IconBookmark).Width(18).Height(18).TextColor(clr)
				})
				if bmBtn.Clicked() {
					a.selectingLines = !a.selectingLines
					a.selectedLines = map[int]bool{}
				}

				// 导出整诗长图 / 本诗金句卡片
				cardBtn := ui.Box(c.Key("reader-share-card")).Width(34).Height(34).Center().Radius(6).Cursor(ui.CursorPointer)
				theme.ApplyPressScale(cardBtn, 0.90)
				cardBtn.Children(func() {
					ui.Icon(c, components.IconShareCard).Width(18).Height(18).TextColor(pal.TextPrimary)
				})
				if cardBtn.Clicked() {
					a.openPoemShareCard(book, poem)
				}
			}

			// Aa 排版面板
			aaBtn := ui.Box(c.Key("reader-aa")).Padding(6, 8).Center().Radius(6).Cursor(ui.CursorPointer)
			theme.ApplyPressScale(aaBtn, 0.92)
			aaBtn.Children(func() {
				ui.Text(c, "Aa").Font(theme.FontSerif).FontSize(15).Bold().TextColor(pal.TextPrimary)
			})
			if aaBtn.Clicked() {
				a.showTypoModal = true
			}
		})
}

func (a *AppUI) renderPeekProgressBanner(c *ui.Context, pal theme.Palette, book domain.Book, current domain.Poem, isTablet bool) {
	mainIdx, mainPoem, hasMainline := a.mainlineProgressInfo(book.ID)
	atMainline := hasMainline && mainIdx == a.activePoemIdx

	var summary string
	switch {
	case atMainline:
		if mainPoem.Title != "" {
			summary = fmt.Sprintf("已回到上次进度 · 第 %d 首《%s》", mainIdx+1, mainPoem.Title)
		} else {
			summary = fmt.Sprintf("已回到上次进度 · 第 %d 首", mainIdx+1)
		}
	case hasMainline:
		if mainPoem.Title != "" {
			summary = fmt.Sprintf("来自%s · 顺序进度在第 %d 首《%s》", a.readerEntrySource, mainIdx+1, mainPoem.Title)
		} else {
			summary = fmt.Sprintf("来自%s · 顺序进度在第 %d 首", a.readerEntrySource, mainIdx+1)
		}
	default:
		summary = fmt.Sprintf("来自%s · 当前浏览不改顺序进度", a.readerEntrySource)
	}

	renderPill := func(key, label string, primary bool, onClick func()) {
		bg, fg, bd := pal.ReaderPaper, pal.TextPrimary, pal.Border
		if primary {
			bg, fg, bd = pal.Accent, pal.AccentText, pal.Accent
		}
		pill := ui.Box(c.Key(key)).
			Padding(5, 11).
			Radius(999).
			Background(bg).
			Border(1, bd).
			Cursor(ui.CursorPointer).
			Transition(theme.ColorTransition())
		theme.ApplyPressScale(pill, 0.94)
		pill.Children(func() {
			t := ui.Text(c, label).FontSize(12).TextColor(fg)
			if primary {
				t.Bold()
			}
		})
		if pill.Clicked() {
			onClick()
		}
	}

	renderActions := func() {
		switch {
		case atMainline:
			renderPill("peek-resume-main", "恢复顺序阅读", true, func() {
				a.returnToMainlineProgress(book.ID)
			})
		case hasMainline:
			renderPill("peek-return-main", "回到进度", true, func() {
				a.returnToMainlineProgress(book.ID)
			})
			if current.IsPoem() {
				renderPill("peek-adopt-cur", "从这里续读", false, func() {
					a.adoptCurrentAsMainlineProgress(book.ID)
				})
			}
		default:
			if current.IsPoem() {
				renderPill("peek-start-here", "从这里开始读", true, func() {
					a.adoptCurrentAsMainlineProgress(book.ID)
				})
			}
		}
	}

	renderDismiss := func() {
		closeBtn := ui.Box(c.Key("peek-dismiss")).Width(24).Height(24).Center().Radius(6).Cursor(ui.CursorPointer)
		theme.ApplyPressScale(closeBtn, 0.90)
		closeBtn.Children(func() {
			ui.Icon(c, components.IconClose).Width(13).Height(13).TextColor(pal.TextMuted)
		})
		if closeBtn.Clicked() {
			a.peekBannerHidden = true
		}
	}

	container := ui.Column(c.Key("peek-progress-bar")).
		FillWidth().
		Padding(8, 20).
		Gap(7).
		Background(pal.HighlightBg).
		BorderColor(pal.Border).
		BorderWidth(1, 0, 1, 0).
		Transition(theme.BarRevealTransition())

	container.Children(func() {
		if isTablet {
			ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, summary).FontSize(12).TextColor(pal.TextSecondary).MaxLines(1).Grow(1)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					renderActions()
					renderDismiss()
				})
			})
			return
		}

		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, summary).FontSize(12).TextColor(pal.TextSecondary).MaxLines(1).Grow(1)
			renderDismiss()
		})
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			renderActions()
		})
	})
}

func (a *AppUI) renderSelectionActionBar(c *ui.Context, pal theme.Palette, book domain.Book, poem domain.Poem) {
	selectedCount := len(a.selectedLines)
	ui.Row(c.Key("selection-bar")).
		FillWidth().
		Padding(10, 24).
		Gap(8).
		Wrap().
		AlignItems(ui.Center).
		Background(pal.HighlightBg).
		BorderColor(pal.Border).
		BorderWidth(1, 0, 1, 0).
		Transition(theme.BarRevealTransition()).
		Children(func() {
			if selectedCount == 0 {
				ui.Text(c, "点按下方诗行勾选句子（同一首诗可多次划选不同段落）").
					FontSize(12).TextColor(pal.TextSecondary).Grow(1)
			} else {
				ui.Textf(c, "已选 %d 行", selectedCount).FontSize(13).Bold().TextColor(pal.TextPrimary).Grow(1)
				if ui.PrimaryButton(c, "🔖 留作散页").Clicked() {
					a.saveSelectedLinesAsQuote(book, poem)
				}
				if ui.Button(c, "出卡").Clicked() {
					content, _ := a.collectSelectedText(poem)
					a.openSelectionShareCard(book, poem, content)
				}
				if ui.Button(c, "设为本诗金句").Clicked() {
					a.saveSelectedLinesAsWidgetExcerpt(book, poem)
				}
			}
			if ui.Button(c, "完成").Clicked() {
				a.selectingLines = false
				a.selectedLines = map[int]bool{}
			}
		})
}

func (a *AppUI) renderPoemContent(c *ui.Context, pal theme.Palette, book domain.Book, poem domain.Poem, isCurrent bool) {
	fontSize := a.settings.FontSize
	if fontSize < 14 {
		fontSize = 18
	}
	lineHeight := a.settings.LineHeight
	if lineHeight < 1.3 {
		lineHeight = 1.85
	}
	poetryFont := theme.PoetryFont(a.settings.FontFamily)
	fontWeight := a.settings.EffectiveFontWeight()
	noteFont := theme.FontNote
	if strings.TrimSpace(a.settings.FontFamily) != "" {
		noteFont = poetryFont
	}

	ui.Column(c.Key("poem-column")).
		FillWidth().
		MaxWidth(layout.ReadingMaxWidthDIP).
		Gap(16).
		Children(func() {
			// 诗题 + 副题 + 作者
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, poem.Title).Font(poetryFont).FontSize(fontSize + 9).Bold().LetterSpacing(0.4).TextColor(pal.TextPrimary)
				if poem.Subtitle != "" {
					ui.Text(c, poem.Subtitle).Font(noteFont).FontSize(fontSize - 3).Italic().TextColor(pal.TextSecondary)
				}
				author := poem.Author
				if author == "" {
					author = book.Author
				}
				if author != "" {
					ui.Text(c, author).Font(noteFont).FontSize(fontSize - 3).TextColor(pal.TextMuted)
				}
				if !poem.IsPoem() {
					ui.Text(c, "附文 · 不计入完读与小组件池").Font(theme.FontSans).FontSize(11).TextColor(pal.TextMuted)
				}
			})

			// 题记（Epigraph）
			if strings.TrimSpace(poem.Epigraph) != "" {
				ui.Row(c).FillWidth().Gap(10).Padding(4, 0).Children(func() {
					ui.Box(c).Width(2).FillHeight().Background(pal.Border)
					ui.Text(c, poem.Epigraph).Font(noteFont).FontSize(fontSize - 3).Italic().LineHeight(lineHeight).TextColor(pal.TextSecondary).Grow(1)
				})
			}

			// 诗节与逐行渲染（支持划选与 Deeplink 高亮）
			lineNum := 0
			ui.Column(c).Gap(18).Padding(8, 0, 8, 0).FillWidth().Children(func() {
				for si, stanza := range poem.Stanzas {
					ui.Column(c.Key(fmt.Sprintf("stanza-%d", si))).Gap(4).FillWidth().Children(func() {
						for _, line := range stanza.Lines {
							lineNum++
							curLine := lineNum
							isHighlighted := isCurrent && a.isLineHighlighted(curLine)
							isSelected := isCurrent && a.selectedLines[curLine]

							lineRow := ui.Box(c.Key(fmt.Sprintf("ln-%d", curLine))).
								FillWidth().
								Padding(3, 8).
								Radius(4).
								Transition(theme.ColorTransition())
							if isSelected {
								lineRow.Background(pal.HighlightBg).Border(1, pal.Accent)
							} else if isHighlighted {
								lineRow.Background(pal.HighlightBg)
							}
							if isCurrent && a.selectingLines {
								lineRow.Cursor(ui.CursorPointer)
							}
							lineRow.Children(func() {
								ui.Text(c, line).
									Font(poetryFont).
									FontSize(fontSize).
									FontWeight(fontWeight).
									LineHeight(lineHeight).
									TextColor(pal.TextPrimary)
							})
							if isCurrent && a.selectingLines && lineRow.Clicked() {
								if a.selectedLines[curLine] {
									delete(a.selectedLines, curLine)
								} else {
									a.selectedLines[curLine] = true
								}
							}
						}
					})
				}
			})

			// 落款 / 写作时间地点（Colophon）
			if strings.TrimSpace(poem.Colophon) != "" {
				ui.Row(c).FillWidth().Justify(ui.End).Padding(8, 0, 0, 0).Children(func() {
					ui.Text(c, poem.Colophon).Font(noteFont).FontSize(fontSize - 4).Italic().TextColor(pal.TextMuted)
				})
			}

			// 诗末内联注释（与 stillpoets-v3-tablet.png 一致）
			if len(poem.Notes) > 0 {
				ui.Column(c.Key("poem-notes")).
					FillWidth().
					Padding(18, 0, 0, 0).
					Gap(8).
					BorderColor(pal.Border).
					BorderWidth(1, 0, 0, 0).
					Children(func() {
						for ni, note := range poem.Notes {
							label := strings.TrimSpace(note.Label)
							if label == "" {
								label = fmt.Sprintf("%d", ni+1)
							}
							ui.Row(c.Key(fmt.Sprintf("note-%d", ni))).FillWidth().Gap(8).AlignItems(ui.Start).Children(func() {
								ui.Text(c, label).Font(theme.FontMono).FontSize(11).Bold().TextColor(pal.TextMuted)
								ui.Text(c, note.Body).Font(theme.FontSans).FontSize(13).LineHeight(1.65).TextColor(pal.TextSecondary).Grow(1)
							})
						}
					})
			}
		})
}

func (a *AppUI) renderReaderFooter(c *ui.Context, pal theme.Palette, book domain.Book, poems []domain.Poem) {
	idx := a.activePoemIdx
	total := len(poems)

	ui.Row(c.Key("reader-footer")).
		FillWidth().
		Padding(12, 24).
		AlignItems(ui.Center).
		BorderColor(pal.Border).
		BorderWidth(1, 0, 0, 0).
		Children(func() {
			prevBtn := ui.Box(c.Key("poem-prev")).Padding(6, 10).Radius(6)
			if idx > 0 {
				prevBtn.Cursor(ui.CursorPointer)
				theme.ApplyPressScale(prevBtn, 0.94)
			}
			prevBtn.Children(func() {
				clr := pal.TextPrimary
				if idx <= 0 {
					clr = pal.Border
				}
				ui.Text(c, "← 上一首").FontSize(13).TextColor(clr)
			})
			if idx > 0 && prevBtn.Clicked() {
				a.navigatePoem(book.ID, idx-1)
			}

			ui.Spacer(c)

			counter := ui.Box(c.Key("poem-counter")).Padding(6, 12).Radius(6).Cursor(ui.CursorPointer)
			theme.ApplyPressScale(counter, 0.94)
			counter.Children(func() {
				ui.Textf(c, "第 %d / %d 首", idx+1, total).Font(theme.FontMono).FontFeatures("tnum").FontSize(12).TextColor(pal.TextMuted)
			})
			if counter.Clicked() {
				a.showTOCModal = true
			}

			ui.Spacer(c)

			nextBtn := ui.Box(c.Key("poem-next")).Padding(6, 10).Radius(6)
			if idx+1 < total {
				nextBtn.Cursor(ui.CursorPointer)
				theme.ApplyPressScale(nextBtn, 0.94)
			}
			nextBtn.Children(func() {
				clr := pal.TextPrimary
				if idx+1 >= total {
					clr = pal.Border
				}
				ui.Text(c, "下一首 →").FontSize(13).TextColor(clr)
			})
			if idx+1 < total && nextBtn.Clicked() {
				a.navigatePoem(book.ID, idx+1)
			}
		})
}

func (a *AppUI) commitPoemIndex(bookID string, nextIdx int) {
	if nextIdx < 0 || nextIdx >= len(a.activePoems) {
		return
	}
	a.activePoemIdx = nextIdx
	a.highlightRanges = nil
	a.selectingLines = false
	a.selectedLines = map[int]bool{}

	poem := a.activePoems[nextIdx]
	bookAuthor := ""
	if bk, ok := a.activeBook(); ok {
		bookAuthor = bk.Author
	}
	wasReadBefore := slices.Contains(a.progressMap[bookID].ReadPoemIDs, poem.ID)
	a.startPoemDwell(bookID, poem, bookAuthor, wasReadBefore)

	if a.readerEntrySource.isPeek() {
		a.recordPoemPeekAsync(bookID, poem, a.activePoems)
		return
	}
	a.recordPoemReadAsync(bookID, poem, a.activePoems)
}

func (a *AppUI) mainlineProgressInfo(bookID string) (int, domain.Poem, bool) {
	prog, ok := a.progressMap[bookID]
	if !ok || prog.LastPoemID == "" {
		return -1, domain.Poem{}, false
	}
	for i, p := range a.activePoems {
		if p.ID == prog.LastPoemID {
			return i, p, true
		}
	}
	return -1, domain.Poem{}, false
}

// returnToMainlineProgress 从偶遇浏览一键跳回本书原本的顺序阅读进度，并恢复主线追踪。
func (a *AppUI) returnToMainlineProgress(bookID string) {
	mainIdx, mainPoem, ok := a.mainlineProgressInfo(bookID)
	if !ok {
		a.readerEntrySource = entryMainline
		a.peekBannerHidden = false
		return
	}
	a.readerEntrySource = entryMainline
	a.peekBannerHidden = false
	a.highlightRanges = nil
	a.selectingLines = false
	a.selectedLines = map[int]bool{}
	a.activePoemIdx = mainIdx
	a.recordPoemReadAsync(bookID, mainPoem, a.activePoems)
	a.showTransientToastOnUI("已回到顺序阅读进度")
}

// adoptCurrentAsMainlineProgress 显式将当前偶遇浏览的诗篇设为新的顺序阅读进度，并退出偶遇模式。
func (a *AppUI) adoptCurrentAsMainlineProgress(bookID string) {
	if a.activePoemIdx < 0 || a.activePoemIdx >= len(a.activePoems) {
		return
	}
	cur := a.activePoems[a.activePoemIdx]
	if !cur.IsPoem() {
		a.showTransientToastOnUI("附文不计入阅读进度")
		return
	}
	a.readerEntrySource = entryMainline
	a.peekBannerHidden = false
	a.recordPoemReadAsync(bookID, cur, a.activePoems)
	a.showTransientToastOnUI("已更新为当前阅读进度")
}

func (a *AppUI) navigatePoem(bookID string, nextIdx int) {
	a.commitPoemIndex(bookID, nextIdx)
}

func (a *AppUI) isLineHighlighted(lineNum int) bool {
	for _, r := range a.highlightRanges {
		if r.Contains(lineNum) {
			return true
		}
	}
	return false
}

func (a *AppUI) collectSelectedText(poem domain.Poem) (string, string) {
	if len(a.selectedLines) == 0 {
		return "", ""
	}
	nums := make([]int, 0, len(a.selectedLines))
	for k, ok := range a.selectedLines {
		if ok {
			nums = append(nums, k)
		}
	}
	slices.Sort(nums)
	wanted := make(map[int]bool, len(nums))
	for _, n := range nums {
		wanted[n] = true
	}

	var lines []string
	cur := 0
	for _, stanza := range poem.Stanzas {
		for _, line := range stanza.Lines {
			cur++
			if wanted[cur] {
				lines = append(lines, strings.TrimSpace(line))
			}
		}
	}
	return strings.Join(lines, "\n"), app.FormatLineRanges(nums)
}

func (a *AppUI) saveSelectedLinesAsQuote(book domain.Book, poem domain.Poem) {
	content, lineSpec := a.collectSelectedText(poem)
	if content == "" {
		return
	}
	a.flushCurrentPoemDwell(true)
	a.selectingLines = false
	a.selectedLines = map[int]bool{}
	a.highlightRanges = app.ParseLineRanges(lineSpec)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := a.svc.CreatePoemExcerptQuote(ctx, book, poem, content, lineSpec); err != nil {
			a.showTransientToast("留存散页失败：" + err.Error())
			return
		}
		a.reloadSync(ctx)
		a.showTransientToast("已留作诗歌节选散页")
	}()
}

func (a *AppUI) saveSelectedLinesAsWidgetExcerpt(book domain.Book, poem domain.Poem) {
	content, lineSpec := a.collectSelectedText(poem)
	if content == "" {
		return
	}
	a.flushCurrentPoemDwell(true)
	a.selectingLines = false
	a.selectedLines = map[int]bool{}
	a.highlightRanges = app.ParseLineRanges(lineSpec)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.svc.SetFavoriteCustomExcerpt(ctx, book, poem, content, lineSpec); err != nil {
			a.showTransientToast("设置金句失败：" + err.Error())
			return
		}
		a.reloadSync(ctx)
		a.showTransientToast("已设为本诗小组件金句")
	}()
}

func (a *AppUI) toggleCurrentPoemFavorite(book domain.Book, poem domain.Poem, active bool) {
	if active {
		a.flushCurrentPoemDwell(true)
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := a.svc.ToggleFavoritePoem(ctx, book, poem, active); err != nil {
			a.showTransientToast("操作失败：" + err.Error())
			return
		}
		a.reloadSync(ctx)
		if active {
			a.showTransientToast("已加入诗篇精选")
		} else {
			a.showTransientToast("已取消精选")
		}
	}()
}
