package views

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"stillpoets/internal/app"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/theme"
)

func (a *AppUI) openChangelogSheet() {
	a.showChangelogModal = true
	a.mu.Lock()
	needLoad := !a.releasesLoaded && !a.releasesLoading
	a.mu.Unlock()
	if needLoad {
		a.loadReleasesAsync(false)
	}
}

// renderChangelogModal 渲染「关于 StillPoets」下的三级底部抽屉，使用 MyGo 原生 Markdown 视图展示完整更新日志。
func (a *AppUI) renderChangelogModal(c *ui.Context, pal theme.Palette) {
	components.AnimatedSheet(c, &a.showChangelogModal, pal, []float32{0.86}, "更新日志", func() {
		a.mu.Lock()
		appVer := a.appVersion
		relLoading := a.releasesLoading
		relList := a.releases
		relErr := a.releasesError
		a.mu.Unlock()

		// 抽屉顶栏：标题 + 刷新 + 关闭
		ui.Row(c.Key("changelog-sheet-header")).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, "更新日志").Font(theme.FontSerif).FontSize(18).Bold().TextColor(pal.TextPrimary)
				ui.Text(c, "GitHub Releases 最新前 5 条版本记录").FontSize(11).TextColor(pal.TextMuted)
			})
			if !relLoading {
				refBtn := ui.Row(c.Key("changelog-sheet-refresh")).
					Padding(5, 10).
					Gap(4).
					Radius(999).
					Background(pal.Background).
					Border(1, pal.Border).
					AlignItems(ui.Center).
					Cursor(ui.CursorPointer)
				theme.ApplyPressScale(refBtn, 0.94)
				refBtn.Children(func() {
					ui.Icon(c, components.IconRefresh).Width(12).Height(12).TextColor(pal.TextSecondary)
					ui.Text(c, "刷新").FontSize(11).TextColor(pal.TextSecondary)
				})
				if refBtn.Clicked() {
					a.loadReleasesAsync(true)
				}
			}
			closeBtn := ui.Box(c.Key("changelog-sheet-close")).Padding(4).Cursor(ui.CursorPointer)
			theme.ApplyPressScale(closeBtn, 0.90)
			closeBtn.Children(func() {
				ui.Icon(c, components.IconClose).Width(16).Height(16).TextColor(pal.TextMuted)
			})
			if closeBtn.Clicked() {
				a.showChangelogModal = false
			}
		})

		a.renderSettingsDivider(c, pal)

		ui.Scroll(c.Key("changelog-sheet-scroll")).Grow(1).Basis(0).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(14).Padding(2, 0, 20, 0).Children(func() {
				switch {
				case relLoading:
					ui.Row(c.Key("rel-loading-row")).FillWidth().Padding(28, 0).Gap(8).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
						ui.Spinner(c.Key("rel-spin")).FontSize(14).TextColor(pal.Accent).Label("正在获取更新日志")
						ui.Text(c, "正在拉取 GitHub Release 更新日志…").FontSize(12).TextColor(pal.TextMuted)
					})
				case relErr != "":
					ui.Column(c).FillWidth().Padding(16).Gap(10).Radius(12).Background(pal.Background).Border(1, pal.Border).Children(func() {
						ui.Text(c, "拉取更新日志失败："+relErr).FontSize(12).LineHeight(1.5).TextColor(pal.TextMuted)
						ui.Row(c).Gap(8).Children(func() {
							if ui.Button(c, "重试").Clicked() {
								a.loadReleasesAsync(true)
							}
							if ui.Button(c, "在浏览器查看 Release").Clicked() {
								a.openExternalURL(app.GitHubReleasesURL)
							}
						})
					})
				case len(relList) == 0:
					ui.Column(c).FillWidth().Padding(20, 12).Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "仓库暂未发布正式 Release 记录。").FontSize(12).TextColor(pal.TextMuted)
						if ui.Button(c, "前往 GitHub Releases 页面").Clicked() {
							a.openExternalURL(app.GitHubReleasesURL)
						}
					})
				default:
					mdOpts := a.changelogMarkdownOptions(pal)
					curTag := formatVersionTag(appVer)
					visible := relList
					hasMore := false
					if len(visible) > maxChangelogSheetItems {
						visible = visible[:maxChangelogSheetItems]
						hasMore = true
					}
					for _, rel := range visible {
						rel := rel
						relTag := formatVersionTag(rel.TagName)
						isCurrent := appVer != "" && strings.EqualFold(relTag, curTag)

						ui.Column(c.Key("rel-card-"+rel.TagName)).
							FillWidth().
							Padding(14, 16).
							Gap(10).
							Radius(12).
							Background(pal.Background).
							Border(1, pal.Border).
							Children(func() {
								// 版本头：Tag + 当前/预发布胶囊 + 发布日期 + GitHub 外链
								ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
									ui.Box(c).Padding(2, 9).Radius(999).Background(pal.HeroBg).Children(func() {
										ui.Text(c, relTag).Font(theme.FontMono).FontSize(11).Bold().TextColor(pal.TextPrimary)
									})
									if isCurrent {
										ui.Box(c).Padding(2, 7).Radius(999).Background(pal.HighlightBg).Children(func() {
											ui.Text(c, "当前版本").FontSize(10).Bold().TextColor(pal.Accent)
										})
									}
									if rel.Prerelease {
										ui.Box(c).Padding(2, 7).Radius(999).Background(pal.HighlightBg).Children(func() {
											ui.Text(c, "预发布").FontSize(10).TextColor(pal.Accent)
										})
									}
									ui.Box(c).Grow(1)
									if !rel.PublishedAt.IsZero() {
										ui.Text(c, rel.PublishedAt.Local().Format("2006-01-02")).
											Font(theme.FontMono).FontSize(11).TextColor(pal.TextMuted)
									}
									linkBtn := ui.Row(c.Key("rel-ext-"+rel.TagName)).
										Padding(3, 7).
										Gap(3).
										Radius(999).
										Background(pal.Surface).
										Border(1, pal.Border).
										AlignItems(ui.Center).
										Cursor(ui.CursorPointer)
									theme.ApplyPressScale(linkBtn, 0.94)
									linkBtn.Children(func() {
										ui.Text(c, "详情").FontSize(10).TextColor(pal.TextSecondary)
										ui.Icon(c, components.IconExternalLink).Width(11).Height(11).TextColor(pal.TextMuted)
									})
									if linkBtn.Clicked() {
										target := rel.HTMLURL
										if target == "" {
											target = app.GitHubReleasesURL
										}
										a.openExternalURL(target)
									}
								})

								if title := rel.DisplayTitle(); title != "" && title != rel.TagName && title != relTag {
									ui.Text(c, title).Font(theme.FontSerif).FontSize(15).Bold().TextColor(pal.TextPrimary)
								}

								mdBody := normalizeReleaseMarkdown(rel)
								if mdBody == "" {
									ui.Text(c, "无额外更新说明，可点击右上角「详情」前往 GitHub Release 查看。").
										FontSize(12).LineHeight(1.55).TextColor(pal.TextMuted)
								} else {
									markdown.View(c.Key("rel-md-"+rel.TagName), mdBody, mdOpts).FillWidth()
								}

								if len(rel.Assets) > 0 {
									a.renderSettingsDivider(c, pal)
									ui.Text(c, "安装包："+formatAssetNames(rel.Assets)).
										Font(theme.FontMono).FontSize(10).LineHeight(1.45).TextColor(pal.TextMuted)
								}
							})
					}

					ui.Column(c.Key("changelog-sheet-more")).
						FillWidth().
						Padding(10, 12, 4, 12).
						Gap(8).
						AlignItems(ui.Center).
						Children(func() {
							if hasMore {
								ui.Textf(c, "抽屉仅展示最新 %d 条更新记录，更多历史版本请前往 GitHub 查看", maxChangelogSheetItems).
									FontSize(11).LineHeight(1.5).TextColor(pal.TextMuted)
							}
							btnLabel := "前往 GitHub Releases 查看全部 ↗"
							if hasMore {
								btnLabel = "前往 GitHub 查看更多历史更新 ↗"
							}
							if ui.Button(c.Key("changelog-more-link"), btnLabel).Clicked() {
								a.openExternalURL(app.GitHubReleasesURL)
							}
						})
				}
			})
		})
	})
}

