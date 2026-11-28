package views

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/domain"
	"stillpoets/internal/storage"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/layout"
	"stillpoets/internal/ui/theme"
)

func (a *AppUI) renderStatsPage(c *ui.Context, pal theme.Palette, isTablet bool) {
	dash := a.statsDashboard

	ui.Column(c.Key("stats-page")).Fill().Children(func() {
		components.BrandHeader(c, pal, "统计", false, nil, nil)

		bottomPad := float32(88)
		if !isTablet && a.useFloatingBottomNav() {
			bottomPad = 108
		}
		maxW := layout.ReadingMaxWidthDIP + 120
		if isTablet {
			maxW = layout.ReadingMaxWidthDIP + 260
		}

		ui.Scroll(c.Key("stats-scroll")).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(8, 20, bottomPad, 20).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(maxW).Gap(22).Children(func() {
					// 1. 顶部语调说明 + 时间窗口切换（本周 / 本月 / 今年 / 全部）
					a.renderStatsPeriodBar(c, pal, isTablet)

					// 2. 核心总览指标卡
					a.renderStatsOverviewGrid(c, pal, dash, isTablet)

					// 3. 读诗热力图（按诗篇粒度）+ 按日下钻明细
					a.renderStatsHeatmapSection(c, pal, dash.Heatmap, isTablet)

					// 4. 阅读时段占比（昼夜节律：五时段环图 / 24 小时柱状图）
					a.renderStatsRhythmSection(c, pal, dash.Rhythm, isTablet)

					// 5. 完读诗集墙
					a.renderStatsFinishedWallSection(c, pal, dash, isTablet)

					// 6. 诗人作者云（支持综合 / 读过 / 精选权重切换与点击下钻）
					a.renderStatsAuthorCloudSection(c, pal, dash, isTablet)

					// 7. 在读与书架进度（支持一键手动完读校准）
					a.renderStatsReadingProgressSection(c, pal, dash.ReadingBooks, isTablet)
				})
			})
		})
	})
}

func (a *AppUI) renderStatsPeriodBar(c *ui.Context, pal theme.Palette, isTablet bool) {
	periods := []struct {
		id    storage.StatsPeriod
		label string
	}{
		{storage.StatsPeriodWeek, "本周"},
		{storage.StatsPeriodMonth, "本月"},
		{storage.StatsPeriodYear, "今年"},
		{storage.StatsPeriodAll, "全部"},
	}

	bar := ui.Row(c.Key("stats-period-bar")).FillWidth().AlignItems(ui.Center).Gap(10).Wrap()
	bar.Children(func() {
		ui.Text(c, "个人读诗年轮 · 静默记录").
			Font(theme.FontNote).FontSize(12.5).TextColor(pal.TextMuted)
		if isTablet {
			ui.Spacer(c)
		}
		ui.Row(c.Key("stats-period-pills")).
			Padding(3).
			Gap(2).
			Radius(8).
			Background(pal.SurfaceMuted).
			Border(1, pal.Border).
			Children(func() {
				for _, p := range periods {
					active := a.statsPeriod == p.id
					bg := ui.Transparent
					fg := pal.TextSecondary
					if active {
						bg = pal.Surface
						fg = pal.TextPrimary
					}
					pill := ui.Box(c.Key("sp-"+string(p.id))).
						Padding(5, 12).
						Radius(6).
						Background(bg).
						Cursor(ui.CursorPointer).
						Transition(theme.ColorTransition())
					if active {
						pill.Border(0.75, pal.Border)
					}
					pill.Children(func() {
						txt := ui.Text(c, p.label).Font(theme.FontSans).FontSize(12).TextColor(fg)
						if active {
							txt.Bold()
						}
					})
					if pill.Clicked() {
						a.setStatsPeriod(p.id)
					}
				}
			})

		// 导出当前周期的读诗报告纪念长图（PRD 9.5）
		exportBtn := ui.Row(c.Key("stats-export-report")).
			Padding(6, 11).
			Gap(5).
			Radius(8).
			Background(pal.Surface).
			Border(1, pal.Border).
			AlignItems(ui.Center).
			Cursor(ui.CursorPointer).
			Transition(theme.ColorTransition())
		theme.ApplyPressScale(exportBtn, 0.94)
		exportBtn.Children(func() {
			ui.Icon(c, components.IconShareCard).Width(13.5).Height(13.5).TextColor(pal.Accent)
			ui.Text(c, "导出报告").Font(theme.FontSans).FontSize(12).Bold().TextColor(pal.TextPrimary)
		})
		if exportBtn.Clicked() {
			a.openStatsReportShareCard()
		}
	})
}

