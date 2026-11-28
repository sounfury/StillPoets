package views

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/theme"
)

const (
	// autoUpdateCheckCooldown 避免冷启动 WhenReady 与紧随其后的首帧 OnActivate 在数秒内连打两次 GitHub API。
	autoUpdateCheckCooldown = 10 * time.Second
)

// AppUpdaterBridge 抽象原生应用内更新安装能力（Android 对接 modules/appupdater；iOS/桌面为 nil）。
type AppUpdaterBridge interface {
	CanInstallPackages() (bool, error)
	OpenInstallPermissionSettings() error
	InstallAPK(apkPath string) error
	DeviceABIs() []string
}

type updateInstallStage int

const (
	updateStagePrompt updateInstallStage = iota
	updateStageDownloading
	updateStageNeedPermission
	updateStageReadyToInstall
	updateStageError
)

// SetAppUpdater 注入原生应用内更新安装器（仅在 Android 端注入，其他平台留空走外部 Release 链接）。
func (a *AppUI) SetAppUpdater(updater AppUpdaterBridge) {
	a.mu.Lock()
	a.appUpdater = updater
	a.mu.Unlock()
}

func (a *AppUI) canInAppUpdate() bool {
	a.mu.Lock()
	u := a.appUpdater
	a.mu.Unlock()
	return u != nil
}

// OnAppActivated 在应用冷启动就绪或从后台切回前台时调用：
// 1. 若用户刚前往系统设置开启「允许安装未知应用」权限后返回，自动衔接调起安装器；
// 2. 自动静默检查一次远端 GitHub Release 更新，发现新版本时弹出轻量更新小弹窗；
// 3. 带短冷却静默触发一次 WebDAV 同步，确保电脑端刚整理的书架与投递的新书切回即见。
func (a *AppUI) OnAppActivated() {
	if a.tryResumePendingInstallAfterPermission() {
		return
	}
	a.TriggerAutoUpdateCheck()
	a.TriggerForegroundSync()
}

// TriggerAutoUpdateCheck 在进入应用时静默检查一次新版本（带极短防重窗口，失败静默不打扰）。
func (a *AppUI) TriggerAutoUpdateCheck() {
	now := time.Now()
	a.mu.Lock()
	if a.updateChecking || a.updateStage == updateStageDownloading {
		a.mu.Unlock()
		return
	}
	if !a.lastAutoCheckAt.IsZero() && now.Sub(a.lastAutoCheckAt) < autoUpdateCheckCooldown {
		a.mu.Unlock()
		return
	}
	a.lastAutoCheckAt = now
	a.updateChecking = true
	ver := a.appVersion
	a.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		res, err := a.svc.CheckLatestRelease(ctx, ver)
		if err != nil {
			a.dispatchUI(func() {
				a.mu.Lock()
				a.updateChecking = false
				a.mu.Unlock()
			})
			return
		}

		if !res.HasUpdate {
			// 已是最新版时顺手清理历史安装包缓存
			go a.svc.CleanupUpdateCache()
		}

		a.dispatchUI(func() {
			a.mu.Lock()
			a.updateChecking = false
			cp := res
			a.updateResult = &cp
			a.updateError = ""
			shouldOpen := res.HasUpdate &&
				res.Latest != nil &&
				!a.showUpdateModal &&
				a.updateStage != updateStageDownloading &&
				!strings.EqualFold(strings.TrimSpace(a.dismissedUpdateTag), strings.TrimSpace(res.Latest.TagName))
			var updater AppUpdaterBridge
			if shouldOpen {
				updater = a.appUpdater
			}
			a.mu.Unlock()

			if shouldOpen {
				a.prepareUpdateModalStateOnUI(*res.Latest, updater)
				a.showUpdateModal = true
			}
		})
	}()
}

