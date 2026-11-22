package app

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"stillpoets/internal/domain"
	"stillpoets/internal/storage"
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

func findTestEPUB(t *testing.T) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join("..", "..", "test-books", "*.epub"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no test EPUB found in test-books/: err=%v", err)
	}
	return matches[0]
}

// 轻量内存 WebDAV 服务器，支持 OPTIONS / MKCOL / PUT / GET / DELETE / PROPFIND。
func newMockDAVServer(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	dirs := map[string]bool{"/dav": true}
	files := map[string][]byte{}

	clean := func(p string) string {
		c := path.Clean("/" + p)
		return strings.TrimSuffix(c, "/")
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				ResourceType resType `xml:"D:resourcetype"`
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
			ms.Responses = append(ms.Responses, response{
				Href:     p + "/",
				PropStat: propstat{Prop: prop{ResourceType: resType{Collection: &struct{}{}}}, Status: "HTTP/1.1 200 OK"},
			})
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
	}))
}
