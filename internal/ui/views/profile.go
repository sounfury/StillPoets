package views

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/mobile"

	"stillpoets/internal/app"
	"stillpoets/internal/domain"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/layout"
	"stillpoets/internal/ui/theme"
	"stillpoets/modules/widgetbridge"
)

func (a *AppUI) renderProfilePage(c *ui.Context, pal theme.Palette, isTablet bool) {
	renderMain := func() {
		ui.Column(c.Key("profile-page")).Fill().Children(func() {
			components.BrandHeader(c, pal, "我的", false, nil, nil)

			bottomPad := float32(96)
			if !isTablet && a.useFloatingBottomNav() {
				bottomPad = 112
			}
			ui.Scroll(c.Key("profile-scroll")).Grow(1).FillWidth().Children(func() {
				ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(10, 20, bottomPad, 20).Children(func() {
					ui.Column(c).FillWidth().MaxWidth(layout.ReadingMaxWidthDIP + 80).Gap(18).Children(func() {
						a.renderCloudSyncSummarySection(c, pal)
						a.renderAppearanceSummarySection(c, pal, isTablet)
						a.renderTypographySummarySection(c, pal)
						a.renderWidgetSummarySection(c, pal)
						a.renderStorageAndAboutSection(c, pal)
					})
				})
			})
		})
	}

	if !isTablet {
		renderMain()
		return
	}

	// 平板端保留左侧导航轨，在主内容区内以右侧推入转场叠放二级设置页
	ui.Box(c.Key("profile-tablet-stage")).Fill().Clip().Children(func() {
		renderMain()
		if a.profileSub != profileSubpageNone {
			a.renderProfileSubpageLayer(c, pal, true)
		}
	})
}

func (a *AppUI) openProfileSubpage(sub profileSubpage) {
	a.profileSub = sub
	a.confirmDisconnect = false
	a.confirmDeleteFont = ""
	if sub == profileSubpageAbout && !a.releasesLoaded && !a.releasesLoading {
		a.loadReleasesAsync(false)
	}
}

func (a *AppUI) closeProfileSubpage() {
	a.profileSub = profileSubpageNone
	a.confirmDisconnect = false
	a.confirmDeleteFont = ""
}

func (a *AppUI) renderProfileSubpageLayer(c *ui.Context, pal theme.Palette, isTablet bool) {
	sub := a.profileSub
	layer := ui.Box(c.Key(fmt.Sprintf("profile-sub-%d", sub))).
		Absolute().
		Fill().
		Background(pal.Background).
		Transition(theme.SubpagePushTransition())
	if grain := pal.PaperGrainOpacity(); grain > 0 {
		isDark := pal.IsDark()
		layer.Draw(func(p *ui.Painter, r ui.Rect) {
			theme.DrawPaperTexture(p, r, 0, isDark, grain)
		})
	}
	layer.Children(func() {
		switch sub {
		case profileSubpageWebDAV:
			a.renderWebDAVSubpage(c, pal, isTablet)
		case profileSubpageFonts:
			a.renderFontsSubpage(c, pal, isTablet)
		case profileSubpageWidget:
			a.renderWidgetSubpage(c, pal, isTablet)
		case profileSubpageAbout:
			a.renderAboutSubpage(c, pal, isTablet)
		}
	})
}

func (a *AppUI) renderProfileSubpageHeader(c *ui.Context, pal theme.Palette, isTablet bool, title string, rightAction func()) {
	safeTop, _, _, _ := c.SafeArea()
	topPad := float32(14)
	if !isTablet {
		topPad = max(safeTop, 8) + 8
	}
	ui.Row(c.Key("sub-header-"+title)).
		FillWidth().
		Padding(topPad, 16, 12, 12).
		Gap(10).
		AlignItems(ui.Center).
		Background(pal.Background).
		BorderColor(pal.Border).
		BorderWidth(0, 0, 1, 0).
		Children(func() {
			backBtn := ui.Row(c.Key("sub-back-btn")).
				Padding(6, 10, 6, 4).
				Gap(2).
				Radius(8).
				AlignItems(ui.Center).
				Cursor(ui.CursorPointer)
			theme.ApplyPressScale(backBtn, 0.94)
			backBtn.Children(func() {
				ui.Icon(c, components.IconBack).Width(18).Height(18).TextColor(pal.TextPrimary)
				ui.Text(c, "返回").Font(theme.FontSans).FontSize(13).TextColor(pal.TextSecondary)
			})
			if backBtn.Clicked() {
				a.closeProfileSubpage()
			}

			ui.Text(c, title).
				Font(theme.FontSerif).
				FontSize(17).
				Bold().
				TextColor(pal.TextPrimary).
				MaxLines(1).
				Grow(1)

			if rightAction != nil {
				rightAction()
			}
		})
}

func (a *AppUI) renderSettingsGroupLabel(c *ui.Context, pal theme.Palette, label string) {
	ui.Text(c, label).
		Font(theme.FontSans).
		FontSize(12).
		Bold().
		TextColor(pal.TextMuted).
		Padding(0, 0, 0, 4)
}

func (a *AppUI) renderSettingsDivider(c *ui.Context, pal theme.Palette) {
	ui.Box(c).FillWidth().Height(1).Background(pal.Border)
}

func (a *AppUI) renderSettingsNavRow(
	c *ui.Context,
	pal theme.Palette,
	key string,
	title string,
	subtitle string,
	value string,
	badge string,
	onClick func(),
) {
	row := ui.Row(c.Key(key)).
		FillWidth().
		Padding(12, 2).
		Gap(10).
		AlignItems(ui.Center).
		Cursor(ui.CursorPointer)
	theme.ApplyPressScale(row, 0.99)
	row.Children(func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, title).Font(theme.FontSans).FontSize(14).TextColor(pal.TextPrimary).MaxLines(1)
			if subtitle != "" {
				ui.Text(c, subtitle).Font(theme.FontSans).FontSize(11).TextColor(pal.TextMuted).MaxLines(1)
			}
		})
		if badge != "" {
			ui.Box(c).Padding(2, 7).Radius(999).Background(pal.HighlightBg).Border(1, pal.Accent).Children(func() {
				ui.Text(c, badge).Font(theme.FontSans).FontSize(10).Bold().TextColor(pal.Accent)
			})
		}
		if value != "" {
			ui.Text(c, value).Font(theme.FontSans).FontSize(12).TextColor(pal.TextMuted).MaxLines(1)
		}
		ui.Icon(c, components.IconChevronRight).Width(15).Height(15).TextColor(pal.TextMuted)
	})
	if row.Clicked() && onClick != nil {
		onClick()
	}
}

func (a *AppUI) syncActivityState() (busy bool, stage string) {
	a.mu.Lock()
	syncInFlight := a.syncInFlight
	stageMsg := a.syncStageMsg
	davBusy := a.davBusy
	davStatus := a.davStatus
	a.mu.Unlock()
	if syncInFlight {
		if stageMsg == "" {
			stageMsg = "正在同步…"
		}
		return true, stageMsg
	}
	if davBusy {
		if davStatus == "" {
			davStatus = "正在处理 WebDAV 请求…"
		}
		return true, davStatus
	}
	return false, ""
}

// --- 一级设置主页 5 大分组 ---

