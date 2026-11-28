package views

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/mobile"

	"stillpoets/internal/domain"
	"stillpoets/internal/storage"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/sharecard"
	"stillpoets/internal/ui/theme"
)

// ---------- 5 大分享卡片入口构造函数 ----------

func (a *AppUI) currentTime() time.Time {
	if a.nowFn != nil {
		return a.nowFn()
	}
	return time.Now()
}

func (a *AppUI) initShareCardOptions() {
	pal := a.palette()
	if a.shareCardOpts.Template == "" {
		a.shareCardOpts = sharecard.DefaultOptions(pal.ID)
	} else {
		// 每次打开继承当前 App 主题，保持与小组件/阅读界面的视觉一致性；用户在弹窗内仍可自由临时切换
		a.shareCardOpts.Theme = sharecard.ThemeFromAppPreset(pal.ID)
	}
	a.shareCardBusy = false
	a.shareCardBusyLabel = ""
	a.shareCardStatus = ""
	a.shareCardScroll = ui.ScrollState{}
}

// openQuoteShareCard 从散页详情弹窗打开「单句卡片」预览。
func (a *AppUI) openQuoteShareCard(qv storage.QuoteView) {
	tag := "独立散句"
	if qv.EffectiveKind == domain.QuoteKindExcerpt {
		tag = "诗歌节选"
	}
	a.initShareCardOptions()
	a.shareCardData = sharecard.CardData{
		Kind:       sharecard.KindQuote,
		Date:       a.currentTime(),
		FontFamily: a.settings.FontFamily,
		FontWeight: a.settings.EffectiveFontWeight(),
		QuoteText:  qv.Quote.Content,
		QuoteTag:   tag,
		BookID:     qv.Quote.BookID,
		SourceBook: qv.Quote.SourceBook,
		Author:     qv.Quote.Author,
		PoemTitle:  qv.Quote.PoemTitle,
	}
	a.showQuoteDetail = false
	a.showShareCardModal = true
}

// openSelectionShareCard 从阅读器长按段落或勾选操作条直接生成所选诗句的「单句卡片」。
func (a *AppUI) openSelectionShareCard(book domain.Book, poem domain.Poem, content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	author := strings.TrimSpace(poem.Author)
	if author == "" {
		author = strings.TrimSpace(book.Author)
	}
	a.initShareCardOptions()
	a.shareCardData = sharecard.CardData{
		Kind:       sharecard.KindQuote,
		Date:       a.currentTime(),
		FontFamily: a.settings.FontFamily,
		FontWeight: a.settings.EffectiveFontWeight(),
		QuoteText:  content,
		QuoteTag:   "诗歌节选",
		BookID:     book.ID,
		BookTitle:  book.Title,
		SourceBook: book.Title,
		Author:     author,
		PoemTitle:  poem.Title,
	}
	a.clearPoemSelectionState()
	a.showShareCardModal = true
}

// openPoemShareCard 从阅读器顶栏打开纯净的「整诗长图」卡片预览。
func (a *AppUI) openPoemShareCard(book domain.Book, poem domain.Poem) {
	author := strings.TrimSpace(poem.Author)
	if author == "" {
		author = strings.TrimSpace(book.Author)
	}
	now := a.currentTime()
	poemCard := sharecard.CardData{
		Kind:         sharecard.KindPoem,
		Date:         now,
		FontFamily:   a.settings.FontFamily,
		FontWeight:   a.settings.EffectiveFontWeight(),
		BookID:       book.ID,
		BookTitle:    book.Title,
		SourceBook:   book.Title,
		Author:       author,
		PoemTitle:    poem.Title,
		PoemSubtitle: poem.Subtitle,
		PoemSection:  poem.Section,
		Epigraph:     poem.Epigraph,
		Stanzas:      poem.Stanzas,
		Colophon:     poem.Colophon,
	}

	a.clearPoemSelectionState()
	a.initShareCardOptions()
	a.shareCardData = poemCard
	a.showShareCardModal = true
}

