package app

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"stillpoets/internal/domain"
	"stillpoets/internal/storage"
	booksync "stillpoets/internal/sync"
	"stillpoets/internal/sync/webdav"
)

func TestParseDeeplinkAndLineRanges(t *testing.T) {
	target, err := ParseDeeplink("stillpoets://poem/book123/poem456?lines=4-6,9&highlight=1")
	if err != nil {
		t.Fatalf("ParseDeeplink poem: %v", err)
	}
	if target.Kind != DeeplinkPoem || target.BookID != "book123" || target.PoemID != "poem456" || !target.Highlight {
		t.Fatalf("unexpected target: %+v", target)
	}
	if len(target.LineRanges) != 2 || !target.LineRanges[0].Contains(5) || !target.LineRanges[1].Contains(9) || target.LineRanges[0].Contains(7) {
		t.Fatalf("unexpected ranges: %+v", target.LineRanges)
	}

	qt, err := ParseDeeplink("stillpoets://quote/q999")
	if err != nil || qt.Kind != DeeplinkQuote || qt.QuoteID != "q999" {
		t.Fatalf("ParseDeeplink quote = %+v, err = %v", qt, err)
	}

	if got := FormatLineRanges([]int{2, 3, 4, 7, 9, 10}); got != "2-4,7,9-10" {
		t.Fatalf("FormatLineRanges = %q, want 2-4,7,9-10", got)
	}
}

