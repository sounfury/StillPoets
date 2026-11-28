package sharecard

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/ui/theme"
)

// RenderCard 是预览弹窗与离屏 PNG 导出共享的唯一卡片渲染视图。
// 返回卡片外层根容器（宽度固定为 widthDIP，高度由内容自然撑开），便于离屏导出精确测量动态高度。
func RenderCard(c *ui.Context, data CardData, opts Options, widthDIP float32) {
	if widthDIP <= 0 {
		widthDIP = float32(DefaultRenderConfig().WidthDIP)
	}
	pal := PaletteByID(opts.Theme)

	switch opts.Template {
	case TemplateArtbook:
		renderArtbookShell(c, data, opts, pal, widthDIP)
	case TemplateSeal:
		renderSealShell(c, data, opts, pal, widthDIP)
	case TemplateEditorial:
		renderEditorialShell(c, data, opts, pal, widthDIP)
	case TemplateCalendar:
		renderCalendarShell(c, data, opts, pal, widthDIP)
	default:
		renderMinimalShell(c, data, opts, pal, widthDIP)
	}
}

// ---------- 5 套模板外壳 ----------

func renderMinimalShell(c *ui.Context, data CardData, opts Options, pal CardPalette, widthDIP float32) {
	root := ui.Column(c.Key("sc-card-root")).
		Width(widthDIP).
		Background(pal.PaperBg).
		Padding(26, 26, 24, 26).
		Gap(18).
		Draw(func(p *ui.Painter, r ui.Rect) {
			if pal.GrainOpacity > 0 {
				theme.DrawPaperTexture(p, r, 0, pal.IsDark, pal.GrainOpacity)
			}
			// 四周极轻细线框（距边 12 DIP）
			in := float32(12)
			if r.W > in*2 && r.H > in*2 {
				p.Stroke(ui.Rect{X: r.X + in, Y: r.Y + in, W: r.W - in*2, H: r.H - in*2}, pal.Border, 0.85, 2)
			}
		})

	root.Children(func() {
		// 顶部克制微标
		ui.Row(c.Key("sc-min-top")).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Box(c.Key("sc-min-dot")).Width(5).Height(5).Radius(99).Background(pal.Accent)
			ui.Text(c, "  "+cardTopTag(data)).
				Font(theme.FontMono).FontSize(9.5).LetterSpacing(0.8).TextColor(pal.TextMuted).Grow(1)
			ui.Text(c, data.EffectiveDate().Format("2006.01.02")).
				Font(theme.FontMono).FontFeatures("tnum").FontSize(9.5).TextColor(pal.TextMuted)
		})

		renderCardBody(c, data, pal, false)

		if opts.ShowWatermark {
			renderCardFooter(c, data, pal, false)
		}
	})
}

func renderSealShell(c *ui.Context, data CardData, opts Options, pal CardPalette, widthDIP float32) {
	root := ui.Column(c.Key("sc-card-root")).
		Width(widthDIP).
		Background(pal.PaperBg).
		Padding(30, 28, 28, 28).
		Gap(18).
		Draw(func(p *ui.Painter, r ui.Rect) {
			if pal.GrainOpacity > 0 {
				theme.DrawPaperTexture(p, r, 0, pal.IsDark, pal.GrainOpacity*1.08)
			}
			drawDoubleFrame(p, r, pal.BorderStrong, pal.Border)
		})

	root.Children(func() {
		// 古典细眉
		ui.Row(c.Key("sc-seal-top")).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "〔 "+cardTopTag(data)+" 〕").
				Font(theme.FontNote).FontSize(10.5).LetterSpacing(1.2).TextColor(pal.TextMuted).Grow(1)
			ui.Text(c, formatChineseDate(data.EffectiveDate())).
				Font(theme.FontNote).FontSize(10).TextColor(pal.TextMuted)
		})

		renderCardBody(c, data, pal, false)

		// 底部矢量朱文方印 + 极轻落款
		ui.Row(c.Key("sc-seal-bottom")).FillWidth().AlignItems(ui.End).Padding(6, 0, 0, 0).Children(func() {
			ui.Column(c.Key("sc-seal-wm")).Grow(1).Gap(2).Children(func() {
				if opts.ShowWatermark {
					ui.Text(c, "StillPoets").
						Font(theme.FontBrand).FontSize(12).Bold().TextColor(pal.TextMuted)
					ui.Text(c, "静读诗笺").
						Font(theme.FontNote).FontSize(9.5).LetterSpacing(1.4).TextColor(pal.TextMuted)
				}
			})
			ui.Box(c.Key("sc-seal-stamp")).Width(28).Height(28).Draw(func(p *ui.Painter, r ui.Rect) {
				drawClassicalSeal(p, r, pal.SealColor)
			})
		})
	})
}

