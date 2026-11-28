package views

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/domain"
	"stillpoets/internal/storage"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/layout"
	"stillpoets/internal/ui/theme"
)

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
