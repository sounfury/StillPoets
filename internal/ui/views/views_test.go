package views

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/mobile"

	"stillpoets/internal/app"
	"stillpoets/internal/domain"
	"stillpoets/internal/storage"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/theme"
)

func maybeSavePNG(t *testing.T, img *image.RGBA, name string) {
	t.Helper()
	dir := os.Getenv("STILLPOETS_PREVIEW_DIR")
	if dir == "" || img == nil {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir preview dir: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatalf("create preview %s: %v", name, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode preview %s: %v", name, err)
	}
}

func TestRenderPhoneAndTabletAllTabsAndThemes(t *testing.T) {
	ctx := t.Context()
	svc, err := app.Open(ctx, t.TempDir(), app.NewMemorySecretStore())
	if err != nil {
		t.Fatalf("app.Open: %v", err)
	}
	defer svc.Close()

	// 导入 3 本真实诗集 + 建分组 + 写散页 + 记录续读，让预览和渲染测试更贴近真实使用场景
	matches, err := filepath.Glob(filepath.Join("..", "..", "..", "test-books", "*.epub"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no test EPUB found: %v", err)
	}
	grp1, err := svc.CreateGroup(ctx, "现代诗")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	grp2, err := svc.CreateGroup(ctx, "外国诗")
	if err != nil {
		t.Fatalf("CreateGroup 2: %v", err)
	}
	var firstBookID string
	for i, epub := range matches {
		if i >= 3 {
			break
		}
		gid := grp1.ID
		if i > 0 {
			gid = grp2.ID
		}
		imp, err := svc.ImportEPUB(ctx, epub, gid)
		if err != nil {
			t.Fatalf("ImportEPUB %s: %v", epub, err)
		}
		if i == 0 {
			firstBookID = imp.Book.ID
		}
	}
	book0, err := svc.GetBook(ctx, firstBookID)
	if err != nil {
		t.Fatalf("GetBook: %v", err)
	}
	poems, err := svc.ListPoems(ctx, book0.ID)
	if err != nil || len(poems) == 0 {
		t.Fatalf("ListPoems: len=%d err=%v", len(poems), err)
	}
	targetPoem := poems[0]
	for _, p := range poems {
		if p.IsPoem() && len(p.Stanzas) > 0 {
			targetPoem = p
			break
		}
	}
	_, _ = svc.RecordPoemRead(ctx, book0.ID, targetPoem.ID, 0, book0.PoemCount)
	_, _ = svc.CreatePoemExcerptQuote(ctx, book0, targetPoem, "那里，一切皆有序而美，\n奢华、安宁，\n与欢愉。", "1-3")
	_, _ = svc.CreatePoemExcerptQuote(ctx, book0, targetPoem, "我以我本来的样子存在，\n这便足够。", "4-5")
	_, _ = svc.CreateStandaloneQuote(ctx, "不急着读完，\n给一句诗多一点时间。", "随记", "我的笔记")

	appUI := NewAppUI(svc, nil, nil)
	appUI.openBook(book0.ID, targetPoem.ID, app.ParseLineRanges("1-2"), false)
	appUI.reloadSync(ctx)

	viewports := []struct {
		name string
		w, h int
	}{
		{"phone", 390, 844},
		{"tablet", 1080, 720},
	}

	for _, preset := range theme.Presets() {
		appUI.settings.ThemePreset = string(preset.ID)
		for _, vp := range viewports {
			// 书架 · 诗集
			appUI.activeTab = components.TabShelf
			appUI.shelfSubTab = components.SubTabBooks
			appUI.readerMobile = false
			img := ui.Render(appUI.Render, vp.w, vp.h, 2.0)
			if img == nil || img.Bounds().Dx() != vp.w*2 {
				t.Fatalf("%s/%s books render failed", preset.ID, vp.name)
			}
			if preset.ID == theme.PresetArtbook || (preset.ID == theme.PresetEditorial && vp.name == "phone") {
				maybeSavePNG(t, img, string(preset.ID)+"-"+vp.name+"-books")
			}

			// 书架 · 散页
			appUI.shelfSubTab = components.SubTabQuotes
			qimg := ui.Render(appUI.Render, vp.w, vp.h, 2.0)
			if qimg == nil {
				t.Fatalf("%s/%s quotes render failed", preset.ID, vp.name)
			}
			if preset.ID == theme.PresetArtbook && vp.name == "phone" {
				maybeSavePNG(t, qimg, "artbook-phone-quotes")
			}

			// 手机全屏阅读器 / 平板分栏阅读器
			appUI.shelfSubTab = components.SubTabBooks
			appUI.readerMobile = vp.name == "phone"
			appUI.selectingLines = true
			appUI.selectedLines[1] = true
			if img := ui.Render(appUI.Render, vp.w, vp.h, 1.0); img == nil {
				t.Fatalf("%s/%s reader render failed", preset.ID, vp.name)
			}
			appUI.selectingLines = false
			appUI.selectedLines = map[int]bool{}
			appUI.readerMobile = false

			// 统计
			appUI.activeTab = components.TabStats
			if img := ui.Render(appUI.Render, vp.w, vp.h, 1.0); img == nil {
				t.Fatalf("%s/%s stats render failed", preset.ID, vp.name)
			}

			// 我的
			appUI.activeTab = components.TabProfile
			if img := ui.Render(appUI.Render, vp.w, vp.h, 1.0); img == nil {
				t.Fatalf("%s/%s profile render failed", preset.ID, vp.name)
			}
		}
	}
}

func advanceFrames(tt *ui.Tester, d time.Duration) {
	steps := int(d / (16 * time.Millisecond))
	for range steps {
		tt.Advance(16 * time.Millisecond)
	}
}

func swipeFinger(tt *ui.Tester, x0, y0, x1, y1 float32) {
	tt.FingerDown(x0, y0)
	tt.Advance(16 * time.Millisecond)
	const steps = 8
	for i := 1; i <= steps; i++ {
		f := float32(i) / float32(steps)
		tt.FingerMove(x0+(x1-x0)*f, y0+(y1-y0)*f)
		tt.Frame()
		tt.Advance(16 * time.Millisecond)
	}
	tt.FingerUp(x1, y1)
	advanceFrames(tt, 600*time.Millisecond)
}

func newGestureTestAppUI(t *testing.T) *AppUI {
	t.Helper()
	ctx := t.Context()
	svc, err := app.Open(ctx, t.TempDir(), app.NewMemorySecretStore())
	if err != nil {
		t.Fatalf("app.Open: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })

	matches, err := filepath.Glob(filepath.Join("..", "..", "..", "test-books", "*.epub"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no test EPUB found: %v", err)
	}
	imp, err := svc.ImportEPUB(ctx, matches[0], "")
	if err != nil {
		t.Fatalf("ImportEPUB: %v", err)
	}
	_, _ = svc.CreateStandaloneQuote(ctx, "给一句诗多一点时间。", "测试诗人", "测试摘录")

	appUI := NewAppUI(svc, nil, nil)
	// 等待 NewAppUI 内部的 autoSelectInitialBook 异步加载完成，避免测试中产生竞态覆盖
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		appUI.mu.Lock()
		ready := len(appUI.activePoems) > 0
		appUI.mu.Unlock()
		if ready {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	poems, err := svc.ListPoems(ctx, imp.Book.ID)
	if err != nil || len(poems) < 3 {
		t.Fatalf("ListPoems expected >=3 poems, got %d, err=%v", len(poems), err)
	}
	appUI.mu.Lock()
	appUI.activeBookID = imp.Book.ID
	appUI.activePoems = poems
	appUI.activePoemIdx = 0
	appUI.mu.Unlock()
	return appUI
}

func TestReaderHorizontalSwipeAndConflicts(t *testing.T) {
	appUI := newGestureTestAppUI(t)
	appUI.activeTab = components.TabShelf
	appUI.shelfSubTab = components.SubTabBooks
	appUI.readerMobile = true

	tt := ui.NewTester(appUI.Render, 390, 844)
	tt.Frame()
	tt.Frame()

	// 1. 左滑切到下一首诗（0 -> 1）
	swipeFinger(tt, 330, 420, 60, 420)
	if appUI.activePoemIdx != 1 {
		t.Fatalf("expected swipe left to advance poem to 1, got %d", appUI.activePoemIdx)
	}

	// 2. 纵向滚动（上下滑）不应误切诗
	swipeFinger(tt, 195, 560, 195, 220)
	if appUI.activePoemIdx != 1 {
		t.Fatalf("expected vertical scroll not to change poem index, got %d", appUI.activePoemIdx)
	}

	// 3. 划选诗行模式下横向滑动应锁定不切诗
	appUI.selectingLines = true
	tt.Frame()
	swipeFinger(tt, 330, 420, 60, 420)
	if appUI.activePoemIdx != 1 {
		t.Fatalf("expected selectingLines to lock horizontal swipe, got %d", appUI.activePoemIdx)
	}
	appUI.selectingLines = false
	tt.Frame()

	// 4. 退出划选后右滑切回上一首诗（1 -> 0）
	swipeFinger(tt, 60, 420, 330, 420)
	if appUI.activePoemIdx != 0 {
		t.Fatalf("expected swipe right to go back to poem 0, got %d", appUI.activePoemIdx)
	}

	// 5. 第 0 首继续右拉不越界
	swipeFinger(tt, 60, 420, 330, 420)
	if appUI.activePoemIdx != 0 {
		t.Fatalf("expected boundary rubberband to stay at 0, got %d", appUI.activePoemIdx)
	}
}

func TestShelfSubTabSwipeGesturePhoneAndTablet(t *testing.T) {
	t.Run("phone", func(t *testing.T) {
		appUI := newGestureTestAppUI(t)
		appUI.activeTab = components.TabShelf
		appUI.shelfSubTab = components.SubTabBooks
		appUI.readerMobile = false

		tt := ui.NewTester(appUI.Render, 390, 844)
		tt.Frame()
		tt.Frame()

		// 手机书架向左滑 -> 切到「散页」
		swipeFinger(tt, 330, 500, 50, 500)
		if appUI.shelfSubTab != components.SubTabQuotes {
			t.Fatalf("expected phone swipe left to switch to SubTabQuotes, got %v", appUI.shelfSubTab)
		}

		// 散页向右滑 -> 切回「诗集」
		swipeFinger(tt, 50, 500, 330, 500)
		if appUI.shelfSubTab != components.SubTabBooks {
			t.Fatalf("expected phone swipe right to switch back to SubTabBooks, got %v", appUI.shelfSubTab)
		}
	})

	t.Run("tablet_isolation_and_switch", func(t *testing.T) {
		appUI := newGestureTestAppUI(t)
		appUI.activeTab = components.TabShelf
		appUI.shelfSubTab = components.SubTabBooks
		appUI.readerMobile = false

		// 平板横屏：左侧导航轨 72 + 左书架 348 = 420，右侧阅读器 x ∈ [420, 1080]
		tt := ui.NewTester(appUI.Render, 1080, 720)
		tt.Frame()
		tt.Frame()

		// 1. 在右栏阅读器左滑：只切诗（0 -> 1），不误切到「散页」
		swipeFinger(tt, 960, 380, 540, 380)
		if appUI.activePoemIdx != 1 {
			t.Fatalf("expected tablet right pane swipe left to advance poem to 1, got %d", appUI.activePoemIdx)
		}
		if appUI.shelfSubTab != components.SubTabBooks {
			t.Fatalf("expected tablet right pane swipe not to switch shelfSubTab, got %v", appUI.shelfSubTab)
		}

		// 2. 右栏处于划选模式时左滑：既不切诗，也不透传切到「散页」
		appUI.selectingLines = true
		tt.Frame()
		swipeFinger(tt, 960, 380, 540, 380)
		if appUI.activePoemIdx != 1 || appUI.shelfSubTab != components.SubTabBooks {
			t.Fatalf("expected selectingLines lock on tablet, got idx=%d subTab=%v", appUI.activePoemIdx, appUI.shelfSubTab)
		}
		appUI.selectingLines = false
		tt.Frame()

		// 3. 在左栏书架区（x ∈ [72, 420]）左滑：切到全宽「散页」
		swipeFinger(tt, 380, 480, 100, 480)
		if appUI.shelfSubTab != components.SubTabQuotes {
			t.Fatalf("expected tablet left shelf swipe left to switch to SubTabQuotes, got %v (pager=%+v)", appUI.shelfSubTab, appUI.shelfPager)
		}

		// 4. 在全宽散页区右滑：切回「诗集 + 阅读器」双栏
		swipeFinger(tt, 220, 420, 820, 420)
		if appUI.shelfSubTab != components.SubTabBooks {
			t.Fatalf("expected tablet quotes swipe right to switch back to SubTabBooks, got %v", appUI.shelfSubTab)
		}
	})
}

func TestAnimatedOverlaysAndNavigationTransitions(t *testing.T) {
	appUI := newGestureTestAppUI(t)
	tt := ui.NewTester(appUI.Render, 1080, 720)
	tt.Frame()

	// 依次验证主导航切换动画、底部手势抽屉（新增/目录/排版）与居中动效弹层（搜索/散页编辑）
	for _, tab := range []components.NavTab{components.TabStats, components.TabProfile, components.TabShelf} {
		appUI.activeTab = tab
		tt.Frame()
		advanceFrames(tt, 260*time.Millisecond)
	}

	appUI.showAddModal = true
	tt.Frame()
	advanceFrames(tt, 360*time.Millisecond)
	appUI.showAddModal = false
	tt.Frame()
	advanceFrames(tt, 360*time.Millisecond)

	appUI.showTOCModal = true
	tt.Frame()
	advanceFrames(tt, 360*time.Millisecond)
	appUI.showTOCModal = false
	tt.Frame()
	advanceFrames(tt, 360*time.Millisecond)

	appUI.showTypoModal = true
	tt.Frame()
	advanceFrames(tt, 360*time.Millisecond)
	appUI.showTypoModal = false
	tt.Frame()
	advanceFrames(tt, 360*time.Millisecond)

	appUI.showSearchModal = true
	tt.Frame()
	advanceFrames(tt, 260*time.Millisecond)
	// 点击左上角遮罩外部应关闭搜索弹层
	tt.FingerDown(20, 20)
	tt.FingerUp(20, 20)
	tt.Frame()
	if appUI.showSearchModal {
		t.Fatalf("expected clicking backdrop to close AnimatedDialog")
	}
	advanceFrames(tt, 260*time.Millisecond)

	// 验证 Toast 浮现与自动退场
	appUI.showTransientToast("云同步完成")
	tt.Frame()
	advanceFrames(tt, 320*time.Millisecond)
	appUI.mu.Lock()
	appUI.toastMessage = ""
	appUI.mu.Unlock()
	tt.Frame()
	advanceFrames(tt, 240*time.Millisecond)

	// 验证手机全屏阅读器推入与退出动画
	_ = domain.LocalStateDownloaded
	phoneTT := ui.NewTester(appUI.Render, 390, 844)
	phoneTT.Frame()
	appUI.readerMobile = true
	phoneTT.Frame()
	advanceFrames(phoneTT, 360*time.Millisecond)
	appUI.readerMobile = false
	phoneTT.Frame()
	advanceFrames(phoneTT, 300*time.Millisecond)
}

func TestWidgetDeeplinkNavigation(t *testing.T) {
	appUI := newGestureTestAppUI(t)
	bookID := appUI.activeBookID
	targetPoem := appUI.activePoems[2]

	// 1. 通过 stillpoets://poem/... 唤起指定诗篇并携带高亮诗行
	appUI.HandleDeeplink("stillpoets://poem/" + bookID + "/" + targetPoem.ID + "?lines=1-2&highlight=1")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		appUI.mu.Lock()
		ok := appUI.activePoemIdx == 2 && len(appUI.highlightRanges) == 1 && appUI.readerMobile
		appUI.mu.Unlock()
		if ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	appUI.mu.Lock()
	gotIdx := appUI.activePoemIdx
	gotRanges := len(appUI.highlightRanges)
	gotMobile := appUI.readerMobile
	appUI.mu.Unlock()
	if gotIdx != 2 || gotRanges != 1 || !gotMobile {
		t.Fatalf("poem deeplink failed: idx=%d ranges=%d mobile=%v", gotIdx, gotRanges, gotMobile)
	}

	// 渲染验证含高亮行及「我的」小组件预览页均无 panic
	tt := ui.NewTester(appUI.Render, 1080, 720)
	tt.Frame()
	appUI.activeTab = components.TabProfile
	tt.Frame()

	// 2. 通过 stillpoets://quote/... 唤起独立散页并弹出详情卡片
	quotes, err := appUI.svc.ListQuotes(t.Context(), storage.QuoteFilter{})
	if err != nil || len(quotes) == 0 {
		t.Fatalf("expected standalone quote for deeplink test, err=%v", err)
	}
	quoteID := quotes[0].Quote.ID
	appUI.HandleDeeplink("stillpoets://quote/" + quoteID)
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		appUI.mu.Lock()
		ok := appUI.showQuoteDetail && appUI.detailQuote != nil && appUI.detailQuote.Quote.ID == quoteID
		appUI.mu.Unlock()
		if ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	appUI.mu.Lock()
	opened := appUI.showQuoteDetail && appUI.shelfSubTab == components.SubTabQuotes && appUI.detailQuote != nil && appUI.detailQuote.Quote.ID == quoteID
	appUI.mu.Unlock()
	if !opened {
		t.Fatalf("quote deeplink did not open quote detail modal")
	}
	tt.Frame()
}

func TestReloadSyncSurvivesExpiredContext(t *testing.T) {
	appUI := newGestureTestAppUI(t)
	if len(appUI.books) == 0 || appUI.syncOverview.InstallID == "" {
		t.Fatalf("expected seeded books and installId, got books=%d installId=%q", len(appUI.books), appUI.syncOverview.InstallID)
	}

	// 模拟上游网络同步超时后的已过期 context：reloadSync 必须自动切换新鲜本地上下文且绝不清空书架或安装 ID
	expiredCtx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	appUI.reloadSync(expiredCtx)

	appUI.mu.Lock()
	gotBooks := len(appUI.books)
	gotQuotes := len(appUI.quotes)
	gotInstallID := appUI.syncOverview.InstallID
	appUI.mu.Unlock()

	if gotBooks == 0 {
		t.Fatalf("expected books to remain loaded when reloadSync receives expired context")
	}
	if gotQuotes == 0 {
		t.Fatalf("expected quotes to remain loaded when reloadSync receives expired context")
	}
	if gotInstallID == "" {
		t.Fatalf("expected installId to remain loaded when reloadSync receives expired context")
	}
}

func TestEditorialPaperAfterThemeSwitchOnTablet(t *testing.T) {
	mobile.SetLook(mobile.Material)
	defer mobile.SetLook(mobile.Automatic)
	appUI := newGestureTestAppUI(t)
	appUI.activeTab = components.TabProfile
	var windowBg string
	appUI.SetWindowBgFunc(func(hex string) { windowBg = hex })
	tt := ui.NewTester(appUI.Render, 1080, 720)
	tt.SetInsets(24, 0, 16, 0)
	if err := tt.Click("刊物风"); err != nil {
		t.Fatal(err)
	}
	checkPaper := func(region string, x, y int) {
		t.Helper()
		if got := tt.Image().RGBAAt(x, y); got != (color.RGBA{255, 255, 255, 255}) {
			t.Errorf("%s at (%d,%d) = %v, want opaque white", region, x, y, got)
		}
	}
	advanceFrames(tt, 400*time.Millisecond)
	checkPaper("top safe area", 540, 8)
	checkPaper("profile header", 400, 45)
	checkPaper("profile scroll gutter", 110, 180)
	checkPaper("profile card interior", 914, 110)
	if windowBg != "#FFFFFF" {
		t.Errorf("native window background = %q, want #FFFFFF", windowBg)
	}
	appUI.activeTab = components.TabStats
	tt.Frame()
	advanceFrames(tt, 400*time.Millisecond)
	checkPaper("stats scroll gutter", 80, 180)
	appUI.activeTab = components.TabShelf
	tt.Frame()
	advanceFrames(tt, 400*time.Millisecond)
	checkPaper("shelf gutter", 80, 180)
	checkPaper("reader margin", 1068, 310)
	appUI.selectShelfSubTab(components.SubTabQuotes)
	tt.Frame()
	advanceFrames(tt, 400*time.Millisecond)
	checkPaper("quotes gutter", 80, 180)
}

func TestCupertinoFloatingGlassNavEditorialPaperAndOriginalCover(t *testing.T) {
	mobile.SetLook(mobile.Cupertino)
	defer mobile.SetLook(mobile.Automatic)

	if !components.DefaultFloatingBottomNav() {
		t.Fatalf("expected Cupertino look to default to floating bottom nav")
	}

	appUI := newGestureTestAppUI(t)
	var syncedBg string
	appUI.SetWindowBgFunc(func(hex string) { syncedBg = hex })

	// 1. 刊物风纸白主题 + iOS 液态玻璃悬浮底栏（默认开启）+ 开启原书封面
	if !appUI.useFloatingBottomNav() {
		t.Fatalf("expected floating bottom nav enabled by default on Cupertino")
	}
	appUI.settings.ThemePreset = string(theme.PresetEditorial)
	appUI.settings.UseOriginalCover = true
	appUI.settings.WidgetRefreshMinutes = 180
	appUI.activeTab = components.TabShelf
	appUI.shelfSubTab = components.SubTabBooks

	// 首次渲染触发原书封面异步解码，等待解码完成后再渲染最终画面
	_ = ui.Render(appUI.Render, 390, 844, 2.0)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		appUI.uiThreadMu.Lock()
		fetching := len(appUI.coverFetching)
		appUI.uiThreadMu.Unlock()
		if fetching == 0 {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}

	imgBooks := ui.Render(appUI.Render, 390, 844, 2.0)
	if imgBooks == nil {
		t.Fatalf("expected non-nil Editorial Cupertino books render")
	}
	if syncedBg != "#FFFFFF" {
		t.Fatalf("expected native window bg synced to #FFFFFF, got %q", syncedBg)
	}
	px := imgBooks.RGBAAt(20, 20)
	if px != (color.RGBA{255, 255, 255, 255}) {
		t.Fatalf("expected crisp paper white background, got RGBA(%d,%d,%d,%d)", px.R, px.G, px.B, px.A)
	}
	maybeSavePNG(t, imgBooks, "ios-editorial-origcover-books")

	// 2. 切换为色块封面对比渲染
	appUI.settings.UseOriginalCover = false
	imgColorBlocks := ui.Render(appUI.Render, 390, 844, 2.0)
	if imgColorBlocks == nil {
		t.Fatalf("expected non-nil Editorial Cupertino color-block books render")
	}
	maybeSavePNG(t, imgColorBlocks, "ios-editorial-colorblock-books")

	// 3. 设置页（含「使用原书封面」开关与「自动换诗频率」选择器）
	appUI.activeTab = components.TabProfile
	imgProfile := ui.Render(appUI.Render, 390, 844, 2.0)
	if imgProfile == nil {
		t.Fatalf("expected non-nil Editorial Cupertino profile render")
	}
	maybeSavePNG(t, imgProfile, "ios-editorial-profile")

	// 4. 关闭悬浮底栏开关：在 Cupertino 下应切回经典贴底导航栏
	off := false
	appUI.settings.FloatingBottomBar = &off
	if appUI.useFloatingBottomNav() {
		t.Fatalf("expected explicit FloatingBottomBar=false to override Cupertino default")
	}
	appUI.activeTab = components.TabShelf
	appUI.shelfSubTab = components.SubTabBooks
	if imgDocked := ui.Render(appUI.Render, 390, 844, 1.0); imgDocked == nil {
		t.Fatalf("expected non-nil docked bottom nav render when FloatingBottomBar=false")
	}

	// 5. 恢复开启并渲染暗夜极简 + iOS 液态玻璃悬浮底栏
	on := true
	appUI.settings.FloatingBottomBar = &on
	appUI.settings.ThemePreset = string(theme.PresetDarkMinimal)
	appUI.activeTab = components.TabShelf
	appUI.shelfSubTab = components.SubTabBooks
	imgDark := ui.Render(appUI.Render, 390, 844, 2.0)
	if imgDark == nil {
		t.Fatalf("expected non-nil DarkMinimal Cupertino render")
	}
	if syncedBg != "#151517" {
		t.Fatalf("expected native window bg synced to #151517, got %q", syncedBg)
	}
	maybeSavePNG(t, imgDark, "ios-dark-books")
}

func TestPeekModeProtectsMainlineProgressAndAllowsReturnOrAdopt(t *testing.T) {
	ctx := t.Context()
	svc, err := app.Open(ctx, t.TempDir(), app.NewMemorySecretStore())
	if err != nil {
		t.Fatalf("app.Open: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })

	bookID := "peek-test-book"
	sp := &domain.SPBook{
		SchemaVersion: 1,
		BookID:        bookID,
		Title:         "偶遇测试诗集",
		Author:        "测试诗人",
		SourceFormat:  "epub",
		Poems: []domain.Poem{
			{ID: "p-0", BookID: bookID, OrderIndex: 0, Kind: domain.EntryKindPoem, Title: "第一首", Stanzas: []domain.Stanza{{Lines: []string{"第一行", "第二行"}}}},
			{ID: "p-1", BookID: bookID, OrderIndex: 1, Kind: domain.EntryKindPoem, Title: "第二首", Stanzas: []domain.Stanza{{Lines: []string{"第三行", "第四行"}}}},
			{ID: "p-2", BookID: bookID, OrderIndex: 2, Kind: domain.EntryKindPoem, Title: "第三首", Stanzas: []domain.Stanza{{Lines: []string{"第五行", "第六行"}}}},
		},
	}
	if err := svc.Store().UpsertImportedBook(ctx, domain.Book{
		ID: bookID, Title: sp.Title, Author: sp.Author, ContentHash: "peek-hash", RawFilename: "peek.epub", PoemCount: 3,
	}, sp); err != nil {
		t.Fatalf("UpsertImportedBook: %v", err)
	}

	appUI := NewAppUI(svc, nil, nil)
	appUI.reloadSync(ctx)

	// 从测试诗集中挑选 3 首正式诗篇
	formalIdx := []int{0, 1, 2}
	p0 := sp.Poems[0]
	p1 := sp.Poems[1]
	p2 := sp.Poems[2]

	waitUntil := func(desc string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if cond() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("timeout waiting for: %s", desc)
	}

	// 1. 建立主线顺序进度：读到第 1 首正式诗 p0
	_, err = appUI.svc.RecordPoemRead(ctx, bookID, p0.ID, 0.3, len(sp.Poems))
	if err != nil {
		t.Fatalf("RecordPoemRead p0: %v", err)
	}
	appUI.reloadSync(ctx)

	// 2. 桌面小组件 Deeplink 跳转到 p2：进入偶遇模式，不覆盖 LastPoemID，但计入 ReadPoemIDs
	appUI.HandleDeeplink("stillpoets://poem/" + bookID + "/" + p2.ID + "?lines=1-2&highlight=1")
	waitUntil("widget deeplink opens p2 in peek mode and records read set", func() bool {
		prog, err := appUI.svc.GetProgress(ctx, bookID)
		if err != nil {
			return false
		}
		appUI.uiThreadMu.Lock()
		ok := appUI.activePoemIdx == formalIdx[2] && appUI.readerEntrySource == entryWidget
		appUI.uiThreadMu.Unlock()
		return ok && slices.Contains(prog.ReadPoemIDs, p2.ID)
	})

	prog, err := appUI.svc.GetProgress(ctx, bookID)
	if err != nil {
		t.Fatalf("GetProgress after widget deeplink: %v", err)
	}
	if prog.LastPoemID != p0.ID {
		t.Fatalf("widget deeplink overwrote mainline LastPoemID: got %q, want %q", prog.LastPoemID, p0.ID)
	}

	// 3. 在偶遇模式下顺手翻到 p1：保持偶遇模式，不改 LastPoemID，但 p1 计入已读集合
	appUI.navigatePoem(bookID, formalIdx[1])
	waitUntil("peek swipe records p1 into ReadPoemIDs", func() bool {
		pr, err := appUI.svc.GetProgress(ctx, bookID)
		return err == nil && slices.Contains(pr.ReadPoemIDs, p1.ID)
	})
	prog, err = appUI.svc.GetProgress(ctx, bookID)
	if err != nil {
		t.Fatalf("GetProgress after peek navigate: %v", err)
	}
	if prog.LastPoemID != p0.ID || appUI.readerEntrySource != entryWidget {
		t.Fatalf("peek navigate should keep mainline=%q and peek mode, got last=%q source=%q", p0.ID, prog.LastPoemID, appUI.readerEntrySource)
	}

	// 4. 在手机端渲染偶遇进度提示条并点击「回到进度」，应跳回 p0 并退出偶遇模式
	ttPhone := ui.NewTester(appUI.Render, 390, 844)
	ttPhone.Frame()
	advanceFrames(ttPhone, 400*time.Millisecond)
	if err := ttPhone.Click("回到进度"); err != nil {
		t.Fatalf("click 回到进度 failed: %v", err)
	}
	if appUI.activePoemIdx != formalIdx[0] || appUI.readerEntrySource != entryMainline {
		t.Fatalf("after 回到进度: idx=%d (want %d), source=%q", appUI.activePoemIdx, formalIdx[0], appUI.readerEntrySource)
	}

	// 5. 从散页入口偶遇 p2，并在平板端点击「从这里续读」，显式将主线更新为 p2
	appUI.openBookWithSource(bookID, p2.ID, nil, true, entryQuote)
	waitUntil("quote jump opens p2 in peek mode", func() bool {
		appUI.uiThreadMu.Lock()
		defer appUI.uiThreadMu.Unlock()
		return appUI.activePoemIdx == formalIdx[2] && appUI.readerEntrySource == entryQuote
	})
	ttTablet := ui.NewTester(appUI.Render, 1080, 720)
	ttTablet.Frame()
	advanceFrames(ttTablet, 400*time.Millisecond)
	if err := ttTablet.Click("从这里续读"); err != nil {
		t.Fatalf("click 从这里续读 failed: %v", err)
	}
	waitUntil("adopt updates LastPoemID to p2", func() bool {
		pr, err := appUI.svc.GetProgress(ctx, bookID)
		return err == nil && pr.LastPoemID == p2.ID
	})
	if appUI.readerEntrySource != entryMainline {
		t.Fatalf("expected entryMainline after adopt, got %q", appUI.readerEntrySource)
	}

	// 6. 小组件恰好跳到当前主线诗 p2：自动回归主线，保留行高亮且不弹冗余偶遇条
	appUI.HandleDeeplink("stillpoets://poem/" + bookID + "/" + p2.ID + "?lines=1-1&highlight=1")
	waitUntil("deeplink to current mainline exits peek mode", func() bool {
		appUI.uiThreadMu.Lock()
		defer appUI.uiThreadMu.Unlock()
		return len(appUI.highlightRanges) == 1 && appUI.activePoemIdx == formalIdx[2] && appUI.readerEntrySource == entryMainline
	})
}

func TestFollowSystemDarkModeAutoSwitch(t *testing.T) {
	ctx := context.Background()
	svc, err := app.Open(ctx, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("app.Open: %v", err)
	}
	defer svc.Close()

	appUI := NewAppUI(svc, nil, nil)
	var (
		mu       sync.Mutex
		lastBg   string
		barDark  bool
		barCalls int
	)
	appUI.SetWindowBgFunc(func(hex string) {
		mu.Lock()
		lastBg = hex
		mu.Unlock()
	})
	appUI.SetSystemBarStyleFunc(func(dark bool) {
		mu.Lock()
		barDark = dark
		barCalls++
		mu.Unlock()
	})

	tt := ui.NewTester(appUI.Render, 390, 844)
	tt.Frame()

	readPalette := func() theme.Palette {
		var p theme.Palette
		appUI.dispatchUI(func() { p = appUI.palette() })
		return p
	}
	readFollow := func() bool {
		var v bool
		appUI.dispatchUI(func() { v = appUI.settings.ShouldFollowSystemDark() })
		return v
	}

	// 1. 默认开启跟随系统深色模式：浅色系统下呈现「画册风」，状态栏使用深色图标（barDark=false）
	if !readFollow() {
		t.Fatalf("expected FollowSystemDark to default to true")
	}
	if got := readPalette().ID; got != theme.PresetArtbook {
		t.Fatalf("light mode initial palette = %q, want %q", got, theme.PresetArtbook)
	}
	mu.Lock()
	if lastBg != theme.Artbook().BgHex || barDark || barCalls == 0 {
		t.Fatalf("light mode native sync mismatch: bg=%q barDark=%v calls=%d", lastBg, barDark, barCalls)
	}
	mu.Unlock()

	// 2. 手机系统进入深色模式：自动切换为「暗夜极简」，同步原生底色与状态栏浅色图标，且不破坏日间偏好
	tt.SetDark(true)
	if got := readPalette().ID; got != theme.PresetDarkMinimal {
		t.Fatalf("dark mode palette = %q, want %q", got, theme.PresetDarkMinimal)
	}
	mu.Lock()
	if lastBg != theme.DarkMinimal().BgHex || !barDark {
		t.Fatalf("dark mode native sync mismatch: bg=%q barDark=%v", lastBg, barDark)
	}
	mu.Unlock()

	// 3. 手机系统退出深色模式：自动恢复原日间主题「画册风」
	tt.SetDark(false)
	if got := readPalette().ID; got != theme.PresetArtbook {
		t.Fatalf("restored light mode palette = %q, want %q", got, theme.PresetArtbook)
	}
	mu.Lock()
	if lastBg != theme.Artbook().BgHex || barDark {
		t.Fatalf("restored light mode native sync mismatch: bg=%q barDark=%v", lastBg, barDark)
	}
	mu.Unlock()

	// 4. 在深色模式下进入「我的」页并点击浅色主题「刊物风」：自动关闭跟随系统深色，避免被系统覆盖，并持久化到设置与小组件快照
	tt.SetDark(true)
	appUI.dispatchUI(func() { appUI.activeTab = components.TabProfile })
	tt.Frame()
	if err := tt.Click("刊物风"); err != nil {
		t.Fatalf("click 刊物风 failed: %v", err)
	}
	if readFollow() {
		t.Fatalf("selecting light theme under dark OS should disable followSystemDark")
	}
	if got := readPalette().ID; got != theme.PresetEditorial {
		t.Fatalf("manual light override under dark OS = %q, want %q", got, theme.PresetEditorial)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st, err := svc.LoadSettings(ctx)
		if err == nil && st.ThemePreset == string(theme.PresetEditorial) && !st.ShouldFollowSystemDark() {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	tl, err := svc.ReadWidgetTimeline()
	if err != nil {
		t.Fatalf("ReadWidgetTimeline: %v", err)
	}
	if tl.Theme.FollowSystemDark {
		t.Fatalf("widget timeline should record followSystemDark=false")
	}
	if tl.Theme.DarkBgColor != theme.DarkMinimal().BgHex {
		t.Fatalf("widget timeline darkBgColor = %q, want %q", tl.Theme.DarkBgColor, theme.DarkMinimal().BgHex)
	}

	// 5. 重新开启跟随系统深色模式：在深色系统下立即切回「暗夜极简」，切回浅色系统后恢复刚选的「刊物风」
	appUI.dispatchUI(func() { appUI.toggleFollowSystemDark(true) })
	if got := readPalette().ID; got != theme.PresetDarkMinimal {
		t.Fatalf("re-enabling follow under dark OS = %q, want %q", got, theme.PresetDarkMinimal)
	}
	tt.SetDark(false)
	if got := readPalette().ID; got != theme.PresetEditorial {
		t.Fatalf("returning to light OS should restore editorial, got %q", got)
	}

	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tl, err = svc.ReadWidgetTimeline()
		if err == nil && tl.Theme.FollowSystemDark {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !tl.Theme.FollowSystemDark {
		t.Fatalf("widget timeline should record followSystemDark=true after toggle")
	}
}