func TestImportEPUBQuotesAndWidgetTimeline(t *testing.T) {
	ctx := t.Context()
	svc, err := Open(ctx, t.TempDir(), NewMemorySecretStore())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer svc.Close()

	epubPath := findTestEPUB(t)
	imported, err := svc.ImportEPUB(ctx, epubPath, "")
	if err != nil {
		t.Fatalf("ImportEPUB: %v", err)
	}
	if imported.Book.PoemCount == 0 || imported.Book.LocalState != domain.LocalStateDownloaded {
		t.Fatalf("unexpected imported book: %+v", imported.Book)
	}

	poems, err := svc.ListPoems(ctx, imported.Book.ID)
	if err != nil || len(poems) == 0 {
		t.Fatalf("ListPoems: len=%d err=%v", len(poems), err)
	}
	var firstPoem domain.Poem
	for _, p := range poems {
		if p.IsPoem() {
			firstPoem = p
			break
		}
	}
	if firstPoem.ID == "" {
		t.Fatalf("no formal poem found in %s", epubPath)
	}

	// 今日重读应返回真实诗篇内容
	today, err := svc.PickTodayReread(ctx)
	if err != nil || today == nil || today.Excerpt == "" {
		t.Fatalf("PickTodayReread = %+v, err=%v", today, err)
	}

	// 同一首诗多次划选不同段落生成多张诗歌节选散页 + 1 张独立散句
	q1, err := svc.CreatePoemExcerptQuote(ctx, imported.Book, firstPoem, "第一段摘录", "1-2")
	if err != nil {
		t.Fatalf("CreatePoemExcerptQuote 1: %v", err)
	}
	q2, err := svc.CreatePoemExcerptQuote(ctx, imported.Book, firstPoem, "第二段摘录", "4-5")
	if err != nil {
		t.Fatalf("CreatePoemExcerptQuote 2: %v", err)
	}
	qStandalone, err := svc.CreateStandaloneQuote(ctx, "自己记下的一句散句", "作者甲", "某本笔记")
	if err != nil {
		t.Fatalf("CreateStandaloneQuote: %v", err)
	}

	// 切到散页小组件池，校验诗歌节选跳回原诗、独立散句不跳原诗
	var notifiedPath, notifiedFonts string
	svc.SetWidgetUpdatedCallback(func(timelinePath, fontsDir string) {
		notifiedPath = timelinePath
		notifiedFonts = fontsDir
	})
	settings, err := svc.LoadSettings(ctx)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	settings.WidgetMode = domain.WidgetModeQuotesRandom
	if err := svc.SaveSettings(ctx, settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if notifiedPath != svc.WidgetTimelinePath() || notifiedFonts != svc.FontsDir() {
		t.Fatalf("widget callback mismatch: path=%q fonts=%q", notifiedPath, notifiedFonts)
	}

	tl, err := svc.ReadWidgetTimeline()
	if err != nil {
		t.Fatalf("ReadWidgetTimeline: %v", err)
	}
	if len(tl.Entries) != 3 {
		t.Fatalf("expected 3 widget entries, got %d", len(tl.Entries))
	}
	links := map[string]string{}
	for _, e := range tl.Entries {
		links[e.DisplayText] = e.Deeplink
	}
	if !strings.HasPrefix(links[q1.Content], "stillpoets://poem/"+imported.Book.ID+"/"+firstPoem.ID) {
		t.Fatalf("poem excerpt deeplink = %q", links[q1.Content])
	}
	if !strings.HasPrefix(links[q2.Content], "stillpoets://poem/"+imported.Book.ID+"/"+firstPoem.ID) {
		t.Fatalf("second excerpt deeplink = %q", links[q2.Content])
	}
	if links[qStandalone.Content] != "stillpoets://quote/"+qStandalone.ID {
		t.Fatalf("standalone deeplink = %q", links[qStandalone.Content])
	}

	// 删除原诗集后，诗歌节选文字保留并降级为独立散句（不再跳原诗）
	if err := svc.DeleteBook(ctx, imported.Book.ID); err != nil {
		t.Fatalf("DeleteBook: %v", err)
	}
	quotes, err := svc.ListQuotes(ctx, storage.QuoteFilter{})
	if err != nil || len(quotes) != 3 {
		t.Fatalf("ListQuotes after delete book: len=%d err=%v", len(quotes), err)
	}
	for _, qv := range quotes {
		if qv.EffectiveKind != domain.QuoteKindStandalone {
			t.Fatalf("expected degraded standalone kind, got %+v", qv)
		}
		if !strings.HasPrefix(qv.Deeplink, "stillpoets://quote/") {
			t.Fatalf("expected quote deeplink after book delete, got %q", qv.Deeplink)
		}
	}
}

func TestTwoDeviceWebDAVSyncAndOnDemandDownload(t *testing.T) {
	srv := newMockDAVServer(t)
	defer srv.Close()

	ctx := t.Context()
	devA, err := Open(ctx, t.TempDir(), NewMemorySecretStore())
	if err != nil {
		t.Fatalf("Open devA: %v", err)
	}
	defer devA.Close()

	devB, err := Open(ctx, t.TempDir(), NewMemorySecretStore())
	if err != nil {
		t.Fatalf("Open devB: %v", err)
	}
	defer devB.Close()

	cfg := WebDAVConfigInput{
		Enabled:      true,
		BaseURL:      srv.URL + "/dav",
		Username:     "poet",
		Password:     "secret-pass",
		AppFolder:    "StillPoets",
		AtomicUpload: false,
	}
	if _, err := devA.TestWebDAVConnection(ctx, cfg); err != nil {
		t.Fatalf("TestWebDAVConnection: %v", err)
	}
	if err := devA.SaveWebDAVConfig(ctx, cfg); err != nil {
		t.Fatalf("SaveWebDAVConfig A: %v", err)
	}
	if err := devB.SaveWebDAVConfig(ctx, cfg); err != nil {
		t.Fatalf("SaveWebDAVConfig B: %v", err)
	}

	// 验证密码未明文写入 webdav_profile.json
	profileRaw, err := os.ReadFile(filepath.Join(devA.DataDir(), webdavProfileFilename))
	if err != nil || strings.Contains(string(profileRaw), "secret-pass") {
		t.Fatalf("password leaked or profile unreadable: err=%v raw=%s", err, string(profileRaw))
	}

	// 设备 A 导入真实 EPUB 并同步
	imported, err := devA.ImportEPUB(ctx, findTestEPUB(t), "")
	if err != nil {
		t.Fatalf("ImportEPUB A: %v", err)
	}
	if _, err := devA.SyncNow(ctx, false); err != nil {
		t.Fatalf("SyncNow A: %v", err)
	}

	// 设备 B 同步：只拿元数据，正文保持 remote 未下载
	if _, err := devB.SyncNow(ctx, false); err != nil {
		t.Fatalf("SyncNow B: %v", err)
	}
	booksB, err := devB.ListBooks(ctx, "")
	if err != nil || len(booksB) != 1 {
		t.Fatalf("devB books = %+v, err=%v", booksB, err)
	}
	if booksB[0].LocalState != domain.LocalStateRemote {
		t.Fatalf("expected remote state on devB before download, got %s", booksB[0].LocalState)
	}

	// 设备 B 按需下载该书正文
	if err := devB.EnsureBookContent(ctx, imported.Book.ID); err != nil {
		t.Fatalf("EnsureBookContent B: %v", err)
	}
	bookB, err := devB.GetBook(ctx, imported.Book.ID)
	if err != nil || bookB.LocalState != domain.LocalStateDownloaded {
		t.Fatalf("expected downloaded state on devB, got %+v err=%v", bookB, err)
	}
	poemsB, err := devB.ListPoems(ctx, imported.Book.ID)
	if err != nil || len(poemsB) == 0 {
		t.Fatalf("devB poems after on-demand download: len=%d err=%v", len(poemsB), err)
	}

	// 设备 B 退役设备 A
	ovA, err := devA.GetSyncOverview(ctx)
	if err != nil {
		t.Fatalf("GetSyncOverview A: %v", err)
	}
	if err := devB.RetireRemoteDevice(ctx, ovA.InstallID); err != nil {
		t.Fatalf("RetireRemoteDevice: %v", err)
	}
}

func TestClearBookCacheProtectsPendingUpload(t *testing.T) {
	ctx := t.Context()
	epubPath := findTestEPUB(t)
	svc, err := Open(ctx, t.TempDir(), NewMemorySecretStore())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer svc.Close()

	imp, err := svc.ImportEPUB(ctx, epubPath, "")
	if err != nil {
		t.Fatalf("ImportEPUB: %v", err)
	}
	if err := svc.ClearBookCache(ctx, imp.Book.ID); err == nil {
		t.Fatalf("expected ClearBookCache to reject book with pending upload")
	}
}

func TestWidgetRefreshIntervalAndEditorialPaperTheme(t *testing.T) {
	ctx := t.Context()
	epubPath := findTestEPUB(t)
	svc, err := Open(ctx, t.TempDir(), NewMemorySecretStore())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer svc.Close()

	imp, err := svc.ImportEPUB(ctx, epubPath, "")
	if err != nil {
		t.Fatalf("ImportEPUB: %v", err)
	}

	// 验证原书封面文件在导入后本地可直接定位（若有封面）
	coverPath, err := svc.EnsureBookCover(ctx, imp.Book.ID)
	if err != nil {
		t.Fatalf("EnsureBookCover: %v", err)
	}
	if imp.Book.CoverHash != "" && coverPath == "" {
		t.Fatalf("expected local cover path for book with coverHash %s", imp.Book.CoverHash)
	}

	// 验证小组件切换间隔持久化 + 刊物风纸白主题自动同步至 widget_timeline.json + 原书封面开关持久化
	st, err := svc.UpdateSettings(ctx, func(s *domain.Settings) {
		s.ThemePreset = "editorial"
		s.UseOriginalCover = true
		s.WidgetRefreshMinutes = 180
		s.WidgetTheme.Preset = "editorial"
		s.WidgetTheme.BgColor = "#FAFAF8" // 模拟老版本缓存的偏黄背景色
	})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if !st.UseOriginalCover || st.WidgetRefreshMinutes != 180 {
		t.Fatalf("unexpected saved settings: %+v", st)
	}

	tl, err := svc.ReadWidgetTimeline()
	if err != nil {
		t.Fatalf("ReadWidgetTimeline: %v", err)
	}
	if tl.RefreshIntervalMinutes != 180 {
		t.Fatalf("timeline RefreshIntervalMinutes = %d, want 180", tl.RefreshIntervalMinutes)
	}
	if tl.Theme.BgColor != "#FFFFFF" {
		t.Fatalf("expected Editorial paper white #FFFFFF in widget timeline, got %s", tl.Theme.BgColor)
	}
}

func findTestEPUB(t *testing.T) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join("..", "..", "test-books", "*.epub"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no test EPUB found in test-books/: err=%v", err)
	}
	return matches[0]
}