func (a *AppUI) renderStatsOverviewGrid(c *ui.Context, pal theme.Palette, dash storage.StatsDashboard, isTablet bool) {
	ov := dash.Overview
	cols := 2
	if isTablet {
		cols = 4
	}

	finishedSub := fmt.Sprintf("藏书共 %d 本", ov.BookCount)
	if a.statsPeriod != storage.StatsPeriodAll && ov.TotalFinishedCount > 0 {
		finishedSub = fmt.Sprintf("累计完读 %d 本", ov.TotalFinishedCount)
	}
	readSub := fmt.Sprintf("活跃 %d 天", ov.ActiveDays)
	if ov.ActiveDays == 0 && ov.ReadPoemCount > 0 {
		readSub = "含历史已读"
	}

	cards := []struct {
		key   string
		label string
		value int
		unit  string
		sub   string
	}{
		{"read", "读过诗篇", ov.ReadPoemCount, "首", readSub},
		{"fin", "已完读", ov.FinishedBookCount, "本", finishedSub},
		{"fav", "诗篇精选", ov.FavoriteCount, "首", "点亮小红心"},
		{"quote", "留存散页", ov.QuoteCount, "张", fmt.Sprintf("离线 %d 本", ov.OfflineBookCount)},
	}

	ui.Grid(c.Key("stats-overview-grid")).Columns(cols).Gap(11).FillWidth().Children(func() {
		for _, item := range cards {
			ui.Column(c.Key("ov-"+item.key)).
				Padding(15, 16).
				Gap(5).
				Radius(8).
				Background(pal.Surface).
				Border(1, pal.Border).
				Children(func() {
					ui.Text(c, item.label).Font(theme.FontSans).FontSize(12).TextColor(pal.TextMuted)
					ui.Row(c).AlignItems(ui.End).Gap(4).Children(func() {
						ui.Textf(c, "%d", item.value).
							Font(theme.FontSerif).FontFeatures("tnum").FontSize(26).Bold().TextColor(pal.TextPrimary)
						ui.Text(c, item.unit).
							Font(theme.FontSans).FontSize(11.5).TextColor(pal.TextMuted).Padding(0, 0, 4, 0)
					})
					ui.Text(c, item.sub).Font(theme.FontNote).FontSize(11).TextColor(pal.TextMuted).MaxLines(1)
				})
		}
	})
}

// --- 模块一：读诗热力图与按日下钻 ---

func (a *AppUI) renderStatsHeatmapSection(c *ui.Context, pal theme.Palette, hm storage.HeatmapData, isTablet bool) {
	ui.Column(c.Key("stats-heatmap-section")).FillWidth().Gap(10).Children(func() {
		// 标题 + 年份切换
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, "读诗热力图").Font(theme.FontSerif).FontSize(15.5).Bold().LetterSpacing(0.3).TextColor(pal.TextPrimary).Grow(1)
			a.renderHeatmapYearSelector(c, pal, hm)
		})

		// 主热力图卡片
		ui.Column(c.Key("stats-heatmap-card")).
			FillWidth().
			Padding(16, 16, 14, 16).
			Gap(12).
			Radius(10).
			Background(pal.Surface).
			Border(1, pal.Border).
			Children(func() {
				// 顶部摘要
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					summary := "还没有留下读诗日期记录，读完一首诗后会在这里轻轻点亮。"
					if hm.ActiveDays > 0 {
						summary = fmt.Sprintf("%s有 %d 个读诗的日子 · 有效读诗 %d 首", hm.ModeLabel, hm.ActiveDays, hm.TotalEvents)
					}
					ui.Text(c, summary).Font(theme.FontNote).FontSize(12).TextColor(pal.TextSecondary).Grow(1)
				})

				// 可横向滚动的日历格子矩阵
				a.renderHeatmapGrid(c, pal, hm)

				// 底部色阶图例
				colors := heatmapColors(pal)
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
					ui.Text(c, "点选任意一天查看明细").Font(theme.FontSans).FontSize(11).TextColor(pal.TextMuted).Grow(1)
					ui.Text(c, "少").Font(theme.FontSans).FontSize(10.5).TextColor(pal.TextMuted)
					for lv, clr := range colors {
						box := ui.Box(c.Key(fmt.Sprintf("hm-leg-%d", lv))).Width(11).Height(11).Radius(2.5).Background(clr)
						if lv == 0 {
							box.Border(0.5, pal.Border)
						}
					}
					ui.Text(c, "多").Font(theme.FontSans).FontSize(10.5).TextColor(pal.TextMuted)
				})

				// 按日下钻明细面板
				if a.statsSelectedDate != "" {
					ui.Divider(c)
					a.renderHeatmapDayDetail(c, pal, isTablet)
				}
			})
	})
}

