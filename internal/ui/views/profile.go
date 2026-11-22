package views

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
	"stillpoets/internal/domain"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/layout"
	"stillpoets/internal/ui/theme"
	"stillpoets/modules/widgetbridge"
)

func (a *AppUI) renderProfilePage(c *ui.Context, pal theme.Palette, isTablet bool) {
	ui.Column(c.Key("profile-page")).Fill().Children(func() {
		components.BrandHeader(c, pal, "我的", false, nil, nil)

		ui.Scroll(c.Key("profile-scroll")).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(12, 20, 96, 20).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(layout.ReadingMaxWidthDIP + 80).Gap(20).Children(func() {
					a.renderThemeAndFontSection(c, pal)
					a.renderWebDAVSection(c, pal)
					a.renderWidgetSection(c, pal)
					a.renderCacheSection(c, pal)
				})
			})
		})
	})
}

func (a *AppUI) renderThemeAndFontSection(c *ui.Context, pal theme.Palette) {
	ui.Column(c.Key("sec-theme")).
		FillWidth().
		Padding(18).
		Gap(14).
		Radius(10).
		Background(pal.Surface).
		Border(1, pal.Border).
		Children(func() {
			ui.Text(c, "外观主题与阅读字体").FontSize(16).Bold().TextColor(pal.TextPrimary)
			ui.Text(c, "仅保存在本机，不影响其他设备的阅读偏好").FontSize(12).TextColor(pal.TextMuted)

			// 3 套内置主题切换卡
			ui.Row(c.Key("theme-presets")).FillWidth().Gap(10).Children(func() {
				for _, preset := range theme.Presets() {
					active := theme.PresetID(a.settings.ThemePreset) == preset.ID ||
						(a.settings.ThemePreset == "" && preset.ID == theme.PresetArtbook)
					card := ui.Column(c.Key("preset-"+string(preset.ID))).
						Grow(1).
						Padding(12).
						Gap(6).
						Radius(8).
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
						ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
							ui.Box(c).Width(12).Height(12).Radius(6).Background(preset.HeroCorner)
							ui.Text(c, preset.Name).FontSize(13).Bold().TextColor(preset.TextPrimary)
						})
						ui.Text(c, preset.Subtitle).FontSize(11).TextColor(preset.TextMuted).MaxLines(1)
					})
					if card.Clicked() {
						a.applyThemePreset(preset)
					}
				}
			})

			// 字号与行距滑块
			ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Gap(4).Children(func() {
					ui.Textf(c, "正文字号：%.0f pt", a.fontSizeSlider).FontSize(12).TextColor(pal.TextSecondary)
					if ui.Slider(c.Key("prof-fontsize"), &a.fontSizeSlider, 14, 28).Changed() {
						a.saveTypography(float32(a.fontSizeSlider), float32(a.lineHeightSlider))
					}
				})
				ui.Column(c).Grow(1).Gap(4).Children(func() {
					ui.Textf(c, "行距倍数：%.2f", a.lineHeightSlider).FontSize(12).TextColor(pal.TextSecondary)
					if ui.Slider(c.Key("prof-lineheight"), &a.lineHeightSlider, 1.4, 2.4).Changed() {
						a.saveTypography(float32(a.fontSizeSlider), float32(a.lineHeightSlider))
					}
				})
			})

			// 自定义字体管理
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
				curFont := a.settings.FontFamily
				if curFont == "" {
					curFont = "系统默认衬线/无衬线"
				}
				ui.Textf(c, "当前字体：%s", curFont).FontSize(13).TextColor(pal.TextSecondary).Grow(1)
				if ui.Button(c, "导入 .ttf / .otf").Clicked() {
					a.triggerImportFont()
				}
			})

			if len(a.localFonts) > 0 {
				ui.Row(c.Key("local-fonts-row")).Gap(8).Wrap().Children(func() {
					renderFontChip := func(family, label string) {
						active := a.settings.FontFamily == family
						bg, fg, bd := pal.Background, pal.TextSecondary, pal.Border
						if active {
							bg, fg, bd = pal.Accent, pal.AccentText, pal.Accent
						}
						chip := ui.Box(c.Key("fnt-"+label)).Padding(5, 10).Radius(999).Background(bg).Border(1, bd).Cursor(ui.CursorPointer)
						chip.Children(func() {
							ui.Text(c, label).FontSize(12).TextColor(fg)
						})
						if chip.Clicked() {
							a.applyFontFamily(family)
						}
					}
					renderFontChip("", "系统默认")
					for _, lf := range a.localFonts {
						renderFontChip(lf.Family, lf.Family)
					}
				})
			}
		})
}

