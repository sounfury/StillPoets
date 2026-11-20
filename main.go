package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"stillpoets/internal/domain"
	spSync "stillpoets/internal/sync"
	"stillpoets/internal/sync/webdav"
	"stillpoets/internal/ui/layout"
	"stillpoets/internal/ui/theme"
)

// stillPoetsApp 持有 Phase 0 骨架的最小应用状态。
type stillPoetsApp struct {
	mu           sync.Mutex
	palette      theme.Palette
	shelfWin     *mygo.Window
	quotesWin    *mygo.Window
	settingsWin  *mygo.Window
	tabs         *mygo.Tabs
	selectedBook int
	books        []domain.Book
	quotes       []domain.Quote
	lastDeeplink string

	// WebDAV 连接测试表单状态（仅驻留内存，真实凭据在 Phase 1 接入系统安全存储）
	davURL       string
	davUser      string
	davPass      string
	davFolder    string
	atomicUpload bool
	davStatus    string
	davBusy      bool
}

func newApp() *stillPoetsApp {
	return &stillPoetsApp{
		palette:   theme.XuanPaper(),
		davFolder: webdav.DefaultAppFolder,
		books: []domain.Book{
			{
				ID:          "demo_haizi",
				Title:       "海子诗选（示例）",
				Author:      "海子",
				PoemCount:   42,
				LocalState:  domain.LocalStateRemote,
				RawFilename: "海子诗选_a1b2c3.epub",
			},
			{
				ID:          "demo_gucheng",
				Title:       "黑眼睛（示例）",
				Author:      "顾城",
				PoemCount:   36,
				LocalState:  domain.LocalStateDownloaded,
				RawFilename: "黑眼睛_d4e5f6.epub",
			},
		},
		quotes: []domain.Quote{
			{
				ID:         "demo_q1",
				Content:    "从明天起，做一个幸福的人。喂马、劈柴，周游世界。",
				Author:     "海子",
				SourceBook: "面朝大海，春暖花开",
			},
		},
	}
}

func (a *stillPoetsApp) shelfView(c *ui.Context) {
	p := a.palette
	c.Root().Background(p.Background)

	listPanel := func() {
		ui.Scroll(c.Key("shelf-scroll")).Fill().Children(func() {
			ui.Column(c).Padding(20).Gap(12).Children(func() {
				ui.Text(c, "诗集书架").FontSize(24).Bold().TextColor(p.TextPrimary)
				ui.Text(c, "元数据自动同步 · 点击云端诗集后按需下载").FontSize(13).TextColor(p.TextMuted)
				if a.lastDeeplink != "" {
					ui.Textf(c, "最近 Deeplink：%s", a.lastDeeplink).FontSize(12).TextColor(p.Accent)
				}
				for i, b := range a.books {
					stateLabel := "云端 · 点击下载"
					stateColor := p.TextMuted
					if b.LocalState == domain.LocalStateDownloaded {
						stateLabel = "已离线缓存"
						stateColor = p.Accent
					}
					card := ui.Column(c.Key(b.ID)).
						Padding(16).
						Gap(6).
						Radius(12).
						Background(p.Surface).
						Border(1, p.Border)
					card.Children(func() {
						ui.Row(c).AlignItems(ui.Center).Children(func() {
							ui.Text(c, b.Title).FontSize(17).Bold().TextColor(p.TextPrimary).Grow(1)
							ui.Text(c, stateLabel).FontSize(12).TextColor(stateColor)
						})
						ui.Textf(c, "%s · %d 首", b.Author, b.PoemCount).FontSize(13).TextColor(p.TextMuted)
					})
					if card.Clicked() {
						a.selectedBook = i
					}
				}
			})
		})
	}

	detailPanel := func() {
		ui.Scroll(c.Key("tablet-reader-preview")).Fill().Children(func() {
			ui.Column(c).Padding(32).Gap(16).MaxWidth(layout.ReadingMaxWidthDIP).Children(func() {
				if a.selectedBook >= 0 && a.selectedBook < len(a.books) {
					b := a.books[a.selectedBook]
					ui.Text(c, b.Title).FontSize(26).Bold().TextColor(p.TextPrimary)
					ui.Textf(c, "作者：%s · 远端原文件：%s", b.Author, b.RawFilename).FontSize(14).TextColor(p.TextMuted)
					ui.Divider(c)
					ui.Text(c, "平板宽屏双栏阅读区已就绪（Phase 1 接入 EPUB 解析与按需下载阅读器）。").
						FontSize(16).
						LineHeight(1.8).
						TextColor(p.TextPrimary)
				}
			})
		})
	}

	layout.MasterDetail(c, 320, listPanel, detailPanel)
}

