package views

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/mobile"

	"stillpoets/internal/app"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/layout"
	"stillpoets/internal/ui/theme"
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
	a.showChangelogModal = false
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
	// MyGo 根视图在移动端已自动避开顶部系统状态栏安全区，与 BrandHeader / renderReaderHeader 保持一致，避免叠加双重 safeTop 留白。
	ui.Row(c.Key("sub-header-"+title)).
		FillWidth().
		Padding(12, 16, 12, 12).
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

type settingsNavRow struct {
	key      string
	title    string
	subtitle string
	value    string
	badge    string
	onClick  func()
}

func (a *AppUI) renderSettingsNavRow(c *ui.Context, pal theme.Palette, rowCfg settingsNavRow) {
	key, title, subtitle, value, badge, onClick := rowCfg.key, rowCfg.title, rowCfg.subtitle, rowCfg.value, rowCfg.badge, rowCfg.onClick
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
				a.renderSettingsNavRow(c, pal, settingsNavRow{
					key:     "nav-webdav-sub",
					title:   "服务器与设备",
					value:   summary,
					onClick: func() { a.openProfileSubpage(profileSubpageWebDAV) },
				})
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
				// 紧凑实时诗句预览条（轻点可轮换中英对照台词，便于直接预览中英文字形）
				spec := inlinePreviewSpecimen(a.inlineSpecimenIdx)
				previewBox := ui.Column(c.Key("typo-inline-preview")).
					FillWidth().
					Padding(12, 14).
					Gap(6).
					Radius(8).
					Background(pal.Background).
					Border(1, pal.Border).
					Cursor(ui.CursorPointer)
				theme.ApplyPressScale(previewBox, 0.985)
				previewBox.Children(func() {
					ui.Text(c, spec.ZH).
						Font(poetryFont).
						FontSize(float32(a.fontSizeSlider)).
						FontWeight(curWeight).
						LineHeight(float32(a.lineHeightSlider)).
						TextColor(pal.TextPrimary).
						MaxLines(3)
					if spec.EN != "" {
						ui.Text(c, spec.EN).
							Font(poetryFont).
							FontSize(max(12, float32(a.fontSizeSlider)-2)).
							FontWeight(curWeight).
							LineHeight(float32(a.lineHeightSlider)).
							Italic().
							TextColor(pal.TextSecondary).
							MaxLines(3)
					}
					if spec.ShowOrigin {
						ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
							ui.Text(c, dpsWeltonFootprint).
								Font(theme.FontMono).
								FontSize(10).
								LetterSpacing(0.4).
								TextColor(pal.TextMuted)
						})
					}
				})
				if previewBox.Clicked() {
					a.inlineSpecimenIdx = (a.inlineSpecimenIdx + 1) % len(dpsInlineSpecimens)
				}

				a.renderSettingsDivider(c, pal)

				subHint := fmt.Sprintf("%.0f pt · %.2f 倍行距 · 调整字号、行距、字重与自定义字体", a.fontSizeSlider, a.lineHeightSlider)
				if len(a.localFonts) > 0 {
					subHint = fmt.Sprintf("%.0f pt · %.2f 倍行距 · 已导入 %d 款自定义字体", a.fontSizeSlider, a.lineHeightSlider, len(a.localFonts))
				}
				a.renderSettingsNavRow(c, pal, settingsNavRow{
					key:      "nav-fonts-sub",
					title:    "排版、字体与字重",
					subtitle: subHint,
					value:    fmt.Sprintf("%s · %s", fontDisplayName, formatFontWeightLabel(curWeight)),
					onClick:  func() { a.openProfileSubpage(profileSubpageFonts) },
				})
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
				a.renderSettingsNavRow(c, pal, settingsNavRow{
					key:     "nav-widget-sub",
					title:   "小组件内容与换诗频率",
					value:   summary,
					badge:   badge,
					onClick: func() { a.openProfileSubpage(profileSubpageWidget) },
				})
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
				a.renderSettingsNavRow(c, pal, settingsNavRow{
					key:      "nav-about-sub",
					title:    "关于 StillPoets",
					subtitle: "更新日志、检查更新、邮件联系与分析日志",
					value:    formatVersionTag(appVer),
					badge:    updateBadge,
					onClick:  func() { a.openProfileSubpage(profileSubpageAbout) },
				})
			})
	})
}
