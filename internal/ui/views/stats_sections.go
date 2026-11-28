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
	"stillpoets/internal/ui/theme"
)

// --- 模块三：完读诗集墙 ---

func renderStatsEventRowBox(c *ui.Context, key string, pal theme.Palette, vPad, gap float32) ui.Element {
	row := ui.Row(c.Key(key)).
		FillWidth().
		Padding(vPad, 11).
		Gap(gap).
		Radius(7).
		Background(pal.Background).
		Border(0.75, pal.Border).
		AlignItems(ui.Center).
		Cursor(ui.CursorPointer)
	theme.ApplyPressScale(row, 0.985)
	return row
}

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
					if components.BookCoverCard(c, pal, components.BookCoverProps{
						Key:      "fin-cov-" + b.ID,
						BookID:   b.ID,
						Title:    b.Title,
						Author:   b.Author,
						CoverImg: a.coverImageFor(b),
						IsRemote: b.LocalState != domain.LocalStateDownloaded,
					}) {
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
