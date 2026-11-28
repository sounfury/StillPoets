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

		// 5. 二级页：关于 StillPoets（含发现新版本 + 更新日志三级入口 + 邮件联系 + 导出日志状态）
		appUI.releasesLoaded = true
		appUI.releases = []app.ReleaseInfo{
			{
				TagName:     "v0.2.0",
				Name:        "StillPoets v0.2.0",
				Body:        "## StillPoets v0.2.0\n\n### 安装包说明\n- **Android (APK)**：内置桌面小组件（`StillPoetsWidgetProvider`）\n- **iOS (IPA)**：内嵌 WidgetKit 小组件（`StillPoetsWidget.appex`）\n\n### 本次核心更新\n- 全新设置页二级导航与字体字重管理\n- WebDAV 同步等待动画",
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

		// 6. 三级底部抽屉：更新日志（Markdown 渲染）
		appUI.openChangelogSheet()
		if !appUI.showChangelogModal {
			t.Fatalf("%s expected changelog sheet open", vp.name)
		}
		img = ui.Render(appUI.Render, vp.w, vp.h, 2.0)
		if img == nil {
			t.Fatalf("%s changelog sheet render failed", vp.name)
		}
		if vp.name == "phone" {
			maybeSavePNG(t, img, "settings-phone-sheet-changelog")
		}

		// 返回主页校验（同时关闭三级抽屉）
		appUI.closeProfileSubpage()
		if appUI.profileSub != profileSubpageNone {
			t.Fatalf("expected profileSub reset to None after close")
		}
		if appUI.showChangelogModal {
			t.Fatalf("expected changelog sheet closed after closeProfileSubpage")
		}
	}
}

func TestNormalizeReleaseMarkdown(t *testing.T) {
	rel := app.ReleaseInfo{
		TagName: "v0.1.2",
		Name:    "StillPoets v0.1.2",
		Body:    "## StillPoets v0.1.2\r\n\r\n### 安装包说明\r\n- **iOS**：`StillPoetsWidget.appex`",
	}
	got := normalizeReleaseMarkdown(rel)
	want := "### 安装包说明\n- **iOS**：`StillPoetsWidget.appex`"
	if got != want {
		t.Fatalf("normalizeReleaseMarkdown mismatch:\ngot:  %q\nwant: %q", got, want)
	}

	other := app.ReleaseInfo{
		TagName: "v0.1.2",
		Name:    "StillPoets v0.1.2",
		Body:    "### 独立更新说明\n- 修复若干问题",
	}
	if got := normalizeReleaseMarkdown(other); got != other.Body {
		t.Fatalf("unexpected strip for non-duplicate heading: got %q", got)
	}
}