func (a *AppUI) changelogMarkdownOptions(pal theme.Palette) *markdown.Options {
	if a.changelogMDOpts != nil && a.changelogMDTheme == pal.ID {
		return a.changelogMDOpts
	}
	mdTheme := &markdown.Theme{
		FontSize:            13,
		Font:                theme.FontSans,
		CodeFont:            theme.FontMono,
		LineHeight:          1.55,
		CodeLineHeight:      1.45,
		Text:                pal.TextSecondary,
		Muted:               pal.TextMuted,
		Link:                pal.Accent,
		Strong:              pal.TextPrimary,
		StrongWeight:        600,
		Code:                pal.TextPrimary,
		CodeBackground:      pal.Surface,
		CodeSize:            0.88,
		BlockSpacing:        0.55,
		ItemSpacing:         0.28,
		ListIndent:          1.35,
		QuoteBar:            pal.Accent,
		QuoteText:           pal.TextMuted,
		QuoteBarWidth:       3,
		Rule:                pal.Border,
		CodeBlockBackground: pal.Surface,
		CodeBlockBorder:     pal.Border,
		CodeBlockRadius:     8,
		CodeBlockPadding:    0.7,
		TableBorder:         pal.Border,
		TableHeader:         pal.Surface,
		Headings: [6]markdown.HeadingStyle{
			{Size: 1.18, Weight: 700, Color: pal.TextPrimary, Top: 0.65, Bottom: 0.30},
			{Size: 1.10, Weight: 700, Color: pal.TextPrimary, Top: 0.60, Bottom: 0.28},
			{Size: 1.03, Weight: 600, Color: pal.TextPrimary, Top: 0.55, Bottom: 0.25},
			{Size: 1.00, Weight: 600, Color: pal.TextPrimary, Top: 0.45, Bottom: 0.20},
			{Size: 0.96, Weight: 600, Color: pal.TextSecondary, Top: 0.40, Bottom: 0.18},
			{Size: 0.92, Weight: 600, Color: pal.TextMuted, Top: 0.35, Bottom: 0.15},
		},
	}
	if a.changelogMDOpts == nil {
		a.changelogMDOpts = &markdown.Options{
			Theme:  mdTheme,
			OnLink: func(rawURL string) { a.openExternalURL(rawURL) },
		}
	} else {
		a.changelogMDOpts.Theme = mdTheme
	}
	a.changelogMDTheme = pal.ID
	return a.changelogMDOpts
}

// normalizeReleaseMarkdown 规范化换行并去除首行与卡片标题/Tag 重复的 Markdown 标题（如 `## StillPoets v0.1.2`）。
func normalizeReleaseMarkdown(rel app.ReleaseInfo) string {
	body := strings.TrimSpace(strings.ReplaceAll(rel.Body, "\r\n", "\n"))
	if body == "" {
		return ""
	}
	lines := strings.Split(body, "\n")
	first := strings.TrimSpace(lines[0])
	if strings.HasPrefix(first, "#") {
		heading := strings.TrimSpace(strings.TrimLeft(first, "#"))
		displayTitle := strings.TrimSpace(rel.DisplayTitle())
		tag := formatVersionTag(rel.TagName)
		if strings.EqualFold(heading, displayTitle) ||
			strings.EqualFold(heading, rel.TagName) ||
			strings.EqualFold(heading, tag) {
			rest := strings.TrimSpace(strings.Join(lines[1:], "\n"))
			if rest != "" {
				return rest
			}
		}
	}
	return body
}

// --- 关于页异步动作 ---
