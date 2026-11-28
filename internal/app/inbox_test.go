package app

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"stillpoets/internal/domain"
	booksync "stillpoets/internal/sync"
	"stillpoets/internal/sync/webdav"
)

func buildMinimalTestEPUB(t *testing.T, title, author string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	writeEntry := func(name, content string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	writeEntry("mimetype", "application/epub+zip")
	writeEntry("META-INF/container.xml", `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`)
	writeEntry("OEBPS/content.opf", `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="BookId">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>`+title+`</dc:title>
    <dc:creator>`+author+`</dc:creator>
  </metadata>
  <manifest>
    <item id="p1" href="poem1.xhtml" media-type="application/xhtml+xml"/>
    <item id="p2" href="poem2.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine>
    <itemref idref="p1"/>
    <itemref idref="p2"/>
  </spine>
</package>`)
	writeEntry("OEBPS/poem1.xhtml", `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>面朝大海</title></head><body>
<h1>面朝大海，春暖花开</h1>
<p>从明天起，做一个幸福的人<br/>喂马、劈柴，周游世界</p>
<p>从明天起，关心粮食和蔬菜<br/>我有一所房子，面朝大海，春暖花开</p>
</body></html>`)
	writeEntry("OEBPS/poem2.xhtml", `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>日记</title></head><body>
<h1>日记</h1>
<p>姐姐，今夜我在德令哈，夜色笼罩<br/>姐姐，我今夜只有戈壁</p>
<p>草原尽头我两手空空<br/>悲痛时握不住一颗泪滴</p>
</body></html>`)
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func putBrowserInboxDelivery(t *testing.T, remote *webdav.Client, bookID, origName, rawName, groupID string, epub []byte) booksync.InboxItem {
	t.Helper()
	ctx := t.Context()
	bookDir := booksync.BooksDir + "/" + bookID
	if err := remote.EnsureDir(ctx, bookDir); err != nil {
		t.Fatalf("ensure book dir: %v", err)
	}
	if _, err := remote.Put(ctx, booksync.BookRawPath(bookID, rawName), epub, "application/epub+zip"); err != nil {
		t.Fatalf("put raw epub: %v", err)
	}
	if err := remote.EnsureDir(ctx, booksync.InboxDir); err != nil {
		t.Fatalf("ensure inbox dir: %v", err)
	}
	sum := sha256.Sum256(epub)
	item := booksync.InboxItem{
		SchemaVersion:     booksync.InboxSchemaVersion,
		ImportID:          bookID,
		BookID:            bookID,
		OriginalFilename:  origName,
		RawFilename:       rawName,
		RawSize:           int64(len(epub)),
		RawSHA256:         hex.EncodeToString(sum[:]),
		GroupID:           groupID,
		InitialGroupVer:   domain.Version{Counter: 1, InstallID: "tm-browser"},
		ParserRuleVersion: booksync.InboxParserRuleVersion,
		SubmittedAt:       1700000123000,
		Source:            "zlibrary",
	}
	readyBytes, err := booksync.EncodeInboxItem(item)
	if err != nil {
		t.Fatalf("encode inbox item: %v", err)
	}
	readyPath, err := booksync.InboxReadyPath(bookID)
	if err != nil {
		t.Fatalf("inbox ready path: %v", err)
	}
	if _, err := remote.Put(ctx, readyPath, readyBytes, "application/json"); err != nil {
		t.Fatalf("put ready.json: %v", err)
	}
	return item
}

func TestInboxAnyDeviceConsumeAndOnDemandDownload(t *testing.T) {
	srv := httptest.NewServer(mockDAVHandler())
	defer srv.Close()
	cfg := syncTestConfig(srv.URL)
	ctx := t.Context()

	svcA, err := Open(ctx, filepath.Join(t.TempDir(), "devA"), NewMemorySecretStore())
	if err != nil {
		t.Fatal(err)
	}
	defer svcA.Close()
	svcB, err := Open(ctx, filepath.Join(t.TempDir(), "devB"), NewMemorySecretStore())
	if err != nil {
		t.Fatal(err)
	}
	defer svcB.Close()

	if err := svcA.SaveWebDAVConfig(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if err := svcB.SaveWebDAVConfig(ctx, cfg); err != nil {
		t.Fatal(err)
	}

	// 设备 A 先建一个分组并同步
	grp, err := svcA.CreateGroup(ctx, "现代诗精选")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svcA.SyncNow(ctx, true); err != nil {
		t.Fatal(err)
	}

	// 模拟油猴直接向 WebDAV 投递原 EPUB + inbox ready 清单
	remote, err := webdav.NewClient(webdav.Config{
		BaseURL:   cfg.BaseURL,
		AppFolder: cfg.AppFolder,
		Username:  cfg.Username,
		Password:  cfg.Password,
	})
	if err != nil {
		t.Fatal(err)
	}
	epubBytes := buildMinimalTestEPUB(t, "海子诗选", "海子")
	putBrowserInboxDelivery(t, remote, "inbox-book-01", "海子诗选.epub", "海子诗选_abcd12.epub", grp.ID, epubBytes)

	// 设备 B（后开的一台设备）无需等设备 A，直接同步即可无感消费 inbox 并落库
	if _, err := svcB.SyncNow(ctx, false); err != nil {
		t.Fatalf("svcB SyncNow: %v", err)
	}
	bookB, err := svcB.GetBook(ctx, "inbox-book-01")
	if err != nil {
		t.Fatalf("svcB GetBook: %v", err)
	}
	if bookB.Title != "海子诗选" || bookB.GroupID != grp.ID || bookB.PoemCount != 2 {
		t.Fatalf("unexpected imported book on B: %+v", bookB)
	}
	if bookB.ContentPath == "" {
		t.Fatalf("expected immutable ContentPath on inbox-imported book, got empty")
	}

	// ready.json 应在发布并回读确认后被清理
	readyPath, _ := booksync.InboxReadyPath("inbox-book-01")
	if _, _, err := remote.GetWithLimit(ctx, readyPath, booksync.MaxInboxReadyBytes); err == nil {
		t.Fatalf("expected %s to be cleaned after confirmed publish", readyPath)
	}

	// 设备 A 同步后立即可见，且可从不可变 ContentPath 按需下载阅读
	if _, err := svcA.SyncNow(ctx, false); err != nil {
		t.Fatalf("svcA SyncNow: %v", err)
	}
	bookA, err := svcA.GetBook(ctx, "inbox-book-01")
	if err != nil {
		t.Fatalf("svcA GetBook: %v", err)
	}
	if bookA.LocalState != domain.LocalStateRemote || bookA.ContentPath != bookB.ContentPath {
		t.Fatalf("unexpected remote state on A: %+v", bookA)
	}
	if err := svcA.EnsureBookContent(ctx, "inbox-book-01"); err != nil {
		t.Fatalf("svcA EnsureBookContent from builds path: %v", err)
	}
	poems, err := svcA.ListPoems(ctx, "inbox-book-01")
	if err != nil || len(poems) != 2 {
		t.Fatalf("svcA ListPoems = %d, %v", len(poems), err)
	}
}

func TestInboxConcurrentTwoDeviceConsumeConvergesToSingleReadableBook(t *testing.T) {
	srv := httptest.NewServer(mockDAVHandler())
	defer srv.Close()
	cfg := syncTestConfig(srv.URL)
	ctx := t.Context()

	svcA, err := Open(ctx, filepath.Join(t.TempDir(), "devA"), NewMemorySecretStore())
	if err != nil {
		t.Fatal(err)
	}
	defer svcA.Close()
	svcB, err := Open(ctx, filepath.Join(t.TempDir(), "devB"), NewMemorySecretStore())
	if err != nil {
		t.Fatal(err)
	}
	defer svcB.Close()
	if err := svcA.SaveWebDAVConfig(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if err := svcB.SaveWebDAVConfig(ctx, cfg); err != nil {
		t.Fatal(err)
	}

	remote, _ := webdav.NewClient(webdav.Config{
		BaseURL:   cfg.BaseURL,
		AppFolder: cfg.AppFolder,
		Username:  cfg.Username,
		Password:  cfg.Password,
	})
	epubBytes := buildMinimalTestEPUB(t, "北岛诗选", "北岛")
	putBrowserInboxDelivery(t, remote, "race-book-01", "北岛诗选.epub", "北岛诗选_112233.epub", "", epubBytes)

	// 模拟两台设备都在对方尚未发布快照时消费了同一份 inbox（构造极端双机同时导入）
	if _, err := svcA.SyncNow(ctx, false); err != nil {
		t.Fatalf("svcA sync: %v", err)
	}
	// 重新放回同一份 ready.json 并让 B 在不知道 A 快照前先解析，验证各自 builds/<attemptId>/ 互不破坏
	putBrowserInboxDelivery(t, remote, "race-book-01", "北岛诗选.epub", "北岛诗选_112233.epub", "", epubBytes)
	if _, err := svcB.SyncNow(ctx, false); err != nil {
		t.Fatalf("svcB sync: %v", err)
	}
	if _, err := svcA.SyncNow(ctx, false); err != nil {
		t.Fatalf("svcA second sync: %v", err)
	}

	booksA, err := svcA.ListBooks(ctx, "")
	if err != nil || len(booksA) != 1 {
		t.Fatalf("booksA count = %d, err=%v; want exactly 1", len(booksA), err)
	}
	booksB, err := svcB.ListBooks(ctx, "")
	if err != nil || len(booksB) != 1 {
		t.Fatalf("booksB count = %d, err=%v; want exactly 1", len(booksB), err)
	}
	if booksA[0].ID != booksB[0].ID || booksA[0].ContentHash != booksB[0].ContentHash {
		t.Fatalf("books diverged: A=%+v B=%+v", booksA[0], booksB[0])
	}
}

func TestInboxSafetyGuardsHashMismatchTombstoneAndDissolvedGroup(t *testing.T) {
	srv := httptest.NewServer(mockDAVHandler())
	defer srv.Close()
	cfg := syncTestConfig(srv.URL)
	ctx := t.Context()

	svc, err := Open(ctx, filepath.Join(t.TempDir(), "dev"), NewMemorySecretStore())
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	if err := svc.SaveWebDAVConfig(ctx, cfg); err != nil {
		t.Fatal(err)
	}

	remote, _ := webdav.NewClient(webdav.Config{
		BaseURL:   cfg.BaseURL,
		AppFolder: cfg.AppFolder,
		Username:  cfg.Username,
		Password:  cfg.Password,
	})
	epubBytes := buildMinimalTestEPUB(t, "顾城诗集", "顾城")

	// 1) 目标分组不存在/已解散 -> 自动回退未分组并正常导入
	putBrowserInboxDelivery(t, remote, "dissolved-grp-book", "顾城诗集.epub", "顾城诗集_aa11.epub", "nonexistent-group", epubBytes)
	if _, err := svc.SyncNow(ctx, false); err != nil {
		t.Fatalf("sync dissolved group book: %v", err)
	}
	b, err := svc.GetBook(ctx, "dissolved-grp-book")
	if err != nil || b.GroupID != "" {
		t.Fatalf("expected fallback to ungrouped book, got %+v, %v", b, err)
	}

	// 2) 删除成永久墓碑后，再出现相同 bookId 的遗留 ready.json -> 必须跳过导入并清理清单
	if err := svc.DeleteBook(ctx, "dissolved-grp-book"); err != nil {
		t.Fatal(err)
	}
	putBrowserInboxDelivery(t, remote, "dissolved-grp-book", "顾城诗集.epub", "顾城诗集_aa11.epub", "", epubBytes)
	if _, err := svc.SyncNow(ctx, false); err != nil {
		t.Fatalf("sync tombstoned inbox: %v", err)
	}
	if _, err := svc.GetBook(ctx, "dissolved-grp-book"); err == nil {
		t.Fatal("tombstoned book must not resurrect from inbox")
	}
	readyPath, _ := booksync.InboxReadyPath("dissolved-grp-book")
	if _, _, err := remote.GetWithLimit(ctx, readyPath, booksync.MaxInboxReadyBytes); err == nil {
		t.Fatal("expected tombstoned book ready.json to be cleaned")
	}

	// 3) 原件哈希不匹配（模拟半传/损坏）-> 保留 ready.json 供重试且不导入损坏书
	badItem := putBrowserInboxDelivery(t, remote, "corrupt-book", "坏书.epub", "坏书_bb22.epub", "", epubBytes)
	// 篡改远端原件内容（保持同长度但字节不同）
	tampered := bytes.Repeat([]byte("X"), len(epubBytes))
	if _, err := remote.Put(ctx, booksync.BookRawPath(badItem.BookID, badItem.RawFilename), tampered, "application/epub+zip"); err != nil {
		t.Fatal(err)
	}
	res, err := svc.SyncNow(ctx, false)
	if err != nil {
		t.Fatalf("sync with corrupt inbox should not fail whole sync: %v", err)
	}
	if !res.PartialSync {
		t.Fatal("expected PartialSync=true when inbox item fails sha256 verification")
	}
	if _, err := svc.GetBook(ctx, "corrupt-book"); err == nil {
		t.Fatal("corrupt inbox book must not be imported")
	}
	badReadyPath, _ := booksync.InboxReadyPath("corrupt-book")
	if _, _, err := remote.GetWithLimit(ctx, badReadyPath, booksync.MaxInboxReadyBytes); err != nil {
		t.Fatal("corrupt inbox ready.json must be kept for retry, not deleted")
	}
}