func (a *AppUI) renderWebDAVSection(c *ui.Context, pal theme.Palette) {
	a.mu.Lock()
	busy := a.davBusy
	status := a.davStatus
	ov := a.syncOverview
	hasSecret := a.davHasSecret
	a.mu.Unlock()

	ui.Column(c.Key("sec-webdav")).
		FillWidth().
		Padding(18).
		Gap(12).
		Radius(10).
		Background(pal.Surface).
		Border(1, pal.Border).
		Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Text(c, "WebDAV 多端同步").FontSize(16).Bold().TextColor(pal.TextPrimary)
					ui.Text(c, "Per-Device 单写者代次快照 · 密码仅存入系统安全密钥链").FontSize(12).TextColor(pal.TextMuted)
				})
				if ov.Configured {
					ui.Text(c, "已启用").FontSize(12).Bold().TextColor(pal.Accent)
				}
			})

			ui.TextInput(c.Key("dav-url"), &a.davURL).Placeholder("https://dav.jianguoyun.com/dav/").Label("服务器地址")
			ui.TextInput(c.Key("dav-user"), &a.davUser).Placeholder("用户名 / 邮箱").Label("用户名")

			passHint := "应用专用密码"
			if hasSecret && a.davPass == "" {
				passHint = "已保存在系统密钥链（留空则沿用原密码）"
			}
			ui.TextInput(c.Key("dav-pass"), &a.davPass).Password().Placeholder(passHint).Label("密码")
			ui.TextInput(c.Key("dav-folder"), &a.davFolder).Placeholder("StillPoets").Label("专属目录名")

			ui.Row(c.Key("dav-atomic-row")).FillWidth().AlignItems(ui.Center).Padding(4, 0).Children(func() {
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Text(c, "启用原子上传（临时文件 + MOVE）").FontSize(13).TextColor(pal.TextPrimary)
					ui.Text(c, "AList / 115 挂载点建议关闭，直接写入新代次并回读校验").FontSize(11).TextColor(pal.TextMuted)
				})
				ui.Switch(c.Key("dav-atomic-sw"), &a.davAtomic)
			})

			ui.Row(c.Key("dav-btns")).Gap(10).Wrap().Children(func() {
				saveLbl := "验证并保存配置"
				if busy {
					saveLbl = "处理中…"
				}
				if ui.PrimaryButton(c, saveLbl).Clicked() && !busy {
					a.verifyAndSaveWebDAV()
				}
				if ov.Configured {
					if ui.Button(c, "立即同步").Clicked() && !busy {
						a.triggerManualSync()
					}
					if ui.Button(c, "断开同步").Clicked() && !busy {
						a.disconnectWebDAV()
					}
				}
			})

			if status != "" {
				ui.Text(c, status).FontSize(12).LineHeight(1.5).TextColor(pal.Accent)
			}

			// 本机安装标识与同步概览
			lastSync := "尚未同步"
			if ov.LastSyncAt > 0 {
				lastSync = time.UnixMilli(ov.LastSyncAt).Local().Format("2006-01-02 15:04:05")
			}
			ui.Column(c.Key("sync-meta-box")).
				FillWidth().
				Padding(12).
				Gap(4).
				Radius(6).
				Background(pal.Background).
				Children(func() {
					ui.Textf(c, "本机安装 ID：%s · 本地代次 rev=%d", ov.InstallID, ov.LocalRev).FontSize(11).TextColor(pal.TextMuted)
					ui.Textf(c, "上次同步：%s · 待上传诗集：%d 本", lastSync, ov.PendingCount).FontSize(11).TextColor(pal.TextMuted)
				})

			// 远端设备列表与旧设备手动退役
			if len(ov.RemoteDevices) > 0 {
				ui.Text(c, "已同步的远端设备（更换手机/重装后可退役旧设备）").FontSize(12).Bold().TextColor(pal.TextSecondary)
				for _, devID := range ov.RemoteDevices {
					devID := devID
					ui.Row(c.Key("rdev-"+devID)).
						FillWidth().
						Padding(8, 12).
						Radius(6).
						Background(pal.Background).
						AlignItems(ui.Center).
						Children(func() {
							ui.Textf(c, "设备 %s", devID).FontSize(12).TextColor(pal.TextPrimary).Grow(1)
							if ui.Button(c, "退役此设备").Clicked() && !busy {
								a.triggerRetireDevice(devID)
							}
						})
				}
			}
		})
}

