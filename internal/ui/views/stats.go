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
					// 月份顶栏矢量绘制，避免窄列文字被裁剪
					ui.Box(c.Key("hm-months")).Width(gridW).Height(monthH).Draw(func(p *ui.Painter, r ui.Rect) {
						lastX := float32(-99)
						for i, w := range hm.Weeks {
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

					// 7 行 × N 列日期方格
					ui.Row(c.Key("hm-cols")).Gap(cellGap).Children(func() {
						for wi, week := range hm.Weeks {
							ui.Column(c.Key(fmt.Sprintf("hm-w-%d", wi))).Gap(cellGap).Children(func() {
								for di := 0; di < 7; di++ {
									day := week.Days[di]
									if day.OutOfRange {
										ui.Box(c.Key(fmt.Sprintf("hm-o-%d-%d", wi, di))).Width(cellSize).Height(cellSize)
										continue
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
							})
						}
					})
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
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			title := formatCNDate(a.statsSelectedDate)
			if len(items) > 0 {
				title += fmt.Sprintf(" · 共读 %d 首", len(items))
			}
			ui.Text(c, title).Font(theme.FontSerif).FontSize(13.5).Bold().TextColor(pal.TextPrimary).Grow(1)
			if len(items) > 0 {
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
				row := ui.Row(c.Key(fmt.Sprintf("hm-ev-%d-%s", i, it.Event.ID))).
					FillWidth().
					Padding(9, 11).
					Gap(10).
					Radius(7).
					Background(pal.Background).
					Border(0.75, pal.Border).
					AlignItems(ui.Center).
					Cursor(ui.CursorPointer)
				theme.ApplyPressScale(row, 0.985)
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

	renderDonut := func() {
		ui.Box(c.Key("rhythm-donut")).Width(122).Height(122).Center().Draw(func(p *ui.Painter, r ui.Rect) {
			cx := r.X + r.W/2
			cy := r.Y + r.H/2
			radius := min(r.W, r.H)/2 - 11
			const strokeW = float32(13)

			if rhythm.TotalEvents == 0 {
				ring := (&ui.Path{}).Circle(cx, cy, radius)
				p.StrokePath(ring, strokeW, pal.Border)
				return
			}

			nonZero := 0
			for _, sl := range rhythm.Slots {
				if sl.Count > 0 {
					nonZero++
				}
			}
			gap := float32(0)
			if nonZero > 1 {
				gap = 0.01
			}
			cursor := float32(0)
			for i, sl := range rhythm.Slots {
				if sl.Count <= 0 {
					continue
				}
				span := float32(sl.Ratio)
				from := cursor + gap/2
				to := cursor + span - gap/2
				if to <= from {
					to = from + 0.006
				}
				arc := (&ui.Path{}).Circle(cx, cy, radius).Trim(from, min(1, to))
				p.StrokePath(arc, strokeW, slotColors[i])
				cursor += span
			}
		}).Children(func() {
			ui.Column(c).Center().Gap(1).Children(func() {
				ui.Textf(c, "%d", rhythm.TotalEvents).
					Font(theme.FontSerif).FontFeatures("tnum").FontSize(22).Bold().TextColor(pal.TextPrimary)
				ui.Text(c, "次读诗").Font(theme.FontSans).FontSize(10.5).TextColor(pal.TextMuted)
			})
		})
	}

	renderBreakdown := func() {
		ui.Column(c.Key("rhythm-slot-list")).Grow(1).FillWidth().Gap(9).Children(func() {
			for i, sl := range rhythm.Slots {
				pct := int(math.Round(sl.Ratio * 100))
				ui.Column(c.Key("rh-slot-" + sl.Key)).FillWidth().Gap(4).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(7).Children(func() {
						ui.Box(c).Width(8).Height(8).Radius(999).Background(slotColors[i])
						ui.Text(c, sl.Label).Font(theme.FontSerif).FontSize(12.5).Bold().TextColor(pal.TextPrimary)
						ui.Text(c, sl.RangeText).Font(theme.FontMono).FontFeatures("tnum").FontSize(10.5).TextColor(pal.TextMuted).Grow(1)
						ui.Textf(c, "%d 首 · %d%%", sl.Count, pct).Font(theme.FontMono).FontFeatures("tnum").FontSize(11).TextColor(pal.TextSecondary)
					})
					ui.Progress(c, sl.Ratio).Height(4).FillWidth().TextColor(slotColors[i]).Background(pal.SurfaceMuted)
				})
			}
		})
	}

	if isTablet {
		ui.Row(c.Key("rhythm-5-tablet")).FillWidth().Gap(22).AlignItems(ui.Center).Children(func() {
			renderDonut()
			renderBreakdown()
		})
		return
	}
	ui.Column(c.Key("rhythm-5-phone")).FillWidth().Gap(14).AlignItems(ui.Center).Children(func() {
		renderDonut()
		renderBreakdown()
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

// --- 模块三：完读诗集墙 ---

func (a *AppUI) renderStatsFinishedWallSection(c *ui.Context, pal theme.Palette, dash storage.StatsDashboard, isTablet bool) {
	ui.Column(c.Key("stats-finished-section")).FillWidth().Gap(10).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, "完读诗集墙").Font(theme.FontSerif).FontSize(15.5).Bold().LetterSpacing(0.3).TextColor(pal.TextPrimary).Grow(1)
			ui.Textf(c, "已合上 %d 卷", len(dash.FinishedBooks)).
				Font(theme.FontMono).FontFeatures("tnum").FontSize(11.5).TextColor(pal.TextMuted)
		})

		if len(dash.FinishedBooks) == 0 {
			emptyMsg := "还没有合上一本完读诗集。全部诗篇读完后会自动点亮，也可在下方进度列表手动校准完读。"
			if a.statsPeriod != storage.StatsPeriodAll {
				emptyMsg = "该时间窗口内尚未合上新的诗集，可切换「全部」回看历史完读墙。"
			}
			ui.Column(c.Key("finished-empty")).
				FillWidth().
				Padding(18).
				Radius(10).
				Background(pal.Surface).
				Border(1, pal.Border).
				Children(func() {
					ui.Text(c, emptyMsg).Font(theme.FontNote).FontSize(12.5).LineHeight(1.6).TextColor(pal.TextMuted)
				})
			return
		}

		cols := 3
		if isTablet {
			cols = 4
		}
		ui.Grid(c.Key("stats-finished-grid")).Columns(cols).Gap(14).FillWidth().Children(func() {
			for _, item := range dash.FinishedBooks {
				item := item
				b := item.Book
				ui.Column(c.Key("fin-wall-" + b.ID)).Gap(5).Children(func() {
					if components.BookCoverCard(
						c, "fin-cov-"+b.ID, b.ID, b.Title, b.Author,
						a.coverImageFor(b), pal, false, b.LocalState != domain.LocalStateDownloaded, false,
					) {
						a.openFinishedBookDetail(item)
					}
					// 完读日期 + 本书精选小红心数
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(4).Padding(0, 1).Children(func() {
						dateText := formatFinishedMonth(item.Progress.FinishedAt)
						ui.Text(c, dateText).Font(theme.FontMono).FontFeatures("tnum").FontSize(10.5).TextColor(pal.TextMuted).Grow(1)
						if item.FavoriteCount > 0 {
							ui.Icon(c, components.IconHeartFilled).Width(10.5).Height(10.5).TextColor(pal.Accent)
							ui.Textf(c, "%d", item.FavoriteCount).Font(theme.FontMono).FontFeatures("tnum").FontSize(10.5).TextColor(pal.TextSecondary)
						}
					})
				})
			}
		})
	})
}

// --- 模块四：诗人作者云 ---

func (a *AppUI) renderStatsAuthorCloudSection(c *ui.Context, pal theme.Palette, dash storage.StatsDashboard, _ bool) {
	modes := []struct {
		id    storage.AuthorWeightMode
		label string
	}{
		{storage.AuthorWeightCombined, "综合"},
		{storage.AuthorWeightRead, "读过"},
		{storage.AuthorWeightCollection, "精选"},
	}

	ui.Column(c.Key("stats-author-section")).FillWidth().Gap(10).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, "诗人云").Font(theme.FontSerif).FontSize(15.5).Bold().LetterSpacing(0.3).TextColor(pal.TextPrimary).Grow(1)
			ui.Row(c.Key("author-mode-pills")).
				Padding(2).
				Gap(2).
				Radius(7).
				Background(pal.SurfaceMuted).
				Border(1, pal.Border).
				Children(func() {
					for _, m := range modes {
						active := a.statsAuthorMode == m.id
						bg, fg := ui.Transparent, pal.TextSecondary
						if active {
							bg, fg = pal.Surface, pal.TextPrimary
						}
						btn := ui.Box(c.Key("am-"+string(m.id))).Padding(4, 9).Radius(5).Background(bg).Cursor(ui.CursorPointer)
						btn.Children(func() {
							t := ui.Text(c, m.label).Font(theme.FontSans).FontSize(11).TextColor(fg)
							if active {
								t.Bold()
							}
						})
						if btn.Clicked() {
							a.setStatsAuthorMode(m.id)
						}
					}
				})
		})

		ui.Column(c.Key("stats-author-card")).
			FillWidth().
			Padding(16, 18).
			Gap(12).
			Radius(10).
			Background(pal.Surface).
			Border(1, pal.Border).
			Children(func() {
				if len(dash.Authors) == 0 {
					ui.Text(c, "阅读诗集、点亮诗篇小红心或留存散页后，这里会汇聚你的常读诗人图谱。").
						Font(theme.FontNote).FontSize(12.5).LineHeight(1.6).TextColor(pal.TextMuted)
					return
				}

				ui.Row(c.Key("author-cloud-wrap")).FillWidth().Wrap().AlignItems(ui.End).Gap(10).Children(func() {
					for i, au := range dash.Authors {
						au := au
						size, font, clr, bold := authorCloudStyle(au.Tier, pal)
						item := ui.Row(c.Key(fmt.Sprintf("au-%d-%s", i, au.Author))).
							Padding(4, 8).
							Gap(4).
							Radius(6).
							AlignItems(ui.End).
							Cursor(ui.CursorPointer)
						if au.Tier >= 5 {
							item.Background(pal.HighlightBg)
						}
						theme.ApplyPressScale(item, 0.94)
						item.Children(func() {
							nameEl := ui.Text(c, au.Author).Font(font).FontSize(size).TextColor(clr)
							if bold {
								nameEl.Bold()
							}
							badgeVal := authorBadgeCount(au, a.statsAuthorMode)
							ui.Textf(c, "%d", badgeVal).
								Font(theme.FontMono).FontFeatures("tnum").FontSize(10).TextColor(pal.TextMuted).Padding(0, 0, 2, 0)
						})
						if item.Clicked() {
							a.openAuthorProfile(au.Author)
						}
					}
				})

				ui.Text(c, "点击任意诗人查看档案、涉及诗集与收藏精选").
					Font(theme.FontSans).FontSize(11).TextColor(pal.TextMuted)
			})
	})
}

