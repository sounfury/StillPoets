package webdav

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEnsureDirAndAtomicPut(t *testing.T) {
	var (
		mu     sync.Mutex
		mkcols []string
		files  = map[string][]byte{}
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch r.Method {
		case "PROPFIND":
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?>
<d:multistatus xmlns:d="DAV:">
  <d:response>
    <d:href>` + r.URL.EscapedPath() + `</d:href>
    <d:propstat>
      <d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop>
      <d:status>HTTP/1.1 200 OK</d:status>
    </d:propstat>
  </d:response>
</d:multistatus>`))
		case "MKCOL":
			mkcols = append(mkcols, r.URL.Path)
			if r.URL.Path == "/dav/StillPoets/" {
				w.WriteHeader(http.StatusMethodNotAllowed) // 模拟目录已存在
				return
			}
			w.WriteHeader(http.StatusCreated)
		case http.MethodPut:
			data, _ := io.ReadAll(r.Body)
			files[r.URL.Path] = data
			w.Header().Set("ETag", `"v1"`)
			w.WriteHeader(http.StatusCreated)
		case "MOVE":
			dst := r.Header.Get("Destination")
			idx := strings.Index(dst, "/dav/")
			dstPath := dst[idx:]
			files[dstPath] = files[r.URL.Path]
			delete(files, r.URL.Path)
			w.Header().Set("ETag", `"v2"`)
			w.WriteHeader(http.StatusNoContent)
		case http.MethodGet:
			data, ok := files[r.URL.Path]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("ETag", `"v2"`)
			_, _ = w.Write(data)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	client, err := NewClient(Config{
		BaseURL:        srv.URL + "/dav",
		Username:       "poet",
		Password:       "secret",
		AtomicUpload:   true,
		RetryBaseDelay: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	ctx := context.Background()
	if err := client.CheckConnection(ctx); err != nil {
		t.Fatalf("CheckConnection failed: %v", err)
	}
	if err := client.EnsureDir(ctx, "state/sub"); err != nil {
		t.Fatalf("EnsureDir failed: %v", err)
	}
	wantDirs := []string{"/dav/StillPoets/", "/dav/StillPoets/state/", "/dav/StillPoets/state/sub/"}
	if len(mkcols) != len(wantDirs) {
		t.Fatalf("mkcols = %v, want %v", mkcols, wantDirs)
	}
	for i := range wantDirs {
		if mkcols[i] != wantDirs[i] {
			t.Fatalf("mkcols[%d] = %q, want %q", i, mkcols[i], wantDirs[i])
		}
	}

	if _, err := client.Put(ctx, "state/device_a_1.json.gz", []byte(`{"ok":true}`), "application/gzip"); err != nil {
		t.Fatalf("Atomic Put failed: %v", err)
	}
	got, etag, err := client.GetWithETag(ctx, "state/device_a_1.json.gz")
	if err != nil {
		t.Fatalf("GetWithETag failed: %v", err)
	}
	if string(got) != `{"ok":true}` || etag != `"v2"` {
		t.Fatalf("got (%q, %q), want (%q, %q)", string(got), etag, `{"ok":true}`, `"v2"`)
	}
}

func TestDirectPutAndRetry429WithRetryAfter(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if attempts == 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if r.Method != http.MethodPut || r.URL.Path != "/dav/StillPoets/state/device_a_1.json.gz" {
			t.Errorf("unexpected direct put request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	client, err := NewClient(Config{
		BaseURL:        srv.URL + "/dav/",
		AtomicUpload:   false,
		MaxRetries:     3,
		RetryBaseDelay: time.Millisecond,
		MaxRetryDelay:  10 * time.Millisecond, // 上限截断便于单测快速执行
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	start := time.Now()
	if _, err := client.Put(context.Background(), "state/device_a_1.json.gz", []byte(`{}`), "application/gzip"); err != nil {
		t.Fatalf("Put with 429 retry failed: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
	if time.Since(start) < 8*time.Millisecond {
		t.Fatalf("expected Retry-After delay (capped at 10ms) to be respected")
	}
}

func TestResolveURLPreservesEscapedBasePath(t *testing.T) {
	client, err := NewClient(Config{
		BaseURL: "https://dav.example.com/root/foo%2Fbar/",
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	got := client.resolveURL("books/海子 诗选.epub", true, false)
	if !strings.Contains(got, "/root/foo%2Fbar/StillPoets/books/") {
		t.Fatalf("resolveURL broke escaped base path: %s", got)
	}
}

func TestRedirectSafetyBlocksPutDowngradeAndCrossOrigin(t *testing.T) {
	otherSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer otherSrv.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "downgrade.gz"):
			// 302 会导致默认 Go 客户端将 PUT 改写为 GET
			http.Redirect(w, r, "/dav/StillPoets/state/target", http.StatusFound)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "cross.gz"):
			http.Redirect(w, r, otherSrv.URL+"/steal", http.StatusFound)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	client, err := NewClient(Config{
		BaseURL:        srv.URL + "/dav/",
		Username:       "u",
		Password:       "p",
		RetryBaseDelay: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	if _, err := client.Put(context.Background(), "state/downgrade.gz", []byte("hi"), "application/gzip"); !errors.Is(err, ErrUnsafeRedirect) {
		t.Fatalf("expected ErrUnsafeRedirect on PUT 302 downgrade, got %v", err)
	}
	if _, err := client.Get(context.Background(), "state/cross.gz"); !errors.Is(err, ErrUnsafeRedirect) {
		t.Fatalf("expected ErrUnsafeRedirect on cross-origin redirect, got %v", err)
	}
}

func TestGetIdentityPreservesRawGzipBytesAndEnforcesLimit(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = gz.Write([]byte(`{"hello":"poetry"}`))
	_ = gz.Close()
	rawGzip := buf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept-Encoding"); got != "identity" {
			t.Errorf("Accept-Encoding = %q, want identity", got)
		}
		// 模拟反代错误附加 Content-Encoding: gzip
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "application/gzip")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(rawGzip)
	}))
	defer srv.Close()

	client, err := NewClient(Config{
		BaseURL:        srv.URL + "/dav/",
		RetryBaseDelay: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	got, _, err := client.GetWithLimit(context.Background(), "state/device_a_1.json.gz", 1024)
	if err != nil {
		t.Fatalf("GetWithLimit failed: %v", err)
	}
	if !bytes.Equal(got, rawGzip) {
		t.Fatalf("raw gzip bytes were altered (transparent decompression happened)")
	}

	if _, _, err := client.GetWithLimit(context.Background(), "state/device_a_1.json.gz", 5); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("expected ErrResponseTooLarge, got %v", err)
	}
}

func TestEnsureDirRejectsRegularFileOn405(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "MKCOL":
			w.WriteHeader(http.StatusMethodNotAllowed)
		case "PROPFIND":
			w.WriteHeader(http.StatusMultiStatus)
			// 返回普通文件（无 <d:collection/>）
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?>
<d:multistatus xmlns:d="DAV:">
  <d:response>
    <d:href>` + r.URL.EscapedPath() + `</d:href>
    <d:propstat>
      <d:prop><d:resourcetype/></d:prop>
      <d:status>HTTP/1.1 200 OK</d:status>
    </d:propstat>
  </d:response>
</d:multistatus>`))
		}
	}))
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL + "/dav/"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if err := client.EnsureDir(context.Background(), "state"); err == nil {
		t.Fatalf("expected EnsureDir to fail when 405 target is not a collection")
	}
}