func (a *AppUI) renderHeatmapYearSelector(c *ui.Context, pal theme.Palette, hm storage.HeatmapData) {
	// 可选项：近一年（0）+ 所有有记录的年份（倒序）
	options := make([]int, 0, len(hm.AvailableYears)+1)
	options = append(options, 0)
	for i := len(hm.AvailableYears) - 1; i >= 0; i-- {
		options = append(options, hm.AvailableYears[i])
	}
	curIdx := 0
	for i, y := range options {
		if y == a.statsHeatmapYear {
			curIdx = i
			break
		}
	}

	ui.Row(c.Key("hm-year-nav")).AlignItems(ui.Center).Gap(4).Children(func() {
		canPrev := curIdx+1 < len(options)
		canNext := curIdx > 0

		prev := ui.Box(c.Key("hm-yr-prev")).Padding(3, 7).Radius(5).Background(pal.SurfaceMuted)
		if canPrev {
			prev.Cursor(ui.CursorPointer)
			theme.ApplyPressScale(prev, 0.92)
		}
		prev.Children(func() {
			clr := pal.TextSecondary
			if !canPrev {
				clr = pal.TextMuted.Alpha(0.38)
			}
			ui.Text(c, "‹").FontSize(13).Bold().TextColor(clr)
		})
		if canPrev && prev.Clicked() {
			a.setStatsHeatmapYear(options[curIdx+1])
		}

		label := hm.ModeLabel
		if label == "" {
			label = "近一年"
		}
		ui.Text(c, label).Font(theme.FontMono).FontFeatures("tnum").FontSize(11.5).TextColor(pal.TextSecondary).Padding(0, 4)

		next := ui.Box(c.Key("hm-yr-next")).Padding(3, 7).Radius(5).Background(pal.SurfaceMuted)
		if canNext {
			next.Cursor(ui.CursorPointer)
			theme.ApplyPressScale(next, 0.92)
		}
		next.Children(func() {
			clr := pal.TextSecondary
			if !canNext {
				clr = pal.TextMuted.Alpha(0.38)
			}
			ui.Text(c, "›").FontSize(13).Bold().TextColor(clr)
		})
		if canNext && next.Clicked() {
			a.setStatsHeatmapYear(options[curIdx-1])
		}
	})
}

func (a *AppUI) renderHeatmapGrid(c *ui.Context, pal theme.Palette, hm storage.HeatmapData) {
	if len(hm.Weeks) == 0 {
		return
	}
	const (
		cellSize = float32(12)
		cellGap  = float32(3)
		step     = cellSize + cellGap
		monthH   = float32(16)
	)
	colors := heatmapColors(pal)
	gridW := float32(len(hm.Weeks))*step - cellGap
	gridH := 7*step - cellGap
	totalH := monthH + 4 + gridH

	ui.Row(c.Key("hm-body")).FillWidth().Gap(6).AlignItems(ui.Start).Children(func() {
		// 左侧固定星期微标（一 / 三 / 五）
		ui.Column(c.Key("hm-weekdays")).Width(16).Padding(monthH+4, 0, 0, 0).Gap(cellGap).Children(func() {
			labels := [7]string{"一", "", "三", "", "五", "", "日"}
			for i, lb := range labels {
				ui.Box(c.Key(fmt.Sprintf("wd-%d", i))).Height(cellSize).Center().Children(func() {
					if lb != "" {
						ui.Text(c, lb).Font(theme.FontSans).FontSize(9.5).TextColor(pal.TextMuted)
					}
				})
			}
		})

		// 右侧可横向滚动的周矩阵
		ui.ScrollHorizontal(c.Key("stats-heatmap-scroll")).
			TrackScroll(&a.statsHeatmapScroll).
			Grow(1).
			Height(totalH + 2).
			Scrollbars(false).
			Children(func() {
				ui.Column(c.Key("hm-weeks-wrap")).Width(gridW).Gap(4).Children(func() {
					renderHeatmapMonths(c, pal, hm.Weeks, gridW, monthH, step)

					a.renderHeatmapWeeks(c, pal, hm.Weeks, colors)
				})
			})
	})
}