func (a *stillPoetsApp) quotesView(c *ui.Context) {
	p := a.palette
	c.Root().Background(p.Background)

	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).Padding(20).Gap(12).MaxWidth(layout.ReadingMaxWidthDIP).Children(func() {
			ui.Text(c, "独立句集").FontSize(24).Bold().TextColor(p.TextPrimary)
			ui.Text(c, "入口 B · 散装金句与读书摘录，支持按出处筛选").FontSize(13).TextColor(p.TextMuted)
			for _, q := range a.quotes {
				ui.Column(c.Key(q.ID)).
					Padding(18).
					Gap(10).
					Radius(14).
					Background(p.Surface).
					Border(1, p.Border).
					Children(func() {
						ui.Text(c, q.Content).FontSize(17).LineHeight(1.8).TextColor(p.TextPrimary)
						ui.Textf(c, "—— %s《%s》", q.Author, q.SourceBook).FontSize(13).TextColor(p.TextMuted)
					})
			}
		})
	})
}

func (a *stillPoetsApp) settingsView(c *ui.Context) {
	p := a.palette
	c.Root().Background(p.Background)

	a.mu.Lock()
	status := a.davStatus
	busy := a.davBusy
	a.mu.Unlock()

	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).Padding(20).Gap(12).MaxWidth(layout.ReadingMaxWidthDIP).Children(func() {
			ui.Text(c, "WebDAV 与设置").FontSize(24).Bold().TextColor(p.TextPrimary)
			ui.Text(c, "默认在输入的 WebDAV 路径下自动创建专属目录").FontSize(13).TextColor(p.TextMuted)

			ui.TextInput(c.Key("dav-url"), &a.davURL).Placeholder("WebDAV 地址（https://...）").Label("服务器地址")
			ui.TextInput(c.Key("dav-user"), &a.davUser).Placeholder("用户名").Label("用户名")
			ui.TextInput(c.Key("dav-pass"), &a.davPass).Password().Placeholder("应用密码").Label("密码")
			ui.TextInput(c.Key("dav-folder"), &a.davFolder).Placeholder("StillPoets").Label("专属目录名")

			ui.Row(c.Key("atomic-row")).Padding(12, 16).AlignItems(ui.Center).Background(p.Surface).Radius(12).Children(func() {
				ui.Text(c, "原子上传（AList/115 不兼容时建议关闭）").Grow(1).TextColor(p.TextPrimary)
				ui.Switch(c, &a.atomicUpload).Label("原子上传")
			})

			btnLabel := "验证连接并初始化目录"
			if busy {
				btnLabel = "正在连接..."
			}
			if ui.PrimaryButton(c, btnLabel).Clicked() && !busy {
				a.testWebDAV()
			}
			if status != "" {
				ui.Text(c, status).FontSize(13).TextColor(p.Accent)
			}
		})
	})
}

// testWebDAV 在后台 goroutine 验证 WebDAV 连通性并初始化 StillPoets/ 目录树。
func (a *stillPoetsApp) testWebDAV() {
	cfg := webdav.Config{
		BaseURL:      a.davURL,
		Username:     a.davUser,
		Password:     a.davPass,
		AppFolder:    a.davFolder,
		AtomicUpload: a.atomicUpload,
		Timeout:      15 * time.Second,
	}

	a.mu.Lock()
	a.davBusy = true
	a.davStatus = "正在验证连接并检查目录..."
	a.mu.Unlock()
	a.settingsWin.Update(func() {})

	go func() {
		msg := ""
		client, err := webdav.NewClient(cfg)
		if err != nil {
			msg = fmt.Sprintf("配置无效：%v", err)
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			res, err := spSync.EnsureWorkspace(ctx, client)
			if err != nil {
				msg = fmt.Sprintf("连接失败：%v", err)
			} else if res.ExistingRemote {
				msg = fmt.Sprintf("连接成功！检测到已有云端书库（目录：%s/）", client.AppFolder())
			} else {
				msg = fmt.Sprintf("连接成功！已自动创建专属目录 %s/", client.AppFolder())
			}
		}

		a.mu.Lock()
		a.davBusy = false
		a.davStatus = msg
		a.mu.Unlock()
		a.settingsWin.Update(func() {})
	}()
}

func main() {
	app := newApp()

	mygo.App.OnOpenURL(func(rawURL string) {
		app.lastDeeplink = rawURL
		if app.shelfWin != nil {
			app.shelfWin.Update(func() {})
		}
	})

	mygo.App.WhenReady(func() {
		app.shelfWin = mygo.NewWindow(mygo.WindowOptions{
			Title:   "诗集",
			Hidden:  true,
			Content: ui.View(app.shelfView),
		})
		app.quotesWin = mygo.NewWindow(mygo.WindowOptions{
			Title:   "句集",
			Hidden:  true,
			Content: ui.View(app.quotesView),
		})
		app.settingsWin = mygo.NewWindow(mygo.WindowOptions{
			Title:   "设置",
			Hidden:  true,
			Content: ui.View(app.settingsView),
		})

		var err error
		app.tabs, err = mygo.NewTabs(
			mygo.TabItem{Title: "诗集", Symbol: "book.closed", Window: app.shelfWin},
			mygo.TabItem{Title: "句集", Symbol: "quote.opening", Window: app.quotesWin},
			mygo.TabItem{Title: "设置", Symbol: "gearshape", Window: app.settingsWin},
		)
		if err != nil {
			log.Fatal(err)
		}
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