func renderEditorialShell(c *ui.Context, data CardData, opts Options, pal CardPalette, widthDIP float32) {
	d := data.EffectiveDate()
	issueNo := fmt.Sprintf("NO.%02d%02d", int(d.Month()), d.Day())

	root := ui.Column(c.Key("sc-card-root")).
		Width(widthDIP).
		Background(pal.PaperBg).
		Padding(24, 24, 22, 24).
		Gap(16).
		Draw(func(p *ui.Painter, r ui.Rect) {
			if pal.GrainOpacity > 0 {
				theme.DrawPaperTexture(p, r, 0, pal.IsDark, pal.GrainOpacity)
			}
		})

	root.Children(func() {
		// 刊物顶部双栏刊头
		ui.Column(c.Key("sc-ed-masthead")).FillWidth().Gap(6).Children(func() {
			ui.Row(c.Key("sc-ed-mast-row")).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "STILLPOETS").
					Font(theme.FontBrand).FontSize(12.5).Bold().LetterSpacing(1.5).TextColor(pal.TextPrimary).Grow(1)
				ui.Text(c, issueNo+" · "+cardTopTag(data)).
					Font(theme.FontMono).FontFeatures("tnum").FontSize(9.5).LetterSpacing(0.6).TextColor(pal.Accent)
			})
			// 刊物双横线（上粗下细）
			ui.Box(c.Key("sc-ed-rule-1")).FillWidth().Height(1.6).Background(pal.BorderStrong)
			ui.Box(c.Key("sc-ed-rule-2")).FillWidth().Height(0.6).Background(pal.Border)
		})

		// 主体区：左侧刊物强调竖条
		ui.Row(c.Key("sc-ed-body-wrap")).FillWidth().Gap(14).AlignItems(ui.Stretch).Children(func() {
			ui.Box(c.Key("sc-ed-accent-bar")).Width(2.6).Radius(2).Background(pal.Accent)
			ui.Column(c.Key("sc-ed-body-col")).Grow(1).Gap(14).Children(func() {
				renderCardBody(c, data, pal, true)
			})
		})

		if opts.ShowWatermark {
			renderCardFooter(c, data, pal, true)
		}
	})
}

func renderCalendarShell(c *ui.Context, data CardData, opts Options, pal CardPalette, widthDIP float32) {
	d := data.EffectiveDate()
	weekdays := [...]string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}
	monthsEN := [...]string{"", "JAN", "FEB", "MAR", "APR", "MAY", "JUN", "JUL", "AUG", "SEP", "OCT", "NOV", "DEC"}

	root := ui.Column(c.Key("sc-card-root")).
		Width(widthDIP).
		Background(pal.PaperBg).
		Padding(26, 24, 24, 24).
		Gap(18).
		Draw(func(p *ui.Painter, r ui.Rect) {
			if pal.GrainOpacity > 0 {
				theme.DrawPaperTexture(p, r, 0, pal.IsDark, pal.GrainOpacity)
			}
		})

	root.Children(func() {
		// 日历头：左侧超大日号 + 中间年月星期 + 右侧卡片标签
		ui.Row(c.Key("sc-cal-head")).FillWidth().AlignItems(ui.End).Gap(14).Children(func() {
			ui.Text(c, fmt.Sprintf("%02d", d.Day())).
				Font(theme.FontBrand).FontFeatures("tnum").FontSize(42).Bold().LineHeight(0.95).TextColor(pal.Accent)
			ui.Column(c.Key("sc-cal-meta")).Grow(1).Gap(3).Padding(0, 0, 4, 0).Children(func() {
				ui.Text(c, fmt.Sprintf("%s · %d", monthsEN[d.Month()], d.Year())).
					Font(theme.FontMono).FontFeatures("tnum").FontSize(10.5).LetterSpacing(1.0).TextColor(pal.TextSecondary)
				ui.Text(c, fmt.Sprintf("%d 月 · %s", int(d.Month()), weekdays[d.Weekday()])).
					Font(theme.FontNote).FontSize(11.5).TextColor(pal.TextMuted)
			})
			ui.Box(c.Key("sc-cal-badge")).Padding(4, 9).Radius(999).Background(pal.AccentSoft).Children(func() {
				ui.Text(c, cardTopTag(data)).Font(theme.FontSans).FontSize(9.5).Bold().TextColor(pal.Accent)
			})
		})

		// 细分割线
		ui.Box(c.Key("sc-cal-divider")).FillWidth().Height(0.9).Background(pal.Border)

		renderCardBody(c, data, pal, false)

		if opts.ShowWatermark {
			renderCardFooter(c, data, pal, false)
		}
	})
}