func (a *AppUI) renderCloudSyncSummarySection(c *ui.Context, pal theme.Palette) {
	syncing, stageMsg := a.syncActivityState()
	ov := a.syncOverview

	ui.Column(c.Key("sec-cloud-group")).FillWidth().Gap(8).Children(func() {
		a.renderSettingsGroupLabel(c, pal, "云端同步")

		ui.Column(c.Key("sec-cloud-card")).
			FillWidth().
			Padding(16, 18, 8, 18).
			Gap(12).
			Radius(12).
			Background(pal.Surface).
			Border(1, pal.Border).
			Children(func() {
				// 顶部概览 + 立即同步（带旋转等待态）
				ui.Row(c.Key("cloud-top-row")).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Column(c).Grow(1).Gap(4).Children(func() {
						ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
							ui.Text(c, "WebDAV 多端同步").Font(theme.FontSerif).FontSize(16).Bold().TextColor(pal.TextPrimary)
							switch {
							case syncing:
								ui.Box(c).Padding(2, 8).Radius(999).Background(pal.HighlightBg).Children(func() {
									ui.Text(c, "同步中").FontSize(10).Bold().TextColor(pal.Accent)
								})
							case ov.Configured && ov.PendingCount > 0:
								ui.Box(c).Padding(2, 8).Radius(999).Background(pal.HighlightBg).Children(func() {
									ui.Textf(c, "待上传 %d 本", ov.PendingCount).FontSize(10).Bold().TextColor(pal.Accent)
								})
							case ov.Configured:
								ui.Box(c).Padding(2, 8).Radius(999).Background(pal.HeroBg).Children(func() {
									ui.Text(c, "已连接").FontSize(10).Bold().TextColor(pal.TextPrimary)
								})
							default:
								ui.Box(c).Padding(2, 8).Radius(999).Background(pal.Background).Border(1, pal.Border).Children(func() {
									ui.Text(c, "未配置").FontSize(10).TextColor(pal.TextMuted)
								})
							}
						})

						subText := "支持坚果云、Nextcloud、群晖 NAS 与 AList"
						if ov.Configured {
							last := "尚未同步"
							if ov.LastSyncAt > 0 {
								last = "上次同步 " + formatSyncTimeShort(ov.LastSyncAt)
							}
							subText = fmt.Sprintf("%s · %d 台设备", last, len(ov.RemoteDevices)+1)
						}
						ui.Text(c, subText).Font(theme.FontSans).FontSize(12).TextColor(pal.TextMuted).MaxLines(1)
					})

					if ov.Configured {
						syncBtn := ui.Row(c.Key("cloud-sync-pill")).
							Padding(7, 13).
							Gap(6).
							Radius(999).
							AlignItems(ui.Center).
							Background(pal.Accent).
							Cursor(ui.CursorPointer).
							Disabled(syncing)
						theme.ApplyPressScale(syncBtn, 0.95)
						syncBtn.Children(func() {
							if syncing {
								ui.Spinner(c.Key("cloud-btn-spin")).FontSize(13).TextColor(pal.AccentText).Label("同步中")
								ui.Text(c, "同步中").Font(theme.FontSans).FontSize(12).Bold().TextColor(pal.AccentText)
							} else {
								ui.Icon(c, components.IconRefresh).Width(13).Height(13).TextColor(pal.AccentText)
								ui.Text(c, "立即同步").Font(theme.FontSans).FontSize(12).Bold().TextColor(pal.AccentText)
							}
						})
						if syncBtn.Clicked() && !syncing {
							a.triggerManualSync()
						}
					} else {
						cfgBtn := ui.Row(c.Key("cloud-setup-pill")).
							Padding(7, 13).
							Radius(999).
							AlignItems(ui.Center).
							Background(pal.Accent).
							Cursor(ui.CursorPointer)
						theme.ApplyPressScale(cfgBtn, 0.95)
						cfgBtn.Children(func() {
							ui.Text(c, "立即配置").Font(theme.FontSans).FontSize(12).Bold().TextColor(pal.AccentText)
						})
						if cfgBtn.Clicked() {
							a.openProfileSubpage(profileSubpageWebDAV)
						}
					}
				})

				// 同步进行中展开内联阶段进度条（带原生旋转等待动画）
				if syncing {
					ui.Row(c.Key("cloud-sync-progress")).
						FillWidth().
						Padding(9, 12).
						Gap(8).
						Radius(8).
						Background(pal.Background).
						Border(1, pal.Border).
						AlignItems(ui.Center).
						Children(func() {
							ui.Spinner(c.Key("cloud-bar-spin")).FontSize(14).TextColor(pal.Accent).Label(stageMsg)
							ui.Text(c, stageMsg).Font(theme.FontSans).FontSize(12).TextColor(pal.TextSecondary).Grow(1).MaxLines(1)
						})
				}

				a.renderSettingsDivider(c, pal)

				summary := "未配置"
				if ov.Configured {
					host := extractURLHost(a.davURL)
					if host != "" {
						summary = fmt.Sprintf("%s · rev %d", host, ov.LocalRev)
					} else {
						summary = fmt.Sprintf("rev %d", ov.LocalRev)
					}
				}
				a.renderSettingsNavRow(
					c, pal,
					"nav-webdav-sub",
					"服务器与设备",
					"",
					summary,
					"",
					func() { a.openProfileSubpage(profileSubpageWebDAV) },
				)
			})
	})
}