// --- 模块五：在读书架进度（含手动完读校准） ---

func (a *AppUI) renderStatsReadingProgressSection(c *ui.Context, pal theme.Palette, items []storage.ReadingBookItem, isTablet bool) {
	if len(items) == 0 {
		return
	}
	ui.Column(c.Key("stats-reading-section")).FillWidth().Gap(10).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "在读与书架进度").Font(theme.FontSerif).FontSize(15.5).Bold().LetterSpacing(0.3).TextColor(pal.TextPrimary).Grow(1)
			ui.Textf(c, "%d 本未完读", len(items)).Font(theme.FontMono).FontFeatures("tnum").FontSize(11.5).TextColor(pal.TextMuted)
		})

		ui.Column(c.Key("stats-reading-list")).Gap(10).FillWidth().Children(func() {
			for _, it := range items {
				b := it.Book
				prog := it.Progress
				ratio := 0.0
				if b.PoemCount > 0 {
					ratio = min(1, float64(prog.ReadPoemCount())/float64(b.PoemCount))
				}
				statusText := fmt.Sprintf("已读 %d / %d 首", prog.ReadPoemCount(), b.PoemCount)

				row := ui.Row(c.Key("stat-rd-"+b.ID)).
					FillWidth().
					Padding(11, 13).
					Gap(12).
					Radius(8).
					Background(pal.Surface).
					Border(1, pal.Border).
					AlignItems(ui.Center)
				row.Children(func() {
					openArea := ui.Row(c.Key("stat-rd-open-" + b.ID)).
						Grow(1).
						Gap(12).
						AlignItems(ui.Center).
						Cursor(ui.CursorPointer)
					theme.ApplyPressScale(openArea, 0.985)
					openArea.Children(func() {
						components.BookCoverThumbnail(c, "st-th-"+b.ID, b.ID, a.coverImageFor(b), pal)
						ui.Column(c).Grow(1).Gap(5).Children(func() {
							ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
								ui.Text(c, b.Title).Font(theme.FontSerif).FontSize(13.5).Bold().TextColor(pal.TextPrimary).Grow(1).MaxLines(1)
								ui.Text(c, statusText).Font(theme.FontMono).FontFeatures("tnum").FontSize(11).TextColor(pal.TextMuted)
							})
							ui.Progress(c, ratio).Height(3).FillWidth().TextColor(pal.Accent).Background(pal.Border)
						})
					})
					if openArea.Clicked() {
						a.activeTab = components.TabShelf
						a.shelfSubTab = components.SubTabBooks
						a.openBook(b.ID, prog.LastPoemID, nil, !isTablet)
					}

					// 手动完读校准按钮（避免序言/后记未翻导致停在 98%）
					markBtn := ui.Box(c.Key("stat-mark-fin-"+b.ID)).
						Padding(5, 9).
						Radius(6).
						Background(pal.SurfaceMuted).
						Border(0.75, pal.Border).
						Cursor(ui.CursorPointer)
					theme.ApplyPressScale(markBtn, 0.93)
					markBtn.Children(func() {
						ui.Text(c, "标为完读").Font(theme.FontSans).FontSize(11).TextColor(pal.TextSecondary)
					})
					if markBtn.Clicked() {
						a.toggleBookFinishedAsync(b.ID, b.Title, true)
					}
				})
			}
		})
	})
}

