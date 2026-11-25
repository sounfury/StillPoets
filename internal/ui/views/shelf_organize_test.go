package views

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
	"stillpoets/internal/domain"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/theme"
)

func TestShelfOrganizeAndBookQuickActions(t *testing.T) {
	ctx := t.Context()
	svc, err := app.Open(ctx, t.TempDir(), app.NewMemorySecretStore())
	if err != nil {
		t.Fatalf("app.Open: %v", err)
	}
	defer svc.Close()

	grpModern, err := svc.CreateGroup(ctx, "现代诗")
	if err != nil {
		t.Fatalf("CreateGroup modern: %v", err)
	}
	grpForeign, err := svc.CreateGroup(ctx, "外国诗")
	if err != nil {
		t.Fatalf("CreateGroup foreign: %v", err)
	}

	seedBook := func(id, title, author, groupID string) domain.Book {
		sp := &domain.SPBook{
			SchemaVersion: 1,
			BookID:        id,
			Title:         title,
			Author:        author,
			SourceFormat:  "epub",
			Poems: []domain.Poem{
				{ID: id + "-p1", BookID: id, OrderIndex: 0, Kind: domain.EntryKindPoem, Title: "第一首", Author: author, Stanzas: []domain.Stanza{{Lines: []string{"星辰落在纸页上"}}}},
			},
		}
		b := domain.Book{
			ID:          id,
			GroupID:     groupID,
			Title:       title,
			Author:      author,
			ContentHash: "hash-" + id,
			RawFilename: id + ".epub",
			PoemCount:   1,
		}
		if err := svc.Store().UpsertImportedBook(ctx, b, sp); err != nil {
			t.Fatalf("UpsertImportedBook %s: %v", id, err)
		}
		return b
	}

	b1 := seedBook("bk-1", "海子诗集", "海子", grpModern.ID)
	b2 := seedBook("bk-2", "恶之花", "波德莱尔", "")
	b3 := seedBook("bk-3", "里尔克诗选", "里尔克", "")
	b4 := seedBook("bk-4", "草叶集", "惠特曼", "")

	appUI := NewAppUI(svc, nil, nil)
	appUI.settings.ThemePreset = string(theme.PresetArtbook)
	appUI.activeTab = components.TabShelf
	appUI.shelfSubTab = components.SubTabBooks
	appUI.reloadSync(ctx)

	// 1. 常规原地整理模式（手机端）：选中 b2, b3 并展示已有分组微标与底部操作条
	appUI.enterShelfOrganize("")
	appUI.toggleShelfBookSelected(b2.ID)
	appUI.toggleShelfBookSelected(b3.ID)
	if len(appUI.selectedShelfBookIDs()) != 2 {
		t.Fatalf("expected 2 selected books, got %d", len(appUI.selectedShelfBookIDs()))
	}
	imgOrgPhone := ui.Render(appUI.Render, 390, 844, 2.0)
	if imgOrgPhone == nil {
		t.Fatalf("phone organize render returned nil")
	}
	maybeSavePNG(t, imgOrgPhone, "shelf-organize-phone")

	// 2. 平板端原地整理模式（左书架底部操作条不越界遮挡右栏阅读器）
	imgOrgTablet := ui.Render(appUI.Render, 1024, 720, 2.0)
	if imgOrgTablet == nil {
		t.Fatalf("tablet organize render returned nil")
	}
	maybeSavePNG(t, imgOrgTablet, "shelf-organize-tablet")

	// 3. 单层「批量移入分组」弹窗渲染
	appUI.showBatchGroupModal = true
	imgBatchModal := ui.Render(appUI.Render, 390, 844, 2.0)
	if imgBatchModal == nil {
		t.Fatalf("batch group modal render returned nil")
	}
	maybeSavePNG(t, imgBatchModal, "shelf-batch-group-modal")
	appUI.showBatchGroupModal = false

	// 4. 空分组定向选入模式（「外国诗」目前为空 -> 切全书架挑书并批量收入）
	appUI.exitShelfOrganize()
	appUI.selectedGrp = grpForeign.ID
	imgEmptyGrp := ui.Render(appUI.Render, 390, 844, 2.0)
	if imgEmptyGrp == nil {
		t.Fatalf("empty group render returned nil")
	}
	maybeSavePNG(t, imgEmptyGrp, "shelf-empty-group-cta")

	appUI.enterShelfOrganizeForGroup(grpForeign.ID)
	if !appUI.shelfOrganizing || appUI.shelfTargetGroupID != grpForeign.ID || appUI.selectedGrp != "" {
		t.Fatalf("unexpected targeted organize state: organizing=%v target=%q grp=%q",
			appUI.shelfOrganizing, appUI.shelfTargetGroupID, appUI.selectedGrp)
	}
	appUI.toggleShelfBookSelected(b2.ID)
	appUI.toggleShelfBookSelected(b3.ID)
	imgTargetPhone := ui.Render(appUI.Render, 390, 844, 2.0)
	if imgTargetPhone == nil {
		t.Fatalf("targeted organize phone render returned nil")
	}
	maybeSavePNG(t, imgTargetPhone, "shelf-organize-target-phone")

	// 执行批量收入并等待落库完成
	appUI.applyBatchGroupMoveAsync(appUI.selectedShelfBookIDs(), grpForeign.ID, grpForeign.Name, true)
	if appUI.shelfOrganizing || appUI.selectedGrp != grpForeign.ID {
		t.Fatalf("expected organize exited and switched to target group, organizing=%v grp=%q",
			appUI.shelfOrganizing, appUI.selectedGrp)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		inForeign, err := svc.ListBooks(ctx, grpForeign.ID)
		if err == nil && len(inForeign) == 2 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	inForeign, err := svc.ListBooks(ctx, grpForeign.ID)
	if err != nil || len(inForeign) != 2 {
		t.Fatalf("expected 2 books in foreign group after batch move, got %d (err=%v)", len(inForeign), err)
	}
	appUI.reloadSync(ctx)

	// 5. 单本长按快捷操作弹窗（选中 b4，可直达分组 + 新建分组 + 常用管理）
	appUI.selectedGrp = ""
	appUI.openBookActionModal(b4.ID)
	imgBookAction := ui.Render(appUI.Render, 390, 844, 2.0)
	if imgBookAction == nil {
		t.Fatalf("book action modal render returned nil")
	}
	maybeSavePNG(t, imgBookAction, "shelf-book-action-modal")
	appUI.showBookActionModal = false

	// 6. 切到散页子 Tab 自动退出整理态，防止残留脏状态
	appUI.enterShelfOrganize(b1.ID)
	appUI.selectShelfSubTab(components.SubTabQuotes)
	if appUI.shelfOrganizing || len(appUI.shelfSelectedBooks) != 0 {
		t.Fatalf("expected switching to quotes subtab to exit organize mode")
	}
}