func (a *AppUI) renderAppearanceSummarySection(c *ui.Context, pal theme.Palette, isTablet bool) {
	ui.Column(c.Key("sec-appearance-group")).FillWidth().Gap(8).Children(func() {
		a.renderSettingsGroupLabel(c, pal, "外观与界面")

		ui.Column(c.Key("sec-appearance-card")).
			FillWidth().
			Padding(16, 18, 6, 18).
			Gap(0).
			Radius(12).
			Background(pal.Surface).
			Border(1, pal.Border).
			Children(func() {
				// 3 套内置主题视觉色卡
				ui.Row(c.Key("theme-presets")).FillWidth().Gap(10).Padding(0, 0, 14, 0).Children(func() {
					for _, preset := range theme.Presets() {
						active := pal.ID == preset.ID
						card := ui.Column(c.Key("preset-" + string(preset.ID))).
							Grow(1).
							Padding(10).
							Gap(8).
							Radius(10).
							Background(preset.Background).
							Cursor(ui.CursorPointer).
							Transition(theme.ColorTransition())
						theme.ApplyPressScale(card, 0.96)
						if active {
							card.Border(2, pal.Accent)
						} else {
							card.Border(1, preset.Border)
						}
						card.Children(func() {
							// 迷你画册双色色块条
							ui.Row(c).FillWidth().Height(26).Radius(6).Clip().Children(func() {
								ui.Box(c).Grow(2).FillHeight().Background(preset.HeroBg)
								ui.Box(c).Grow(1).FillHeight().Background(preset.HeroCorner)
							})
							ui.Column(c).Gap(2).Children(func() {
								ui.Text(c, preset.Name).Font(theme.FontSerif).FontSize(13).Bold().TextColor(preset.TextPrimary)
								ui.Text(c, preset.Subtitle).FontSize(10).TextColor(preset.TextMuted).MaxLines(1)
							})
						})
						if card.Clicked() {
							a.applyThemePreset(preset)
						}
					}
				})

				a.renderSettingsDivider(c, pal)

				// 跟随系统深色模式
				ui.Row(c.Key("follow-system-dark-row")).
					FillWidth().
					Padding(12, 2).
					Gap(12).
					AlignItems(ui.Center).
					Children(func() {
						followDark := a.settings.ShouldFollowSystemDark()
						ui.Column(c).Grow(1).Gap(2).Children(func() {
							ui.Text(c, "跟随系统深色模式").FontSize(14).TextColor(pal.TextPrimary)
							desc := "系统进入深色模式时自动切换为「暗夜极简」"
							if !followDark {
								desc = "已关闭自动切换，始终保持所选主题"
							} else if a.systemDark {
								desc = "当前已随系统切换为「暗夜极简」"
							}
							ui.Text(c, desc).FontSize(11).TextColor(pal.TextMuted).MaxLines(1)
						})
						if mobile.Switch(c.Key("sw-follow-system-dark"), &followDark).Changed() {
							a.toggleFollowSystemDark(followDark)
						}
					})

				a.renderSettingsDivider(c, pal)

				// 书架使用原书封面
				ui.Row(c.Key("cover-style-row")).
					FillWidth().
					Padding(12, 2).
					Gap(12).
					AlignItems(ui.Center).
					Children(func() {
						useOriginal := a.settings.UseOriginalCover
						ui.Column(c).Grow(1).Gap(2).Children(func() {
							ui.Text(c, "书架使用原书封面").FontSize(14).TextColor(pal.TextPrimary)
							desc := "优先展示 EPUB 原书封面，无封面自动回退色块"
							if !useOriginal {
								desc = "统一使用画册几何色块封面"
							}
							ui.Text(c, desc).FontSize(11).TextColor(pal.TextMuted).MaxLines(1)
						})
						if mobile.Switch(c.Key("sw-original-cover"), &useOriginal).Changed() {
							a.toggleUseOriginalCover(useOriginal)
						}
					})

				// 手机端悬浮液态玻璃底栏开关
				if !isTablet {
					a.renderSettingsDivider(c, pal)
					ui.Row(c.Key("floating-nav-row")).
						FillWidth().
						Padding(12, 2).
						Gap(12).
						AlignItems(ui.Center).
						Children(func() {
							floatingOn := a.useFloatingBottomNav()
							ui.Column(c).Grow(1).Gap(2).Children(func() {
								ui.Text(c, "液态玻璃悬浮底栏").FontSize(14).TextColor(pal.TextPrimary)
								desc := "关闭后切换为经典贴底导航栏"
								if !floatingOn {
									desc = "开启后切换为液态玻璃悬浮胶囊底栏"
								}
								ui.Text(c, desc).FontSize(11).TextColor(pal.TextMuted).MaxLines(1)
							})
							if mobile.Switch(c.Key("sw-floating-nav"), &floatingOn).Changed() {
								a.toggleFloatingBottomBar(floatingOn)
							}
						})
				}
			})
	})
}

func (a *AppUI) renderTypographySummarySection(c *ui.Context, pal theme.Palette) {
	poetryFont := theme.PoetryFont(a.settings.FontFamily)
	curWeight := a.settings.EffectiveFontWeight()
	fontDisplayName := a.settings.FontFamily
	if strings.TrimSpace(fontDisplayName) == "" {
		fontDisplayName = "系统默认"
	}

	ui.Column(c.Key("sec-typo-group")).FillWidth().Gap(8).Children(func() {
		a.renderSettingsGroupLabel(c, pal, "阅读排版与字体")

		ui.Column(c.Key("sec-typo-card")).
			FillWidth().
			Padding(16, 18, 6, 18).
			Gap(12).
			Radius(12).
			Background(pal.Surface).
			Border(1, pal.Border).
			Children(func() {
				// 紧凑实时诗句预览条
				ui.Box(c.Key("typo-inline-preview")).
					FillWidth().
					Padding(12, 14).
					Radius(8).
					Background(pal.Background).
					Border(1, pal.Border).
					Children(func() {
						ui.Text(c, "纸上有风，留一首诗给自己。").
							Font(poetryFont).
							FontSize(float32(a.fontSizeSlider)).
							FontWeight(curWeight).
							LineHeight(float32(a.lineHeightSlider)).
							TextColor(pal.TextPrimary).
							MaxLines(2)
					})

				// 字号滑块
				ui.Column(c).FillWidth().Gap(4).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
						ui.Text(c, "正文字号").FontSize(13).TextColor(pal.TextSecondary).Grow(1)
						ui.Textf(c, "%.0f pt", a.fontSizeSlider).Font(theme.FontMono).FontSize(12).TextColor(pal.Accent)
					})
					if ui.Slider(c.Key("prof-fontsize"), &a.fontSizeSlider, 14, 28).Changed() {
						a.saveTypography(float32(a.fontSizeSlider), float32(a.lineHeightSlider))
					}
				})

				// 行距滑块
				ui.Column(c).FillWidth().Gap(4).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
						ui.Text(c, "行距倍数").FontSize(13).TextColor(pal.TextSecondary).Grow(1)
						ui.Textf(c, "%.2f", a.lineHeightSlider).Font(theme.FontMono).FontSize(12).TextColor(pal.Accent)
					})
					if ui.Slider(c.Key("prof-lineheight"), &a.lineHeightSlider, 1.4, 2.4).Changed() {
						a.saveTypography(float32(a.fontSizeSlider), float32(a.lineHeightSlider))
					}
				})

				a.renderSettingsDivider(c, pal)

				subHint := "支持导入自定义 .ttf / .otf 与正文字重调整"
				if len(a.localFonts) > 0 {
					subHint = fmt.Sprintf("已导入 %d 款自定义字体", len(a.localFonts))
				}
				a.renderSettingsNavRow(
					c, pal,
					"nav-fonts-sub",
					"字体与字重",
					subHint,
					fmt.Sprintf("%s · %s", fontDisplayName, formatFontWeightLabel(curWeight)),
					"",
					func() { a.openProfileSubpage(profileSubpageFonts) },
				)
			})
	})
}

func (a *AppUI) renderWidgetSummarySection(c *ui.Context, pal theme.Palette) {
	a.mu.Lock()
	preview := a.widgetPreview
	pinnedCount := a.activeWidgetCount
	a.mu.Unlock()

	ui.Column(c.Key("sec-widget-group")).FillWidth().Gap(8).Children(func() {
		a.renderSettingsGroupLabel(c, pal, "桌面小组件")

		ui.Column(c.Key("sec-widget-card")).
			FillWidth().
			Padding(16, 18, 6, 18).
			Gap(12).
			Radius(12).
			Background(pal.Surface).
			Border(1, pal.Border).
			Children(func() {
				a.renderWidgetPreviewCard(c, pal, preview)

				a.renderSettingsDivider(c, pal)

				badge := ""
				if pinnedCount > 0 {
					badge = fmt.Sprintf("已添加 %d", pinnedCount)
				}
				summary := fmt.Sprintf("%s · %s",
					formatWidgetModeShort(a.settings.WidgetMode),
					formatWidgetIntervalLabel(a.settings.WidgetRefreshMinutes),
				)
				a.renderSettingsNavRow(
					c, pal,
					"nav-widget-sub",
					"小组件内容与换诗频率",
					"",
					summary,
					badge,
					func() { a.openProfileSubpage(profileSubpageWidget) },
				)
			})
	})
}