func (a *AppUI) renderWidgetSection(c *ui.Context, pal theme.Palette) {
	a.mu.Lock()
	preview := a.widgetPreview
	wCount := a.widgetCount
	pinnedCount := a.activeWidgetCount
	a.mu.Unlock()

	ui.Column(c.Key("sec-widget")).
		FillWidth().
		Padding(18).
		Gap(14).
		Radius(10).
		Background(pal.Surface).
		Border(1, pal.Border).
		Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Text(c, "桌面小组件（诗笺）").FontSize(16).Bold().TextColor(pal.TextPrimary)
					ui.Text(c, "自动同步当前主题与内容池；点击诗句直达整首诗并高亮原行").
						FontSize(12).LineHeight(1.5).TextColor(pal.TextMuted)
				})
				if pinnedCount > 0 {
					ui.Textf(c, "已添加 %d 个", pinnedCount).FontSize(11).Bold().TextColor(pal.Accent)
				}
			})

			// 所见即所得的小组件实时预览卡片
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

			needsBook := a.settings.WidgetMode == domain.WidgetModeBookRandom ||
				a.settings.WidgetMode == domain.WidgetModeBookSequential ||
				a.settings.WidgetMode == domain.WidgetModeBookFavorites
			if needsBook && len(a.books) > 0 {
				ui.Text(c, "选择目标诗集：").FontSize(12).TextColor(pal.TextSecondary)
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
			}

			ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Wrap().Children(func() {
				info := "尚未生成候选诗句，请先导入诗集或记一张散页"
				if wCount > 0 {
					info = fmt.Sprintf("已预编译 %d 条候选内容 · 支持桌面右上角一键换诗", wCount)
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
		Radius(16).
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
				FontSize(15).
				LineHeight(1.45).
				TextColor(pal.TextPrimary).
				MaxLines(4)
			if a.settings.FontFamily != "" {
				poemEl.Font(a.settings.FontFamily)
			}
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, metaText).FontSize(11).TextColor(pal.TextMuted).Grow(1).MaxLines(1)
				ui.Text(c, hintText).FontSize(10).TextColor(pal.Accent)
			})
		})
}

func (a *AppUI) renderCacheSection(c *ui.Context, pal theme.Palette) {
	ui.Column(c.Key("sec-cache")).
		FillWidth().
		Padding(18).
		Gap(10).
		Radius(10).
		Background(pal.Surface).
		Border(1, pal.Border).
		Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Text(c, "本地离线正文缓存").FontSize(16).Bold().TextColor(pal.TextPrimary)
					ui.Textf(c, "当前已离线缓存 %d 本诗集；清理缓存保留云端书目、阅读进度与散页", a.stats.OfflineBookCount).
						FontSize(12).TextColor(pal.TextMuted)
				})
				if a.stats.OfflineBookCount > 0 {
					if ui.Button(c, "清理已同步缓存").Clicked() {
						a.clearAllCachesAsync()
					}
				}
			})
		})
}

// --- 「我的」页面异步动作 ---

