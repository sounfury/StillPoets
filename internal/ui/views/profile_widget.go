package views

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
	"stillpoets/internal/domain"
	"stillpoets/internal/ui/layout"
	"stillpoets/internal/ui/theme"
	"stillpoets/modules/widgetbridge"
)

func (a *AppUI) renderWidgetSubpage(c *ui.Context, pal theme.Palette, isTablet bool) {
	a.mu.Lock()
	preview := a.widgetPreview
	wCount := a.widgetCount
	pinnedCount := a.activeWidgetCount
	a.mu.Unlock()

	ui.Column(c.Key("subpage-widget")).Fill().Children(func() {
		a.renderProfileSubpageHeader(c, pal, isTablet, "桌面小组件", nil)

		ui.Scroll(c.Key("subpage-widget-scroll")).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(16, 20, 48, 20).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(layout.ReadingMaxWidthDIP + 80).Gap(16).Children(func() {
					ui.Column(c.Key("sec-widget")).
						FillWidth().
						Padding(18).
						Gap(14).
						Radius(12).
						Background(pal.Surface).
						Border(1, pal.Border).
						Children(func() {
							ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
								ui.Column(c).Grow(1).Gap(2).Children(func() {
									ui.Text(c, "桌面小组件（诗笺）").Font(theme.FontSerif).FontSize(16).Bold().TextColor(pal.TextPrimary)
									ui.Text(c, "自动同步当前主题、字体与字重；点击诗句直达整首诗并高亮原行").
										FontSize(12).LineHeight(1.5).TextColor(pal.TextMuted)
								})
								if pinnedCount > 0 {
									ui.Box(c).Padding(2, 8).Radius(999).Background(pal.HighlightBg).Children(func() {
										ui.Textf(c, "已添加 %d 个", pinnedCount).FontSize(11).Bold().TextColor(pal.Accent)
									})
								}
							})

							a.renderWidgetPreviewCard(c, pal, preview)

							modes := []struct {
								mode  domain.WidgetSourceMode
								label string
							}{
								{domain.WidgetModeGlobalRandom, "全局诗集随机"},
								{domain.WidgetModeBookRandom, "指定诗集随机"},
								{domain.WidgetModeBookSequential, "指定诗集顺序"},
								{domain.WidgetModeBookFavorites, "指定诗集精选池 ❤️"},
								{domain.WidgetModeQuotesRandom, "散页池随机"},
							}

							ui.Column(c).FillWidth().Gap(8).Children(func() {
								ui.Text(c, "内容来源").FontSize(12).Bold().TextColor(pal.TextSecondary)
								ui.Row(c.Key("widget-modes")).Gap(8).Wrap().Children(func() {
									for _, m := range modes {
										active := a.settings.WidgetMode == m.mode
										bg, fg, bd := pal.Background, pal.TextSecondary, pal.Border
										if active {
											bg, fg, bd = pal.Accent, pal.AccentText, pal.Accent
										}
										chip := ui.Box(c.Key("wm-"+string(m.mode))).Padding(6, 12).Radius(999).Background(bg).Border(1, bd).Cursor(ui.CursorPointer)
										theme.ApplyPressScale(chip, 0.96)
										chip.Children(func() {
											ui.Text(c, m.label).FontSize(12).TextColor(fg)
										})
										if chip.Clicked() {
											a.updateWidgetMode(m.mode, a.settings.WidgetBookID)
										}
									}
								})
							})

							needsBook := a.settings.WidgetMode == domain.WidgetModeBookRandom ||
								a.settings.WidgetMode == domain.WidgetModeBookSequential ||
								a.settings.WidgetMode == domain.WidgetModeBookFavorites
							if needsBook && len(a.books) > 0 {
								ui.Column(c).FillWidth().Gap(8).Children(func() {
									ui.Text(c, "选择目标诗集").FontSize(12).Bold().TextColor(pal.TextSecondary)
									ui.Row(c.Key("widget-book-picker")).Gap(8).Wrap().Children(func() {
										for _, b := range a.books {
											active := a.settings.WidgetBookID == b.ID
											bg, fg, bd := pal.Background, pal.TextSecondary, pal.Border
											if active {
												bg, fg, bd = pal.Accent, pal.AccentText, pal.Accent
											}
											chip := ui.Box(c.Key("wbk-"+b.ID)).Padding(5, 10).Radius(6).Background(bg).Border(1, bd).Cursor(ui.CursorPointer)
											theme.ApplyPressScale(chip, 0.96)
											chip.Children(func() {
												ui.Text(c, b.Title).FontSize(12).TextColor(fg)
											})
											if chip.Clicked() {
												a.updateWidgetMode(a.settings.WidgetMode, b.ID)
											}
										}
									})
								})
							}

							curInterval := domain.NormalizeWidgetRefreshMinutes(a.settings.WidgetRefreshMinutes)
							ui.Column(c.Key("widget-interval-sec")).FillWidth().Gap(8).Children(func() {
								ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
									ui.Text(c, "自动换诗频率").FontSize(12).Bold().TextColor(pal.TextSecondary).Grow(1)
									ui.Text(c, formatWidgetIntervalLabel(curInterval)).FontSize(11).TextColor(pal.Accent)
								})
								intervals := []struct {
									minutes int
									label   string
								}{
									{15, "15 分钟"},
									{30, "30 分钟"},
									{60, "1 小时"},
									{180, "3 小时"},
									{360, "6 小时"},
									{720, "12 小时"},
									{1440, "每天"},
								}
								ui.Row(c.Key("widget-intervals")).Gap(8).Wrap().Children(func() {
									for _, opt := range intervals {
										active := curInterval == opt.minutes
										bg, fg, bd := pal.Background, pal.TextSecondary, pal.Border
										if active {
											bg, fg, bd = pal.Accent, pal.AccentText, pal.Accent
										}
										chip := ui.Box(c.Key(fmt.Sprintf("wi-%d", opt.minutes))).
											Padding(5, 11).
											Radius(999).
											Background(bg).
											Border(1, bd).
											Cursor(ui.CursorPointer)
										theme.ApplyPressScale(chip, 0.96)
										chip.Children(func() {
											ui.Text(c, opt.label).FontSize(12).TextColor(fg)
										})
										if chip.Clicked() && !active {
											a.updateWidgetRefreshInterval(opt.minutes)
										}
									}
								})
							})

							ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Wrap().Children(func() {
								info := "尚未生成候选诗句，请先导入诗集或记一张散页"
								if wCount > 0 {
									info = fmt.Sprintf("已预编译 %d 条候选内容 · %s自动换诗 · 支持桌面一键换诗", wCount, formatWidgetIntervalLabel(curInterval))
								}
								ui.Text(c, info).FontSize(11).TextColor(pal.TextMuted).Grow(1).MaxLines(2)
								if ui.PrimaryButton(c, "添加到桌面").Clicked() {
									a.pinWidgetToHomeScreen()
								}
								if ui.Button(c, "立即刷新").Clicked() {
									a.refreshWidgetSnapshot()
								}
							})
						})
				})
			})
		})
	})
}