func (a *AppUI) renderStorageAndAboutSection(c *ui.Context, pal theme.Palette) {
	a.mu.Lock()
	appVer := a.appVersion
	updRes := a.updateResult
	a.mu.Unlock()
	if appVer == "" {
		appVer = app.DefaultAppVersion
	}

	ui.Column(c.Key("sec-storage-group")).FillWidth().Gap(8).Children(func() {
		a.renderSettingsGroupLabel(c, pal, "存储与关于")

		ui.Column(c.Key("sec-storage-card")).
			FillWidth().
			Padding(6, 18).
			Gap(0).
			Radius(12).
			Background(pal.Surface).
			Border(1, pal.Border).
			Children(func() {
				// 本地离线正文缓存行
				ui.Row(c.Key("cache-row")).
					FillWidth().
					Padding(12, 2).
					Gap(12).
					AlignItems(ui.Center).
					Children(func() {
						ui.Column(c).Grow(1).Gap(2).Children(func() {
							ui.Text(c, "本地离线正文缓存").FontSize(14).TextColor(pal.TextPrimary)
							ui.Textf(c, "已缓存 %d 本 · 约 %s（清理保留云端书目与散页）",
								a.stats.OfflineBookCount, formatBytesSize(a.cacheSizeBytes)).
								FontSize(11).TextColor(pal.TextMuted).MaxLines(2)
						})
						if a.stats.OfflineBookCount > 0 {
							if ui.Button(c, "清理缓存").Clicked() {
								a.clearAllCachesAsync()
							}
						}
					})

				a.renderSettingsDivider(c, pal)

				// 关于 StillPoets 二级页入口
				updateBadge := ""
				if updRes != nil && updRes.HasUpdate {
					updateBadge = "有新版本"
				}
				a.renderSettingsNavRow(
					c, pal,
					"nav-about-sub",
					"关于 StillPoets",
					"更新日志、检查更新、邮件联系与分析日志",
					formatVersionTag(appVer),
					updateBadge,
					func() { a.openProfileSubpage(profileSubpageAbout) },
				)
			})
	})
}

// --- 二级页 1：WebDAV 配置与设备 ---