func renderArtbookShell(c *ui.Context, data CardData, opts Options, pal CardPalette, widthDIP float32) {
	shadowClr := ui.RGBA(24, 20, 14, 0.14)
	if pal.IsDark {
		shadowClr = ui.RGBA(0, 0, 0, 0.45)
	}

	root := ui.Column(c.Key("sc-card-root")).
		Width(widthDIP).
		Background(pal.OuterBg).
		Padding(16).
		Draw(func(p *ui.Painter, r ui.Rect) {
			if pal.GrainOpacity > 0 {
				theme.DrawPaperTexture(p, r, 0, pal.IsDark, pal.GrainOpacity*1.1)
			}
		})

	root.Children(func() {
		inner := ui.Column(c.Key("sc-art-inner")).
			FillWidth().
			Background(pal.PaperBg).
			Padding(22, 22, 22, 22).
			Gap(16).
			Radius(6).
			Border(0.75, pal.Border).
			Shadow(0, 4, 14, -2, shadowClr).
			Draw(func(p *ui.Painter, r ui.Rect) {
				if pal.GrainOpacity > 0 {
					theme.DrawPaperTexture(p, r, 6, pal.IsDark, pal.GrainOpacity*0.85)
				}
				// 顶部内嵌画册色带
				p.Fill(ui.Rect{X: r.X + 22, Y: r.Y, W: 36, H: 3}, pal.OuterCorner, 1.5)
			})

		inner.Children(func() {
			ui.Row(c.Key("sc-art-top")).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, cardTopTag(data)).
					Font(theme.FontMono).FontSize(9.5).LetterSpacing(1.0).TextColor(pal.Accent).Grow(1)
				ui.Text(c, data.EffectiveDate().Format("2006 / 01 / 02")).
					Font(theme.FontMono).FontFeatures("tnum").FontSize(9.5).TextColor(pal.TextMuted)
			})

			renderCardBody(c, data, pal, false)

			if opts.ShowWatermark {
				renderCardFooter(c, data, pal, false)
			}
		})
	})
}

func renderCardFooter(c *ui.Context, data CardData, pal CardPalette, compact bool) {
	topPad := float32(6)
	if compact {
		topPad = 2
	}
	ui.Column(c.Key("sc-footer")).FillWidth().Padding(topPad, 0, 0, 0).Gap(10).Children(func() {
		ui.Box(c.Key("sc-footer-line")).FillWidth().Height(0.75).Background(pal.Border)
		ui.Row(c.Key("sc-footer-row")).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "StillPoets").
				Font(theme.FontBrand).FontSize(11.5).Bold().LetterSpacing(0.4).TextColor(pal.TextMuted).Grow(1)
			ui.Text(c, "留给诗的静处").
				Font(theme.FontNote).FontSize(9.5).LetterSpacing(0.8).TextColor(pal.TextMuted)
		})
	})
}

// ---------- 5 类卡片主体内容 ----------

func renderCardBody(c *ui.Context, data CardData, pal CardPalette, compact bool) {
	switch data.Kind {
	case KindPoem:
		renderPoemBody(c, data, pal)
	case KindCollection:
		renderCollectionBody(c, data, pal)
	case KindFinished:
		renderFinishedBody(c, data, pal)
	case KindStatsReport:
		renderStatsReportBody(c, data, pal)
	default:
		renderQuoteBody(c, data, pal, compact)
	}
}

// 1. 单句卡片主体
func renderQuoteBody(c *ui.Context, data CardData, pal CardPalette, compact bool) {
	poetryFont := theme.PoetryFont(data.FontFamily)
	weight := data.EffectiveFontWeight()
	text := strings.TrimSpace(data.QuoteText)
	if text == "" {
		text = "不急着读完，\n给一句诗多一点时间。"
	}

	// 根据字数微调字号，短句更大气，长摘录更疏朗不显挤
	runeCount := len([]rune(text))
	fontSize := float32(19.5)
	lineHeight := float32(1.88)
	if runeCount > 110 {
		fontSize = 15.5
		lineHeight = 1.82
	} else if runeCount > 55 {
		fontSize = 17.2
		lineHeight = 1.85
	}

	topPad := float32(4)
	botPad := float32(6)
	if compact {
		topPad = 2
		botPad = 4
	}

	ui.Column(c.Key("sc-quote-body")).FillWidth().Padding(topPad, 0, botPad, 0).Gap(10).Children(func() {
		// 轻盈开引号装饰
		ui.Text(c, "“").
			Font(theme.FontBrand).FontSize(30).LineHeight(0.85).TextColor(pal.Accent).Opacity(0.55)

		ui.Text(c, text).
			Font(poetryFont).
			FontSize(fontSize).
			FontWeight(weight).
			LineHeight(lineHeight).
			TextColor(pal.TextPrimary)

		// 出处署名（右对齐纸书注脚）
		attr := buildQuoteAttribution(data)
		if attr != "" {
			ui.Row(c.Key("sc-quote-attr-row")).FillWidth().Justify(ui.End).Padding(6, 0, 0, 0).Children(func() {
				ui.Text(c, "—— "+attr).
					Font(theme.FontNote).FontSize(12).LineHeight(1.5).TextColor(pal.TextSecondary)
			})
		}
	})
}

