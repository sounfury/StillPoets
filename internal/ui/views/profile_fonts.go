package views

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
	"stillpoets/internal/domain"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/layout"
	"stillpoets/internal/ui/theme"
)

func (a *AppUI) renderFontsSubpage(c *ui.Context, pal theme.Palette, isTablet bool) {
	poetryFont := theme.PoetryFont(a.settings.FontFamily)
	curWeight := a.settings.EffectiveFontWeight()
	curFontName := a.settings.FontFamily
	if strings.TrimSpace(curFontName) == "" {
		curFontName = "文学衬线（系统默认）"
	}

	ui.Column(c.Key("subpage-fonts")).Fill().Children(func() {
		a.renderProfileSubpageHeader(c, pal, isTablet, "阅读排版与字体", func() {
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
					a.renderFontSpecimen(c, pal, poetryFont, curFontName, curWeight)

					a.renderFontMetrics(c, pal)

					// 3. 正文字重 4 档分段选择器
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

					a.renderBuiltInFonts(c, pal)

					a.renderLocalFonts(c, pal)
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

func (a *AppUI) renderFontSpecimen(c *ui.Context, pal theme.Palette, poetryFont, curFontName string, curWeight int) {
	// 1. 实时诗句标本预览卡（默认梭罗 7 行开场誓词；轻点切换中英对照台词试字体）
	spec := fontsPreviewSpecimen(a.fontsSpecimenIdx)
	ui.Column(c.Key("fonts-specimen-card")).
		FillWidth().
		Padding(16, 18, 14, 18).
		Gap(10).
		Radius(12).
		Background(pal.Surface).
		Border(1, pal.Border).
		Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "实时排版预览").FontSize(11).TextColor(pal.TextMuted).Grow(1)
				hint := "轻点切换中英对照"
				if spec.ShowOrigin {
					hint = dpsWeltonFootprint
				}
				ui.Text(c, hint).Font(theme.FontMono).FontSize(10).TextColor(pal.TextMuted)
			})
			specBox := ui.Column(c.Key("fonts-specimen-box")).
				FillWidth().
				Padding(14, 16).
				Radius(8).
				Background(pal.Background).
				Border(1, pal.Border).
				Gap(10).
				Cursor(ui.CursorPointer)
			theme.ApplyPressScale(specBox, 0.99)
			specBox.Children(func() {
				ui.Text(c, spec.ZH).
					Font(poetryFont).
					FontSize(float32(a.fontSizeSlider)).
					FontWeight(curWeight).
					LineHeight(float32(a.lineHeightSlider)).
					TextColor(pal.TextPrimary)
				if spec.EN != "" {
					ui.Box(c).Width(24).Height(1).Background(pal.Border)
					ui.Text(c, spec.EN).
						Font(poetryFont).
						FontSize(max(12.5, float32(a.fontSizeSlider)-1.5)).
						FontWeight(curWeight).
						LineHeight(float32(a.lineHeightSlider)).
						Italic().
						TextColor(pal.TextSecondary)
				}
			})
			if specBox.Clicked() {
				a.fontsSpecimenIdx = (a.fontsSpecimenIdx + 1) % len(dpsFontsSpecimens)
			}
			ui.Textf(c, "%s · %s · %.0f pt · %.2f×", curFontName, formatFontWeightLabel(curWeight), a.fontSizeSlider, a.lineHeightSlider).
				Font(theme.FontMono).FontSize(11).TextColor(pal.TextSecondary).MaxLines(1)
		})
}

func (a *AppUI) renderFontMetrics(c *ui.Context, pal theme.Palette) {
	// 2. 字号与行距调节
	ui.Column(c.Key("fonts-metrics-sec")).FillWidth().Gap(8).Children(func() {
		a.renderSettingsGroupLabel(c, pal, "字号与行距")
		ui.Column(c).
			FillWidth().
			Padding(16, 18).
			Gap(14).
			Radius(12).
			Background(pal.Surface).
			Border(1, pal.Border).
			Children(func() {
				ui.Column(c).FillWidth().Gap(4).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
						ui.Text(c, "正文字号").FontSize(13).TextColor(pal.TextSecondary).Grow(1)
						ui.Textf(c, "%.0f pt", a.fontSizeSlider).Font(theme.FontMono).FontSize(12).TextColor(pal.Accent)
					})
					if ui.Slider(c.Key("sub-fontsize"), &a.fontSizeSlider, 14, 28).Changed() {
						a.saveTypography(float32(a.fontSizeSlider), float32(a.lineHeightSlider))
					}
				})

				ui.Column(c).FillWidth().Gap(4).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
						ui.Text(c, "行距倍数").FontSize(13).TextColor(pal.TextSecondary).Grow(1)
						ui.Textf(c, "%.2f", a.lineHeightSlider).Font(theme.FontMono).FontSize(12).TextColor(pal.Accent)
					})
					if ui.Slider(c.Key("sub-lineheight"), &a.lineHeightSlider, 1.4, 2.4).Changed() {
						a.saveTypography(float32(a.fontSizeSlider), float32(a.lineHeightSlider))
					}
				})
			})
	})
}

func (a *AppUI) renderBuiltInFonts(c *ui.Context, pal theme.Palette) {
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
}

func (a *AppUI) renderFontDeleteConfirmation(c *ui.Context, pal theme.Palette, lf app.LocalFont, active, confirming bool) {
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
}

func (a *AppUI) renderLocalFont(c *ui.Context, pal theme.Palette, lf app.LocalFont) {
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

		a.renderFontDeleteConfirmation(c, pal, lf, active, confirming)
	})
}

func (a *AppUI) renderLocalFonts(c *ui.Context, pal theme.Palette) {
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
					a.renderLocalFont(c, pal, lf)
				}
			})
	})
}
