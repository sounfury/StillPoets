package app

import (
	"archive/zip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"stillpoets/internal/domain"
	"stillpoets/internal/storage"
	booksync "stillpoets/internal/sync"
	"stillpoets/internal/sync/webdav"
)

func TestWebDAVWorkspaceSwitchUploadsImportedBookToEmptyWorkspace(t *testing.T) {
	t.Run("from local import files", func(t *testing.T) {
		testReuploadToEmptyWorkspace(t, false)
	})
	t.Run("from the first workspace", func(t *testing.T) {
		testReuploadToEmptyWorkspace(t, true)
	})
}

func testReuploadToEmptyWorkspace(t *testing.T, removeLocalFiles bool) {
	t.Helper()
	ctx := t.Context()
	var sourceUnavailable atomic.Bool
	source := migrationTestServer(t, "first-password", func(*http.Request) bool {
		return sourceUnavailable.Load()
	})
	target := migrationTestServer(t, "second-password", nil)
	first := syncTestConfig(source.URL)
	first.Password = "first-password"
	second := syncTestConfig(target.URL)
	second.Password = "second-password"
	device := openMigrationTestService(t, t.TempDir(), NewMemorySecretStore())
	if _, err := device.TestWebDAVConnection(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := device.SaveWebDAVConfig(ctx, first); err != nil {
		t.Fatal(err)
	}
	imported, err := device.ImportEPUB(ctx, createMigrationEPUB(t), "")
	if err != nil {
		t.Fatal(err)
	}
	if res, err := device.SyncNow(ctx, true); err != nil || !res.Published || res.PartialSync {
		t.Fatalf("first upload = %+v, %v", res, err)
	}
	oldClient, err := device.newWebDAVClient()
	if err != nil {
		t.Fatal(err)
	}
	if err := booksync.NewEngine(oldClient).VerifyBookAssets(ctx, imported.Book); err != nil {
		t.Fatalf("first workspace does not contain the matching book assets: %v", err)
	}
	if overview, err := device.GetSyncOverview(ctx); err != nil || overview.PendingCount != 0 {
		t.Fatalf("expected first upload to drain the queue: %+v, %v", overview, err)
	}

	if removeLocalFiles {
		device.removeLocalBookCacheFiles(imported.Book.ID)
	} else {
		// The device should re-upload from its import files even if the old provider goes away.
		sourceUnavailable.Store(true)
	}
	if res, err := device.TestWebDAVConnection(ctx, second); err != nil || res.ExistingRemote || res.DeviceCount != 0 {
		t.Fatalf("expected an empty second workspace: %+v, %v", res, err)
	}
	if err := device.SaveWebDAVConfig(ctx, second); err != nil {
		t.Fatal(err)
	}
	if overview, err := device.GetSyncOverview(ctx); err != nil || overview.PendingCount != 1 {
		t.Fatalf("expected the previously uploaded book to be queued again: %+v, %v", overview, err)
	}
	if res, err := device.SyncNow(ctx, true); err != nil || !res.Published || res.PartialSync || res.PendingBookUploads != 0 {
		t.Fatalf("second upload = %+v, %v", res, err)
	}
	newClient, err := device.newWebDAVClient()
	if err != nil {
		t.Fatal(err)
	}
	if err := booksync.NewEngine(newClient).VerifyBookAssets(ctx, imported.Book); err != nil {
		t.Fatalf("second workspace does not contain the matching book assets: %v", err)
	}

	reader := openMigrationTestService(t, t.TempDir(), NewMemorySecretStore())
	if err := reader.SaveWebDAVConfig(ctx, second); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.SyncNow(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := reader.EnsureBookContent(ctx, imported.Book.ID); err != nil {
		t.Fatalf("reader could not open the book uploaded to the new workspace: %v", err)
	}
	if poems, err := reader.ListPoems(ctx, imported.Book.ID); err != nil || len(poems) != 1 {
		t.Fatalf("reader did not receive the book's poems: %d, %v", len(poems), err)
	}
}

func createMigrationEPUB(t *testing.T) string {
	t.Helper()
	ep := filepath.Join(t.TempDir(), "migration.epub")
	file, err := os.Create(ep)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	bundle := zip.NewWriter(file)
	for name, content := range map[string]string{
		"META-INF/container.xml": `<container><rootfiles><rootfile full-path="book.opf"/></rootfiles></container>`,
		"book.opf": `<package><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>测试诗集</dc:title></metadata>` +
			`<manifest><item id="poem" href="poem.xhtml" media-type="application/xhtml+xml"/></manifest>` +
			`<spine><itemref idref="poem"/></spine></package>`,
		"poem.xhtml": `<html><body><h1>一首诗</h1><p>第一行<br/>第二行</p></body></html>`,
	} {
		entry, err := bundle.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := bundle.Close(); err != nil {
		t.Fatal(err)
	}
	return ep
}

func TestWebDAVWorkspaceSwitchResumesAssetMigration(t *testing.T) {
	ctx := t.Context()
	var sourceUnavailable atomic.Bool
	source := migrationTestServer(t, "old-provider-password", func(r *http.Request) bool {
		return sourceUnavailable.Load() && r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/books/")
	})
	target := migrationTestServer(t, "new-provider-password", nil)
	oldConfig := syncTestConfig(source.URL)
	oldConfig.Password = "old-provider-password"
	newConfig := syncTestConfig(target.URL)
	newConfig.Password = "new-provider-password"
	book, poem := seedMigrationBook(t, oldConfig)

	dataDir, secrets := t.TempDir(), NewMemorySecretStore()
	// An installed iOS version has the original profile shape and a single password key.
	if err := writeJSONAtomic(filepath.Join(dataDir, webdavProfileFilename), webDAVProfileFromInput(oldConfig)); err != nil {
		t.Fatal(err)
	}
	if err := secrets.Set(webdavPasswordKey, []byte(oldConfig.Password)); err != nil {
		t.Fatal(err)
	}
	migrating := openMigrationTestService(t, dataDir, secrets)
	if _, err := migrating.SyncNow(ctx, false); err != nil {
		t.Fatal(err)
	}
	if got, err := migrating.GetBook(ctx, book.ID); err != nil || got.LocalState != domain.LocalStateRemote {
		t.Fatalf("migrating device should only have the remote book record: %+v, %v", got, err)
	}
	if _, err := migrating.ToggleFavoritePoem(ctx, book, poem, true); err != nil {
		t.Fatal(err)
	}
	if _, err := migrating.RecordPoemRead(ctx, book.ID, poem.ID, 0, book.PoemCount); err != nil {
		t.Fatal(err)
	}
	if err := migrating.SaveWebDAVConfig(ctx, newConfig); err != nil {
		t.Fatal(err)
	}
	profileData, err := os.ReadFile(filepath.Join(dataDir, webdavProfileFilename))
	if err != nil || strings.Contains(string(profileData), oldConfig.Password) || strings.Contains(string(profileData), newConfig.Password) {
		t.Fatalf("migration profile must store only workspace metadata: %v", err)
	}
	if _, err := secrets.Get(webdavPasswordKey); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy password was not removed after switching: %v", err)
	}

	sourceUnavailable.Store(true)
	if res, err := migrating.SyncNow(ctx, true); err != nil || !res.PartialSync || res.PendingBookUploads != 1 {
		t.Fatalf("unavailable source should leave the book pending: %+v, %v", res, err)
	}
	reader := openMigrationTestService(t, t.TempDir(), NewMemorySecretStore())
	if err := reader.SaveWebDAVConfig(ctx, newConfig); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.SyncNow(ctx, false); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.GetBook(ctx, book.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("book was published before its assets: %v", err)
	}
	if err := migrating.Close(); err != nil {
		t.Fatal(err)
	}

	sourceUnavailable.Store(false)
	migrating = openMigrationTestService(t, dataDir, secrets)
	if res, err := migrating.SyncNow(ctx, false); err != nil || res.PartialSync || res.PendingBookUploads != 0 {
		t.Fatalf("sync after restart should migrate book assets: %+v, %v", res, err)
	}
	if _, err := secrets.Get(webDAVWorkspacePasswordKey(webDAVProfileFromInput(oldConfig))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source password was not removed after migration: %v", err)
	}
	if _, err := reader.SyncNow(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := reader.EnsureBookContent(ctx, book.ID); err != nil {
		t.Fatalf("reader could not download migrated book: %v", err)
	}
	if favorites, err := reader.ListFavorites(ctx, book.ID); err != nil || len(favorites) != 1 || favorites[0].PoemID != poem.ID {
		t.Fatalf("migrated favorites = %+v, %v", favorites, err)
	}
	if progress, err := reader.GetProgress(ctx, book.ID); err != nil || progress.LastPoemID != poem.ID {
		t.Fatalf("migrated reading progress = %+v, %v", progress, err)
	}
}

func migrationTestServer(t *testing.T, password string, unavailable func(*http.Request) bool) *httptest.Server {
	t.Helper()
	handler := mockDAVHandler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, _ := r.BasicAuth()
		if user != "poet" || pass != password {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if unavailable != nil && unavailable(r) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		r.SetBasicAuth("poet", "secret-pass")
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func openMigrationTestService(t *testing.T, dataDir string, secrets SecretStore) *Service {
	t.Helper()
	svc, err := Open(t.Context(), dataDir, secrets)
	if err != nil {
		t.Fatal(err)
	}
	svc.debounceDelay = time.Hour
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

func seedMigrationBook(t *testing.T, cfg WebDAVConfigInput) (domain.Book, domain.Poem) {
	t.Helper()
	const installID = "source-install"
	const bookID = "test-book"
	poem := domain.Poem{ID: "test-poem", BookID: bookID, Title: "正文", Kind: domain.EntryKindPoem}
	bundle, hash, err := booksync.EncodeSPBook(&domain.SPBook{BookID: bookID, Title: "旧工作区诗集", Poems: []domain.Poem{poem}})
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("original EPUB backup")
	book := domain.Book{
		ID: bookID, Title: "旧工作区诗集", ContentHash: hash, ContentSize: int64(len(bundle)),
		RawFilename: "诗集.epub", RawSize: int64(len(raw)), PoemCount: 1,
		ContentVer: domain.Version{Counter: 1, InstallID: installID},
	}
	client, err := webdav.NewClient(webdav.Config{BaseURL: cfg.BaseURL, Username: cfg.Username, Password: cfg.Password, AppFolder: cfg.AppFolder})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.CloseIdleConnections)
	engine := booksync.NewEngine(client)
	ctx := context.Background()
	if _, err := engine.UploadBookAssets(ctx, book, bundle, raw, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Sync(ctx, booksync.SyncRequest{
		InstallID: installID, ForcePush: true, LocalData: booksync.SnapshotData{Books: []domain.Book{book}},
	}); err != nil {
		t.Fatal(err)
	}
	return book, poem
}

func TestWebDAVReconcilesMissingRemoteBooksAfterPreUpgradeSwitch(t *testing.T) {
	ctx := t.Context()
	oldServer := migrationTestServer(t, "old-pass", nil)
	newServer := migrationTestServer(t, "new-pass", nil)

	oldCfg := syncTestConfig(oldServer.URL)
	oldCfg.Password = "old-pass"
	newCfg := syncTestConfig(newServer.URL)
	newCfg.Password = "new-pass"

	book, poem := seedMigrationBook(t, oldCfg)

	// 1. iOS 从旧 WebDAV 同步并按需下载正文，随后模拟旧版只落 SQLite（磁盘无 cache/spbook 与 cache/raw）
	iosDir, iosSecrets := t.TempDir(), NewMemorySecretStore()
	ios := openMigrationTestService(t, iosDir, iosSecrets)
	if err := ios.SaveWebDAVConfig(ctx, oldCfg); err != nil {
		t.Fatal(err)
	}
	if _, err := ios.SyncNow(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := ios.EnsureBookContent(ctx, book.ID); err != nil {
		t.Fatalf("iOS download from old workspace failed: %v", err)
	}
	ios.removeLocalBookCacheFiles(book.ID)
	if err := ios.Close(); err != nil {
		t.Fatal(err)
	}

	// 2. 模拟用户在升版前（v0.1.4）直接改配新 WebDAV（坚果云）：无 migrationSources、无 pending_uploads，
	// 且新工作区只有 iOS 推上去的元数据快照，完全没有 books/ 目录。
	if err := writeJSONAtomic(filepath.Join(iosDir, webdavProfileFilename), webDAVProfileFromInput(newCfg)); err != nil {
		t.Fatal(err)
	}
	if err := iosSecrets.Set(webdavPasswordKey, []byte(newCfg.Password)); err != nil {
		t.Fatal(err)
	}
	newClient, err := webdav.NewClient(webdav.Config{
		BaseURL: newCfg.BaseURL, Username: newCfg.Username, Password: newCfg.Password, AppFolder: newCfg.AppFolder,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer newClient.CloseIdleConnections()
	if _, err := booksync.NewEngine(newClient).Sync(ctx, booksync.SyncRequest{
		InstallID: "legacy-ios-pre-upgrade",
		ForcePush: true,
		LocalData: booksync.SnapshotData{Books: []domain.Book{book}},
	}); err != nil {
		t.Fatal(err)
	}

	// 3. 新安卓端先同步了只有元数据的坚果云，此时无法下载正文
	android := openMigrationTestService(t, t.TempDir(), NewMemorySecretStore())
	if err := android.SaveWebDAVConfig(ctx, newCfg); err != nil {
		t.Fatal(err)
	}
	if _, err := android.SyncNow(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := android.EnsureBookContent(ctx, book.ID); err == nil {
		t.Fatalf("expected android download to fail before iOS reconciles missing books/")
	}

	// 4. iOS 升级重启后触发一次普通同步：应自动发现远端 books/ 缺失、从本地 SQLite 重建并上传正文包，更新 ContentHash/ContentVer 并发布快照
	ios = openMigrationTestService(t, iosDir, iosSecrets)
	if ov, err := ios.GetSyncOverview(ctx); err != nil || ov.PendingCount != 0 {
		t.Fatalf("expected 0 pending uploads before reconcile, got %+v, %v", ov, err)
	}
	res, err := ios.SyncNow(ctx, false)
	if err != nil || !res.Published || res.PartialSync || res.PendingBookUploads != 0 {
		t.Fatalf("reconcile sync = %+v, %v", res, err)
	}

	// 再次同步应保持幂等，不重复上传/发布
	res2, err := ios.SyncNow(ctx, false)
	if err != nil || res2.Published || res2.PartialSync || res2.PendingBookUploads != 0 {
		t.Fatalf("second sync should be idempotent, got %+v, %v", res2, err)
	}

	// 5. 新安卓端再次同步后即可正常按需下载并读取全本诗篇
	if _, err := android.SyncNow(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := android.EnsureBookContent(ctx, book.ID); err != nil {
		t.Fatalf("android could not open reconciled book: %v", err)
	}
	poems, err := android.ListPoems(ctx, book.ID)
	if err != nil || len(poems) != 1 || poems[0].ID != poem.ID || poems[0].Title != poem.Title {
		t.Fatalf("android poems = %+v, %v", poems, err)
	}
}

func TestWebDAVMigrationFallbackAndPendingErrorOverview(t *testing.T) {
	ctx := t.Context()
	var oldDown atomic.Bool
	oldServer := migrationTestServer(t, "old-pass", func(*http.Request) bool {
		return oldDown.Load()
	})
	newServer := migrationTestServer(t, "new-pass", nil)

	oldCfg := syncTestConfig(oldServer.URL)
	oldCfg.Password = "old-pass"
	newCfg := syncTestConfig(newServer.URL)
	newCfg.Password = "new-pass"

	book, poem := seedMigrationBook(t, oldCfg)

	t.Run("rescues downloaded book from sqlite when old workspace is unreachable", func(t *testing.T) {
		oldDown.Store(false)
		dev := openMigrationTestService(t, t.TempDir(), NewMemorySecretStore())
		if err := dev.SaveWebDAVConfig(ctx, oldCfg); err != nil {
			t.Fatal(err)
		}
		if _, err := dev.SyncNow(ctx, false); err != nil {
			t.Fatal(err)
		}
		if err := dev.EnsureBookContent(ctx, book.ID); err != nil {
			t.Fatal(err)
		}
		dev.removeLocalBookCacheFiles(book.ID)

		// 旧工作区彻底下线后再切到新工作区
		oldDown.Store(true)
		if err := dev.SaveWebDAVConfig(ctx, newCfg); err != nil {
			t.Fatal(err)
		}
		if res, err := dev.SyncNow(ctx, true); err != nil || res.PartialSync || res.PendingBookUploads != 0 {
			t.Fatalf("fallback migration sync = %+v, %v", res, err)
		}

		reader := openMigrationTestService(t, t.TempDir(), NewMemorySecretStore())
		if err := reader.SaveWebDAVConfig(ctx, newCfg); err != nil {
			t.Fatal(err)
		}
		if _, err := reader.SyncNow(ctx, false); err != nil {
			t.Fatal(err)
		}
		if err := reader.EnsureBookContent(ctx, book.ID); err != nil {
			t.Fatalf("reader failed to open rescued book: %v", err)
		}
		if poems, err := reader.ListPoems(ctx, book.ID); err != nil || len(poems) != 1 || poems[0].ID != poem.ID {
			t.Fatalf("reader poems = %+v, %v", poems, err)
		}
	})

	t.Run("exposes pending upload error in overview when neither local nor old workspace has content", func(t *testing.T) {
		oldDown.Store(false)
		dev := openMigrationTestService(t, t.TempDir(), NewMemorySecretStore())
		if err := dev.SaveWebDAVConfig(ctx, oldCfg); err != nil {
			t.Fatal(err)
		}
		if _, err := dev.SyncNow(ctx, false); err != nil {
			t.Fatal(err)
		}
		// 不下载正文（保持 LocalStateRemote），旧工作区下线后切盘
		oldDown.Store(true)
		emptyServer := migrationTestServer(t, "empty-pass", nil)
		emptyCfg := syncTestConfig(emptyServer.URL)
		emptyCfg.Password = "empty-pass"
		if err := dev.SaveWebDAVConfig(ctx, emptyCfg); err != nil {
			t.Fatal(err)
		}
		if res, err := dev.SyncNow(ctx, true); err != nil || !res.PartialSync || res.PendingBookUploads != 1 {
			t.Fatalf("expected partial sync with 1 pending upload, got %+v, %v", res, err)
		}
		ov, err := dev.GetSyncOverview(ctx)
		if err != nil || ov.PendingCount != 1 || ov.PendingError == "" {
			t.Fatalf("expected SyncOverview to expose PendingError, got %+v, %v", ov, err)
		}
	})
}