// 2. 整诗长图主体
func renderPoemBody(c *ui.Context, data CardData, pal CardPalette) {
	poetryFont := theme.PoetryFont(data.FontFamily)
	weight := data.EffectiveFontWeight()

	ui.Column(c.Key("sc-poem-body")).FillWidth().Gap(14).Children(func() {
		// 诗题 + 副标题 + 诗人
		ui.Column(c.Key("sc-poem-header")).FillWidth().Gap(6).Children(func() {
			title := strings.TrimSpace(data.PoemTitle)
			if title == "" {
				title = "无题"
			}
			ui.Text(c, title).
				Font(poetryFont).FontSize(20).Bold().LineHeight(1.38).TextColor(pal.TextPrimary)

			if sub := strings.TrimSpace(data.PoemSubtitle); sub != "" {
				ui.Text(c, sub).
					Font(theme.FontNote).FontSize(12.5).LineHeight(1.5).TextColor(pal.TextSecondary)
			}

			meta := []string{}
			if a := strings.TrimSpace(data.Author); a != "" {
				meta = append(meta, a)
			}
			if sec := strings.TrimSpace(data.PoemSection); sec != "" {
				meta = append(meta, sec)
			}
			if len(meta) > 0 {
				ui.Text(c, strings.Join(meta, " · ")).
					Font(theme.FontNote).FontSize(12).TextColor(pal.TextMuted)
			}
		})

		// 题记（若有）
		if epi := strings.TrimSpace(data.Epigraph); epi != "" {
			ui.Row(c.Key("sc-poem-epigraph")).FillWidth().Gap(10).AlignItems(ui.Stretch).Children(func() {
				ui.Box(c.Key("sc-poem-epi-bar")).Width(2).Radius(1).Background(pal.BorderStrong)
				ui.Text(c, epi).
					Font(theme.FontNote).FontSize(12).Italic().LineHeight(1.65).TextColor(pal.TextSecondary).Grow(1)
			})
		}

		// 完整诗节正文
		ui.Column(c.Key("sc-poem-stanzas")).FillWidth().Gap(14).Padding(4, 0, 2, 0).Children(func() {
			if len(data.Stanzas) == 0 && strings.TrimSpace(data.QuoteText) != "" {
				ui.Text(c, strings.TrimSpace(data.QuoteText)).
					Font(poetryFont).FontSize(15.5).FontWeight(weight).LineHeight(1.85).TextColor(pal.TextPrimary)
				return
			}
			for si, stanza := range data.Stanzas {
				lines := make([]string, 0, len(stanza.Lines))
				for _, line := range stanza.Lines {
					if trimmed := strings.TrimRight(line, " \t"); trimmed != "" {
						lines = append(lines, trimmed)
					}
				}
				if len(lines) == 0 {
					continue
				}
				ui.Text(c.Key(fmt.Sprintf("sc-stanza-%d", si)), strings.Join(lines, "\n")).
					Font(poetryFont).
					FontSize(15.2).
					FontWeight(weight).
					LineHeight(1.86).
					TextColor(pal.TextPrimary)
			}
		})

		// 落款与原书出处
		colophon := strings.TrimSpace(data.Colophon)
		bookTitle := strings.TrimSpace(data.BookTitle)
		if bookTitle == "" {
			bookTitle = strings.TrimSpace(data.SourceBook)
		}
		if colophon != "" || bookTitle != "" {
			ui.Column(c.Key("sc-poem-tail")).FillWidth().Gap(4).Padding(6, 0, 0, 0).AlignItems(ui.End).Children(func() {
				if colophon != "" {
					ui.Text(c, colophon).Font(theme.FontNote).FontSize(11).Italic().TextColor(pal.TextMuted)
				}
				if bookTitle != "" {
					ui.Text(c, "选自《"+bookTitle+"》").Font(theme.FontNote).FontSize(11).TextColor(pal.TextSecondary)
				}
			})
		}
	})
}

