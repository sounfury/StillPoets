package views

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
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