func (a *AppUI) renderHeatmapDayDetail(c *ui.Context, pal theme.Palette, isTablet bool) {
	items := a.statsDayEvents
	initialCnt, rereadCnt := 0, 0
	for _, it := range items {
		if it.Event.IsReread {
			rereadCnt++
		} else {
			initialCnt++
		}
	}

	ui.Column(c.Key("hm-day-detail")).FillWidth().Gap(8).Children(func() {
		a.renderHeatmapDayHeader(c, pal, len(items), initialCnt, rereadCnt)

		if a.statsDayLoading {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(4, 0).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "加载当日读诗明细…").FontSize(12).TextColor(pal.TextMuted)
			})
			return
		}
		if len(items) == 0 {
			ui.Text(c, "这一天没有留下读诗记录。").Font(theme.FontNote).FontSize(12).TextColor(pal.TextMuted).Padding(2, 0)
			return
		}

		ui.Column(c.Key("hm-day-list")).FillWidth().Gap(6).Children(func() {
			for i, it := range items {
				it := it
				a.renderHeatmapDayEvent(c, pal, it, i, isTablet)
			}
		})
	})
}

// --- 模块二：阅读时段占比（昼夜节律） ---

func (a *AppUI) renderStatsRhythmSection(c *ui.Context, pal theme.Palette, rhythm storage.ReadingRhythm, isTablet bool) {
	ui.Column(c.Key("stats-rhythm-section")).FillWidth().Gap(10).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, "阅读时段").Font(theme.FontSerif).FontSize(15.5).Bold().LetterSpacing(0.3).TextColor(pal.TextPrimary).Grow(1)
			ui.Row(c.Key("rhythm-mode-switch")).
				Padding(2).
				Gap(2).
				Radius(7).
				Background(pal.SurfaceMuted).
				Border(1, pal.Border).
				Children(func() {
					renderToggle := func(key, label string, active bool, onClick func()) {
						bg, fg := ui.Transparent, pal.TextSecondary
						if active {
							bg, fg = pal.Surface, pal.TextPrimary
						}
						btn := ui.Box(c.Key(key)).Padding(4, 9).Radius(5).Background(bg).Cursor(ui.CursorPointer)
						btn.Children(func() {
							t := ui.Text(c, label).Font(theme.FontSans).FontSize(11).TextColor(fg)
							if active {
								t.Bold()
							}
						})
						if btn.Clicked() {
							onClick()
						}
					}
					renderToggle("rh-5", "五时段", !a.statsRhythm24h, func() { a.statsRhythm24h = false })
					renderToggle("rh-24", "24 小时", a.statsRhythm24h, func() { a.statsRhythm24h = true })
				})
		})

		ui.Column(c.Key("stats-rhythm-card")).
			FillWidth().
			Padding(16).
			Gap(14).
			Radius(10).
			Background(pal.Surface).
			Border(1, pal.Border).
			Children(func() {
				// 克制的个人读诗节律摘要文案
				ui.Text(c, rhythm.Insight).Font(theme.FontNote).FontSize(12.5).LineHeight(1.55).TextColor(pal.TextSecondary)

				if a.statsRhythm24h {
					a.renderRhythm24HourChart(c, pal, rhythm)
				} else {
					a.renderRhythmFiveSlots(c, pal, rhythm, isTablet)
				}
			})
	})
}