// 3. 诗集精选合辑长图主体
func renderCollectionBody(c *ui.Context, data CardData, pal CardPalette) {
	poetryFont := theme.PoetryFont(data.FontFamily)
	weight := data.EffectiveFontWeight()

	ui.Column(c.Key("sc-col-body")).FillWidth().Gap(16).Children(func() {
		// 顶部书封 + 书名作者 + 精选计数
		ui.Row(c.Key("sc-col-head")).FillWidth().Gap(14).AlignItems(ui.Center).Children(func() {
			renderMiniBookCover(c, "sc-col-cover", data.BookID, data.CoverImg, pal, 46, 60)
			ui.Column(c.Key("sc-col-meta")).Grow(1).Gap(4).Children(func() {
				title := strings.TrimSpace(data.BookTitle)
				if title == "" {
					title = "诗集精选"
				}
				ui.Text(c, "《"+title+"》").
					Font(theme.FontSerif).FontSize(17.5).Bold().LineHeight(1.3).TextColor(pal.TextPrimary)
				if a := strings.TrimSpace(data.Author); a != "" {
					ui.Text(c, a).Font(theme.FontNote).FontSize(12).TextColor(pal.TextSecondary)
				}
				totalFav := data.FavoriteCount
				if totalFav < len(data.CollectionItems) {
					totalFav = len(data.CollectionItems)
				}
				ui.Text(c, fmt.Sprintf("精选诗篇 %d 首", totalFav)).
					Font(theme.FontMono).FontFeatures("tnum").FontSize(10.5).TextColor(pal.Accent)
			})
		})

		ui.Box(c.Key("sc-col-sep")).FillWidth().Height(0.75).Background(pal.Border)

		// 精选清单（上限 20 首，保证长图可读性与内存安全）
		const maxShow = 20
		items := data.CollectionItems
		moreCount := 0
		if len(items) > maxShow {
			moreCount = len(items) - maxShow
			items = items[:maxShow]
		}

		ui.Column(c.Key("sc-col-list")).FillWidth().Gap(12).Children(func() {
			if len(items) == 0 {
				ui.Text(c, "尚未收录本书精选诗篇").Font(theme.FontNote).FontSize(13).TextColor(pal.TextMuted)
				return
			}
			for i, item := range items {
				ui.Column(c.Key(fmt.Sprintf("sc-col-item-%d", i))).
					FillWidth().
					Padding(10, 12).
					Gap(6).
					Radius(6).
					Background(pal.Surface).
					Border(0.65, pal.Border).
					Children(func() {
						ui.Row(c.Key(fmt.Sprintf("sc-col-it-head-%d", i))).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
							ui.Text(c, fmt.Sprintf("%02d", i+1)).
								Font(theme.FontMono).FontFeatures("tnum").FontSize(10.5).Bold().TextColor(pal.Accent)
							pt := strings.TrimSpace(item.PoemTitle)
							if pt == "" {
								pt = "精选诗篇"
							}
							ui.Text(c, pt).
								Font(theme.FontSerif).FontSize(13.5).Bold().TextColor(pal.TextPrimary).Grow(1)
							if item.Author != "" && item.Author != data.Author {
								ui.Text(c, item.Author).Font(theme.FontNote).FontSize(10.5).TextColor(pal.TextMuted)
							}
						})
						if ex := strings.TrimSpace(item.Excerpt); ex != "" {
							ui.Text(c, ex).
								Font(poetryFont).FontSize(12.8).FontWeight(weight).LineHeight(1.7).TextColor(pal.TextSecondary)
						}
					})
			}
			if moreCount > 0 {
				ui.Text(c, fmt.Sprintf("…… 及另 %d 首已收录精选", moreCount)).
					Font(theme.FontNote).FontSize(11).TextColor(pal.TextMuted)
			}
		})
	})
}

