package views

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
	"stillpoets/internal/domain"
	"stillpoets/internal/storage"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/sharecard"
	"stillpoets/internal/ui/theme"
)

func TestShareCardModalAllEntriesAndAsyncExport(t *testing.T) {
	ctx := t.Context()
	svc, err := app.Open(ctx, t.TempDir(), app.NewMemorySecretStore())
	if err != nil {
		t.Fatalf("app.Open: %v", err)
	}
	defer svc.Close()

	sp := &domain.SPBook{
		SchemaVersion: 1,
		BookID:        "book-share-1",
		Title:         "海子诗集",
		Author:        "海子",
		SourceFormat:  "epub",
		Poems: []domain.Poem{
			{
				ID:         "p-1",
				BookID:     "book-share-1",
				OrderIndex: 0,
				Kind:       domain.EntryKindPoem,
				Title:      "日记",
				Author:     "海子",
				Epigraph:   "姐姐，今夜我在德令哈。",
				Stanzas: []domain.Stanza{
					{Lines: []string{"姐姐，今夜我在德令哈，夜色笼罩", "姐姐，我今夜只有戈壁"}},
					{Lines: []string{"草原尽头我两手空空", "悲痛时握不住一颗泪滴"}},
				},
				Colophon: "1988.7.25",
			},
			{
				ID:         "p-2",
				BookID:     "book-share-1",
				OrderIndex: 1,
				Kind:       domain.EntryKindPoem,
				Title:      "面朝大海，春暖花开",
				Author:     "海子",
				Stanzas: []domain.Stanza{
					{Lines: []string{"从明天起，做一个幸福的人", "喂马、劈柴，周游世界"}},
				},
			},
		},
	}
	book := domain.Book{
		ID:          "book-share-1",
		Title:       "海子诗集",
		Author:      "海子",
		ContentHash: "hash-share-1",
		RawFilename: "haizi.epub",
		PoemCount:   2,
	}
	if err := svc.Store().UpsertImportedBook(ctx, book, sp); err != nil {
		t.Fatalf("UpsertImportedBook: %v", err)
	}
	if _, err := svc.ToggleFavoritePoem(ctx, book, sp.Poems[0], true); err != nil {
		t.Fatalf("ToggleFavoritePoem: %v", err)
	}
	if _, err := svc.MarkBookFinished(ctx, book.ID, true); err != nil {
		t.Fatalf("MarkBookFinished: %v", err)
	}
	q, err := svc.CreateStandaloneQuote(ctx, "不急着读完，\n给一句诗多一点时间。", "随记", "枕边札记")
	if err != nil {
		t.Fatalf("CreateStandaloneQuote: %v", err)
	}

	appUI := NewAppUI(svc, nil, nil)
	appUI.nowFn = func() time.Time {
		return time.Date(2026, 11, 28, 21, 15, 0, 0, time.Local)
	}
	appUI.openBook(book.ID, "p-1", nil, false)
	appUI.reloadSync(ctx)

	// 1. 散页详情入口：单句卡片
	appUI.openQuoteShareCard(storage.QuoteView{Quote: q, EffectiveKind: domain.QuoteKindStandalone})
	if !appUI.showShareCardModal || appUI.shareCardData.Kind != sharecard.KindQuote {
		t.Fatalf("quote share card did not open cleanly")
	}
	if img := ui.Render(appUI.Render, 390, 844, 1.0); img == nil {
		t.Fatalf("phone render quote share modal returned nil")
	}

	// 2. 阅读器划选入口：单句卡片
	appUI.openSelectionShareCard(book, sp.Poems[0], "姐姐，今夜我在德令哈，夜色笼罩")
	if !appUI.showShareCardModal || appUI.shareCardData.QuoteTag != "诗歌节选" {
		t.Fatalf("selection share card unexpected state: %+v", appUI.shareCardData)
	}

	// 3. 阅读器顶栏入口：纯净的整诗长图（单段/选句分享已收口至长按段落交互）
	appUI.openPoemShareCard(book, sp.Poems[0])
	if appUI.shareCardData.Kind != sharecard.KindPoem {
		t.Fatalf("poem share card unexpected kind: %v", appUI.shareCardData.Kind)
	}
	if img := ui.Render(appUI.Render, 1080, 720, 1.0); img == nil {
		t.Fatalf("tablet render poem share modal returned nil")
	}

	// 4. 诗集精选合辑入口
	favs, err := svc.ListFavorites(ctx, book.ID)
	if err != nil || len(favs) == 0 {
		t.Fatalf("ListFavorites: %v (len=%d)", err, len(favs))
	}
	appUI.openCollectionShareCard(book, favs)
	if appUI.shareCardData.Kind != sharecard.KindCollection || len(appUI.shareCardData.CollectionItems) != 1 {
		t.Fatalf("collection share card unexpected items: %+v", appUI.shareCardData)
	}

	// 5. 完读纪念卡入口
	appUI.openFinishedShareCard(storage.FinishedBookItem{
		Book:          book,
		Progress:      domain.ReadingProgress{BookID: book.ID, Status: domain.ReadStatusFinished, FinishedAt: appUI.nowFn().Unix(), ReadPoemIDs: []string{"p-1", "p-2"}},
		FavoriteCount: 1,
		QuoteCount:    1,
		TopFavorites:  favs,
	})
	if appUI.shareCardData.Kind != sharecard.KindFinished {
		t.Fatalf("finished share card did not open")
	}

	// 6. 统计报告入口
	appUI.activeTab = components.TabStats
	appUI.openStatsReportShareCard()
	if appUI.shareCardData.Kind != sharecard.KindStatsReport {
		t.Fatalf("stats report share card did not open")
	}
	appUI.shareCardOpts = sharecard.Options{
		Template:      sharecard.TemplateSeal,
		Theme:         sharecard.ThemeXuanPaper,
		ShowWatermark: true,
	}
	if img := ui.Render(appUI.Render, 390, 844, 1.0); img == nil {
		t.Fatalf("phone render stats report share modal returned nil")
	}

	// 7. 验证「保存到相册」异步导出链路（真实生成 1080px 宽 PNG 文件）
	var (
		saveMu   sync.Mutex
		savedLoc string
		saveDone = make(chan struct{}, 1)
	)
	appUI.SetSavePhotoFunc(func(path string) (string, error) {
		saveMu.Lock()
		savedLoc = path
		saveMu.Unlock()
		select {
		case saveDone <- struct{}{}:
		default:
		}
		return "系统相册", nil
	})
	appUI.exportShareCardAsync("save")
	select {
	case <-saveDone:
	case <-time.After(15 * time.Second):
		t.Fatalf("timed out waiting for async save photo export")
	}

	saveMu.Lock()
	exportedPath := savedLoc
	saveMu.Unlock()
	raw, err := os.ReadFile(exportedPath)
	if err != nil {
		t.Fatalf("read exported png: %v", err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode exported png: %v", err)
	}
	if cfg.Width != 1080 || cfg.Height < 600 {
		t.Fatalf("unexpected exported png dimensions: %dx%d", cfg.Width, cfg.Height)
	}

	// 等待 UI 状态回落 busy=false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		appUI.uiThreadMu.Lock()
		busy := appUI.shareCardBusy
		appUI.uiThreadMu.Unlock()
		if !busy {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}

	// 8. 验证「系统分享」取消时不报错误提示
	shareDone := make(chan struct{}, 1)
	appUI.SetShareImageFunc(func(path, _ string) error {
		defer func() {
			select {
			case shareDone <- struct{}{}:
			default:
			}
		}()
		return context.Canceled
	})
	appUI.shareCardOpts.Theme = sharecard.ThemeFromAppPreset(theme.PresetDarkMinimal)
	appUI.exportShareCardAsync("share")
	select {
	case <-shareDone:
	case <-time.After(15 * time.Second):
		t.Fatalf("timed out waiting for async share image export")
	}
	time.Sleep(40 * time.Millisecond)
	appUI.uiThreadMu.Lock()
	statusAfterCancel := appUI.shareCardStatus
	appUI.uiThreadMu.Unlock()
	if statusAfterCancel != "" {
		t.Fatalf("expected empty status after user cancel, got %q", statusAfterCancel)
	}
}