func (a *AppUI) renderRhythmFiveSlots(c *ui.Context, pal theme.Palette, rhythm storage.ReadingRhythm, isTablet bool) {
	slotColors := rhythmSlotColors(pal)

	if isTablet {
		ui.Row(c.Key("rhythm-5-tablet")).FillWidth().Gap(22).AlignItems(ui.Center).Children(func() {
			renderRhythmDonut(c, pal, rhythm, slotColors)
			renderRhythmBreakdown(c, pal, rhythm, slotColors)
		})
		return
	}
	ui.Column(c.Key("rhythm-5-phone")).FillWidth().Gap(14).AlignItems(ui.Center).Children(func() {
		renderRhythmDonut(c, pal, rhythm, slotColors)
		renderRhythmBreakdown(c, pal, rhythm, slotColors)
	})
}

func (a *AppUI) renderRhythm24HourChart(c *ui.Context, pal theme.Palette, rhythm storage.ReadingRhythm) {
	maxCnt := max(1, rhythm.PeakHourCount)

	ui.Column(c.Key("rhythm-24h")).FillWidth().Gap(8).Children(func() {
		ui.Box(c.Key("rhythm-24h-bars")).FillWidth().Height(110).Draw(func(p *ui.Painter, r ui.Rect) {
			// 底部基线与中位参考线
			baseY := r.Y + r.H - 4
			p.Line(r.X, baseY, r.X+r.W, baseY, 1, pal.Border)
			midY := r.Y + (baseY-r.Y)*0.5
			p.Line(r.X, midY, r.X+r.W, midY, 0.75, pal.Border.Alpha(0.45))

			slotW := r.W / 24
			barW := max(4, slotW-4)
			usableH := baseY - r.Y - 10
			for h := 0; h < 24; h++ {
				cnt := rhythm.Hourly[h]
				bx := r.X + float32(h)*slotW + (slotW-barW)/2
				if cnt <= 0 {
					p.Fill(ui.Rect{X: bx, Y: baseY - 2, W: barW, H: 2}, pal.SurfaceMuted, 1)
					continue
				}
				bh := max(5, usableH*float32(cnt)/float32(maxCnt))
				clr := pal.TextSecondary.Alpha(0.55)
				if cnt == rhythm.PeakHourCount {
					clr = pal.Accent
				}
				p.Fill(ui.Rect{X: bx, Y: baseY - bh, W: barW, H: bh}, clr, 2)
			}
		})

		ui.Row(c).FillWidth().Children(func() {
			ticks := []string{"00时", "06时", "12时", "18时", "23时"}
			for i, tk := range ticks {
				if i > 0 {
					ui.Spacer(c)
				}
				ui.Text(c, tk).Font(theme.FontMono).FontFeatures("tnum").FontSize(10.5).TextColor(pal.TextMuted)
			}
		})

		if rhythm.TotalEvents > 0 {
			ui.Textf(c, "最常读诗整点：%02d:00–%02d:59（%d 首）",
				rhythm.PeakHour, rhythm.PeakHour, rhythm.PeakHourCount).
				Font(theme.FontMono).FontFeatures("tnum").FontSize(11).TextColor(pal.TextMuted)
		}
	})
}

func renderHeatmapMonths(c *ui.Context, pal theme.Palette, weeks []storage.HeatmapWeek, gridW, monthH, step float32) {
	// 月份顶栏矢量绘制，避免窄列文字被裁剪
	ui.Box(c.Key("hm-months")).Width(gridW).Height(monthH).Draw(func(p *ui.Painter, r ui.Rect) {
		lastX := float32(-99)
		for i, w := range weeks {
			if w.MonthLabel == "" {
				continue
			}
			x := r.X + float32(i)*step
			if x-lastX < 28 {
				continue
			}
			p.Text(x, r.Y+1, w.MonthLabel, 10, pal.TextMuted)
			lastX = x
		}
	})
}

func (a *AppUI) renderHeatmapCell(c *ui.Context, pal theme.Palette, colors [5]ui.Color, day storage.HeatmapDay, wi, di int) {
	const cellSize = float32(12)
	if day.OutOfRange {
		ui.Box(c.Key(fmt.Sprintf("hm-o-%d-%d", wi, di))).Width(cellSize).Height(cellSize)
		return
	}
	lv := max(0, min(4, day.Level))
	bg := colors[lv]
	if day.InFuture {
		bg = pal.SurfaceMuted.Alpha(0.35)
	}
	selected := a.statsSelectedDate == day.Date
	cell := ui.Box(c.Key("hm-d-" + day.Date)).
		Width(cellSize).
		Height(cellSize).
		Radius(2.5).
		Background(bg)
	if selected {
		cell.Border(1.5, pal.TextPrimary)
	} else if lv == 0 && !day.InFuture {
		cell.Border(0.5, pal.Border.Alpha(0.7))
	}
	if !day.InFuture {
		cell.Cursor(ui.CursorPointer)
		if cell.Clicked() {
			a.selectHeatmapDate(day.Date)
		}
	}
}

