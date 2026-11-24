package main

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
	"stillpoets/internal/ui/theme"
	"stillpoets/internal/ui/views"
	"stillpoets/modules/appupdater"
	"stillpoets/modules/widgetbridge"
)

type mygoSecretsAdapter struct {
	mod mygo.SecretsModule
}

func (s mygoSecretsAdapter) Get(key string) ([]byte, error)     { return s.mod.Get(key) }
func (s mygoSecretsAdapter) Set(key string, value []byte) error { return s.mod.Set(key, value) }
func (s mygoSecretsAdapter) Delete(key string) error            { return s.mod.Delete(key) }

type mygoAppUpdaterAdapter struct{}

func (mygoAppUpdaterAdapter) CanInstallPackages() (bool, error) {
	return appupdater.CanInstallPackages()
}
func (mygoAppUpdaterAdapter) OpenInstallPermissionSettings() error {
	return appupdater.OpenInstallPermissionSettings()
}
func (mygoAppUpdaterAdapter) InstallAPK(apkPath string) error { return appupdater.InstallAPK(apkPath) }
func (mygoAppUpdaterAdapter) DeviceABIs() []string            { return appupdater.DeviceABIs() }

func main() {
	var (
		mu         sync.Mutex
		svc        *app.Service
		appUI      *views.AppUI
		win        *mygo.Window
		pendingURL string
	)

	mygo.App.OnOpenURL(func(rawURL string) {
		mu.Lock()
		uiRef := appUI
		if uiRef == nil {
			pendingURL = rawURL
		}
		mu.Unlock()
		if uiRef != nil {
			uiRef.HandleDeeplink(rawURL)
		}
	})

	mygo.App.OnBeforeQuit(func(_ *mygo.QuitEvent) {
		if svc != nil {
			_ = svc.Close()
		}
	})

	mygo.App.WhenReady(func() {
		dataDir, err := mygo.App.Path(mygo.PathUserData)
		if err != nil || dataDir == "" {
			cwd, _ := os.Getwd()
			dataDir = filepath.Join(cwd, ".stillpoets-data")
		} else {
			dataDir = filepath.Join(dataDir, "StillPoets")
		}

		secrets := mygoSecretsAdapter{mod: mygo.Secrets.Service("com.stillpoets.webdav")}
		svc, err = app.Open(context.Background(), dataDir, secrets)
		if err != nil {
			log.Fatalf("初始化 StillPoets 数据层失败：%v", err)
		}

		// 绑定原生桌面小组件刷新通道，并在启动后同步一次最新快照路径与内容
		svc.SetWidgetUpdatedCallback(widgetbridge.Reload)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := svc.RebuildWidgetTimeline(ctx); err != nil {
				widgetbridge.Reload(svc.WidgetTimelinePath(), svc.FontsDir())
			}
		}()

		pickFile := func(title string, extensions []string) (string, error) {
			paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
				Parent: win,
				Title:  title,
				Filters: []mygo.FileFilter{
					{Name: title, Extensions: extensions},
				},
			})
			if err != nil || len(paths) == 0 {
				return "", err
			}
			return paths[0], nil
		}

		initialDark := mygo.Theme.IsDark()
		initialPal := theme.Resolve(theme.PresetArtbook, true, initialDark)
		if st, err := svc.LoadSettings(context.Background()); err == nil {
			initialPal = theme.Resolve(theme.PresetID(st.ThemePreset), st.ShouldFollowSystemDark(), initialDark)
		}

		uiInst := views.NewAppUI(svc, pickFile, nil)
		uiInst.SetAppVersion(mygo.App.Version())
		if appupdater.Available() {
			uiInst.SetAppUpdater(mygoAppUpdaterAdapter{})
		}
		uiInst.SetOpenURLFunc(func(rawURL string) error {
			return mygo.Shell.OpenExternal(rawURL)
		})
		uiInst.SetExportFileFunc(func(filePath string) (string, error) {
			err := mygo.Share(mygo.ShareOptions{
				Text:  "StillPoets 诊断分析日志",
				Files: []string{filePath},
			})
			if err == nil {
				return filePath, nil
			}
			if errors.Is(err, errors.ErrUnsupported) {
				saved, saveErr := mygo.Dialog.SaveCopy(filePath, filepath.Base(filePath))
				if saveErr != nil {
					return "", saveErr
				}
				return saved, nil
			}
			// 移动端若系统分享不可用，回退到系统文件保存器（iOS Files / Android SAF）
			if saved, saveErr := mygo.Dialog.SaveCopy(filePath, filepath.Base(filePath)); saveErr == nil {
				return saved, nil
			}
			return "", err
		})
		uiInst.SetCopyTextFunc(func(text string) {
			mygo.Clipboard.WriteText(text)
		})
		uiInst.SetInitialSystemDark(initialDark)
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:           "StillPoets",
			Width:           1080,
			Height:          720,
			MinWidth:        360,
			MinHeight:       600,
			BackgroundColor: initialPal.BgHex,
			StateKey:        "stillpoets-main",
			Content:         ui.View(uiInst.Render),
		})
		uiInst.SetUIDispatch(win.Update)
		uiInst.SetWindowBgFunc(func(hex string) {
			_ = win.SetBackgroundColor(hex)
		})
		syncSystemBars := func(darkContent bool) {
			if darkContent {
				mygo.SystemBars.SetStyle(mygo.BarStyleLight)
			} else {
				mygo.SystemBars.SetStyle(mygo.BarStyleDark)
			}
		}
		uiInst.SetSystemBarStyleFunc(syncSystemBars)
		syncSystemBars(initialPal.ID == theme.PresetDarkMinimal)

		// 监听手机系统深色/浅色模式变化，实时通知 UI 自动切换暗夜极简或恢复日间主题，并同步刷新桌面小组件配色
		mygo.Theme.OnUpdated(func() {
			uiInst.NotifySystemDarkMode(mygo.Theme.IsDark())
			go widgetbridge.Reload(svc.WidgetTimelinePath(), svc.FontsDir())
		})

		// 每次用户切回/点进应用时自动检测一次更新（并在授权未知应用安装权限返回后自动衔接安装）
		mygo.App.OnActivate(func(_ bool) {
			uiInst.OnAppActivated()
		})

		mu.Lock()
		appUI = uiInst
		queuedURL := pendingURL
		pendingURL = ""
		mu.Unlock()

		if queuedURL != "" {
			uiInst.HandleDeeplink(queuedURL)
		}
		uiInst.TriggerStartupSync()
		uiInst.TriggerAutoUpdateCheck()
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