func TestSyncPersistsRemoteDeviceRevsAndSkipsUnchangedGET(t *testing.T) {
	var mu sync.Mutex
	requests := make(map[string]int)
	handler := mockDAVHandler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests[r.Method+" "+r.URL.Path]++
		mu.Unlock()
		handler.ServeHTTP(w, r)
	}))
	defer srv.Close()
	cfg := syncTestConfig(srv.URL)
	remote := syncTestRemote(t, cfg, "dev-remote")
	ctx := t.Context()
	dataDir, secrets := t.TempDir(), NewMemorySecretStore()
	svc, err := Open(ctx, dataDir, secrets)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if err := svc.SaveWebDAVConfig(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if res, err := svc.SyncNow(ctx, false); err != nil || !res.Published || res.SyncedDeviceRevs["dev-remote"] != 1 {
		t.Fatalf("initial sync = %+v, %v", res, err)
	}
	assertRemoteRevs := func(want map[string]uint64) {
		t.Helper()
		meta, err := svc.Store().GetSyncMeta(ctx)
		if err != nil || !maps.Equal(meta.KnownRemoteRevs, want) {
			t.Fatalf("remote revs = %v, %v, want %v", meta.KnownRemoteRevs, err, want)
		}
	}
	assertRemoteRevs(map[string]uint64{"dev-remote": 1})
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	svc, err = Open(ctx, dataDir, secrets)
	if err != nil {
		t.Fatal(err)
	}
	assertRemoteRevs(map[string]uint64{"dev-remote": 1})
	mu.Lock()
	clear(requests)
	mu.Unlock()
	if res, err := svc.SyncNow(ctx, false); err != nil || res.Published || len(res.RemoteDevices) != 1 || res.SyncedDeviceRevs["dev-remote"] != 1 {
		t.Fatalf("unchanged sync = %+v, %v", res, err)
	}
	mu.Lock()
	got := maps.Clone(requests)
	mu.Unlock()
	want := map[string]int{
		"PROPFIND /dav/StillPoets/state/": 1,
		"PROPFIND /dav/StillPoets/inbox/": 1,
	}
	if !maps.Equal(got, want) {
		t.Errorf("unchanged sync requests = %v, want %v", got, want)
	}

	bad, _ := booksync.SnapshotPath("dev-remote", 2)
	if _, err := remote.Put(ctx, bad, []byte("corrupt"), booksync.ContentTypeGzip); err != nil {
		t.Fatal(err)
	}
	if res, err := svc.SyncNow(ctx, false); err != nil || !res.PartialSync || !res.IncompleteDevices["dev-remote"] || len(res.SyncedDeviceRevs) != 0 {
		t.Fatalf("sync with corrupt latest rev = %+v, %v", res, err)
	}
	assertRemoteRevs(map[string]uint64{"dev-remote": 1})
	if err := svc.RetireRemoteDevice(ctx, "dev-remote"); err == nil {
		t.Fatal("retirement accepted an incomplete remote device")
	}
	if err := remote.Delete(ctx, bad); err != nil {
		t.Fatal(err)
	}
	if err := svc.RetireRemoteDevice(ctx, "dev-remote"); err != nil {
		t.Fatalf("retirement of known verified rev: %v", err)
	}
	if res, err := svc.SyncNow(ctx, false); err != nil || len(res.RemoteDevices) != 0 {
		t.Fatalf("sync after remote deletion = %+v, %v", res, err)
	}
	assertRemoteRevs(nil)
}