// --- 统计交互控制器 ---

func (a *AppUI) setStatsPeriod(p storage.StatsPeriod) {
	if a.statsPeriod == p {
		return
	}
	a.mu.Lock()
	a.statsPeriod = p
	a.mu.Unlock()
	a.refreshStatsDashboardAsync()
}

func (a *AppUI) setStatsHeatmapYear(year int) {
	if a.statsHeatmapYear == year {
		return
	}
	a.mu.Lock()
	a.statsHeatmapYear = year
	a.mu.Unlock()
	if year == 0 {
		a.statsHeatmapScroll.X = math.MaxFloat32
	} else {
		a.statsHeatmapScroll.X = 0
	}
	a.refreshStatsDashboardAsync()
}

func (a *AppUI) setStatsAuthorMode(mode storage.AuthorWeightMode) {
	if a.statsAuthorMode == mode {
		return
	}
	a.mu.Lock()
	a.statsAuthorMode = mode
	a.mu.Unlock()
	a.refreshStatsDashboardAsync()
}

func (a *AppUI) refreshStatsDashboardAsync() {
	a.mu.Lock()
	a.statsSeq++
	reqSeq := a.statsSeq
	period := a.statsPeriod
	year := a.statsHeatmapYear
	authorMode := a.statsAuthorMode
	a.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		dash, err := a.svc.GetStatsDashboard(ctx, storage.StatsQuery{
			Period:     period,
			Year:       year,
			AuthorMode: authorMode,
		})
		if err != nil {
			return
		}
		a.dispatchUI(func() {
			a.mu.Lock()
			match := a.statsSeq == reqSeq
			a.mu.Unlock()
			if match {
				a.statsDashboard = dash
			}
		})
	}()
}