// 4. 完读纪念卡主体
func renderFinishedBody(c *ui.Context, data CardData, pal CardPalette) {
	poetryFont := theme.PoetryFont(data.FontFamily)
	weight := data.EffectiveFontWeight()

	ui.Column(c.Key("sc-fin-body")).FillWidth().Gap(16).Children(func() {
		// 书封 + 书名作者 + 完读日期
		ui.Row(c.Key("sc-fin-head")).FillWidth().Gap(14).AlignItems(ui.Center).Children(func() {
			renderMiniBookCover(c, "sc-fin-cover", data.BookID, data.CoverImg, pal, 56, 74)
			ui.Column(c.Key("sc-fin-meta")).Grow(1).Gap(5).Children(func() {
				ui.Row(c.Key("sc-fin-pill-row")).FillWidth().Children(func() {
					ui.Box(c.Key("sc-fin-pill")).Padding(2, 8).Radius(999).Background(pal.AccentSoft).Children(func() {
						ui.Text(c, "完读纪念").Font(theme.FontSans).FontSize(9.5).Bold().TextColor(pal.Accent)
					})
				})
				title := strings.TrimSpace(data.BookTitle)
				if title == "" {
					title = "未命名诗集"
				}
				ui.Text(c, "《"+title+"》").
					Font(theme.FontSerif).FontSize(18).Bold().LineHeight(1.3).TextColor(pal.TextPrimary)
				if a := strings.TrimSpace(data.Author); a != "" {
					ui.Text(c, a).Font(theme.FontNote).FontSize(12.5).TextColor(pal.TextSecondary)
				}
				finDate := "已读完"
				if data.FinishedAt > 0 {
					finDate = time.Unix(data.FinishedAt, 0).Format("2006 年 01 月 02 日 完读")
				}
				ui.Text(c, finDate).
					Font(theme.FontMono).FontFeatures("tnum").FontSize(10).TextColor(pal.TextMuted)
			})
		})

		// 三项阅读回望指标
		ui.Row(c.Key("sc-fin-metrics")).FillWidth().Gap(8).Children(func() {
			renderMetric := func(key, label, value string) {
				ui.Column(c.Key(key)).Grow(1).Padding(10, 10).Gap(3).Radius(6).Background(pal.Surface).Border(0.65, pal.Border).Children(func() {
					ui.Text(c, label).Font(theme.FontSans).FontSize(9.5).TextColor(pal.TextMuted)
					ui.Text(c, value).Font(theme.FontSerif).FontFeatures("tnum").FontSize(14).Bold().TextColor(pal.TextPrimary)
				})
			}
			readStr := fmt.Sprintf("%d 首", data.ReadPoemCount)
			if data.TotalPoemCount > 0 {
				readStr = fmt.Sprintf("%d/%d", data.ReadPoemCount, data.TotalPoemCount)
			}
			renderMetric("sc-fm-read", "已读诗篇", readStr)
			renderMetric("sc-fm-fav", "诗篇精选", fmt.Sprintf("%d 首", data.FavoriteCount))
			renderMetric("sc-fm-qt", "留存散页", fmt.Sprintf("%d 张", data.QuoteCount))
		})

		// 本书代表诗句（最多展示 2 首精选节选，突出纪念仪式感）
		if len(data.CollectionItems) > 0 {
			ui.Column(c.Key("sc-fin-excerpts")).FillWidth().Gap(10).Children(func() {
				ui.Text(c, "留存诗句").Font(theme.FontNote).FontSize(11).TextColor(pal.TextMuted)
				limit := min(2, len(data.CollectionItems))
				for i := 0; i < limit; i++ {
					it := data.CollectionItems[i]
					ui.Row(c.Key(fmt.Sprintf("sc-fin-ex-%d", i))).FillWidth().Gap(10).AlignItems(ui.Stretch).Children(func() {
						ui.Box(c.Key(fmt.Sprintf("sc-fin-ex-bar-%d", i))).Width(2).Radius(1).Background(pal.Accent)
						ui.Column(c.Key(fmt.Sprintf("sc-fin-ex-col-%d", i))).Grow(1).Gap(4).Children(func() {
							if ex := strings.TrimSpace(it.Excerpt); ex != "" {
								ui.Text(c, ex).
									Font(poetryFont).FontSize(13.5).FontWeight(weight).LineHeight(1.75).TextColor(pal.TextPrimary)
							}
							if it.PoemTitle != "" {
								ui.Text(c, "——《"+it.PoemTitle+"》").
									Font(theme.FontNote).FontSize(10.5).TextColor(pal.TextMuted)
							}
						})
					})
				}
			})
		}
	})
}