func (a *AppUI) renderWidgetPreviewCard(c *ui.Context, pal theme.Palette, preview *domain.WidgetTimeline) {
	poemText := dpsShortDefaultQuote
	metaText := dpsShortDefaultMeta
	tagText := "诗笺"
	hintText := "读全诗 ›"
	if preview != nil && len(preview.Entries) > 0 {
		e := preview.Entries[0]
		poemText = e.DisplayText
		if e.EntryType == "quote" {
			tagText = "散页"
		}
		if strings.HasPrefix(e.Deeplink, "stillpoets://quote/") {
			hintText = "看散页 ›"
		}
		var parts []string
		if strings.TrimSpace(e.PoemTitle) != "" {
			parts = append(parts, "《"+strings.TrimSpace(e.PoemTitle)+"》")
		} else if strings.TrimSpace(e.SourceTitle) != "" {
			parts = append(parts, "《"+strings.TrimSpace(e.SourceTitle)+"》")
		}
		if strings.TrimSpace(e.Author) != "" {
			parts = append(parts, strings.TrimSpace(e.Author))
		}
		if len(parts) > 0 {
			metaText = strings.Join(parts, " · ")
		} else {
			metaText = "StillPoets"
		}
	}

	ui.Column(c.Key("widget-live-preview")).
		FillWidth().
		MaxWidth(360).
		Padding(14, 18).
		Gap(10).
		Radius(14).
		Background(pal.Background).
		Border(1, pal.Border).
		Transition(theme.ColorTransition()).
		Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Box(c).Width(6).Height(6).Radius(3).Background(pal.Accent)
				ui.Text(c, tagText).FontSize(10).TextColor(pal.TextMuted).Grow(1)
				ui.Text(c, "4×2 预览").FontSize(10).TextColor(pal.TextMuted)
			})
			poemEl := ui.Text(c, poemText).
				Font(theme.PoetryFont(a.settings.FontFamily)).
				FontSize(15).
				FontWeight(a.settings.EffectiveFontWeight()).
				LineHeight(1.45).
				TextColor(pal.TextPrimary).
				MaxLines(4)
			_ = poemEl
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, metaText).FontSize(11).TextColor(pal.TextMuted).Grow(1).MaxLines(1)
				ui.Text(c, hintText).FontSize(10).TextColor(pal.Accent)
			})
		})
}

// --- 格式化辅助函数 ---

