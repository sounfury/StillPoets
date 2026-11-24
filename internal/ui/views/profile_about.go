package views

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"stillpoets/internal/app"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/layout"
	"stillpoets/internal/ui/theme"
)

// --- 二级页 4：关于 StillPoets（更新日志 / 检查更新 / 邮件联系 / 分析日志导出） ---

func (a *AppUI) renderAboutSubpage(c *ui.Context, pal theme.Palette, isTablet bool) {
	a.mu.Lock()
	appVer := a.appVersion
	updChecking := a.updateChecking
	updRes := a.updateResult
	updErr := a.updateError
	relLoading := a.releasesLoading
	relList := a.releases
	relErr := a.releasesError
	expLog := a.exportingLog
	expStatus := a.exportLogStatus
	diagSize := a.diagLogSizeBytes
	a.mu.Unlock()

	if appVer == "" {
		appVer = app.DefaultAppVersion
	}

	ui.Column(c.Key("subpage-about")).Fill().Children(func() {
		a.renderProfileSubpageHeader(c, pal, isTablet, "关于 StillPoets", func() {
			ghBtn := ui.Row(c.Key("about-gh-btn")).
				Padding(5, 10).
				Gap(5).
				Radius(999).
				Background(pal.Surface).
				Border(1, pal.Border).
				AlignItems(ui.Center).
				Cursor(ui.CursorPointer)
			theme.ApplyPressScale(ghBtn, 0.95)
			ghBtn.Children(func() {
				ui.Text(c, "Releases").FontSize(11).TextColor(pal.TextSecondary)
				ui.Icon(c, components.IconExternalLink).Width(12).Height(12).TextColor(pal.TextMuted)
			})
			if ghBtn.Clicked() {
				a.openExternalURL(app.GitHubReleasesURL)
			}
		})

		ui.Scroll(c.Key("subpage-about-scroll")).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(16, 20, 48, 20).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(layout.ReadingMaxWidthDIP + 80).Gap(18).Children(func() {
					// 1. 品牌与当前版本卡
					ui.Column(c.Key("about-brand-card")).
						FillWidth().
						Padding(20).
						Gap(8).
						Radius(12).
						Background(pal.Surface).
						Border(1, pal.Border).
						Children(func() {
							ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
								ui.Text(c, "StillPoets").Font(theme.FontSerif).FontSize(22).Bold().TextColor(pal.TextPrimary)
								ui.Box(c).Padding(2, 9).Radius(999).Background(pal.HeroBg).Children(func() {
									ui.Text(c, formatVersionTag(appVer)).Font(theme.FontMono).FontSize(11).Bold().TextColor(pal.TextPrimary)
								})
							})
							ui.Text(c, "私有化多端现代诗阅读、独立散页与诗笺小组件。一切藏书与笔记归属于你的设备与网盘。").
								FontSize(12).LineHeight(1.55).TextColor(pal.TextMuted)
						})

					// 2. 版本与检查更新卡
					ui.Column(c.Key("about-update-group")).FillWidth().Gap(8).Children(func() {
						a.renderSettingsGroupLabel(c, pal, "版本更新")
						ui.Column(c.Key("about-update-card")).
							FillWidth().
							Padding(16, 18).
							Gap(12).
							Radius(12).
							Background(pal.Surface).
							Border(1, pal.Border).
							Children(func() {
								ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
									ui.Column(c).Grow(1).Gap(2).Children(func() {
										ui.Text(c, "检查更新").FontSize(14).Bold().TextColor(pal.TextPrimary)
										ui.Text(c, "仅检测 GitHub Release 是否有新包发布，不自动下载安装").
											FontSize(11).TextColor(pal.TextMuted)
									})
									chkBtn := ui.Row(c.Key("about-check-update-btn")).
										Padding(7, 13).
										Gap(6).
										Radius(999).
										AlignItems(ui.Center).
										Background(pal.Accent).
										Cursor(ui.CursorPointer).
										Disabled(updChecking)
									theme.ApplyPressScale(chkBtn, 0.95)
									chkBtn.Children(func() {
										if updChecking {
											ui.Spinner(c.Key("upd-spin")).FontSize(13).TextColor(pal.AccentText).Label("检查中")
											ui.Text(c, "检查中…").FontSize(12).Bold().TextColor(pal.AccentText)
										} else {
											ui.Icon(c, components.IconRefresh).Width(13).Height(13).TextColor(pal.AccentText)
											ui.Text(c, "检查更新").FontSize(12).Bold().TextColor(pal.AccentText)
										}
									})
									if chkBtn.Clicked() && !updChecking {
										a.checkAppUpdateAsync()
									}
								})

								switch {
								case updChecking:
									ui.Row(c.Key("upd-loading-bar")).
										FillWidth().
										Padding(9, 12).
										Gap(8).
										Radius(8).
										Background(pal.Background).
										Border(1, pal.Border).
										AlignItems(ui.Center).
										Children(func() {
											ui.Spinner(c.Key("upd-bar-spin")).FontSize(13).TextColor(pal.Accent).Label("正在检查最新版本")
											ui.Text(c, "正在查询 GitHub Releases 最新版本…").FontSize(12).TextColor(pal.TextSecondary)
										})
								case updErr != "":
									ui.Text(c, "检查失败："+updErr).FontSize(12).LineHeight(1.45).TextColor(pal.Accent)
								case updRes != nil && updRes.NoRelease:
									ui.Text(c, "仓库暂未发布正式 Release 安装包。").FontSize(12).TextColor(pal.TextMuted)
								case updRes != nil && updRes.Latest != nil && updRes.HasUpdate:
									latest := updRes.Latest
									ui.Column(c.Key("upd-found-box")).
										FillWidth().
										Padding(12, 14).
										Gap(8).
										Radius(10).
										Background(pal.Background).
										Border(1, pal.Accent).
										Children(func() {
											ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
												ui.Textf(c, "发现新版本 %s", formatVersionTag(latest.TagName)).
													Font(theme.FontSerif).FontSize(14).Bold().TextColor(pal.Accent).Grow(1)
												if !latest.PublishedAt.IsZero() {
													ui.Text(c, latest.PublishedAt.Local().Format("2006-01-02")).
														Font(theme.FontMono).FontSize(11).TextColor(pal.TextMuted)
												}
											})
											if mdBody := normalizeReleaseMarkdown(*latest); mdBody != "" {
												markdown.View(c.Key("upd-found-md-"+latest.TagName), mdBody, a.changelogMarkdownOptions(pal)).
													FillWidth()
											}
											if len(latest.Assets) > 0 {
												ui.Text(c, "包含安装包："+formatAssetNames(latest.Assets)).
													Font(theme.FontMono).FontSize(11).TextColor(pal.TextMuted).MaxLines(2)
											}
											ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
												if ui.Button(c, "查看更新日志").Clicked() {
													a.openChangelogSheet()
												}
												targetURL := latest.HTMLURL
												if targetURL == "" {
													targetURL = app.GitHubReleasesURL
												}
												if ui.PrimaryButton(c, "前往 Release 下载").Clicked() {
													a.openExternalURL(targetURL)
												}
											})
										})
								case updRes != nil && updRes.Latest != nil:
									ui.Row(c.Key("upd-latest-row")).
										FillWidth().
										Padding(9, 12).
										Gap(8).
										Radius(8).
										Background(pal.Background).
										AlignItems(ui.Center).
										Children(func() {
											ui.Icon(c, components.IconCheck).Width(14).Height(14).TextColor(pal.Accent)
											ui.Textf(c, "当前已是最新版本（%s，远端最新 %s）",
												formatVersionTag(updRes.CurrentVersion),
												formatVersionTag(updRes.Latest.TagName)).
												FontSize(12).TextColor(pal.TextSecondary).Grow(1)
										})
								}

								a.renderSettingsDivider(c, pal)

								// 三级入口：更新日志底部抽屉（Markdown 渲染）
								changelogSub := "查看 GitHub Releases 历次版本更新说明"
								changelogValue := "查看"
								switch {
								case relLoading:
									changelogSub = "正在获取 GitHub Releases 更新记录…"
									changelogValue = "加载中"
								case relErr != "":
									changelogSub = "上次获取失败，点击进入后可重试"
									changelogValue = "重试"
								case len(relList) > 0:
									latestRel := relList[0]
									if !latestRel.PublishedAt.IsZero() {
										changelogSub = fmt.Sprintf("最新 %s · 发布于 %s",
											formatVersionTag(latestRel.TagName),
											latestRel.PublishedAt.Local().Format("2006-01-02"))
									} else {
										changelogSub = fmt.Sprintf("最新 %s · 共 %d 个版本记录",
											formatVersionTag(latestRel.TagName), len(relList))
									}
									changelogValue = fmt.Sprintf("共 %d 版", len(relList))
								}
								a.renderSettingsNavRow(
									c, pal,
									"about-changelog-entry",
									"更新日志",
									changelogSub,
									changelogValue,
									"",
									func() { a.openChangelogSheet() },
								)
							})
					})

					// 3. 联系与诊断卡（邮件联系 + 导出分析日志）
					ui.Column(c.Key("about-support-group")).FillWidth().Gap(8).Children(func() {
						a.renderSettingsGroupLabel(c, pal, "反馈与诊断")
						ui.Column(c.Key("about-support-card")).
							FillWidth().
							Padding(6, 18).
							Gap(0).
							Radius(12).
							Background(pal.Surface).
							Border(1, pal.Border).
							Children(func() {
								// 邮件联系
								ui.Row(c.Key("about-email-row")).
									FillWidth().
									Padding(12, 2).
									Gap(8).
									AlignItems(ui.Center).
									Children(func() {
										ui.Icon(c, components.IconMail).Width(18).Height(18).TextColor(pal.TextSecondary)
										ui.Column(c).Grow(1).Gap(2).Children(func() {
											ui.Text(c, "邮件联系").FontSize(14).TextColor(pal.TextPrimary)
											ui.Text(c, app.ContactEmail).Font(theme.FontMono).FontSize(11).TextColor(pal.TextMuted).MaxLines(1)
										})
										copyBtn := ui.Box(c.Key("about-email-copy")).
											Padding(6, 10).
											Radius(999).
											Background(pal.Background).
											Border(1, pal.Border).
											Cursor(ui.CursorPointer)
										theme.ApplyPressScale(copyBtn, 0.95)
										copyBtn.Children(func() {
											ui.Text(c, "复制").FontSize(11).TextColor(pal.TextSecondary)
										})
										if copyBtn.Clicked() {
											a.copyContactEmail()
										}
										mailBtn := ui.Box(c.Key("about-email-send")).
											Padding(6, 10).
											Radius(999).
											Background(pal.Background).
											Border(1, pal.Accent).
											Cursor(ui.CursorPointer)
										theme.ApplyPressScale(mailBtn, 0.95)
										mailBtn.Children(func() {
											ui.Text(c, "发邮件").FontSize(11).Bold().TextColor(pal.Accent)
										})
										if mailBtn.Clicked() {
											a.openContactEmail(appVer)
										}
									})

								a.renderSettingsDivider(c, pal)

								// 分析日志导出
								ui.Column(c.Key("about-diag-wrap")).FillWidth().Padding(12, 2).Gap(10).Children(func() {
									ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
										ui.Icon(c, components.IconFileText).Width(18).Height(18).TextColor(pal.TextSecondary)
										ui.Column(c).Grow(1).Gap(2).Children(func() {
											ui.Text(c, "分析日志").FontSize(14).TextColor(pal.TextPrimary)
											ui.Textf(c, "导出脱敏运行与同步诊断日志（约 %s），不含 WebDAV 账号密码",
												formatBytesSize(diagSize)).
												FontSize(11).LineHeight(1.45).TextColor(pal.TextMuted).MaxLines(2)
										})
										expBtn := ui.Row(c.Key("about-export-log-btn")).
											Padding(7, 13).
											Gap(6).
											Radius(999).
											AlignItems(ui.Center).
											Background(pal.Background).
											Border(1, pal.Accent).
											Cursor(ui.CursorPointer).
											Disabled(expLog)
										theme.ApplyPressScale(expBtn, 0.95)
										expBtn.Children(func() {
											if expLog {
												ui.Spinner(c.Key("exp-log-spin")).FontSize(13).TextColor(pal.Accent).Label("正在导出日志")
												ui.Text(c, "导出中…").FontSize(12).Bold().TextColor(pal.Accent)
											} else {
												ui.Icon(c, components.IconDownload).Width(13).Height(13).TextColor(pal.Accent)
												ui.Text(c, "导出日志").FontSize(12).Bold().TextColor(pal.Accent)
											}
										})
										if expBtn.Clicked() && !expLog {
											a.exportDiagnosticLogAsync()
										}
									})

									if expStatus != "" {
										ui.Box(c.Key("about-export-status")).
											FillWidth().
											Padding(8, 12).
											Radius(8).
											Background(pal.Background).
											Border(1, pal.Border).
											Children(func() {
												ui.Text(c, expStatus).FontSize(11).LineHeight(1.45).TextColor(pal.TextSecondary)
											})
									}
								})
							})
					})

				})
			})
		})
	})
}

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
	components.AnimatedSheet(c, &a.showChangelogModal, pal, []float32{0.72, 0.92}, "更新日志", func() {
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
				ui.Text(c, "GitHub Releases 历次版本记录").FontSize(11).TextColor(pal.TextMuted)
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

		ui.Scroll(c.Key("changelog-sheet-scroll")).Grow(1).FillWidth().Children(func() {
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
					for _, rel := range relList {
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

					ui.Row(c).FillWidth().Justify(ui.Center).Padding(4, 0, 0, 0).Children(func() {
						if ui.Button(c, "在浏览器查看全部 Release").Clicked() {
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

func (a *AppUI) checkAppUpdateAsync() {
	a.mu.Lock()
	if a.updateChecking {
		a.mu.Unlock()
		return
	}
	a.updateChecking = true
	a.updateError = ""
	ver := a.appVersion
	a.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		res, err := a.svc.CheckLatestRelease(ctx, ver)
		a.dispatchUI(func() {
			a.mu.Lock()
			a.updateChecking = false
			if err != nil {
				a.updateError = err.Error()
			} else {
				cp := res
				a.updateResult = &cp
				a.updateError = ""
			}
			a.mu.Unlock()
		})
		if err != nil {
			a.showTransientToast("检查更新失败")
		} else if res.HasUpdate && res.Latest != nil {
			a.showTransientToast("发现新版本 " + formatVersionTag(res.Latest.TagName))
		} else if res.NoRelease {
			a.showTransientToast("仓库暂未发布正式版本")
		} else {
			a.showTransientToast("当前已是最新版本")
		}
	}()
}

func (a *AppUI) loadReleasesAsync(force bool) {
	a.mu.Lock()
	if a.releasesLoading || (!force && a.releasesLoaded) {
		a.mu.Unlock()
		return
	}
	a.releasesLoading = true
	a.releasesError = ""
	ver := a.appVersion
	a.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		list, err := a.svc.FetchReleases(ctx, ver, 10)
		a.dispatchUI(func() {
			a.mu.Lock()
			a.releasesLoading = false
			a.releasesLoaded = true
			if err != nil {
				a.releasesError = err.Error()
			} else {
				a.releases = list
				a.releasesError = ""
			}
			a.mu.Unlock()
		})
	}()
}

func (a *AppUI) exportDiagnosticLogAsync() {
	a.mu.Lock()
	if a.exportingLog {
		a.mu.Unlock()
		return
	}
	a.exportingLog = true
	a.exportLogStatus = ""
	ver := a.appVersion
	exportFn := a.exportFileFn
	a.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		outPath, sizeBytes, err := a.svc.ExportDiagnosticLog(ctx, ver)
		cancel()

		if err != nil {
			a.dispatchUI(func() {
				a.mu.Lock()
				a.exportingLog = false
				a.exportLogStatus = "生成诊断日志失败：" + err.Error()
				a.mu.Unlock()
			})
			a.showTransientToast("导出分析日志失败")
			return
		}

		statusMsg := fmt.Sprintf("已生成分析日志 %s（%s）", filepath.Base(outPath), formatBytesSize(sizeBytes))
		toastMsg := "已导出分析日志"
		if exportFn != nil {
			exportedTo, expErr := exportFn(outPath)
			switch {
			case expErr != nil:
				statusMsg = fmt.Sprintf("日志已保存于 %s（%s），调起系统分享/保存失败：%v", outPath, formatBytesSize(sizeBytes), expErr)
				toastMsg = "日志已生成，但系统分享未完成"
			case exportedTo == "":
				statusMsg = fmt.Sprintf("已取消另存为；本机副本保留于 %s（%s）", filepath.Base(outPath), formatBytesSize(sizeBytes))
				toastMsg = "已取消导出"
			default:
				statusMsg = fmt.Sprintf("已导出 %s（%s）", filepath.Base(outPath), formatBytesSize(sizeBytes))
			}
		} else {
			statusMsg = fmt.Sprintf("已生成分析日志：%s（%s）", outPath, formatBytesSize(sizeBytes))
		}

		diagSize := a.svc.DiagnosticLogSizeBytes()
		a.dispatchUI(func() {
			a.mu.Lock()
			a.exportingLog = false
			a.exportLogStatus = statusMsg
			a.diagLogSizeBytes = diagSize
			a.mu.Unlock()
		})
		a.showTransientToast(toastMsg)
	}()
}

func (a *AppUI) openExternalURL(rawURL string) {
	a.mu.Lock()
	fn := a.openURLFn
	a.mu.Unlock()
	if fn == nil {
		a.showTransientToastOnUI("当前环境不支持打开外部链接")
		return
	}
	go func() {
		if err := fn(rawURL); err != nil {
			a.showTransientToast("无法打开链接：" + err.Error())
		}
	}()
}

func (a *AppUI) openContactEmail(appVer string) {
	subject := fmt.Sprintf("StillPoets 反馈 (%s)", formatVersionTag(appVer))
	mailto := fmt.Sprintf("mailto:%s?subject=%s", app.ContactEmail, url.QueryEscape(subject))
	a.mu.Lock()
	openFn := a.openURLFn
	copyFn := a.copyTextFn
	a.mu.Unlock()
	if openFn == nil {
		if copyFn != nil {
			copyFn(app.ContactEmail)
			a.showTransientToastOnUI("已复制联系邮箱：" + app.ContactEmail)
			return
		}
		a.showTransientToastOnUI("联系邮箱：" + app.ContactEmail)
		return
	}
	go func() {
		if err := openFn(mailto); err != nil {
			if copyFn != nil {
				copyFn(app.ContactEmail)
				a.showTransientToast("未检测到邮件客户端，已复制邮箱：" + app.ContactEmail)
				return
			}
			a.showTransientToast("无法打开邮件客户端：" + err.Error())
		}
	}()
}

func (a *AppUI) copyContactEmail() {
	a.mu.Lock()
	copyFn := a.copyTextFn
	a.mu.Unlock()
	if copyFn != nil {
		copyFn(app.ContactEmail)
	}
	a.showTransientToastOnUI("已复制联系邮箱：" + app.ContactEmail)
}

func formatAssetNames(assets []app.ReleaseAsset) string {
	names := make([]string, 0, len(assets))
	for _, a := range assets {
		if strings.TrimSpace(a.Name) == "" {
			continue
		}
		if a.Size > 0 {
			names = append(names, fmt.Sprintf("%s (%s)", a.Name, formatBytesSize(a.Size)))
		} else {
			names = append(names, a.Name)
		}
	}
	return strings.Join(names, " · ")
}