func (a *AppUI) renderWebDAVSubpage(c *ui.Context, pal theme.Palette, isTablet bool) {
	syncing, stageMsg := a.syncActivityState()
	a.mu.Lock()
	busy := a.davBusy
	retiringID := a.davRetiringID
	status := a.davStatus
	ov := a.syncOverview
	hasSecret := a.davHasSecret
	a.mu.Unlock()

	ui.Column(c.Key("subpage-webdav")).Fill().Children(func() {
		a.renderProfileSubpageHeader(c, pal, isTablet, "WebDAV 配置与设备", nil)

		ui.Scroll(c.Key("subpage-webdav-scroll")).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(16, 20, 48, 20).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(layout.ReadingMaxWidthDIP + 80).Gap(16).Children(func() {
					// 服务器凭据表单卡片
					ui.Column(c.Key("webdav-form-card")).
						FillWidth().
						Padding(18).
						Gap(12).
						Radius(12).
						Background(pal.Surface).
						Border(1, pal.Border).
						Children(func() {
							ui.Column(c).Gap(2).Children(func() {
								ui.Text(c, "服务器连接").Font(theme.FontSerif).FontSize(16).Bold().TextColor(pal.TextPrimary)
								ui.Text(c, "Per-Device 单写者代次快照 · 密码仅存入系统安全密钥链").FontSize(12).TextColor(pal.TextMuted)
							})

							ui.TextInput(c.Key("dav-url"), &a.davURL).Placeholder("https://dav.jianguoyun.com/dav/").Label("服务器地址")
							ui.TextInput(c.Key("dav-user"), &a.davUser).Placeholder("用户名 / 邮箱").Label("用户名")

							passHint := "应用专用密码"
							if hasSecret && a.davPass == "" {
								passHint = "已保存在系统密钥链（留空则沿用原密码）"
							}
							ui.TextInput(c.Key("dav-pass"), &a.davPass).Password().Placeholder(passHint).Label("密码")
							ui.TextInput(c.Key("dav-folder"), &a.davFolder).Placeholder("StillPoets").Label("专属目录名")

							ui.Row(c.Key("dav-atomic-row")).FillWidth().AlignItems(ui.Center).Padding(4, 0).Gap(12).Children(func() {
								ui.Column(c).Grow(1).Gap(2).Children(func() {
									ui.Text(c, "启用原子上传（临时文件 + MOVE）").FontSize(13).TextColor(pal.TextPrimary)
									ui.Text(c, "AList / 115 挂载点建议关闭，直接写入新代次并回读校验").FontSize(11).TextColor(pal.TextMuted)
								})
								mobile.Switch(c.Key("dav-atomic-sw"), &a.davAtomic)
							})

							// 主操作按钮（带旋转等待动画）
							ui.Row(c.Key("dav-actions")).FillWidth().Gap(10).Wrap().Children(func() {
								saveBtn := ui.Row(c.Key("dav-save-btn")).
									Grow(1).
									Padding(10, 16).
									Gap(8).
									Radius(10).
									Justify(ui.Center).
									AlignItems(ui.Center).
									Background(pal.Accent).
									Cursor(ui.CursorPointer).
									Disabled(busy || syncing)
								theme.ApplyPressScale(saveBtn, 0.97)
								saveBtn.Children(func() {
									if busy {
										ui.Spinner(c.Key("dav-save-spin")).FontSize(14).TextColor(pal.AccentText).Label("正在验证并保存")
										ui.Text(c, "正在验证并保存…").Font(theme.FontSans).FontSize(13).Bold().TextColor(pal.AccentText)
									} else {
										ui.Text(c, "验证并保存配置").Font(theme.FontSans).FontSize(13).Bold().TextColor(pal.AccentText)
									}
								})
								if saveBtn.Clicked() && !busy && !syncing {
									a.verifyAndSaveWebDAV()
								}

								if ov.Configured {
									syncBtn := ui.Row(c.Key("dav-sync-btn")).
										Padding(10, 16).
										Gap(6).
										Radius(10).
										Justify(ui.Center).
										AlignItems(ui.Center).
										Background(pal.Background).
										Border(1, pal.Border).
										Cursor(ui.CursorPointer).
										Disabled(busy || syncing)
									theme.ApplyPressScale(syncBtn, 0.97)
									syncBtn.Children(func() {
										if syncing {
											ui.Spinner(c.Key("dav-sync-spin")).FontSize(13).TextColor(pal.Accent).Label("同步中")
											ui.Text(c, "同步中…").Font(theme.FontSans).FontSize(13).TextColor(pal.Accent)
										} else {
											ui.Icon(c, components.IconRefresh).Width(14).Height(14).TextColor(pal.TextPrimary)
											ui.Text(c, "立即同步").Font(theme.FontSans).FontSize(13).TextColor(pal.TextPrimary)
										}
									})
									if syncBtn.Clicked() && !busy && !syncing {
										a.triggerManualSync()
									}
								}
							})

							if syncing {
								ui.Row(c.Key("dav-inline-progress")).
									FillWidth().
									Padding(9, 12).
									Gap(8).
									Radius(8).
									Background(pal.Background).
									Border(1, pal.Border).
									AlignItems(ui.Center).
									Children(func() {
										ui.Spinner(c.Key("dav-prog-spin")).FontSize(14).TextColor(pal.Accent).Label(stageMsg)
										ui.Text(c, stageMsg).FontSize(12).TextColor(pal.TextSecondary).Grow(1).MaxLines(2)
									})
							} else if status != "" {
								ui.Text(c, status).FontSize(12).LineHeight(1.5).TextColor(pal.Accent)
							}
						})

					// 同步元状态卡片
					lastSync := "尚未同步"
					if ov.LastSyncAt > 0 {
						lastSync = time.UnixMilli(ov.LastSyncAt).Local().Format("2006-01-02 15:04:05")
					}
					ui.Column(c.Key("webdav-meta-card")).
						FillWidth().
						Padding(16, 18).
						Gap(8).
						Radius(12).
						Background(pal.Surface).
						Border(1, pal.Border).
						Children(func() {
							ui.Text(c, "本机同步状态").Font(theme.FontSerif).FontSize(15).Bold().TextColor(pal.TextPrimary)
							ui.Textf(c, "本机安装 ID：%s · 本地代次 rev %d", ov.InstallID, ov.LocalRev).
								Font(theme.FontMono).FontSize(11).TextColor(pal.TextMuted)
							ui.Textf(c, "上次同步：%s · 待上传诗集：%d 本", lastSync, ov.PendingCount).
								FontSize(12).TextColor(pal.TextSecondary)
						})

					// 远端设备管理卡片
					ui.Column(c.Key("webdav-devices-card")).
						FillWidth().
						Padding(16, 18).
						Gap(10).
						Radius(12).
						Background(pal.Surface).
						Border(1, pal.Border).
						Children(func() {
							ui.Column(c).Gap(2).Children(func() {
								ui.Text(c, "已同步的远端设备").Font(theme.FontSerif).FontSize(15).Bold().TextColor(pal.TextPrimary)
								ui.Text(c, "更换手机或重装后可手动退役旧设备快照，避免长期残留").FontSize(11).TextColor(pal.TextMuted)
							})
							if len(ov.RemoteDevices) == 0 {
								ui.Text(c, "当前未发现其他远端设备快照").FontSize(12).TextColor(pal.TextMuted)
							} else {
								for _, devID := range ov.RemoteDevices {
									devID := devID
									isRetiring := busy && retiringID == devID
									ui.Row(c.Key("rdev-"+devID)).
										FillWidth().
										Padding(10, 12).
										Gap(10).
										Radius(8).
										Background(pal.Background).
										Border(1, pal.Border).
										AlignItems(ui.Center).
										Children(func() {
											ui.Textf(c, "设备 %s", devID).Font(theme.FontMono).FontSize(12).TextColor(pal.TextPrimary).Grow(1)
											retBtn := ui.Row(c.Key("retire-btn-"+devID)).
												Padding(5, 11).
												Gap(5).
												Radius(8).
												AlignItems(ui.Center).
												Background(pal.Surface).
												Border(1, pal.Border).
												Cursor(ui.CursorPointer).
												Disabled(busy || syncing)
											retBtn.Children(func() {
												if isRetiring {
													ui.Spinner(c.Key("ret-spin-" + devID)).FontSize(12).TextColor(pal.Accent).Label("退役中")
													ui.Text(c, "退役中…").FontSize(11).TextColor(pal.Accent)
												} else {
													ui.Text(c, "退役此设备").FontSize(11).TextColor(pal.TextSecondary)
												}
											})
											if retBtn.Clicked() && !busy && !syncing {
												a.triggerRetireDevice(devID)
											}
										})
								}
							}
						})

					// 危险操作：断开同步（二次确认）
					if ov.Configured {
						ui.Column(c.Key("webdav-danger-card")).
							FillWidth().
							Padding(14, 18).
							Gap(10).
							Radius(12).
							Background(pal.Surface).
							Border(1, pal.Border).
							Children(func() {
								if !a.confirmDisconnect {
									discBtn := ui.Row(c.Key("dav-disc-trigger")).
										FillWidth().
										Justify(ui.Center).
										Padding(6, 0).
										Cursor(ui.CursorPointer).
										Disabled(busy || syncing)
									discBtn.Children(func() {
										ui.Text(c, "断开同步并清除密钥链密码").FontSize(13).Bold().TextColor(pal.Accent)
									})
									if discBtn.Clicked() && !busy && !syncing {
										a.confirmDisconnect = true
									}
								} else {
									ui.Text(c, "断开后本机书架与散页不会丢失，但会移除密钥链中的 WebDAV 密码。确认断开？").
										FontSize(12).LineHeight(1.5).TextColor(pal.TextSecondary)
									ui.Row(c).FillWidth().Gap(10).Justify(ui.End).Children(func() {
										if ui.Button(c, "取消").Clicked() {
											a.confirmDisconnect = false
										}
										if ui.PrimaryButton(c, "确认断开").Clicked() {
											a.confirmDisconnect = false
											a.disconnectWebDAV()
										}
									})
								}
							})
					}
				})
			})
		})
	})
}

// --- 二级页 2：字体与字重 ---