func TestListDirMultiPropstatStatusAndExcludesSelf(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?>
<d:multistatus xmlns:d="DAV:">
  <d:response>
    <d:href>/dav/StillPoets/state/</d:href>
    <d:propstat>
      <d:prop><d:displayname>state</d:displayname><d:resourcetype><d:collection/></d:resourcetype></d:prop>
      <d:status>HTTP/1.1 200 OK</d:status>
    </d:propstat>
  </d:response>
  <d:response>
    <d:href>/dav/StillPoets/state/device_iphone-1_3.json.gz</d:href>
    <d:propstat>
      <d:prop><d:displayname>显示名可不同</d:displayname><d:getcontentlength>512</d:getcontentlength></d:prop>
      <d:status>HTTP/1.1 200 OK</d:status>
    </d:propstat>
    <d:propstat>
      <d:prop><d:getetag/></d:prop>
      <d:status>HTTP/1.1 404 Not Found</d:status>
    </d:propstat>
  </d:response>
  <d:response>
    <d:href>/dav/StillPoets/state/forbidden.json.gz</d:href>
    <d:status>HTTP/1.1 403 Forbidden</d:status>
  </d:response>
  <d:response>
    <d:href>/dav/StillPoets/state/nested/deep.json.gz</d:href>
    <d:propstat>
      <d:prop><d:displayname>deep.json.gz</d:displayname></d:prop>
      <d:status>HTTP/1.1 200 OK</d:status>
    </d:propstat>
  </d:response>
</d:multistatus>`))
	}))
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL + "/dav/"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	entries, err := client.ListDir(context.Background(), "state")
	if err != nil {
		t.Fatalf("ListDir failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 valid direct child, got %d: %+v", len(entries), entries)
	}
	if entries[0].Name != "device_iphone-1_3.json.gz" || entries[0].ContentLength != 512 {
		t.Fatalf("unexpected entry: %+v", entries[0])
	}
}

func TestNewClientRejectsEmbeddedCredentials(t *testing.T) {
	if _, err := NewClient(Config{BaseURL: "https://user:pass@dav.example.com/dav/"}); err == nil {
		t.Fatalf("expected NewClient to reject embedded userinfo in BaseURL")
	}
}

func TestCrossOriginHTTPSGetRedirectStripsCredentials(t *testing.T) {
	var cdnGotAuth string
	cdnSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cdnGotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("poem-payload"))
	}))
	defer cdnSrv.Close()

	davSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "cdn.gz") {
			http.Redirect(w, r, cdnSrv.URL+"/dl/cdn.gz", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer davSrv.Close()

	client, err := NewClient(Config{
		BaseURL:                     davSrv.URL + "/dav/",
		Username:                    "u",
		Password:                    "secret",
		AllowCrossOriginGetRedirect: true,
		AllowInsecureTLS:            true,
		RetryBaseDelay:              time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	got, err := client.Get(context.Background(), "books/cdn.gz")
	if err != nil {
		t.Fatalf("cross-origin HTTPS GET failed: %v", err)
	}
	if string(got) != "poem-payload" {
		t.Fatalf("unexpected payload: %q", string(got))
	}
	if cdnGotAuth != "" {
		t.Fatalf("Authorization leaked to cross-origin CDN: %q", cdnGotAuth)
	}
}