func (a *AppUI) renderHeatmapWeeks(c *ui.Context, pal theme.Palette, weeks []storage.HeatmapWeek, colors [5]ui.Color) {
	const cellGap = float32(3)
	// 7 行 × N 列日期方格
	ui.Row(c.Key("hm-cols")).Gap(cellGap).Children(func() {
		for wi, week := range weeks {
			ui.Column(c.Key(fmt.Sprintf("hm-w-%d", wi))).Gap(cellGap).Children(func() {
				for di := 0; di < 7; di++ {
					a.renderHeatmapCell(c, pal, colors, week.Days[di], wi, di)
				}
			})
		}
	})
}

func (a *AppUI) renderHeatmapDayHeader(c *ui.Context, pal theme.Palette, count, initialCnt, rereadCnt int) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
		title := formatCNDate(a.statsSelectedDate)
		if count > 0 {
			title += fmt.Sprintf(" · 共读 %d 首", count)
		}
		ui.Text(c, title).Font(theme.FontSerif).FontSize(13.5).Bold().TextColor(pal.TextPrimary).Grow(1)
		if count > 0 {
			sub := fmt.Sprintf("%d 初读", initialCnt)
			if rereadCnt > 0 {
				sub += fmt.Sprintf(" · %d 重读", rereadCnt)
			}
			ui.Text(c, sub).Font(theme.FontMono).FontFeatures("tnum").FontSize(11).TextColor(pal.TextMuted)
		}
		closeBtn := ui.Box(c.Key("hm-day-close")).Padding(3).Cursor(ui.CursorPointer)
		closeBtn.Children(func() {
			ui.Icon(c, components.IconClose).Width(13).Height(13).TextColor(pal.TextMuted)
		})
		if closeBtn.Clicked() {
			a.statsSelectedDate = ""
			a.statsDayEvents = nil
		}
	})
}

func (a *AppUI) renderHeatmapDayEvent(c *ui.Context, pal theme.Palette, it storage.ReadingEventItem, i int, isTablet bool) {
	row := renderStatsEventRowBox(c, fmt.Sprintf("hm-ev-%d-%s", i, it.Event.ID), pal, 9, 10)
	row.Children(func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
				poemTitle := it.Event.PoemTitle
				if poemTitle == "" {
					poemTitle = "无题"
				}
				ui.Text(c, poemTitle).Font(theme.FontSerif).FontSize(13.5).Bold().TextColor(pal.TextPrimary).MaxLines(1)
				if it.Event.IsReread {
					ui.Box(c).Padding(1, 5).Radius(4).Background(pal.SurfaceMuted).Children(func() {
						ui.Text(c, "重读").Font(theme.FontSans).FontSize(9.5).TextColor(pal.TextSecondary)
					})
				}
				if it.IsFavorite {
					ui.Icon(c, components.IconHeartFilled).Width(12).Height(12).TextColor(pal.Accent)
				}
				if it.BookLocalState != domain.LocalStateDownloaded {
					ui.Icon(c, components.IconCloud).Width(12).Height(12).TextColor(pal.TextMuted)
				}
			})
			meta := fmt.Sprintf("《%s》", it.BookTitle)
			if it.Event.Author != "" {
				meta += " · " + it.Event.Author
			}
			ui.Text(c, meta).Font(theme.FontNote).FontSize(11.5).TextColor(pal.TextMuted).MaxLines(1)
		})

		timeLabel := formatEventClock(it.Event.ReadAt, it.Event.ReadHour)
		ui.Text(c, timeLabel).Font(theme.FontMono).FontFeatures("tnum").FontSize(11).TextColor(pal.TextMuted)
		ui.Icon(c, components.IconChevronRight).Width(13).Height(13).TextColor(pal.TextMuted)
	})
	if row.Clicked() {
		a.jumpToPoemFromStats(it.Event.BookID, it.Event.PoemID, nil, !isTablet)
	}
}
