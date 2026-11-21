package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
	"stillpoets/internal/ui/views"
)

type mygoSecretsAdapter struct {
	mod mygo.SecretsModule
}

func (s mygoSecretsAdapter) Get(key string) ([]byte, error)     { return s.mod.Get(key) }
func (s mygoSecretsAdapter) Set(key string, value []byte) error { return s.mod.Set(key, value) }
func (s mygoSecretsAdapter) Delete(key string) error            { return s.mod.Delete(key) }

func main() {
	var (
		svc   *app.Service
		appUI *views.AppUI
		win   *mygo.Window
	)

	mygo.App.OnOpenURL(func(rawURL string) {
		if appUI != nil {
			appUI.HandleDeeplink(rawURL)
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

		appUI = views.NewAppUI(svc, pickFile, nil)
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:           "StillPoets",
			Width:           1080,
			Height:          720,
			MinWidth:        360,
			MinHeight:       600,
			BackgroundColor: "#F4F4EF",
			StateKey:        "stillpoets-main",
			Content:         ui.View(appUI.Render),
		})
		appUI.SetRepaint(func() {
			if win != nil {
				win.Update(func() {})
			}
		})
		appUI.TriggerStartupSync()
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
