package webdav

import (
	"context"
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
		mu      sync.Mutex
		mkcols  []string
		methods []string
		files   = map[string][]byte{}
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		methods = append(methods, r.Method+" "+r.URL.Path)

		switch r.Method {
		case "PROPFIND":
			w.WriteHeader(http.StatusMultiStatus)
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
	if err := client.EnsureDir(ctx, "meta/sub"); err != nil {
		t.Fatalf("EnsureDir failed: %v", err)
	}
	wantDirs := []string{"/dav/StillPoets/", "/dav/StillPoets/meta/", "/dav/StillPoets/meta/sub/"}
	if len(mkcols) != len(wantDirs) {
		t.Fatalf("mkcols = %v, want %v", mkcols, wantDirs)
	}
	for i := range wantDirs {
		if mkcols[i] != wantDirs[i] {
			t.Fatalf("mkcols[%d] = %q, want %q", i, mkcols[i], wantDirs[i])
		}
	}

	if _, err := client.Put(ctx, "meta/library.json", []byte(`{"ok":true}`), "application/json"); err != nil {
		t.Fatalf("Atomic Put failed: %v", err)
	}
	got, etag, err := client.GetWithETag(ctx, "meta/library.json")
	if err != nil {
		t.Fatalf("GetWithETag failed: %v", err)
	}
	if string(got) != `{"ok":true}` || etag != `"v2"` {
		t.Fatalf("got (%q, %q), want (%q, %q)", string(got), etag, `{"ok":true}`, `"v2"`)
	}
}

func TestDirectPutAndRetry429(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if r.Method != http.MethodPut || r.URL.Path != "/dav/StillPoets/meta/progress.json" {
			t.Errorf("unexpected direct put request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	client, err := NewClient(Config{
		BaseURL:        srv.URL + "/dav/",
		AtomicUpload:   false, // 模拟 AList/115 关闭原子上传
		MaxRetries:     3,
		RetryBaseDelay: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	if _, err := client.Put(context.Background(), "meta/progress.json", []byte(`{}`), "application/json"); err != nil {
		t.Fatalf("Put with 429 retry failed: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}