// openCollectionShareCard 生成某本诗集的「精选合辑长图」（支持从目录抽屉或完读详情弹窗触发）。
func (a *AppUI) openCollectionShareCard(book domain.Book, favs []domain.FavoritePoem) {
	if len(favs) == 0 {
		a.showTransientToastOnUI("本书还没有点亮 ❤️ 精选诗篇")
		return
	}
	// 按更新时间或诗题排序保证稳定顺序
	sorted := append([]domain.FavoritePoem(nil), favs...)
	slices.SortFunc(sorted, func(x, y domain.FavoritePoem) int {
		if x.CreatedAt < y.CreatedAt {
			return -1
		}
		if x.CreatedAt > y.CreatedAt {
			return 1
		}
		return strings.Compare(x.PoemTitle, y.PoemTitle)
	})

	items := make([]sharecard.CollectionItem, 0, len(sorted))
	for _, f := range sorted {
		items = append(items, sharecard.CollectionItem{
			PoemTitle: f.PoemTitle,
			Author:    f.Author,
			Excerpt:   strings.TrimSpace(f.Excerpt),
		})
	}

	a.initShareCardOptions()
	a.shareCardData = sharecard.CardData{
		Kind:            sharecard.KindCollection,
		Date:            a.currentTime(),
		FontFamily:      a.settings.FontFamily,
		FontWeight:      a.settings.EffectiveFontWeight(),
		BookID:          book.ID,
		BookTitle:       book.Title,
		Author:          book.Author,
		CoverImg:        a.coverImageFor(book),
		FavoriteCount:   len(items),
		CollectionItems: items,
	}
	a.showTOCModal = false
	a.showFinishedModal = false
	a.showShareCardModal = true
}

// openFinishedShareCard 从完读详情弹窗打开「完读纪念卡」。
func (a *AppUI) openFinishedShareCard(item storage.FinishedBookItem) {
	items := make([]sharecard.CollectionItem, 0, len(item.TopFavorites))
	for _, fav := range item.TopFavorites {
		if strings.TrimSpace(fav.Excerpt) == "" && strings.TrimSpace(fav.PoemTitle) == "" {
			continue
		}
		items = append(items, sharecard.CollectionItem{
			PoemTitle: fav.PoemTitle,
			Author:    fav.Author,
			Excerpt:   strings.TrimSpace(fav.Excerpt),
		})
	}

	a.initShareCardOptions()
	a.shareCardData = sharecard.CardData{
		Kind:            sharecard.KindFinished,
		Date:            a.currentTime(),
		FontFamily:      a.settings.FontFamily,
		FontWeight:      a.settings.EffectiveFontWeight(),
		BookID:          item.Book.ID,
		BookTitle:       item.Book.Title,
		Author:          item.Book.Author,
		CoverImg:        a.coverImageFor(item.Book),
		FinishedAt:      item.Progress.FinishedAt,
		ReadPoemCount:   item.Progress.ReadPoemCount(),
		TotalPoemCount:  item.Book.PoemCount,
		FavoriteCount:   item.FavoriteCount,
		QuoteCount:      item.QuoteCount,
		CollectionItems: items,
	}
	a.showFinishedModal = false
	a.showShareCardModal = true
}

// openStatsReportShareCard 从统计页顶部周期栏打开当前窗口的「读诗报告长图」。
func (a *AppUI) openStatsReportShareCard() {
	now := a.currentTime()
	dash := a.statsDashboard
	periodLabel, rangeNote := statsReportTitles(a.statsPeriod, now)

	finishedTitles := make([]string, 0, len(dash.FinishedBooks))
	for _, fb := range dash.FinishedBooks {
		if t := strings.TrimSpace(fb.Book.Title); t != "" {
			finishedTitles = append(finishedTitles, t)
		}
	}

	a.initShareCardOptions()
	a.shareCardData = sharecard.CardData{
		Kind:              sharecard.KindStatsReport,
		Date:              now,
		FontFamily:        a.settings.FontFamily,
		FontWeight:        a.settings.EffectiveFontWeight(),
		ReportPeriodLabel: periodLabel,
		ReportRangeNote:   rangeNote,
		Overview:          dash.Overview,
		Heatmap:           dash.Heatmap,
		Rhythm:            dash.Rhythm,
		TopAuthors:        dash.Authors,
		FinishedTitles:    finishedTitles,
	}
	a.showShareCardModal = true
}

