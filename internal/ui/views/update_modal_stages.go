package views

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"stillpoets/internal/app"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/theme"
)

func (a *AppUI) applyUpdateDownloadProgress(p app.APKDownloadProgress) {
	a.dispatchUI(func() {
		a.mu.Lock()
		if a.updateStage == updateStageDownloading {
			a.updateDownloaded = p.Downloaded
			if p.Total > 0 {
				a.updateTotal = p.Total
			}
			a.updateSpeedBPS = p.SpeedBytesPerSec
		}
		a.mu.Unlock()
	})
}

func (a *AppUI) handleUpdateDownloadError(err error) {
	canceled := errors.Is(err, context.Canceled)
	a.dispatchUI(func() {
		a.mu.Lock()
		a.updateCancelFn = nil
		if canceled {
			a.updateStage = updateStagePrompt
			a.updateStageError = ""
			a.updateDownloaded = 0
			a.updateSpeedBPS = 0
		} else {
			a.updateStage = updateStageError
			a.updateStageError = err.Error()
		}
		a.mu.Unlock()
		if !canceled {
			a.showUpdateModal = true
		}
	})
}

func (a *AppUI) finishUpdateDownload(updater AppUpdaterBridge, res app.APKDownloadResult) {
	// 下载并校验完成，检查「允许安装未知应用」权限并调起系统安装器
	a.dispatchUI(func() {
		a.mu.Lock()
		a.updateCancelFn = nil
		a.updateAPKPath = res.FilePath
		a.updateDownloaded = res.Size
		a.updateTotal = res.Size
		a.updateVerifiedSHA = res.VerifiedSHA
		a.mu.Unlock()
		a.showUpdateModal = true
	})

	allowed, permErr := updater.CanInstallPackages()
	if permErr == nil && !allowed {
		a.dispatchUI(func() {
			a.mu.Lock()
			a.updateStage = updateStageNeedPermission
			a.updateStageError = ""
			a.mu.Unlock()
			a.showUpdateModal = true
		})
		return
	}
	a.launchAPKInstallAsync(res.FilePath)
}

func (a *AppUI) renderUpdateHeader(c *ui.Context, pal theme.Palette, stage updateInstallStage, appVer, releaseTag string, asset *app.ReleaseAsset) {
	// 1. 顶部标题与版本变化胶囊
	ui.Row(c.Key("upd-dlg-header")).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Box(c).
			Width(34).Height(34).
			Radius(10).
			Center().
			Background(pal.HeroBg).
			Border(1, pal.Border).
			Children(func() {
				ui.Icon(c, components.IconDownload).Width(17).Height(17).TextColor(pal.Accent)
			})
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			title := "发现新版本"
			switch stage {
			case updateStageDownloading:
				title = "正在下载更新"
			case updateStageNeedPermission:
				title = "需要安装权限"
			case updateStageReadyToInstall:
				title = "安装包已就绪"
			case updateStageError:
				title = "更新未能完成"
			}
			ui.Text(c, title).Font(theme.FontSerif).FontSize(17).Bold().TextColor(pal.TextPrimary)
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Textf(c, "%s → %s", formatVersionTag(appVer), formatVersionTag(releaseTag)).
					Font(theme.FontMono).FontSize(11).Bold().TextColor(pal.Accent)
				if asset != nil && asset.Size > 0 {
					ui.Textf(c, "· %s", formatBytesSize(asset.Size)).
						Font(theme.FontMono).FontSize(11).TextColor(pal.TextMuted)
				}
			})
		})
		closeBtn := ui.Box(c.Key("upd-dlg-close")).Padding(4).Cursor(ui.CursorPointer)
		theme.ApplyPressScale(closeBtn, 0.90)
		closeBtn.Children(func() {
			ui.Icon(c, components.IconClose).Width(16).Height(16).TextColor(pal.TextMuted)
		})
		if closeBtn.Clicked() {
			a.dismissUpdateModalOnUI()
		}
	})
}