func formatBytesSize(bytes int64) string {
	if bytes <= 0 {
		return "0 KB"
	}
	const (
		kb = 1024.0
		mb = 1024.0 * 1024.0
		gb = 1024.0 * 1024.0 * 1024.0
	)
	f := float64(bytes)
	switch {
	case f >= gb:
		return fmt.Sprintf("%.2f GB", f/gb)
	case f >= mb:
		return fmt.Sprintf("%.1f MB", f/mb)
	default:
		return fmt.Sprintf("%.1f KB", f/kb)
	}
}

func formatSyncTimeShort(ms int64) string {
	t := time.UnixMilli(ms).Local()
	now := time.Now().Local()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04")
	}
	return t.Format("01-02 15:04")
}

func extractURLHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Host
}

func formatWidgetModeShort(mode domain.WidgetSourceMode) string {
	switch mode {
	case domain.WidgetModeBookRandom:
		return "诗集随机"
	case domain.WidgetModeBookSequential:
		return "诗集顺序"
	case domain.WidgetModeBookFavorites:
		return "精选池"
	case domain.WidgetModeQuotesRandom:
		return "散页随机"
	default:
		return "全局随机"
	}
}

func formatWidgetIntervalLabel(minutes int) string {
	m := domain.NormalizeWidgetRefreshMinutes(minutes)
	switch {
	case m >= 1440:
		return "每天"
	case m%60 == 0:
		return fmt.Sprintf("每 %d 小时", m/60)
	default:
		return fmt.Sprintf("每 %d 分钟", m)
	}
}

func formatVersionTag(ver string) string {
	v := strings.TrimSpace(ver)
	if v == "" {
		v = app.DefaultAppVersion
	}
	if !strings.HasPrefix(v, "v") && !strings.HasPrefix(v, "V") {
		return "v" + v
	}
	return v
}

// --- 「我的」页面异步动作 ---

func (a *AppUI) updateWidgetSettingsAsync(mutate func(*domain.Settings), toastFn func(domain.WidgetTimeline) string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := a.svc.UpdateSettings(ctx, mutate); err != nil {
			return
		}
		if tl, err := a.svc.ReadWidgetTimeline(); err == nil {
			cp := tl
			activeCnt := widgetbridge.ActiveWidgetCount()
			a.dispatchUI(func() {
				a.widgetPreview = &cp
				a.widgetCount = len(tl.Entries)
				a.activeWidgetCount = activeCnt
			})
			a.showTransientToast(toastFn(tl))
		}
	}()
}

func (a *AppUI) updateWidgetRefreshInterval(minutes int) {
	normalized := domain.NormalizeWidgetRefreshMinutes(minutes)
	a.settings.WidgetRefreshMinutes = normalized
	a.updateWidgetSettingsAsync(
		func(st *domain.Settings) { st.WidgetRefreshMinutes = normalized },
		func(domain.WidgetTimeline) string {
			return fmt.Sprintf("小组件已设为%s自动换诗", formatWidgetIntervalLabel(normalized))
		},
	)
}

func (a *AppUI) updateWidgetMode(mode domain.WidgetSourceMode, bookID string) {
	a.settings.WidgetMode = mode
	a.settings.WidgetBookID = bookID
	a.updateWidgetSettingsAsync(
		func(st *domain.Settings) {
			st.WidgetMode = mode
			st.WidgetBookID = bookID
		},
		func(tl domain.WidgetTimeline) string {
			return fmt.Sprintf("小组件已同步更新（%d 条）", len(tl.Entries))
		},
	)
}

func (a *AppUI) refreshWidgetSnapshot() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tl, err := a.svc.ReshuffleWidgetTimeline(ctx)
		if err != nil {
			a.showTransientToast("刷新失败：" + err.Error())
			return
		}
		cp := tl
		activeCnt := widgetbridge.ActiveWidgetCount()
		a.dispatchUI(func() {
			a.widgetPreview = &cp
			a.widgetCount = len(tl.Entries)
			a.activeWidgetCount = activeCnt
		})
		a.showTransientToast(fmt.Sprintf("已为桌面小组件换一批诗（%d 条）", len(tl.Entries)))
	}()
}

func (a *AppUI) pinWidgetToHomeScreen() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = a.svc.RebuildWidgetTimeline(ctx)
		ok, err := widgetbridge.RequestPin()
		switch {
		case errors.Is(err, errors.ErrUnsupported):
			a.showTransientToast("请在设备主屏幕长按添加「StillPoets 诗笺」小组件")
		case err != nil:
			a.showTransientToast("无法呼出添加弹窗，请在桌面长按添加小组件")
		case !ok:
			a.showTransientToast("请在主屏幕长按空白处，搜索添加「StillPoets 诗笺」小组件")
		default:
			a.showTransientToast("已呼出桌面添加引导")
		}
	}()
}