func (a *AppUI) applyThemePreset(preset theme.Palette) {
	a.settings.ThemePreset = string(preset.ID)
	a.settings.WidgetTheme.Preset = string(preset.ID)
	a.settings.WidgetTheme.BgColor = preset.BgHex
	a.settings.WidgetTheme.TextColor = preset.TextHex
	a.settings.WidgetTheme.AccentColor = preset.AccentHex
	st := a.settings
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.svc.SaveSettings(ctx, st)
	}()
}

func (a *AppUI) saveTypography(fontSize, lineHeight float32) {
	a.settings.FontSize = fontSize
	a.settings.LineHeight = lineHeight
	st := a.settings
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.svc.SaveSettings(ctx, st)
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
	st := a.settings
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.svc.SaveSettings(ctx, st)
	}()
}

func (a *AppUI) updateWidgetMode(mode domain.WidgetSourceMode, bookID string) {
	a.settings.WidgetMode = mode
	a.settings.WidgetBookID = bookID
	st := a.settings
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.svc.SaveSettings(ctx, st); err == nil {
			if tl, err := a.svc.RebuildWidgetTimeline(ctx); err == nil {
				cp := tl
				a.mu.Lock()
				a.widgetPreview = &cp
				a.widgetCount = len(tl.Entries)
				a.activeWidgetCount = widgetbridge.ActiveWidgetCount()
				a.mu.Unlock()
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
		a.mu.Lock()
		a.widgetPreview = &cp
		a.widgetCount = len(tl.Entries)
		a.activeWidgetCount = widgetbridge.ActiveWidgetCount()
		a.mu.Unlock()
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
	a.requestRepaint()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		res, err := a.svc.TestWebDAVConnection(ctx, input)
		msg := ""
		if err != nil {
			msg = "连接失败：" + err.Error()
		} else {
			if err := a.svc.SaveWebDAVConfig(ctx, input); err != nil {
				msg = "保存凭据失败：" + err.Error()
			} else {
				a.mu.Lock()
				a.davPass = "" // 清空界面表单中的明文密码
				a.mu.Unlock()
				if res.ExistingRemote {
					msg = fmt.Sprintf("连接并保存成功！检测到远端已有 %d 台设备快照，正在同步…", res.DeviceCount)
				} else if res.LegacyWorkspace {
					msg = "连接并保存成功！检测到旧版 manifest.json，建议更换目录名以隔离新协议。"
				} else {
					msg = "连接并保存成功！已自动创建专属目录，正在首次同步…"
				}
				_, _ = a.svc.SyncNow(ctx, true)
			}
		}
		a.reloadSync(ctx)
		a.mu.Lock()
		a.davBusy = false
		a.davStatus = msg
		a.mu.Unlock()
		a.requestRepaint()
	}()
}

func (a *AppUI) triggerManualSync() {
	a.mu.Lock()
	a.davBusy = true
	a.davStatus = "正在同步…"
	a.mu.Unlock()
	a.requestRepaint()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
		a.reloadSync(ctx)
		a.mu.Lock()
		a.davBusy = false
		a.davStatus = msg
		a.mu.Unlock()
		a.requestRepaint()
	}()
}

func (a *AppUI) disconnectWebDAV() {
	go func() {
		_ = a.svc.ClearWebDAVConfig()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		a.reloadSync(ctx)
		a.mu.Lock()
		a.davPass = ""
		a.davStatus = "已断开 WebDAV 并清除密钥链密码"
		a.mu.Unlock()
		a.requestRepaint()
	}()
}

func (a *AppUI) triggerRetireDevice(devID string) {
	a.mu.Lock()
	a.davBusy = true
	a.davStatus = fmt.Sprintf("正在安全退役旧设备 %s…", devID)
	a.mu.Unlock()
	a.requestRepaint()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		err := a.svc.RetireRemoteDevice(ctx, devID)
		msg := fmt.Sprintf("已安全退役设备 %s", devID)
		if err != nil {
			msg = "退役失败：" + err.Error()
		}
		a.reloadSync(ctx)
		a.mu.Lock()
		a.davBusy = false
		a.davStatus = msg
		a.mu.Unlock()
		a.requestRepaint()
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