func (a *AppUI) renderUpdatePrompt(c *ui.Context, pal theme.Palette, rel *app.ReleaseInfo, canInApp bool, targetURL string) {
	mdBody := normalizeReleaseMarkdown(*rel)
	if mdBody == "" {
		mdBody = "新版本已发布，包含功能优化与问题修复。"
	}
	ui.Scroll(c.Key("upd-dlg-notes-scroll")).FillWidth().MaxHeight(260).Children(func() {
		ui.Column(c).FillWidth().Padding(2, 2, 6, 2).Children(func() {
			markdown.View(c.Key("upd-dlg-md-"+rel.TagName), mdBody, a.changelogMarkdownOptions(pal)).
				FillWidth()
		})
	})

	ui.Box(c).FillWidth().Height(1).Background(pal.Border)

	ui.Row(c.Key("upd-dlg-prompt-actions")).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		if canInApp {
			browserLink := ui.Box(c.Key("upd-dlg-browser-link")).Padding(6, 4).Cursor(ui.CursorPointer)
			theme.ApplyPressScale(browserLink, 0.96)
			browserLink.Children(func() {
				ui.Text(c, "浏览器下载").FontSize(11).TextColor(pal.TextMuted)
			})
			if browserLink.Clicked() {
				a.openExternalURL(targetURL)
			}
		}
		ui.Box(c).Grow(1)
		if ui.Button(c, "稍后再说").Clicked() {
			a.dismissUpdateModalOnUI()
		}
		if canInApp {
			if ui.PrimaryButton(c, "立即更新").Clicked() {
				a.startUpdateDownloadAsync()
			}
		} else {
			if ui.PrimaryButton(c, "前往下载").Clicked() {
				a.dismissUpdateModalOnUI()
				a.openExternalURL(targetURL)
			}
		}
	})
}

func (a *AppUI) renderUpdateDownloading(c *ui.Context, pal theme.Palette, asset *app.ReleaseAsset, downloaded, total int64, speed float64) {
	ratio := 0.0
	if total > 0 && downloaded > 0 {
		ratio = float64(downloaded) / float64(total)
		if ratio > 1 {
			ratio = 1
		}
	}
	pct := int(math.Round(ratio * 100))

	ui.Column(c.Key("upd-dlg-dl-box")).
		FillWidth().
		Padding(14, 16).
		Gap(10).
		Radius(10).
		Background(pal.Background).
		Border(1, pal.Border).
		Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c.Key("upd-dlg-dl-spin")).FontSize(13).TextColor(pal.Accent).Label("下载更新中")
				if asset != nil {
					ui.Text(c, asset.Name).Font(theme.FontMono).FontSize(11).TextColor(pal.TextSecondary).MaxLines(1).Grow(1)
				} else {
					ui.Text(c, "正在下载安装包…").FontSize(12).TextColor(pal.TextSecondary).Grow(1)
				}
				if total > 0 {
					ui.Textf(c, "%d%%", pct).Font(theme.FontMono).FontFeatures("tnum").FontSize(12).Bold().TextColor(pal.Accent)
				}
			})

			// 进度条
			ui.Box(c.Key("upd-dlg-progress-bar")).
				FillWidth().
				Height(8).
				Draw(func(p *ui.Painter, r ui.Rect) {
					track := ui.Rect{X: r.X, Y: r.Y + 1, W: r.W, H: 6}
					p.Fill(track, pal.Border.Alpha(0.7), 3)
					if ratio > 0 {
						w := track.W * float32(ratio)
						if w < 6 {
							w = 6
						}
						if w > track.W {
							w = track.W
						}
						p.Fill(ui.Rect{X: track.X, Y: track.Y, W: w, H: track.H}, pal.Accent, 3)
					}
				})

			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				sizeInfo := formatBytesSize(downloaded)
				if total > 0 {
					sizeInfo = fmt.Sprintf("%s / %s", formatBytesSize(downloaded), formatBytesSize(total))
				}
				ui.Text(c, sizeInfo).Font(theme.FontMono).FontFeatures("tnum").FontSize(11).TextColor(pal.TextMuted).Grow(1)
				if speed > 0 {
					ui.Text(c, formatBytesSize(int64(speed))+"/s").
						Font(theme.FontMono).FontFeatures("tnum").FontSize(11).TextColor(pal.TextMuted)
				}
			})
		})

	ui.Text(c, "下载完成后将自动校验安装包完整性，并拉起 Android 系统安装确认页。").
		FontSize(11).LineHeight(1.45).TextColor(pal.TextMuted)

	ui.Row(c.Key("upd-dlg-dl-actions")).FillWidth().Gap(8).Justify(ui.End).Children(func() {
		if ui.Button(c, "取消下载").Clicked() {
			a.cancelUpdateDownloadOnUI()
		}
	})
}

