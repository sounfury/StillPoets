package views

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/mobile"

	"stillpoets/internal/app"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/layout"
	"stillpoets/internal/ui/theme"
)

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
								passHint = "已保存当前密码，更换服务器或账号时请填写"
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
							if ov.PendingError != "" {
								ui.Textf(c, "最近上传异常：%s", ov.PendingError).
									FontSize(11).LineHeight(1.45).TextColor(pal.Accent).MaxLines(3)
							}
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
					msg = fmt.Sprintf("首次同步部分完成（待上传诗集 %d 本，远端快照警告 %d 条）", syncRes.PendingBookUploads, len(syncRes.Warnings))
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
			msg = fmt.Sprintf("部分同步完成（待上传诗集 %d 本，远端快照警告 %d 条）", res.PendingBookUploads, len(res.Warnings))
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