func (a *AppUI) renderFontsSubpage(c *ui.Context, pal theme.Palette, isTablet bool) {
	poetryFont := theme.PoetryFont(a.settings.FontFamily)
	curWeight := a.settings.EffectiveFontWeight()
	curFontName := a.settings.FontFamily
	if strings.TrimSpace(curFontName) == "" {
		curFontName = "文学衬线（系统默认）"
	}

	ui.Column(c.Key("subpage-fonts")).Fill().Children(func() {
		a.renderProfileSubpageHeader(c, pal, isTablet, "字体与字重", func() {
			impBtn := ui.Row(c.Key("fonts-header-import")).
				Padding(5, 11).
				Radius(999).
				Background(pal.Surface).
				Border(1, pal.Accent).
				AlignItems(ui.Center).
				Cursor(ui.CursorPointer)
			theme.ApplyPressScale(impBtn, 0.95)
			impBtn.Children(func() {
				ui.Text(c, "+ 导入字体").FontSize(12).Bold().TextColor(pal.Accent)
			})
			if impBtn.Clicked() {
				a.triggerImportFont()
			}
		})

		ui.Scroll(c.Key("subpage-fonts-scroll")).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(16, 20, 48, 20).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(layout.ReadingMaxWidthDIP + 80).Gap(18).Children(func() {
					// 1. 实时诗句标本预览卡
					ui.Column(c.Key("fonts-specimen-card")).
						FillWidth().
						Padding(20, 20, 14, 20).
						Gap(12).
						Radius(12).
						Background(pal.Surface).
						Border(1, pal.Border).
						Children(func() {
							ui.Column(c).FillWidth().Padding(14, 16).Radius(8).Background(pal.Background).Border(1, pal.Border).Gap(6).Children(func() {
								ui.Text(c, "那里，一切皆有序而美，\n奢华、安宁，与欢愉。").
									Font(poetryFont).
									FontSize(float32(a.fontSizeSlider)).
									FontWeight(curWeight).
									LineHeight(float32(a.lineHeightSlider)).
									TextColor(pal.TextPrimary)
							})
							ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
								ui.Text(c, "实时排版预览").FontSize(11).TextColor(pal.TextMuted).Grow(1)
								ui.Textf(c, "%s · %s · %.0f pt", curFontName, formatFontWeightLabel(curWeight), a.fontSizeSlider).
									Font(theme.FontMono).FontSize(11).TextColor(pal.TextSecondary).MaxLines(1)
							})
						})

					// 2. 正文字重 4 档分段选择器
					ui.Column(c.Key("fonts-weight-sec")).FillWidth().Gap(8).Children(func() {
						a.renderSettingsGroupLabel(c, pal, "正文字重")
						ui.Column(c).
							FillWidth().
							Padding(16, 18).
							Gap(10).
							Radius(12).
							Background(pal.Surface).
							Border(1, pal.Border).
							Children(func() {
								a.renderFontWeightSegmented(c, pal, "sub-fw")
								ui.Text(c, "应用于诗歌正文与桌面小组件；部分单字重字体可能仅响应常规与偏粗档位。").
									FontSize(11).LineHeight(1.5).TextColor(pal.TextMuted)
							})
					})

					// 3. 内置字体
					ui.Column(c.Key("fonts-builtin-sec")).FillWidth().Gap(8).Children(func() {
						a.renderSettingsGroupLabel(c, pal, "内置字体")
						ui.Column(c).
							FillWidth().
							Padding(6, 16).
							Radius(12).
							Background(pal.Surface).
							Border(1, pal.Border).
							Children(func() {
								isDefault := strings.TrimSpace(a.settings.FontFamily) == ""
								row := ui.Row(c.Key("font-item-default")).
									FillWidth().
									Padding(10, 4).
									Gap(10).
									AlignItems(ui.Center).
									Cursor(ui.CursorPointer)
								row.Children(func() {
									ui.Column(c).Grow(1).Gap(2).Children(func() {
										ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
											ui.Text(c, "文学衬线（系统默认）").Font(theme.FontSerif).FontSize(15).TextColor(pal.TextPrimary)
											if isDefault {
												ui.Box(c).Padding(2, 7).Radius(999).Background(pal.HighlightBg).Children(func() {
													ui.Text(c, "当前使用").FontSize(10).Bold().TextColor(pal.Accent)
												})
											}
										})
										ui.Text(c, "宋体 / PingFang Serif · 随系统语言自适应").FontSize(11).TextColor(pal.TextMuted)
									})
									if isDefault {
										ui.Icon(c, components.IconCheck).Width(16).Height(16).TextColor(pal.Accent)
									}
								})
								if row.Clicked() && !isDefault {
									a.applyFontFamily("")
									a.showTransientToastOnUI("已切换为系统默认字体")
								}
							})
					})

					// 4. 用户已上传字体列表（支持切换与删除）
					ui.Column(c.Key("fonts-custom-sec")).FillWidth().Gap(8).Children(func() {
						ui.Textf(c, "已上传字体（%d）", len(a.localFonts)).
							Font(theme.FontSans).FontSize(12).Bold().TextColor(pal.TextMuted).Padding(0, 0, 0, 4)

						ui.Column(c.Key("fonts-custom-card")).
							FillWidth().
							Padding(8, 16).
							Gap(0).
							Radius(12).
							Background(pal.Surface).
							Border(1, pal.Border).
							Children(func() {
								if len(a.localFonts) == 0 {
									ui.Column(c).FillWidth().Padding(18, 4).Gap(8).AlignItems(ui.Center).Children(func() {
										ui.Text(c, "尚未上传自定义字体").Font(theme.FontSerif).FontSize(14).TextColor(pal.TextSecondary)
										ui.Text(c, "支持导入 .ttf 或 .otf 字体文件，导入后仅保存在本机并同步给小组件。").
											FontSize(12).LineHeight(1.5).TextColor(pal.TextMuted)
										if ui.Button(c, "+ 导入 .ttf / .otf 字体").Clicked() {
											a.triggerImportFont()
										}
									})
									return
								}

								for idx, lf := range a.localFonts {
									lf := lf
									if idx > 0 {
										a.renderSettingsDivider(c, pal)
									}
									active := a.settings.FontFamily == lf.Family
									confirming := a.confirmDeleteFont == lf.Filename

									ui.Column(c.Key("font-wrap-"+lf.Filename)).FillWidth().Padding(10, 2).Gap(8).Children(func() {
										ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
											selectArea := ui.Column(c.Key("font-sel-" + lf.Filename)).
												Grow(1).
												Gap(3).
												Cursor(ui.CursorPointer)
											selectArea.Children(func() {
												ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
													ui.Text(c, lf.Family).Font(lf.Family).FontSize(15).TextColor(pal.TextPrimary)
													if active {
														ui.Box(c).Padding(2, 7).Radius(999).Background(pal.HighlightBg).Children(func() {
															ui.Text(c, "当前使用").FontSize(10).Bold().TextColor(pal.Accent)
														})
													}
												})
												ui.Textf(c, "%s · %s", lf.Filename, formatBytesSize(lf.Size)).
													Font(theme.FontMono).FontSize(11).TextColor(pal.TextMuted).MaxLines(1)
											})
											if selectArea.Clicked() && !active {
												a.applyFontFamily(lf.Family)
												a.showTransientToastOnUI("已启用字体：" + lf.Family)
											}

											if active {
												ui.Icon(c, components.IconCheck).Width(16).Height(16).TextColor(pal.Accent)
											}

											delBtn := ui.Box(c.Key("font-del-" + lf.Filename)).
												Padding(6).
												Radius(6).
												Cursor(ui.CursorPointer)
											theme.ApplyPressScale(delBtn, 0.90)
											delBtn.Children(func() {
												ui.Icon(c, components.IconTrash).Width(15).Height(15).TextColor(pal.TextMuted)
											})
											if delBtn.Clicked() {
												if confirming {
													a.confirmDeleteFont = ""
												} else {
													a.confirmDeleteFont = lf.Filename
												}
											}
										})

										if confirming {
											ui.Row(c.Key("font-del-confirm-"+lf.Filename)).
												FillWidth().
												Padding(8, 10).
												Gap(8).
												Radius(8).
												Background(pal.Background).
												Border(1, pal.Border).
												AlignItems(ui.Center).
												Children(func() {
													hint := "删除后不可恢复，确认删除该字体文件？"
													if active {
														hint = "当前正在使用该字体，删除后将自动回退系统默认字体。"
													}
													ui.Text(c, hint).FontSize(11).TextColor(pal.TextSecondary).Grow(1).MaxLines(2)
													if ui.Button(c, "取消").Clicked() {
														a.confirmDeleteFont = ""
													}
													if ui.PrimaryButton(c, "确认删除").Clicked() {
														a.confirmDeleteFont = ""
														a.deleteLocalFont(lf)
													}
												})
										}
									})
								}
							})
					})
				})
			})
		})
	})
}