func (a *AppUI) renderUpdatePermission(c *ui.Context, pal theme.Palette, verifiedSHA bool) {
	ui.Column(c.Key("upd-dlg-perm-box")).
		FillWidth().
		Padding(14, 16).
		Gap(8).
		Radius(10).
		Background(pal.Background).
		Border(1, pal.Border).
		Children(func() {
			verifyNote := "安装包已下载完成"
			if verifiedSHA {
				verifyNote = "安装包已下载并通过 SHA-256 完整性校验"
			}
			ui.Text(c, verifyNote).FontSize(13).Bold().TextColor(pal.TextPrimary)
			ui.Text(c, "请在系统设置中为 StillPoets 开启「允许安装未知应用」权限，开启后返回本应用即可直接覆盖安装（无需重新下载）。").
				FontSize(12).LineHeight(1.5).TextColor(pal.TextSecondary)
		})

	ui.Row(c.Key("upd-dlg-perm-actions")).FillWidth().Gap(8).Justify(ui.End).Children(func() {
		if ui.Button(c, "稍后").Clicked() {
			a.dismissUpdateModalOnUI()
		}
		if ui.PrimaryButton(c, "前往授权").Clicked() {
			a.openInstallPermissionSettingsAsync()
		}
	})
}

func (a *AppUI) renderUpdateReady(c *ui.Context, pal theme.Palette, apkPath string) {
	ui.Column(c.Key("upd-dlg-ready-box")).
		FillWidth().
		Padding(14, 16).
		Gap(8).
		Radius(10).
		Background(pal.Background).
		Border(1, pal.Border).
		Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, components.IconCheck).Width(15).Height(15).TextColor(pal.Accent)
				ui.Text(c, "已调起系统安装程序").FontSize(13).Bold().TextColor(pal.TextPrimary)
			})
			ui.Text(c, "请在系统弹出的确认框中点击「更新」完成升级。若系统安装页未弹出，可点击下方「立即安装」重试。").
				FontSize(12).LineHeight(1.5).TextColor(pal.TextSecondary)
		})

	ui.Row(c.Key("upd-dlg-ready-actions")).FillWidth().Gap(8).Justify(ui.End).Children(func() {
		if ui.Button(c, "关闭").Clicked() {
			a.dismissUpdateModalOnUI()
		}
		if ui.PrimaryButton(c, "立即安装").Clicked() {
			a.launchAPKInstallAsync(apkPath)
		}
	})
}

func (a *AppUI) renderUpdateError(c *ui.Context, pal theme.Palette, stageErr, targetURL string, canInApp bool) {
	ui.Column(c.Key("upd-dlg-err-box")).
		FillWidth().
		Padding(14, 16).
		Gap(6).
		Radius(10).
		Background(pal.Background).
		Border(1, pal.Accent).
		Children(func() {
			ui.Text(c, stageErr).FontSize(12).LineHeight(1.5).TextColor(pal.Accent)
			ui.Text(c, "若网络直连 GitHub 较慢或中断，可重试下载或改用浏览器打开 Release 页面获取安装包。").
				FontSize(11).LineHeight(1.45).TextColor(pal.TextMuted)
		})

	ui.Row(c.Key("upd-dlg-err-actions")).FillWidth().Gap(8).Justify(ui.End).Children(func() {
		if ui.Button(c, "浏览器下载").Clicked() {
			a.openExternalURL(targetURL)
		}
		if canInApp {
			if ui.PrimaryButton(c, "重试").Clicked() {
				a.startUpdateDownloadAsync()
			}
		} else if ui.PrimaryButton(c, "关闭").Clicked() {
			a.dismissUpdateModalOnUI()
		}
	})
}