// tryResumePendingInstallAfterPermission 检查用户是否从「允许安装未知应用」系统设置页返回并已完成授权。
func (a *AppUI) tryResumePendingInstallAfterPermission() bool {
	a.mu.Lock()
	stage := a.updateStage
	apkPath := a.updateAPKPath
	updater := a.appUpdater
	a.mu.Unlock()

	if stage != updateStageNeedPermission || apkPath == "" || updater == nil {
		return false
	}

	go func() {
		allowed, err := updater.CanInstallPackages()
		if err != nil || !allowed {
			return
		}
		a.launchAPKInstallAsync(apkPath)
	}()
	return true
}

// openUpdateModalForRelease 打开应用内更新小弹窗；若 autoStartDownload 为 true 且当前端支持应用内更新，直接开始下载。
func (a *AppUI) openUpdateModalForRelease(rel app.ReleaseInfo, autoStartDownload bool) {
	a.mu.Lock()
	updater := a.appUpdater
	downloading := a.updateStage == updateStageDownloading
	sameTag := a.updateModalRel != nil && strings.EqualFold(a.updateModalRel.TagName, rel.TagName)
	a.mu.Unlock()

	if downloading && sameTag {
		a.showUpdateModal = true
		return
	}
	a.prepareUpdateModalStateOnUI(rel, updater)
	a.showUpdateModal = true
	if autoStartDownload && updater != nil {
		a.startUpdateDownloadAsync()
	}
}

func (a *AppUI) prepareUpdateModalStateOnUI(rel app.ReleaseInfo, updater AppUpdaterBridge) {
	relCopy := rel
	var apkAsset *app.ReleaseAsset
	if updater != nil {
		if matched, ok := app.SelectAndroidAPKAsset(rel.Assets, updater.DeviceABIs()); ok {
			apkAsset = matched
		}
	}
	a.mu.Lock()
	a.updateModalRel = &relCopy
	a.updateAPKAsset = apkAsset
	a.updateStage = updateStagePrompt
	a.updateStageError = ""
	a.updateDownloaded = 0
	a.updateTotal = 0
	if apkAsset != nil {
		a.updateTotal = apkAsset.Size
	}
	a.updateSpeedBPS = 0
	a.updateAPKPath = ""
	a.updateVerifiedSHA = false
	a.mu.Unlock()
}

func (a *AppUI) dismissUpdateModalOnUI() {
	a.mu.Lock()
	if a.updateModalRel != nil && a.updateStage != updateStageDownloading {
		a.dismissedUpdateTag = a.updateModalRel.TagName
	}
	a.mu.Unlock()
	a.showUpdateModal = false
}

