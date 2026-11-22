package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
	"stillpoets/internal/ui/views"
	"stillpoets/modules/widgetbridge"
)

type mygoSecretsAdapter struct {
	mod mygo.SecretsModule
}

func (s mygoSecretsAdapter) Get(key string) ([]byte, error)     { return s.mod.Get(key) }
func (s mygoSecretsAdapter) Set(key string, value []byte) error { return s.mod.Set(key, value) }
func (s mygoSecretsAdapter) Delete(key string) error            { return s.mod.Delete(key) }

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

		uiInst := views.NewAppUI(svc, pickFile, nil)
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:           "StillPoets",
			Width:           1080,
			Height:          720,
			MinWidth:        360,
			MinHeight:       600,
			BackgroundColor: "#F4F4EF",
			StateKey:        "stillpoets-main",
			Content:         ui.View(uiInst.Render),
		})
		uiInst.SetUIDispatch(win.Update)

		mu.Lock()
		appUI = uiInst
		queuedURL := pendingURL
		pendingURL = ""
		mu.Unlock()

		if queuedURL != "" {
			uiInst.HandleDeeplink(queuedURL)
		}
		uiInst.TriggerStartupSync()
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