func statsReportTitles(p storage.StatsPeriod, now time.Time) (string, string) {
	switch p {
	case storage.StatsPeriodWeek:
		return "本周读诗手记", fmt.Sprintf("截至 %s · 近七日回望", now.Format("2006.01.02"))
	case storage.StatsPeriodMonth:
		return fmt.Sprintf("%d 年 %d 月读诗报告", now.Year(), int(now.Month())), "本月静默记录 · 温柔回望"
	case storage.StatsPeriodYear:
		return fmt.Sprintf("%d 年度读诗报告", now.Year()), fmt.Sprintf("%d.01.01 — %s", now.Year(), now.Format("2006.01.02"))
	default:
		return "个人读诗年轮总览", fmt.Sprintf("截至 %s · 全部阅读足迹", now.Format("2006.01.02"))
	}
}

// ---------- 统一「高清卡片预览与导出」弹窗 ----------

func (a *AppUI) renderShareCardModal(c *ui.Context, pal theme.Palette) {
	if !a.showShareCardModal {
		return
	}

	winW, winH := c.Size()
	dlgW := float32(470)
	if winW > 0 && winW-24 < dlgW {
		dlgW = max(296, winW-24)
	}
	dlgMaxH := float32(760)
	if winH > 0 && winH-44 < dlgMaxH {
		dlgMaxH = max(460, winH-44)
	}

	components.AnimatedDialog(c, &a.showShareCardModal, pal, components.DialogOptions{Key: "share-card-export", MaxWidth: dlgW, MaxHeight: dlgMaxH}, func() {
		// 1. 顶栏：卡片标题 + 关闭按钮
		ui.Row(c.Key("sc-dlg-top")).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, "分享卡片 · "+a.shareCardData.Kind.Label()).
				Font(theme.FontSerif).FontSize(16.5).Bold().TextColor(pal.TextPrimary).Grow(1).MaxLines(1)

			closeBtn := ui.Box(c.Key("sc-dlg-close")).Padding(4).Cursor(ui.CursorPointer)
			theme.ApplyPressScale(closeBtn, 0.90)
			closeBtn.Children(func() {
				ui.Icon(c, components.IconClose).Width(16).Height(16).TextColor(pal.TextMuted)
			})
			if closeBtn.Clicked() && !a.shareCardBusy {
				a.showShareCardModal = false
			}
		})

		a.renderShareCardPreview(c, pal, dlgW, dlgMaxH)

		a.renderShareCardControls(c, pal)

		// 导出结果/错误提示行
		if a.shareCardStatus != "" {
			ui.Text(c, a.shareCardStatus).Font(theme.FontSans).FontSize(11.5).TextColor(pal.Accent)
		}

		a.renderShareCardActions(c, pal)
	})
}

// exportShareCardAsync 在后台 goroutine 中以 3x 分辨率（1080px 宽）离屏生成高清 PNG 并执行保存或系统分享。
func (a *AppUI) exportShareCardAsync(mode string) {
	if a.shareCardBusy {
		return
	}
	data := a.shareCardData
	opts := a.shareCardOpts
	exportDir := a.svc.ExportsDir()

	a.shareCardBusy = true
	a.shareCardStatus = ""
	if mode == "save" {
		a.shareCardBusyLabel = "正在生成高清图片并保存至相册…"
	} else {
		a.shareCardBusyLabel = "正在生成高清图片…"
	}

	a.mu.Lock()
	savePhotoFn := a.savePhotoFn
	shareImageFn := a.shareImageFn
	exportFileFn := a.exportFileFn
	a.mu.Unlock()

	go func() {
		res, err := sharecard.ExportPNGFile(exportDir, data, opts, sharecard.DefaultRenderConfig())
		if err != nil {
			a.dispatchUI(func() {
				a.shareCardBusy = false
				a.shareCardBusyLabel = ""
				a.shareCardStatus = "图片生成失败：" + err.Error()
			})
			return
		}

		if mode == "save" {
			a.saveShareCard(res, savePhotoFn, exportFileFn)
			return
		}

		a.shareExportedCard(res.FilePath, data.Kind, shareImageFn, exportFileFn)
	}()
}