func (a *AppUI) startUpdateDownloadAsync() {
	a.mu.Lock()
	if a.updateStage == updateStageDownloading {
		a.mu.Unlock()
		return
	}
	updater := a.appUpdater
	rel := a.updateModalRel
	ver := a.appVersion
	if rel == nil && a.updateResult != nil && a.updateResult.Latest != nil {
		cp := *a.updateResult.Latest
		rel = &cp
		a.updateModalRel = rel
	}
	if updater == nil || rel == nil {
		a.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.updateCancelFn = cancel
	a.updateStage = updateStageDownloading
	a.updateStageError = ""
	a.updateDownloaded = 0
	a.updateSpeedBPS = 0
	if a.updateAPKAsset != nil {
		a.updateTotal = a.updateAPKAsset.Size
	} else {
		a.updateTotal = 0
	}
	relCopy := *rel
	a.mu.Unlock()

	go func() {
		defer cancel()
		abis := updater.DeviceABIs()
		res, err := a.svc.DownloadReleaseAPK(ctx, ver, relCopy, abis, func(p app.APKDownloadProgress) {
			a.applyUpdateDownloadProgress(p)
		})

		if err != nil {
			a.handleUpdateDownloadError(err)
			return
		}

		a.finishUpdateDownload(updater, res)
	}()
}

func (a *AppUI) cancelUpdateDownloadOnUI() {
	a.mu.Lock()
	cancel := a.updateCancelFn
	a.updateCancelFn = nil
	a.updateStage = updateStagePrompt
	a.updateStageError = ""
	a.updateDownloaded = 0
	a.updateSpeedBPS = 0
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *AppUI) openInstallPermissionSettingsAsync() {
	a.mu.Lock()
	updater := a.appUpdater
	a.mu.Unlock()
	if updater == nil {
		return
	}
	go func() {
		if err := updater.OpenInstallPermissionSettings(); err != nil {
			a.dispatchUI(func() {
				a.mu.Lock()
				a.updateStage = updateStageError
				a.updateStageError = "无法打开系统授权页：" + err.Error()
				a.mu.Unlock()
			})
		}
	}()
}

func (a *AppUI) launchAPKInstallAsync(apkPath string) {
	a.mu.Lock()
	updater := a.appUpdater
	a.mu.Unlock()
	if updater == nil || strings.TrimSpace(apkPath) == "" {
		return
	}
	go func() {
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
		if err := updater.InstallAPK(apkPath); err != nil {
			a.dispatchUI(func() {
				a.mu.Lock()
				a.updateStage = updateStageError
				a.updateStageError = "调起系统安装器失败：" + err.Error()
				a.mu.Unlock()
				a.showUpdateModal = true
			})
			return
		}
		a.dispatchUI(func() {
			a.mu.Lock()
			a.updateStage = updateStageReadyToInstall
			a.updateStageError = ""
			a.mu.Unlock()
			a.showUpdateModal = true
		})
	}()
}

// renderUpdateModal 渲染发现新版本与 Android 应用内下载安装的小弹窗。
func (a *AppUI) renderUpdateModal(c *ui.Context, pal theme.Palette) {
	if !a.showUpdateModal {
		return
	}
	a.mu.Lock()
	rel := a.updateReleaseLocked()
	appVer := a.appVersion
	updater := a.appUpdater
	stage := a.updateStage
	asset := a.updateAPKAsset
	downloaded := a.updateDownloaded
	total := a.updateTotal
	speed := a.updateSpeedBPS
	apkPath := a.updateAPKPath
	verifiedSHA := a.updateVerifiedSHA
	stageErr := a.updateStageError
	a.mu.Unlock()

	if rel == nil {
		a.showUpdateModal = false
		return
	}
	if appVer == "" {
		appVer = app.DefaultAppVersion
	}

	canInApp := updater != nil && asset != nil
	targetURL := rel.HTMLURL
	if targetURL == "" {
		targetURL = app.GitHubReleasesURL
	}

	wasOpen := a.showUpdateModal
	components.AnimatedDialog(c, &a.showUpdateModal, pal, components.DialogOptions{Key: "app-update", MaxWidth: 396, MaxHeight: 520}, func() {
		a.renderUpdateHeader(c, pal, stage, appVer, rel.TagName, asset)

		ui.Box(c).FillWidth().Height(1).Background(pal.Border)

		// 2. 按阶段渲染主体与操作栏
		switch stage {
		case updateStagePrompt:
			a.renderUpdatePrompt(c, pal, rel, canInApp, targetURL)

		case updateStageDownloading:
			a.renderUpdateDownloading(c, pal, asset, downloaded, total, speed)

		case updateStageNeedPermission:
			a.renderUpdatePermission(c, pal, verifiedSHA)

		case updateStageReadyToInstall:
			a.renderUpdateReady(c, pal, apkPath)

		case updateStageError:
			a.renderUpdateError(c, pal, stageErr, targetURL, canInApp)
		}
	})

	// 若用户通过遮罩或 Esc 关闭弹窗，且非下载中状态，则同样记录本次忽略版本避免回前台反复弹窗
	if wasOpen && !a.showUpdateModal && stage != updateStageDownloading {
		a.rememberDismissedUpdate()
	}
}

func (a *AppUI) updateReleaseLocked() *app.ReleaseInfo {
	if a.updateModalRel != nil {
		return a.updateModalRel
	}
	if a.updateResult == nil || a.updateResult.Latest == nil {
		return nil
	}
	rel := *a.updateResult.Latest
	return &rel
}

func (a *AppUI) rememberDismissedUpdate() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.updateModalRel != nil {
		a.dismissedUpdateTag = a.updateModalRel.TagName
	}
}