func (a *AppUI) renderFontWeightSegmented(c *ui.Context, pal theme.Palette, keyPrefix string) {
	curWeight := a.settings.EffectiveFontWeight()
	options := []struct {
		weight int
		label  string
	}{
		{domain.FontWeightLight, "纤细 300"},
		{domain.FontWeightRegular, "常规 400"},
		{domain.FontWeightMedium, "中等 500"},
		{domain.FontWeightSemiBold, "偏粗 600"},
	}

	ui.Row(c.Key(keyPrefix+"-seg")).
		FillWidth().
		Padding(3).
		Gap(4).
		Radius(10).
		Background(pal.Background).
		Border(1, pal.Border).
		Children(func() {
			for _, opt := range options {
				active := curWeight == opt.weight
				bg := ui.Transparent
				fg := pal.TextSecondary
				if active {
					bg = pal.Accent
					fg = pal.AccentText
				}
				btn := ui.Box(c.Key(fmt.Sprintf("%s-%d", keyPrefix, opt.weight))).
					Grow(1).
					Padding(7, 4).
					Radius(8).
					Center().
					Background(bg).
					Cursor(ui.CursorPointer).
					Transition(theme.ColorTransition())
				theme.ApplyPressScale(btn, 0.96)
				btn.Children(func() {
					ui.Text(c, opt.label).Font(theme.FontSans).FontSize(11).FontWeight(opt.weight).TextColor(fg).MaxLines(1)
				})
				if btn.Clicked() && !active {
					a.applyFontWeight(opt.weight)
				}
			}
		})
}

func formatFontWeightLabel(weight int) string {
	switch domain.NormalizeFontWeight(weight) {
	case domain.FontWeightLight:
		return "纤细 300"
	case domain.FontWeightMedium:
		return "中等 500"
	case domain.FontWeightSemiBold:
		return "偏粗 600"
	default:
		return "常规 400"
	}
}

// --- 二级页 3：桌面小组件 ---

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
	poemText := "纸上有风，留一首诗给自己。"
	metaText := "导入诗集或记一张散页后将自动轮播"
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

func (a *AppUI) applyThemePreset(preset theme.Palette) {
	disableFollow := false
	if a.settings.ShouldFollowSystemDark() {
		if (a.systemDark && preset.ID != theme.PresetDarkMinimal) ||
			(!a.systemDark && preset.ID == theme.PresetDarkMinimal) {
			disableFollow = true
		}
	}

	a.settings.ThemePreset = string(preset.ID)
	if disableFollow {
		off := false
		a.settings.FollowSystemDark = &off
	}
	a.settings.WidgetTheme.Preset = string(preset.ID)
	a.settings.WidgetTheme.BgColor = preset.BgHex
	a.settings.WidgetTheme.TextColor = preset.TextHex
	a.settings.WidgetTheme.AccentColor = preset.AccentHex
	a.syncNativeAppearance(a.palette())

	if disableFollow {
		a.showTransientToastOnUI(fmt.Sprintf("已切换为「%s」（已关闭跟随系统深色）", preset.Name))
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = a.svc.UpdateSettings(ctx, func(st *domain.Settings) {
			st.ThemePreset = string(preset.ID)
			if disableFollow {
				off := false
				st.FollowSystemDark = &off
			}
			st.WidgetTheme.Preset = string(preset.ID)
			st.WidgetTheme.BgColor = preset.BgHex
			st.WidgetTheme.TextColor = preset.TextHex
			st.WidgetTheme.AccentColor = preset.AccentHex
		})
	}()
}

func (a *AppUI) toggleFollowSystemDark(enabled bool) {
	v := enabled
	a.settings.FollowSystemDark = &v
	restoreLight := enabled && theme.PresetID(a.settings.ThemePreset) == theme.PresetDarkMinimal
	if restoreLight {
		a.settings.ThemePreset = string(theme.PresetArtbook)
	}
	a.syncNativeAppearance(a.palette())

	if enabled {
		if a.systemDark {
			a.showTransientToastOnUI("已开启跟随系统深色，已自动切为「暗夜极简」")
		} else {
			a.showTransientToastOnUI("已开启跟随系统深色模式")
		}
	} else {
		a.showTransientToastOnUI("已关闭跟随系统深色模式")
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = a.svc.UpdateSettings(ctx, func(st *domain.Settings) {
			vv := enabled
			st.FollowSystemDark = &vv
			if restoreLight {
				st.ThemePreset = string(theme.PresetArtbook)
			}
		})
	}()
}

func (a *AppUI) toggleUseOriginalCover(useOriginal bool) {
	a.settings.UseOriginalCover = useOriginal
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = a.svc.UpdateSettings(ctx, func(st *domain.Settings) {
			st.UseOriginalCover = useOriginal
		})
		if useOriginal {
			a.showTransientToast("已切换为原书封面（无封面自动回退色块）")
		} else {
			a.showTransientToast("已切换为统一色块封面")
		}
	}()
}

func (a *AppUI) toggleFloatingBottomBar(enabled bool) {
	v := enabled
	a.settings.FloatingBottomBar = &v
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = a.svc.UpdateSettings(ctx, func(st *domain.Settings) {
			vv := enabled
			st.FloatingBottomBar = &vv
		})
		if enabled {
			a.showTransientToast("已开启液态玻璃悬浮底栏")
		} else {
			a.showTransientToast("已切换为经典贴底导航栏")
		}
	}()
}

func (a *AppUI) updateWidgetRefreshInterval(minutes int) {
	normalized := domain.NormalizeWidgetRefreshMinutes(minutes)
	a.settings.WidgetRefreshMinutes = normalized
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := a.svc.UpdateSettings(ctx, func(st *domain.Settings) {
			st.WidgetRefreshMinutes = normalized
		}); err == nil {
			if tl, err := a.svc.ReadWidgetTimeline(); err == nil {
				cp := tl
				activeCnt := widgetbridge.ActiveWidgetCount()
				a.dispatchUI(func() {
					a.widgetPreview = &cp
					a.widgetCount = len(tl.Entries)
					a.activeWidgetCount = activeCnt
				})
				a.showTransientToast(fmt.Sprintf("小组件已设为%s自动换诗", formatWidgetIntervalLabel(normalized)))
			}
		}
	}()
}

func (a *AppUI) saveTypography(fontSize, lineHeight float32) {
	a.settings.FontSize = fontSize
	a.settings.LineHeight = lineHeight
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = a.svc.UpdateSettings(ctx, func(st *domain.Settings) {
			st.FontSize = fontSize
			st.LineHeight = lineHeight
		})
	}()
}

func (a *AppUI) applyFontWeight(weight int) {
	normalized := domain.NormalizeFontWeight(weight)
	a.settings.FontWeight = normalized
	a.settings.WidgetTheme.FontWeight = normalized
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = a.svc.UpdateSettings(ctx, func(st *domain.Settings) {
			st.FontWeight = normalized
			st.WidgetTheme.FontWeight = normalized
		})
	}()
}

func (a *AppUI) applyFontFamily(family string) {
	a.settings.FontFamily = family
	a.settings.WidgetTheme.FontFamily = family
	fontFile := ""
	if family != "" {
		for _, lf := range a.localFonts {
			if lf.Family == family {
				fontFile = lf.Filename
				break
			}
		}
	}
	a.settings.WidgetTheme.FontFile = fontFile
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = a.svc.UpdateSettings(ctx, func(st *domain.Settings) {
			st.FontFamily = family
			st.WidgetTheme.FontFamily = family
			st.WidgetTheme.FontFile = fontFile
		})
	}()
}

