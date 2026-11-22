package views

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/domain"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/layout"
	"stillpoets/internal/ui/theme"
)

func (a *AppUI) renderStatsPage(c *ui.Context, pal theme.Palette, isTablet bool) {
	ui.Column(c.Key("stats-page")).Fill().Children(func() {
		components.BrandHeader(c, pal, "统计", false, nil, nil)

		bottomPad := float32(88)
		if !isTablet && a.useFloatingBottomNav() {
			bottomPad = 108
		}
		ui.Scroll(c.Key("stats-scroll")).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(12, 20, bottomPad, 20).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(layout.ReadingMaxWidthDIP + 100).Gap(18).Children(func() {
					ui.Text(c, "阅读总览").FontSize(14).Bold().TextColor(pal.TextSecondary)

					cols := 2
					if isTablet {
						cols = 3
					}
					metrics := []struct {
						label string
						value int
						unit  string
					}{
						{"藏书诗集", a.stats.BookCount, "本"},
						{"已完读", a.stats.FinishedBookCount, "本"},
						{"累计已读", a.stats.ReadPoemCount, "首"},
						{"诗篇精选", a.stats.FavoriteCount, "首"},
						{"留存散页", a.stats.QuoteCount, "张"},
						{"已离线缓存", a.stats.OfflineBookCount, "本"},
					}

					ui.Grid(c.Key("stats-grid")).Columns(cols).Gap(12).FillWidth().Children(func() {
						for _, m := range metrics {
							ui.Column(c.Key("metric-"+m.label)).
								Padding(16).
								Gap(6).
								Radius(8).
								Background(pal.Surface).
								Border(1, pal.Border).
								Children(func() {
									ui.Text(c, m.label).FontSize(12).TextColor(pal.TextMuted)
									ui.Row(c).AlignItems(ui.End).Gap(4).Children(func() {
										ui.Textf(c, "%d", m.value).FontSize(26).Bold().TextColor(pal.TextPrimary)
										ui.Text(c, m.unit).FontSize(12).TextColor(pal.TextMuted).Padding(0, 0, 4, 0)
									})
								})
						}
					})

					// 诗集阅读进度清单
					if len(a.books) > 0 {
						ui.Text(c, "书架阅读进度").FontSize(14).Bold().TextColor(pal.TextSecondary).Padding(8, 0, 0, 0)
						ui.Column(c.Key("stats-book-list")).Gap(10).FillWidth().Children(func() {
							for _, b := range a.books {
								prog := a.progressMap[b.ID]
								ratio := 0.0
								if b.PoemCount > 0 {
									ratio = float64(prog.ReadPoemCount()) / float64(b.PoemCount)
									if ratio > 1 {
										ratio = 1
									}
								}
								statusText := fmt.Sprintf("已读 %d / %d 首", prog.ReadPoemCount(), b.PoemCount)
								if prog.Status == domain.ReadStatusFinished {
									statusText = "已完读 · " + statusText
								}

								row := ui.Row(c.Key("stat-bk-"+b.ID)).
									FillWidth().
									Padding(12, 14).
									Gap(12).
									Radius(8).
									Background(pal.Surface).
									Border(1, pal.Border).
									AlignItems(ui.Center).
									Cursor(ui.CursorPointer)
								theme.ApplyPressScale(row, 0.985)
								row.Children(func() {
									components.BookCoverThumbnail(c, "st-th-"+b.ID, b.ID, a.coverImageFor(b), pal)
									ui.Column(c).Grow(1).Gap(5).Children(func() {
										ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
											ui.Text(c, b.Title).FontSize(14).Bold().TextColor(pal.TextPrimary).Grow(1).MaxLines(1)
											ui.Text(c, statusText).FontSize(11).TextColor(pal.TextMuted)
										})
										ui.Progress(c, ratio).Height(3).FillWidth().TextColor(pal.Accent).Background(pal.Border)
									})
								})
								if row.Clicked() {
									a.activeTab = components.TabShelf
									a.shelfSubTab = components.SubTabBooks
									a.openBook(b.ID, prog.LastPoemID, nil, !isTablet)
								}
							}
						})
					}

					// V1.1 预告说明
					ui.Column(c.Key("stats-roadmap")).
						FillWidth().
						Padding(14, 16).
						Gap(4).
						Radius(8).
						Background(pal.SurfaceMuted).
						Children(func() {
							ui.Text(c, "V1.1 计划扩展").FontSize(12).Bold().TextColor(pal.TextSecondary)
							ui.Text(c, "完读封面墙、年度读诗热力图、阅读时段占比与常读诗人云将在 V1.1 引入。").
								FontSize(12).LineHeight(1.6).TextColor(pal.TextMuted)
						})
				})
			})
		})
	})
}
