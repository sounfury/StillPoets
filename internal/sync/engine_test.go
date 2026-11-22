package sync

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"stillpoets/internal/domain"
	"stillpoets/internal/sync/webdav"
)

type mockWebDAV struct {
	mu    sync.Mutex
	dirs  map[string]bool
	files map[string][]byte
}

func newMockWebDAV() *mockWebDAV {
	return &mockWebDAV{
		dirs:  map[string]bool{"/dav/": true},
		files: map[string][]byte{},
	}
}

func (m *mockWebDAV) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()

		p := r.URL.Path
		switch r.Method {
		case "MKCOL":
			dirPath := p
			if !strings.HasSuffix(dirPath, "/") {
				dirPath += "/"
			}
			if m.dirs[dirPath] {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			m.dirs[dirPath] = true
			w.WriteHeader(http.StatusCreated)

		case "PROPFIND":
			depth := r.Header.Get("Depth")
			dirPath := p
			if !strings.HasSuffix(dirPath, "/") {
				dirPath += "/"
			}
			if !m.dirs[dirPath] {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			var sb strings.Builder
			sb.WriteString(`<?xml version="1.0" encoding="utf-8"?><d:multistatus xmlns:d="DAV:">`)
			sb.WriteString(fmt.Sprintf(`<d:response><d:href>%s</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`, dirPath))
			if depth == "1" {
				for fpath, data := range m.files {
					if path.Dir(fpath)+"/" == dirPath {
						sb.WriteString(fmt.Sprintf(`<d:response><d:href>%s</d:href><d:propstat><d:prop><d:getcontentlength>%d</d:getcontentlength></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`, fpath, len(data)))
					}
				}
			}
			sb.WriteString(`</d:multistatus>`)
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = w.Write([]byte(sb.String()))

		case http.MethodPut:
			data, _ := io.ReadAll(r.Body)
			m.files[p] = data
			w.WriteHeader(http.StatusCreated)

		case http.MethodGet:
			data, ok := m.files[p]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)

		case http.MethodDelete:
			delete(m.files, p)
			w.WriteHeader(http.StatusNoContent)

		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
}

func TestEngineGenerationalSyncFallbackAndRetention(t *testing.T) {
	mock := newMockWebDAV()
	srv := mock.server()
	defer srv.Close()

	client, err := webdav.NewClient(webdav.Config{
		BaseURL:        srv.URL + "/dav/",
		AtomicUpload:   false, // 模拟 AList+115 关闭原子 MOVE
		RetryBaseDelay: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	engine := NewEngine(client)
	ctx := context.Background()

	// 1. 设备 A 连续发布 3 次代次（rev 1, 2, 3），验证最终仅保留最近 2 份（rev 2, 3）
	dataA := SnapshotData{
		Groups: []domain.BookGroup{
			{ID: "g1", Name: "现代诗", NameVer: v(1, "dev-a"), SortOrder: 0, SortVer: v(1, "dev-a"), CreatedAt: 100},
		},
		Quotes: []domain.Quote{
			{ID: "q1", Content: "黑夜给了我黑色的眼睛", BodyVer: v(1, "dev-a"), CreatedAt: 100},
		},
	}
	var revA uint64
	var counterA uint64 = 1
	for i := 0; i < 3; i++ {
		counterA++
		dataA.Quotes[0].Content = fmt.Sprintf("黑夜给了我黑色的眼睛-v%d", i+1)
		dataA.Quotes[0].BodyVer = v(counterA, "dev-a")

		res, err := engine.Sync(ctx, SyncRequest{
			InstallID:    "dev-a",
			LocalRev:     revA,
			LocalCounter: counterA,
			LocalData:    dataA,
			HasDirty:     true,
		})
		if err != nil {
			t.Fatalf("dev-a sync #%d failed: %v", i+1, err)
		}
		if !res.Published || res.PublishedRev != uint64(i+1) {
			t.Fatalf("dev-a sync #%d unexpected published rev: %+v", i+1, res)
		}
		revA = res.PublishedRev
	}

	mock.mu.Lock()
	_, hasRev1 := mock.files["/dav/StillPoets/state/device_dev-a_1.json.gz"]
	_, hasRev2 := mock.files["/dav/StillPoets/state/device_dev-a_2.json.gz"]
	_, hasRev3 := mock.files["/dav/StillPoets/state/device_dev-a_3.json.gz"]
	// 2. 模拟设备 A 发布 rev 4 时中途断网，留下截断损坏的 gzip 文件！
	mock.files["/dav/StillPoets/state/device_dev-a_4.json.gz"] = []byte{0x1f, 0x8b, 0x08, 0x00, 0xde, 0xad}
	mock.mu.Unlock()

	if hasRev1 || !hasRev2 || !hasRev3 {
		t.Fatalf("retention mismatch: rev1=%v rev2=%v rev3=%v (want false, true, true)", hasRev1, hasRev2, hasRev3)
	}

	// 3. 设备 B 同步：遇到 A 的坏文件 rev 4，自动回退读取 A 的有效 rev 3，合并后发布自己的 rev 1，且标记 PartialSync=true
	dataB := SnapshotData{
		Quotes: []domain.Quote{
			{ID: "q2", Content: "面朝大海，春暖花开", BodyVer: v(1, "dev-b"), CreatedAt: 150},
		},
		Progress: []domain.ReadingProgress{
			{
				BookID:      "b1",
				LastPoemID:  "p2",
				PositionVer: v(1, "dev-b"),
				Status:      domain.ReadStatusReading,
				StatusVer:   v(1, "dev-b"),
				ReadPoemIDs: []string{"p1", "p2"},
			},
		},
	}
	resB, err := engine.Sync(ctx, SyncRequest{
		InstallID:    "dev-b",
		LocalRev:     0,
		LocalCounter: 1,
		LocalData:    dataB,
		HasDirty:     true,
	})
	if err != nil {
		t.Fatalf("dev-b sync failed despite valid fallback rev: %v", err)
	}
	if !resB.PartialSync || len(resB.Warnings) != 1 || resB.Warnings[0].Rev != 4 {
		t.Fatalf("expected PartialSync warning on corrupted rev 4, got partial=%v warnings=%+v", resB.PartialSync, resB.Warnings)
	}
	if len(resB.MergedData.Groups) != 1 || len(resB.MergedData.Quotes) != 2 {
		t.Fatalf("dev-b merged data incomplete: groups=%d quotes=%d", len(resB.MergedData.Groups), len(resB.MergedData.Quotes))
	}
	if !resB.Published || resB.PublishedRev != 1 {
		t.Fatalf("dev-b failed to publish rev 1: %+v", resB)
	}
	// Lamport counter 必须吸收 A 的最大计数（4）
	if resB.MaxCounter < 4 {
		t.Fatalf("dev-b MaxCounter = %d, want >= 4", resB.MaxCounter)
	}

	// 4. 手动退役旧设备 dev-a：确认 dev-b rev 1 有效后清理 dev-a 所有文件
	if err := engine.RetireDevice(ctx, "dev-b", resB.PublishedRev, "dev-a"); err != nil {
		t.Fatalf("RetireDevice failed: %v", err)
	}
	mock.mu.Lock()
	for k := range mock.files {
		if strings.Contains(k, "device_dev-a_") {
			t.Fatalf("retired device file still exists: %s", k)
		}
	}
	mock.mu.Unlock()
}

func TestUploadBookAssetsAndWorkspaceDetection(t *testing.T) {
	mock := newMockWebDAV()
	// 预先放一个旧版 manifest.json 验证旧工作区检测
	mock.dirs["/dav/StillPoets/"] = true
	mock.files["/dav/StillPoets/manifest.json"] = []byte(`{"schemaVersion":1}`)

	srv := mock.server()
	defer srv.Close()

	client, err := webdav.NewClient(webdav.Config{
		BaseURL:        srv.URL + "/dav/",
		AtomicUpload:   false,
		RetryBaseDelay: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	ctx := context.Background()

	initRes, err := EnsureWorkspace(ctx, client)
	if err != nil {
		t.Fatalf("EnsureWorkspace failed: %v", err)
	}
	if initRes.ExistingRemote || !initRes.LegacyWorkspace {
		t.Fatalf("expected LegacyWorkspace=true & ExistingRemote=false, got %+v", initRes)
	}

	sp := &domain.SPBook{
		BookID:       "book-1",
		Title:        "海子诗选",
		Author:       "海子",
		SourceFormat: "epub",
		ParsedAt:     100,
		Poems: []domain.Poem{
			{ID: "p1", BookID: "book-1", OrderIndex: 0, Title: "九月", Stanzas: []domain.Stanza{{Lines: []string{"目击众神死亡的草原上野花一片"}}}},
		},
	}
	spBytes, spHash, err := EncodeSPBook(sp)
	if err != nil {
		t.Fatalf("EncodeSPBook failed: %v", err)
	}
	book := domain.Book{
		ID:          "book-1",
		Title:       "海子诗选",
		Author:      "海子",
		ContentHash: spHash,
		ContentSize: int64(len(spBytes)),
		RawFilename: "海子诗选_a1b2c3.epub",
		PoemCount:   1,
		ContentVer:  v(1, "dev-a"),
	}

	engine := NewEngine(client)
	uploadRes, err := engine.UploadBookAssets(ctx, book, spBytes, []byte("PK-fake-epub-bytes"), []byte("webp-cover"))
	if err != nil {
		t.Fatalf("UploadBookAssets failed: %v", err)
	}
	if uploadRes.CoverWarning != nil {
		t.Fatalf("unexpected cover warning: %v", uploadRes.CoverWarning)
	}

	// 验证解析包、原始 epub、封面均已落盘
	mock.mu.Lock()
	var keys []string
	for k := range mock.files {
		keys = append(keys, k)
	}
	mock.mu.Unlock()
	slices.Sort(keys)
	for _, want := range []string{
		"/dav/StillPoets/books/book-1/parsed.spbook.json.gz",
		"/dav/StillPoets/books/book-1/海子诗选_a1b2c3.epub",
		"/dav/StillPoets/books/book-1/cover.webp",
	} {
		if !slices.Contains(keys, want) {
			t.Fatalf("missing uploaded asset %q in %v", want, keys)
		}
	}
}

func TestParseSnapshotFilenameStrict(t *testing.T) {
	valid := []struct {
		name string
		id   string
		rev  uint64
	}{
		{"device_abc123_1.json.gz", "abc123", 1},
		{"device_iphone-15_42.json.gz", "iphone-15", 42},
	}
	for _, tc := range valid {
		id, rev, ok := ParseSnapshotFilename(tc.name)
		if !ok || id != tc.id || rev != tc.rev {
			t.Fatalf("ParseSnapshotFilename(%q) = (%q, %d, %v), want (%q, %d, true)", tc.name, id, rev, ok, tc.id, tc.rev)
		}
	}

	invalid := []string{
		"device_bad_id_1.json.gz",
		"device_abc_0.json.gz",
		"device_abc_01.json.gz",
		"device_abc_1.json",
		"manifest.json",
		".device_abc_1.json.gz.tmp",
	}
	for _, name := range invalid {
		if _, _, ok := ParseSnapshotFilename(name); ok {
			t.Fatalf("ParseSnapshotFilename(%q) unexpectedly succeeded", name)
		}
	}
}

func TestRetireDeviceCoveredAbortsOnNewerRev(t *testing.T) {
	mock := newMockWebDAV()
	srv := mock.server()
	defer srv.Close()

	client, err := webdav.NewClient(webdav.Config{
		BaseURL:        srv.URL + "/dav/",
		RetryBaseDelay: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx := context.Background()
	engine := NewEngine(client)

	// 设备 A 发布 rev=1，设备 B 发布 rev=1
	if _, err := engine.Sync(ctx, SyncRequest{InstallID: "dev-a", ForcePush: true}); err != nil {
		t.Fatalf("sync A: %v", err)
	}
	resB, err := engine.Sync(ctx, SyncRequest{InstallID: "dev-b", ForcePush: true})
	if err != nil || resB.SyncedDeviceRevs["dev-a"] != 1 {
		t.Fatalf("sync B: err=%v synced=%+v", err, resB.SyncedDeviceRevs)
	}

	// 在退役前，设备 A 又偷偷发布了 rev=2
	if _, err := engine.Sync(ctx, SyncRequest{InstallID: "dev-a", LocalRev: 1, ForcePush: true}); err != nil {
		t.Fatalf("sync A rev2: %v", err)
	}

	// 设备 B 拿着只覆盖到 rev=1 的证明尝试退役 A，必须被拒绝，且 A 的 rev=2 必须保留
	if err := engine.RetireDeviceCovered(ctx, "dev-b", resB.PublishedRev, "dev-a", 1); err == nil {
		t.Fatalf("expected RetireDeviceCovered to abort when target has rev 2 > covered 1")
	}
	if _, err := client.Get(ctx, "state/device_dev-a_2.json.gz"); err != nil {
		t.Fatalf("target newer rev 2 should still exist, err=%v", err)
	}
}

func TestDecodeSnapshotRejectsOversizedLamportCounter(t *testing.T) {
	raw, err := EncodeSnapshot(&DeviceSnapshot{
		SchemaVersion: ProtocolSchemaVersion,
		InstallID:     "dev-bad",
		Rev:           1,
		MaxCounter:    ^uint64(0),
	})
	if err != nil {
		t.Fatalf("EncodeSnapshot: %v", err)
	}
	if _, err := DecodeSnapshot(raw, "dev-bad", 1); err == nil {
		t.Fatalf("expected DecodeSnapshot to reject MaxCounter > MaxInt64")
	}
}