func (a *AppUI) renderShareCardPreview(c *ui.Context, pal theme.Palette, dlgW, dlgMaxH float32) {
	// 2. 实时可滚动预览画板（预览宽度适配弹窗，严格复用 sharecard.RenderCard）
	previewW := min(float32(348), max(float32(252), dlgW-68))
	previewMaxH := max(float32(220), dlgMaxH-296)
	stageBg := pal.SurfaceMuted
	if pal.IsDark() {
		stageBg = ui.Hex("#111214")
	}

	ui.Box(c.Key("sc-preview-stage")).
		FillWidth().
		MaxHeight(previewMaxH).
		Grow(1).
		Radius(10).
		Background(stageBg).
		Border(0.75, pal.Border).
		Clip().
		Children(func() {
			ui.Scroll(c.Key("sc-preview-scroll")).
				TrackScroll(&a.shareCardScroll).
				Fill().
				Children(func() {
					ui.Column(c.Key("sc-preview-center")).
						FillWidth().
						AlignItems(ui.Center).
						Padding(16, 12, 16, 12).
						Children(func() {
							cardShadow := ui.RGBA(20, 18, 14, 0.18)
							if pal.IsDark() {
								cardShadow = ui.RGBA(0, 0, 0, 0.48)
							}
							ui.Box(c.Key("sc-preview-frame")).
								Width(previewW).
								Radius(4).
								Clip().
								Shadow(0, 6, 18, -2, cardShadow).
								Children(func() {
									sharecard.RenderCard(c, a.shareCardData, a.shareCardOpts, previewW)
								})
						})
				})
		})
}

func (a *AppUI) renderShareCardTemplate(c *ui.Context, pal theme.Palette, tpl sharecard.TemplateMeta) {
	active := a.shareCardOpts.Template == tpl.ID
	bg, fg, bd := pal.Background, pal.TextSecondary, pal.Border
	if active {
		bg, fg, bd = pal.Accent, pal.AccentText, pal.Accent
	}
	pill := ui.Box(c.Key("sc-tpl-"+string(tpl.ID))).
		Padding(5, 11).
		Radius(999).
		Background(bg).
		Border(0.9, bd).
		Cursor(ui.CursorPointer).
		Transition(theme.ColorTransition())
	theme.ApplyPressScale(pill, 0.95)
	pill.Children(func() {
		txt := ui.Text(c, tpl.Name).Font(theme.FontSans).FontSize(11.5).TextColor(fg)
		if active {
			txt.Bold()
		}
	})
	if pill.Clicked() && !a.shareCardBusy {
		a.shareCardOpts.Template = tpl.ID
		a.shareCardStatus = ""
	}
}

func (a *AppUI) renderShareCardTheme(c *ui.Context, pal theme.Palette, th sharecard.CardPalette) {
	active := a.shareCardOpts.Theme == th.ID
	bg, fg, bd := pal.Background, pal.TextSecondary, pal.Border
	if active {
		bg, fg, bd = pal.Surface, pal.TextPrimary, pal.Accent
	}
	chip := ui.Row(c.Key("sc-th-"+string(th.ID))).
		Padding(4, 9, 4, 7).
		Gap(5).
		Radius(999).
		Background(bg).
		Border(1.1, bd).
		AlignItems(ui.Center).
		Cursor(ui.CursorPointer).
		Transition(theme.ColorTransition())
	theme.ApplyPressScale(chip, 0.95)
	chip.Children(func() {
		ui.Box(c.Key("sc-th-dot-"+string(th.ID))).
			Width(11).Height(11).Radius(99).
			Background(th.SwatchDot).
			Border(0.75, pal.Border)
		txt := ui.Text(c, th.Name).Font(theme.FontSans).FontSize(11).TextColor(fg)
		if active {
			txt.Bold()
		}
	})
	if chip.Clicked() && !a.shareCardBusy {
		a.shareCardOpts.Theme = th.ID
		a.shareCardStatus = ""
	}
}