func (a *AppUI) selectHeatmapDate(date string) {
	if date == "" {
		return
	}
	if a.statsSelectedDate == date {
		a.statsSelectedDate = ""
		a.statsDayEvents = nil
		a.statsDayLoading = false
		return
	}
	a.statsSelectedDate = date
	a.statsDayLoading = true
	a.mu.Lock()
	a.statsDaySeq++
	reqSeq := a.statsDaySeq
	a.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		items, err := a.svc.ListReadingEventsByDate(ctx, date)
		a.dispatchUI(func() {
			a.mu.Lock()
			match := a.statsDaySeq == reqSeq && a.statsSelectedDate == date
			a.mu.Unlock()
			if !match {
				return
			}
			a.statsDayLoading = false
			if err == nil {
				a.statsDayEvents = items
			}
		})
	}()
}

func (a *AppUI) openFinishedBookDetail(item storage.FinishedBookItem) {
	cp := item
	a.detailFinishedBook = &cp
	a.showFinishedModal = true
}

func (a *AppUI) openAuthorProfile(author string) {
	author = strings.TrimSpace(author)
	if author == "" {
		return
	}
	a.detailAuthorName = author
	a.detailAuthorProfile = nil
	a.detailAuthorLoading = true
	a.showAuthorModal = true

	a.mu.Lock()
	a.authorProfileSeq++
	reqSeq := a.authorProfileSeq
	a.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		prof, err := a.svc.GetAuthorProfile(ctx, author)
		a.dispatchUI(func() {
			a.mu.Lock()
			match := a.authorProfileSeq == reqSeq
			a.mu.Unlock()
			if !match {
				return
			}
			a.detailAuthorLoading = false
			if err == nil {
				cp := prof
				a.detailAuthorProfile = &cp
			}
		})
	}()
}

func (a *AppUI) toggleBookFinishedAsync(bookID, bookTitle string, finished bool) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := a.svc.MarkBookFinished(ctx, bookID, finished); err != nil {
			a.showTransientToast("更新完读状态失败：" + err.Error())
			return
		}
		a.reloadSync(nil)
		if finished {
			a.showTransientToast(fmt.Sprintf("已将《%s》收入完读墙", bookTitle))
		} else {
			a.showTransientToast(fmt.Sprintf("已将《%s》设为重读中", bookTitle))
		}
	}()
}