func (a *AppUI) deleteLocalFont(lf app.LocalFont) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.svc.DeleteFont(ctx, lf.Filename); err != nil {
			a.showTransientToast("删除字体失败：" + err.Error())
			return
		}
		a.reloadSync(ctx)
		a.showTransientToast("已删除字体：" + lf.Family)
	}()
}

func (a *AppUI) updateWidgetMode(mode domain.WidgetSourceMode, bookID string) {
	a.settings.WidgetMode = mode
	a.settings.WidgetBookID = bookID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := a.svc.UpdateSettings(ctx, func(st *domain.Settings) {
			st.WidgetMode = mode
			st.WidgetBookID = bookID
		}); err == nil {
			if tl, err := a.svc.ReadWidgetTimeline(); err == nil {
				cp := tl
				activeCnt := widgetbridge.ActiveWidgetCount()
				a.dispatchUI(func() {
					a.widgetPreview = &cp
					a.widgetCount = len(tl.Entries)
					a.activeWidgetCount = activeCnt
				})
				a.showTransientToast(fmt.Sprintf("小组件已同步更新（%d 条）", len(tl.Entries)))
			}
		}
	}()
}

func (a *AppUI) refreshWidgetSnapshot() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tl, err := a.svc.RebuildWidgetTimeline(ctx)
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
		a.showTransientToast(fmt.Sprintf("桌面小组件已刷新（%d 条）", len(tl.Entries)))
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

func (a *AppUI) verifyAndSaveWebDAV() {
	if a.davBusy {
		return
	}
	input := app.WebDAVConfigInput{
		BaseURL:      strings.TrimSpace(a.davURL),
		Username:     strings.TrimSpace(a.davUser),
		Password:     a.davPass,
		AppFolder:    strings.TrimSpace(a.davFolder),
		AtomicUpload: a.davAtomic,
		Enabled:      true,
	}
	a.mu.Lock()
	a.davBusy = true
	a.davStatus = "正在验证 WebDAV 连接并初始化专属目录…"
	a.mu.Unlock()

	go func() {
		testCtx, testCancel := context.WithTimeout(context.Background(), 25*time.Second)
		res, err := a.svc.TestWebDAVConnection(testCtx, input)
		testCancel()

		msg := ""
		if err != nil {
			msg = "连接失败：" + err.Error()
		} else {
			saveCtx, saveCancel := context.WithTimeout(context.Background(), 10*time.Second)
			saveErr := a.svc.SaveWebDAVConfig(saveCtx, input)
			saveCancel()
			if saveErr != nil {
				msg = "保存凭据失败：" + saveErr.Error()
			} else {
				statusMsg := "连接并保存成功！已自动创建专属目录，正在首次同步…"
				if res.LegacyWorkspace && res.ExistingRemote {
					statusMsg = fmt.Sprintf("连接并保存成功！检测到 %d 台设备快照及旧版 manifest.json（建议核对目录），正在首次同步…", res.DeviceCount)
				} else if res.LegacyWorkspace {
					statusMsg = "连接并保存成功！检测到旧版 manifest.json，建议更换目录名以隔离新协议，正在首次同步…"
				} else if res.ExistingRemote {
					statusMsg = fmt.Sprintf("连接并保存成功！检测到远端已有 %d 台设备快照，正在首次同步…", res.DeviceCount)
				}
				a.dispatchUI(func() {
					a.davPass = ""
					a.mu.Lock()
					a.davStatus = statusMsg
					a.mu.Unlock()
				})
				a.reloadSync(nil)

				syncCtx, syncCancel := context.WithTimeout(context.Background(), 3*time.Minute)
				syncRes, syncErr := a.svc.SyncNow(syncCtx, true)
				syncCancel()
				if syncErr != nil {
					msg = "配置已保存，但首次同步失败：" + syncErr.Error()
				} else if syncRes.PartialSync {
					msg = fmt.Sprintf("首次同步部分完成（%d 条远端警告或待补传资产，可点击「立即同步」重试）", len(syncRes.Warnings))
				} else {
					msg = fmt.Sprintf("连接并首次同步成功！（远端设备 %d 台）", len(syncRes.RemoteDevices))
				}
			}
		}
		a.reloadSync(nil)
		a.dispatchUI(func() {
			a.mu.Lock()
			a.davBusy = false
			a.davStatus = msg
			a.mu.Unlock()
		})
	}()
}

func (a *AppUI) triggerManualSync() {
	if busy, _ := a.syncActivityState(); busy {
		return
	}
	a.mu.Lock()
	a.davBusy = true
	a.davStatus = "正在准备同步…"
	a.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		res, err := a.svc.SyncNow(ctx, false)
		msg := ""
		if err != nil {
			msg = "同步失败：" + err.Error()
		} else if res.PartialSync {
			msg = fmt.Sprintf("部分同步完成（发现 %d 条远端快照警告或待补传资产）", len(res.Warnings))
		} else {
			msg = fmt.Sprintf("云同步成功（远端设备 %d 台）", len(res.RemoteDevices))
		}
		a.reloadSync(nil)
		a.dispatchUI(func() {
			a.mu.Lock()
			a.davBusy = false
			a.davStatus = msg
			a.mu.Unlock()
		})
	}()
}

func (a *AppUI) disconnectWebDAV() {
	go func() {
		err := a.svc.ClearWebDAVConfig()
		a.reloadSync(nil)
		status := "已断开 WebDAV 并清除密钥链密码"
		if err != nil {
			status = "已断开同步配置，但清除密码时出现告警：" + err.Error()
		}
		a.dispatchUI(func() {
			a.davPass = ""
			a.mu.Lock()
			a.davStatus = status
			a.mu.Unlock()
		})
	}()
}

func (a *AppUI) triggerRetireDevice(devID string) {
	if busy, _ := a.syncActivityState(); busy {
		return
	}
	a.mu.Lock()
	a.davBusy = true
	a.davRetiringID = devID
	a.davStatus = fmt.Sprintf("正在安全退役旧设备 %s…", devID)
	a.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		err := a.svc.RetireRemoteDevice(ctx, devID)
		msg := fmt.Sprintf("已安全退役设备 %s", devID)
		if err != nil {
			msg = "退役失败：" + err.Error()
		}
		a.reloadSync(nil)
		a.dispatchUI(func() {
			a.mu.Lock()
			a.davBusy = false
			a.davRetiringID = ""
			a.davStatus = msg
			a.mu.Unlock()
		})
	}()
}

func (a *AppUI) clearAllCachesAsync() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		n, err := a.svc.ClearAllBookCaches(ctx)
		if err != nil {
			a.showTransientToast("清理失败：" + err.Error())
			return
		}
		a.reloadSync(ctx)
		if n == 0 {
			a.showTransientToast("没有可清理的已同步缓存（未同步新书已保留）")
		} else {
			a.showTransientToast(fmt.Sprintf("已清理 %d 本离线正文缓存", n))
		}
	}()
}