func (a *AppUI) renderShareCardControls(c *ui.Context, pal theme.Palette) {
	// 3. 模板选择器（5 套模板横向滑动胶囊）
	ui.Column(c.Key("sc-controls")).FillWidth().Gap(9).Children(func() {
		ui.ScrollHorizontal(c.Key("sc-tpl-scroll")).FillWidth().Children(func() {
			ui.Row(c.Key("sc-tpl-row")).Gap(6).AlignItems(ui.Center).Children(func() {
				for _, tpl := range sharecard.Templates() {
					tpl := tpl
					a.renderShareCardTemplate(c, pal, tpl)
				}
			})
		})

		// 4. 纸面主题色板选择器 + 水印角标开关
		ui.Row(c.Key("sc-theme-row")).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			ui.ScrollHorizontal(c.Key("sc-theme-scroll")).Grow(1).Children(func() {
				ui.Row(c.Key("sc-theme-pills")).Gap(6).AlignItems(ui.Center).Children(func() {
					for _, th := range sharecard.AllThemes() {
						th := th
						a.renderShareCardTheme(c, pal, th)
					}
				})
			})

			// 极轻品牌角标开关
			wm := a.shareCardOpts.ShowWatermark
			ui.Row(c.Key("sc-wm-toggle")).Gap(5).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "角标").Font(theme.FontSans).FontSize(11).TextColor(pal.TextMuted)
				if mobile.Switch(c.Key("sc-wm-sw"), &wm).Changed() && !a.shareCardBusy {
					a.shareCardOpts.ShowWatermark = wm
				}
			})
		})
	})
}

func (a *AppUI) renderShareCardActions(c *ui.Context, pal theme.Palette) {
	// 5. 底部操作按钮：保存到相册 / 系统分享
	ui.Row(c.Key("sc-actions")).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		if a.shareCardBusy {
			ui.Row(c.Key("sc-busy-row")).FillWidth().Center().Gap(8).Padding(6, 0).Children(func() {
				ui.Spinner(c)
				msg := a.shareCardBusyLabel
				if msg == "" {
					msg = "正在生成 3× 高清图片…"
				}
				ui.Text(c, msg).Font(theme.FontSans).FontSize(12.5).TextColor(pal.TextSecondary)
			})
			return
		}

		saveBtn := ui.Button(c.Key("sc-btn-save"), "保存到相册").Grow(1)
		if saveBtn.Clicked() {
			a.exportShareCardAsync("save")
		}
		shareBtn := ui.PrimaryButton(c.Key("sc-btn-share"), "系统分享").Grow(1)
		if shareBtn.Clicked() {
			a.exportShareCardAsync("share")
		}
	})
}

func (a *AppUI) saveShareCard(res sharecard.ExportResult, savePhotoFn, exportFileFn func(string) (string, error)) {
	var savedInfo string
	var saveErr error
	if savePhotoFn != nil {
		savedInfo, saveErr = savePhotoFn(res.FilePath)
	} else if exportFileFn != nil {
		savedInfo, saveErr = exportFileFn(res.FilePath)
	} else {
		savedInfo = res.FilePath
	}
	a.dispatchUI(func() {
		a.shareCardBusy = false
		a.shareCardBusyLabel = ""
		if saveErr != nil {
			if errors.Is(saveErr, context.Canceled) {
				a.shareCardStatus = ""
				return
			}
			a.shareCardStatus = saveErr.Error()
			return
		}
		msg := fmt.Sprintf("已保存高清卡片（%d×%d）", res.WidthPx, res.HeightPx)
		if savedInfo != "" && savedInfo != res.FilePath {
			msg = fmt.Sprintf("已保存高清卡片（%d×%d）", res.WidthPx, res.HeightPx)
		}
		a.shareCardStatus = ""
		a.showTransientToastOnUI(msg)
	})
}

func (a *AppUI) shareExportedCard(path string, kind sharecard.CardKind, shareImageFn func(string, string) error, exportFileFn func(string) (string, error)) {
	// mode == "share"
	var shareErr error
	if shareImageFn != nil {
		shareErr = shareImageFn(path, "StillPoets · "+kind.Label())
	} else if exportFileFn != nil {
		_, shareErr = exportFileFn(path)
	}
	a.dispatchUI(func() {
		a.shareCardBusy = false
		a.shareCardBusyLabel = ""
		if shareErr != nil {
			if errors.Is(shareErr, context.Canceled) {
				a.shareCardStatus = ""
				return
			}
			a.shareCardStatus = "分享失败：" + shareErr.Error()
			return
		}
		a.shareCardStatus = ""
	})
}