func (a *AppUI) jumpToPoemFromStats(bookID, poemID string, lines []app.LineRange, enterMobile bool) {
	a.showFinishedModal = false
	a.showAuthorModal = false
	a.activeTab = components.TabShelf
	a.shelfSubTab = components.SubTabBooks
	a.shelfPager.scopeKey = ""
	a.openBookWithSource(bookID, poemID, lines, enterMobile, entryStats)
}

// --- 主题配色与排版辅助 ---

func heatmapColors(pal theme.Palette) [5]ui.Color {
	switch pal.ID {
	case theme.PresetEditorial:
		return [5]ui.Color{
			ui.Hex("#EFEFEF"),
			ui.Hex("#D6E0FA"),
			ui.Hex("#9BB4F2"),
			ui.Hex("#5479DC"),
			ui.Hex("#2345B5"),
		}
	case theme.PresetDarkMinimal:
		return [5]ui.Color{
			ui.Hex("#25262B"),
			ui.Hex("#3E2D29"),
			ui.Hex("#693D34"),
			ui.Hex("#9C5546"),
			ui.Hex("#D0735E"),
		}
	default: // 画册风
		return [5]ui.Color{
			ui.Hex("#EAE9E1"),
			ui.Hex("#D7DFCE"),
			ui.Hex("#A8BC9B"),
			ui.Hex("#6E8A68"),
			ui.Hex("#BC5B46"),
		}
	}
}

func rhythmSlotColors(pal theme.Palette) [5]ui.Color {
	switch pal.ID {
	case theme.PresetEditorial:
		return [5]ui.Color{
			ui.Hex("#8EA8E8"),
			ui.Hex("#5B664E"),
			ui.Hex("#752B36"),
			ui.Hex("#2345B5"),
			ui.Hex("#2C323B"),
		}
	case theme.PresetDarkMinimal:
		return [5]ui.Color{
			ui.Hex("#8DA688"),
			ui.Hex("#B39362"),
			ui.Hex("#D0735E"),
			ui.Hex("#6D89A8"),
			ui.Hex("#5B5E6E"),
		}
	default: // 画册风
		return [5]ui.Color{
			ui.Hex("#9EB393"),
			ui.Hex("#C2A06E"),
			ui.Hex("#BC5B46"),
			ui.Hex("#334C68"),
			ui.Hex("#5C5A55"),
		}
	}
}

func authorCloudStyle(tier int, pal theme.Palette) (size float32, font string, clr ui.Color, bold bool) {
	switch tier {
	case 5:
		return 22, theme.FontSerif, pal.Accent, true
	case 4:
		return 18, theme.FontSerif, pal.TextPrimary, true
	case 3:
		return 15.5, theme.FontSerif, pal.TextPrimary.Alpha(0.88), false
	case 2:
		return 13.5, theme.FontNote, pal.TextSecondary, false
	default:
		return 12, theme.FontNote, pal.TextMuted, false
	}
}

func authorBadgeCount(au storage.AuthorStat, mode storage.AuthorWeightMode) int {
	switch mode {
	case storage.AuthorWeightRead:
		return au.ReadCount
	case storage.AuthorWeightCollection:
		return au.FavoriteCount + au.QuoteCount
	default:
		return au.ReadCount + au.FavoriteCount + au.QuoteCount
	}
}

func formatCNDate(dateStr string) string {
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return dateStr
	}
	if t.Year() == time.Now().Year() {
		return fmt.Sprintf("%d月%d日", int(t.Month()), t.Day())
	}
	return fmt.Sprintf("%d年%d月%d日", t.Year(), int(t.Month()), t.Day())
}

func formatFinishedMonth(tsMs int64) string {
	if tsMs <= 0 {
		return "已完读"
	}
	t := time.UnixMilli(tsMs).Local()
	return fmt.Sprintf("%d.%02d", t.Year(), int(t.Month()))
}

func formatFinishedFullDate(tsMs int64) string {
	if tsMs <= 0 {
		return "已完读"
	}
	t := time.UnixMilli(tsMs).Local()
	return fmt.Sprintf("%d年%d月%d日 完读", t.Year(), int(t.Month()), t.Day())
}

func formatEventClock(readAtMs int64, fallbackHour int) string {
	if readAtMs > 0 {
		return time.UnixMilli(readAtMs).Local().Format("15:04")
	}
	return fmt.Sprintf("%02d:00", fallbackHour)
}