func TestProfileSubpageTopPaddingTypographyAndChangelogSheet(t *testing.T) {
	ctx := t.Context()
	svc, err := app.Open(ctx, t.TempDir(), app.NewMemorySecretStore())
	if err != nil {
		t.Fatalf("app.Open: %v", err)
	}
	defer svc.Close()

	appUI := NewAppUI(svc, nil, nil)
	appUI.SetAppVersion("0.1.2")
	appUI.activeTab = components.TabProfile
	appUI.reloadSync(ctx)
	appUI.releasesLoaded = true
	appUI.releases = []app.ReleaseInfo{
		{
			TagName:     "v0.1.7",
			Name:        "StillPoets v0.1.7",
			Body:        "## StillPoets v0.1.7\n\n### 本次核心更新\n- **完整统计看板**：完读墙、热力图与诗人云。",
			HTMLURL:     "https://github.com/sounfury/StillPoets/releases/tag/v0.1.7",
			PublishedAt: time.Date(2026, 11, 25, 13, 40, 0, 0, time.UTC),
			Assets: []app.ReleaseAsset{
				{Name: "StillPoets-0.1.7-ios-arm64-unsigned.ipa", Size: 9271890},
				{Name: "StillPoets-0.1.7-android-arm64.apk", Size: 15800000},
			},
		},
		{
			TagName:     "v0.1.6",
			Name:        "StillPoets v0.1.6",
			Body:        "- 同步自动巡检远端缺失诗集",
			HTMLURL:     "https://github.com/sounfury/StillPoets/releases/tag/v0.1.6",
			PublishedAt: time.Date(2026, 11, 24, 20, 0, 0, 0, time.UTC),
		},
		{
			TagName:     "v0.1.5",
			Name:        "StillPoets v0.1.5",
			Body:        "- 死亡诗社默认文案与排版预览",
			HTMLURL:     "https://github.com/sounfury/StillPoets/releases/tag/v0.1.5",
			PublishedAt: time.Date(2026, 11, 24, 16, 0, 0, 0, time.UTC),
		},
		{
			TagName:     "v0.1.4",
			Name:        "StillPoets v0.1.4",
			Body:        "- Android 应用内自动更新",
			HTMLURL:     "https://github.com/sounfury/StillPoets/releases/tag/v0.1.4",
			PublishedAt: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC),
		},
		{
			TagName:     "v0.1.3",
			Name:        "StillPoets v0.1.3",
			Body:        "- WebDAV 同步链路提速与 Markdown 更新日志抽屉",
			HTMLURL:     "https://github.com/sounfury/StillPoets/releases/tag/v0.1.3",
			PublishedAt: time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC),
		},
		{
			TagName:     "v0.1.2",
			Name:        "StillPoets v0.1.2",
			Body:        "- 应被截断的第六条历史版本",
			HTMLURL:     "https://github.com/sounfury/StillPoets/releases/tag/v0.1.2",
			PublishedAt: time.Date(2026, 11, 24, 8, 35, 0, 0, time.UTC),
		},
		{
			TagName:     "v0.1.1",
			Name:        "StillPoets v0.1.1",
			Body:        "- 应被截断的第七条历史版本",
			HTMLURL:     "https://github.com/sounfury/StillPoets/releases/tag/v0.1.1",
			PublishedAt: time.Date(2026, 11, 23, 8, 35, 0, 0, time.UTC),
		},
	}

	tt := ui.NewTester(appUI.Render, 390, 844)
	tt.Frame()
	advanceFrames(tt, 300*time.Millisecond)

	// 1. 「我的」主页不应直接暴露字号/行距滑块（防止滚动误触）
	if _, ok := tt.Find("正文字号"); ok {
		t.Fatalf("profile main page should not expose font size slider directly")
	}
	if _, ok := tt.Find("行距倍数"); ok {
		t.Fatalf("profile main page should not expose line height slider directly")
	}

	// 2. 进入「阅读排版与字体」二级页：顶部不叠加冗余空白，且包含字号与行距调节
	appUI.openProfileSubpage(profileSubpageFonts)
	tt.Frame()
	advanceFrames(tt, 450*time.Millisecond)
	if rect, ok := tt.Find("返回"); !ok {
		t.Fatalf("expected typography subpage back button")
	} else if rect.Y > 36 {
		t.Fatalf("subpage header top offset too large (possible double safe-area): y=%.1f", rect.Y)
	}
	if _, ok := tt.Find("+ 导入字体"); !ok {
		t.Fatalf("expected typography subpage import font action")
	}
	if _, ok := tt.Find("正文字号"); !ok {
		t.Fatalf("expected font size slider inside typography subpage")
	}
	if _, ok := tt.Find("行距倍数"); !ok {
		t.Fatalf("expected line height slider inside typography subpage")
	}
	maybeSavePNG(t, tt.Image(), "settings-phone-sub-fonts-settled")

	// 3. 进入「关于 StillPoets」二级页，点击「更新日志」打开三级底部抽屉
	appUI.openProfileSubpage(profileSubpageAbout)
	tt.Frame()
	advanceFrames(tt, 450*time.Millisecond)
	if rect, ok := tt.Find("Releases"); !ok {
		t.Fatalf("expected about subpage Releases button")
	} else if rect.Y > 36 {
		t.Fatalf("about subpage header top offset too large: y=%.1f", rect.Y)
	}
	if err := tt.Click("更新日志"); err != nil {
		t.Fatalf("click changelog entry: %v", err)
	}
	advanceFrames(tt, 600*time.Millisecond)
	if !appUI.showChangelogModal {
		t.Fatalf("expected changelog drawer open after clicking entry")
	}
	if _, ok := tt.Find("GitHub Releases 最新前 5 条版本记录"); !ok {
		t.Fatalf("expected changelog drawer subtitle rendered")
	}
	if _, ok := tt.Find("StillPoets v0.1.7"); !ok {
		t.Fatalf("expected newest release v0.1.7 rendered in changelog drawer")
	}
	if _, ok := tt.Find("StillPoets v0.1.2"); ok {
		t.Fatalf("expected 6th release v0.1.2 NOT rendered inside changelog drawer")
	}
	if _, ok := tt.Find("StillPoets v0.1.1"); ok {
		t.Fatalf("expected 7th release v0.1.1 NOT rendered inside changelog drawer")
	}
	if _, ok := tt.Find("前往 GitHub 查看更多历史更新 ↗"); !ok {
		t.Fatalf("expected external link button for viewing more releases")
	}
	maybeSavePNG(t, tt.Image(), "settings-phone-sheet-changelog-settled")
}