func TestSyncWorkspaceClientLifecycle(t *testing.T) {
	var mu sync.Mutex
	var connections []string
	closed := make(chan string, 16)
	handler := mockDAVHandler()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		connections = append(connections, r.RemoteAddr)
		mu.Unlock()
		handler.ServeHTTP(w, r)
	}))
	srv.Config.ConnState = func(c net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			closed <- c.RemoteAddr().String()
		}
	}
	srv.Start()
	defer srv.Close()
	cfg := syncTestConfig(srv.URL)
	syncTestRemote(t, cfg, "dev-remote").CloseIdleConnections()
	nextCfg := cfg
	nextCfg.AppFolder = "Other"
	syncTestRemote(t, nextCfg, "dev-remote").CloseIdleConnections()
	ctx := t.Context()
	svc, err := Open(ctx, t.TempDir(), NewMemorySecretStore())
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	if err := svc.SaveWebDAVConfig(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	client, epoch, err := svc.newWebDAVClientWithEpoch()
	if err != nil {
		t.Fatal(err)
	}
	lastConnection := func() string {
		mu.Lock()
		defer mu.Unlock()
		return connections[len(connections)-1]
	}
	var firstAddr string
	for round := range 2 {
		if res, err := svc.SyncNow(ctx, false); err != nil || res.SyncedDeviceRevs["dev-remote"] != 1 {
			t.Fatalf("sync = %+v, %v", res, err)
		}
		if addr := lastConnection(); round == 0 {
			firstAddr = addr
		} else if addr != firstAddr {
			t.Errorf("second sync used connection %s, want %s", addr, firstAddr)
		}
	}
	if reused, gotEpoch, err := svc.newWebDAVClientWithEpoch(); err != nil || reused != client || gotEpoch != epoch {
		t.Fatalf("client not reused: %p epoch=%d err=%v, want %p epoch=%d", reused, gotEpoch, err, client, epoch)
	}
	assertClosed := func(addr string) {
		t.Helper()
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		for {
			select {
			case got := <-closed:
				if got == addr {
					return
				}
			case <-timer.C:
				t.Fatalf("connection %s stayed open", addr)
			}
		}
	}
	assertReset := func() {
		t.Helper()
		meta, err := svc.Store().GetSyncMeta(ctx)
		if err != nil || meta.KnownRemoteRevs != nil || meta.LocalRev != 0 {
			t.Fatalf("workspace sync state not reset: %+v, %v", meta, err)
		}
	}

	oldAddr := lastConnection()
	if err := svc.SaveWebDAVConfig(ctx, nextCfg); err != nil {
		t.Fatal(err)
	}
	assertClosed(oldAddr)
	assertReset()
	nextClient, nextEpoch, err := svc.newWebDAVClientWithEpoch()
	if err != nil || nextClient == client || nextEpoch == epoch {
		t.Fatalf("workspace reused old client or epoch: %p epoch=%d err=%v", nextClient, nextEpoch, err)
	}
	if res, err := svc.SyncNow(ctx, false); err != nil || !res.Published || res.SyncedDeviceRevs["dev-remote"] != 1 {
		t.Fatalf("new workspace sync = %+v, %v", res, err)
	}
	oldAddr = lastConnection()
	if err := svc.ClearWebDAVConfig(); err != nil {
		t.Fatal(err)
	}
	assertClosed(oldAddr)
	assertReset()
	if c, _, err := svc.newWebDAVClientWithEpoch(); err == nil || c != nil {
		t.Fatalf("disconnected service returned client %p, err=%v", c, err)
	}
	if err := svc.SaveWebDAVConfig(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SyncNow(ctx, false); err != nil {
		t.Fatal(err)
	}
	oldAddr = lastConnection()
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	assertClosed(oldAddr)
	if c, _, err := svc.newWebDAVClientWithEpoch(); err == nil || c != nil {
		t.Fatalf("closed service returned client %p, err=%v", c, err)
	}
}

func TestSyncWorkspaceSwitchCancelsInFlightSync(t *testing.T) {
	started := make(chan struct{})
	serverRelease := make(chan struct{})
	handler := mockDAVHandler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PROPFIND" && r.URL.Path == "/dav/StillPoets/state/" {
			close(started)
			<-serverRelease
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer srv.Close()
	defer close(serverRelease)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	svc, err := Open(ctx, t.TempDir(), NewMemorySecretStore())
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	cfg := syncTestConfig(srv.URL)
	if err := svc.SaveWebDAVConfig(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		_, err := svc.SyncNow(ctx, false)
		finished <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("first sync did not start")
	}
	cfg.AppFolder = "Other"
	if err := svc.SaveWebDAVConfig(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("old workspace sync returned %v, want context.Canceled", err)
	}
	meta, err := svc.Store().GetSyncMeta(ctx)
	if err != nil || meta.KnownRemoteRevs != nil || meta.LocalRev != 0 {
		t.Fatalf("old workspace left sync state: %+v, %v", meta, err)
	}
	if res, err := svc.SyncNow(ctx, false); err != nil || !res.Published || res.PublishedRev != 1 {
		t.Fatalf("new workspace sync = %+v, %v", res, err)
	}
}

func syncTestConfig(baseURL string) WebDAVConfigInput {
	return WebDAVConfigInput{
		BaseURL: baseURL + "/dav", AppFolder: "StillPoets", Enabled: true,
		Username: "poet", Password: "secret-pass",
	}
}

func syncTestRemote(t *testing.T, cfg WebDAVConfigInput, id string) *webdav.Client {
	t.Helper()
	client, err := webdav.NewClient(webdav.Config{
		BaseURL: cfg.BaseURL, AppFolder: cfg.AppFolder, Username: cfg.Username, Password: cfg.Password,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.CloseIdleConnections)
	if _, err := booksync.NewEngine(client).Sync(t.Context(), booksync.SyncRequest{InstallID: id, ForcePush: true}); err != nil {
		t.Fatal(err)
	}
	return client
}

// 轻量内存 WebDAV 服务器，支持 OPTIONS / MKCOL / PUT / GET / DELETE / PROPFIND。
func newMockDAVServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(mockDAVHandler())
}

func mockDAVHandler() http.Handler {
	var mu sync.Mutex
	dirs := map[string]bool{"/dav": true}
	files := map[string][]byte{}

	clean := func(p string) string {
		c := path.Clean("/" + p)
		return strings.TrimSuffix(c, "/")
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "poet" || pass != "secret-pass" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		p := clean(r.URL.Path)

		mu.Lock()
		defer mu.Unlock()

		switch r.Method {
		case http.MethodOptions:
			w.Header().Set("DAV", "1, 2")
			w.WriteHeader(http.StatusOK)
		case "MKCOL":
			dirs[p] = true
			w.WriteHeader(http.StatusCreated)
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			files[p] = body
			dirs[path.Dir(p)] = true
			w.Header().Set("ETag", `"etag-1"`)
			w.WriteHeader(http.StatusCreated)
		case http.MethodGet:
			data, ok := files[p]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("ETag", `"etag-1"`)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
		case http.MethodDelete:
			delete(files, p)
			delete(dirs, p)
			w.WriteHeader(http.StatusNoContent)
		case "PROPFIND":
			if !dirs[p] {
				if _, isFile := files[p]; !isFile {
					w.WriteHeader(http.StatusNotFound)
					return
				}
			}
			type resType struct {
				Collection *struct{} `xml:"D:collection,omitempty"`
			}
			type prop struct {
				ResourceType  resType `xml:"D:resourcetype"`
				ContentLength int     `xml:"D:getcontentlength,omitempty"`
			}
			type propstat struct {
				Prop   prop   `xml:"D:prop"`
				Status string `xml:"D:status"`
			}
			type response struct {
				Href     string   `xml:"D:href"`
				PropStat propstat `xml:"D:propstat"`
			}
			type multistatus struct {
				XMLName   xml.Name   `xml:"D:multistatus"`
				XmlnsD    string     `xml:"xmlns:D,attr"`
				Responses []response `xml:"D:response"`
			}
			ms := multistatus{XmlnsD: "DAV:"}
			if dirs[p] {
				ms.Responses = append(ms.Responses, response{
					Href:     r.URL.EscapedPath(),
					PropStat: propstat{Prop: prop{ResourceType: resType{Collection: &struct{}{}}}, Status: "HTTP/1.1 200 OK"},
				})
			} else {
				ms.Responses = append(ms.Responses, response{
					Href:     r.URL.EscapedPath(),
					PropStat: propstat{Prop: prop{ContentLength: len(files[p])}, Status: "HTTP/1.1 200 OK"},
				})
			}
			prefix := p + "/"
			for fp := range files {
				if strings.HasPrefix(fp, prefix) && !strings.Contains(strings.TrimPrefix(fp, prefix), "/") {
					ms.Responses = append(ms.Responses, response{
						Href:     fp,
						PropStat: propstat{Status: "HTTP/1.1 200 OK"},
					})
				}
			}
			for dp := range dirs {
				if dp != p && strings.HasPrefix(dp, prefix) && !strings.Contains(strings.TrimPrefix(dp, prefix), "/") {
					ms.Responses = append(ms.Responses, response{
						Href:     dp + "/",
						PropStat: propstat{Prop: prop{ResourceType: resType{Collection: &struct{}{}}}, Status: "HTTP/1.1 200 OK"},
					})
				}
			}
			out, _ := xml.Marshal(ms)
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = w.Write(out)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}