// 5. 周期读诗报告长图主体
func renderStatsReportBody(c *ui.Context, data CardData, pal CardPalette) {
	periodLabel := strings.TrimSpace(data.ReportPeriodLabel)
	if periodLabel == "" {
		periodLabel = "个人读诗年轮报告"
	}

	ui.Column(c.Key("sc-rep-body")).FillWidth().Gap(16).Children(func() {
		// 报告标题头
		ui.Column(c.Key("sc-rep-head")).FillWidth().Gap(4).Children(func() {
			ui.Text(c, periodLabel).
				Font(theme.FontSerif).FontSize(20).Bold().LineHeight(1.3).TextColor(pal.TextPrimary)
			sub := strings.TrimSpace(data.ReportRangeNote)
			if sub == "" {
				sub = "静默记录 · 温柔回望"
			}
			ui.Text(c, sub).Font(theme.FontNote).FontSize(11.5).TextColor(pal.TextMuted)
		})

		// 2x2 核心指标卡
		ov := data.Overview
		renderStatTile := func(key, label, value, unit string) {
			ui.Column(c.Key(key)).Grow(1).Padding(11, 12).Gap(4).Radius(6).Background(pal.Surface).Border(0.65, pal.Border).Children(func() {
				ui.Text(c, label).Font(theme.FontSans).FontSize(10).TextColor(pal.TextMuted)
				ui.Row(c.Key(key + "-val")).AlignItems(ui.Baseline).Gap(4).Children(func() {
					ui.Text(c, value).Font(theme.FontSerif).FontFeatures("tnum").FontSize(20).Bold().TextColor(pal.TextPrimary)
					ui.Text(c, unit).Font(theme.FontNote).FontSize(10.5).TextColor(pal.TextSecondary)
				})
			})
		}
		ui.Column(c.Key("sc-rep-grid")).FillWidth().Gap(8).Children(func() {
			ui.Row(c.Key("sc-rep-row-1")).FillWidth().Gap(8).Children(func() {
				renderStatTile("sc-st-poems", "读过诗篇", fmt.Sprintf("%d", ov.ReadPoemCount), "首")
				renderStatTile("sc-st-fin", "完读诗集", fmt.Sprintf("%d", ov.FinishedBookCount), "本")
			})
			ui.Row(c.Key("sc-rep-row-2")).FillWidth().Gap(8).Children(func() {
				renderStatTile("sc-st-days", "读诗天数", fmt.Sprintf("%d", ov.ActiveDays), "天")
				renderStatTile("sc-st-favqt", "精选与散页", fmt.Sprintf("%d", ov.FavoriteCount+ov.QuoteCount), "则")
			})
		})

		// 热力图缩略图（有周网格数据时展示）
		if len(data.Heatmap.Weeks) > 0 {
			ui.Column(c.Key("sc-rep-hm-sec")).FillWidth().Padding(11, 12).Gap(8).Radius(6).Background(pal.Surface).Border(0.65, pal.Border).Children(func() {
				ui.Row(c.Key("sc-rep-hm-head")).FillWidth().AlignItems(ui.Center).Children(func() {
					ui.Text(c, "读诗足迹").Font(theme.FontSerif).FontSize(12.5).Bold().TextColor(pal.TextPrimary).Grow(1)
					ui.Text(c, fmt.Sprintf("活跃 %d 天 · %d 篇次", data.Heatmap.ActiveDays, data.Heatmap.TotalEvents)).
						Font(theme.FontMono).FontFeatures("tnum").FontSize(9.5).TextColor(pal.TextMuted)
				})
				hm := data.Heatmap
				hmColors := pal.HeatmapColors
				ui.Box(c.Key("sc-rep-hm-canvas")).FillWidth().Height(66).Draw(func(p *ui.Painter, r ui.Rect) {
					drawMiniHeatmap(p, r, hm, hmColors)
				})
			})
		}

		// 昼夜节律洞察 + 24h 迷你波形条
		ui.Column(c.Key("sc-rep-rhythm")).FillWidth().Padding(11, 12).Gap(8).Radius(6).Background(pal.Surface).Border(0.65, pal.Border).Children(func() {
			ui.Text(c, "昼夜节律").Font(theme.FontSerif).FontSize(12.5).Bold().TextColor(pal.TextPrimary)
			insight := strings.TrimSpace(data.Rhythm.Insight)
			if insight == "" {
				insight = "每一首停留片刻的诗，都会在这里留下微光。"
			}
			ui.Text(c, insight).Font(theme.FontNote).FontSize(11.5).LineHeight(1.6).TextColor(pal.TextSecondary)

			hourly := data.Rhythm.Hourly
			accent := pal.Accent
			track := pal.Border
			ui.Box(c.Key("sc-rep-rhythm-bars")).FillWidth().Height(36).Draw(func(p *ui.Painter, r ui.Rect) {
				drawMiniHourlyBars(p, r, hourly, accent, track)
			})
			ui.Row(c.Key("sc-rep-rhythm-axis")).FillWidth().Children(func() {
				ui.Text(c, "00:00").Font(theme.FontMono).FontSize(8.5).TextColor(pal.TextMuted).Grow(1)
				ui.Text(c, "06:00").Font(theme.FontMono).FontSize(8.5).TextColor(pal.TextMuted).Grow(1)
				ui.Text(c, "12:00").Font(theme.FontMono).FontSize(8.5).TextColor(pal.TextMuted).Grow(1)
				ui.Text(c, "18:00").Font(theme.FontMono).FontSize(8.5).TextColor(pal.TextMuted).Grow(1)
				ui.Text(c, "24:00").Font(theme.FontMono).FontSize(8.5).TextColor(pal.TextMuted)
			})
		})

		// TOP 5 诗人
		if len(data.TopAuthors) > 0 {
			ui.Column(c.Key("sc-rep-authors")).FillWidth().Padding(11, 12).Gap(8).Radius(6).Background(pal.Surface).Border(0.65, pal.Border).Children(func() {
				ui.Text(c, "常读诗人").Font(theme.FontSerif).FontSize(12.5).Bold().TextColor(pal.TextPrimary)
				limit := min(5, len(data.TopAuthors))
				topScore := data.TopAuthors[0].Score
				if topScore <= 0 {
					topScore = 1
				}
				for i := 0; i < limit; i++ {
					au := data.TopAuthors[i]
					ratio := float32(au.Score) / float32(topScore)
					if ratio < 0.12 {
						ratio = 0.12
					}
					ui.Column(c.Key(fmt.Sprintf("sc-rep-au-%d", i))).FillWidth().Gap(3).Children(func() {
						ui.Row(c.Key(fmt.Sprintf("sc-rep-au-row-%d", i))).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
							ui.Text(c, fmt.Sprintf("%d.", i+1)).Font(theme.FontMono).FontSize(10).TextColor(pal.Accent).Width(16)
							ui.Text(c, au.Author).Font(theme.FontSerif).FontSize(12.5).Bold().TextColor(pal.TextPrimary).Grow(1)
							ui.Text(c, fmt.Sprintf("读 %d · 选 %d", au.ReadCount, au.FavoriteCount+au.QuoteCount)).
								Font(theme.FontMono).FontFeatures("tnum").FontSize(9.5).TextColor(pal.TextMuted)
						})
						accent := pal.Accent
						track := pal.Border
						rVal := ratio
						ui.Box(c.Key(fmt.Sprintf("sc-rep-au-bar-%d", i))).FillWidth().Height(3.5).Draw(func(p *ui.Painter, r ui.Rect) {
							p.Fill(r, track, 2)
							p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * rVal, H: r.H}, accent, 2)
						})
					})
				}
			})
		}

		// 近期完读诗集名录
		if len(data.FinishedTitles) > 0 {
			limit := min(6, len(data.FinishedTitles))
			formatted := make([]string, 0, limit)
			for i := 0; i < limit; i++ {
				formatted = append(formatted, "《"+data.FinishedTitles[i]+"》")
			}
			line := strings.Join(formatted, " · ")
			if len(data.FinishedTitles) > limit {
				line += fmt.Sprintf(" 等 %d 本", len(data.FinishedTitles))
			}
			ui.Column(c.Key("sc-rep-finbooks")).FillWidth().Gap(4).Children(func() {
				ui.Text(c, "完读诗集").Font(theme.FontNote).FontSize(10.5).TextColor(pal.TextMuted)
				ui.Text(c, line).Font(theme.FontNote).FontSize(11.5).LineHeight(1.6).TextColor(pal.TextSecondary)
			})
		}
	})
}

// ---------- 辅助文案格式化 ----------

func cardTopTag(data CardData) string {
	if data.Kind == KindQuote && strings.TrimSpace(data.QuoteTag) != "" {
		return strings.TrimSpace(data.QuoteTag)
	}
	return data.Kind.Label()
}

func buildQuoteAttribution(data CardData) string {
	parts := []string{}
	if a := strings.TrimSpace(data.Author); a != "" {
		parts = append(parts, a)
	}
	book := strings.TrimSpace(data.SourceBook)
	if book == "" {
		book = strings.TrimSpace(data.BookTitle)
	}
	if book != "" {
		parts = append(parts, "《"+book+"》")
	}
	if pt := strings.TrimSpace(data.PoemTitle); pt != "" && pt != book {
		parts = append(parts, pt)
	}
	return strings.Join(parts, " · ")
}

func formatChineseDate(t time.Time) string {
	return fmt.Sprintf("%d年%d月%d日", t.Year(), int(t.Month()), t.Day())
}
