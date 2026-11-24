package views

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
	"stillpoets/internal/domain"
	"stillpoets/internal/ui/components"
)

func TestProfileRedesignSubpagesSyncSpinnerAndAbout(t *testing.T) {
	ctx := t.Context()
	svc, err := app.Open(ctx, t.TempDir(), app.NewMemorySecretStore())
	if err != nil {
		t.Fatalf("app.Open: %v", err)
	}
	defer svc.Close()

	// 准备一款本地自定义字体用于渲染「字体与字重」二级页
	srcFont := filepath.Join(t.TempDir(), "LXGWWenKai.ttf")
	if err := os.WriteFile(srcFont, []byte("mock-font-data"), 0o600); err != nil {
		t.Fatalf("write mock font: %v", err)
	}
	if _, _, err := svc.ImportFont(srcFont); err != nil {
		t.Fatalf("ImportFont: %v", err)
	}
	if _, err := svc.UpdateSettings(ctx, func(st *domain.Settings) {
		st.FontWeight = domain.FontWeightMedium
	}); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}

	appUI := NewAppUI(svc, nil, nil)
	appUI.SetAppVersion("0.1.0")
	appUI.activeTab = components.TabProfile
	appUI.reloadSync(ctx)

	// 模拟已连接 WebDAV + 同步进行中等待态（验证按钮 Spinner + 卡片内联进度条 + 底部加载胶囊）
	appUI.syncOverview.Configured = true
	appUI.syncOverview.LocalRev = 18
	appUI.syncOverview.LastSyncAt = time.Now().UnixMilli()
	appUI.syncOverview.RemoteDevices = []string{"dev-a8f3c2"}
	appUI.davURL = "https://dav.jianguoyun.com/dav/"
	appUI.handleSyncToast(app.SyncToastState{
		Phase:   app.SyncToastSyncing,
		Message: "正在拉取并合并远端快照…",
	})

	viewports := []struct {
		name string
		w, h int
	}{
		{"phone", 390, 844},
		{"tablet", 1080, 720},
	}

	for _, vp := range viewports {
		// 1. 「我的」主页（含同步等待动画）
		appUI.profileSub = profileSubpageNone
		img := ui.Render(appUI.Render, vp.w, vp.h, 2.0)
		if img == nil || img.Bounds().Dx() != vp.w*2 {
			t.Fatalf("%s profile home render failed", vp.name)
		}
		if vp.name == "phone" {
			maybeSavePNG(t, img, "settings-phone-home-syncing")
		}

		// 2. 二级页：WebDAV 配置与设备（含保存等待动画与设备退役态）
		appUI.openProfileSubpage(profileSubpageWebDAV)
		appUI.davBusy = true
		appUI.davRetiringID = "dev-a8f3c2"
		img = ui.Render(appUI.Render, vp.w, vp.h, 2.0)
		if img == nil {
			t.Fatalf("%s webdav subpage render failed", vp.name)
		}
		if vp.name == "phone" {
			maybeSavePNG(t, img, "settings-phone-sub-webdav")
		}
		appUI.davBusy = false
		appUI.davRetiringID = ""

		// 3. 二级页：字体与字重（含 4 档字重、已上传字体与删除确认行）
		appUI.openProfileSubpage(profileSubpageFonts)
		appUI.confirmDeleteFont = "LXGWWenKai.ttf"
		img = ui.Render(appUI.Render, vp.w, vp.h, 2.0)
		if img == nil {
			t.Fatalf("%s fonts subpage render failed", vp.name)
		}
		if vp.name == "phone" {
			maybeSavePNG(t, img, "settings-phone-sub-fonts")
		}
		appUI.confirmDeleteFont = ""

		// 4. 二级页：桌面小组件
		appUI.openProfileSubpage(profileSubpageWidget)
		if img := ui.Render(appUI.Render, vp.w, vp.h, 1.0); img == nil {
			t.Fatalf("%s widget subpage render failed", vp.name)
		}

		// 5. 二级页：关于 StillPoets（含发现新版本 + 更新日志列表 + 邮件联系 + 导出日志状态）
		appUI.releasesLoaded = true
		appUI.releases = []app.ReleaseInfo{
			{
				TagName:     "v0.2.0",
				Name:        "StillPoets v0.2.0",
				Body:        "- 全新设置页二级导航与字体字重管理\n- WebDAV 同步等待动画\n- 关于页支持检查更新与分析日志导出",
				HTMLURL:     "https://github.com/sounfury/StillPoets/releases/tag/v0.2.0",
				PublishedAt: time.Now(),
				Assets: []app.ReleaseAsset{
					{Name: "StillPoets-0.2.0-android-arm64.apk", Size: 16 << 20},
				},
			},
		}
		appUI.updateResult = &app.UpdateCheckResult{
			CurrentVersion: "0.1.0",
			Latest:         &appUI.releases[0],
			HasUpdate:      true,
		}
		appUI.exportLogStatus = "已导出 StillPoets-diagnostic-20261124-120000.log（4.2 KB）"
		appUI.openProfileSubpage(profileSubpageAbout)
		img = ui.Render(appUI.Render, vp.w, vp.h, 2.0)
		if img == nil {
			t.Fatalf("%s about subpage render failed", vp.name)
		}
		if vp.name == "phone" {
			maybeSavePNG(t, img, "settings-phone-sub-about")
		}

		// 返回主页校验
		appUI.closeProfileSubpage()
		if appUI.profileSub != profileSubpageNone {
			t.Fatalf("expected profileSub reset to None after close")
		}
	}
}
