package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectAndroidAPKAsset(t *testing.T) {
	assets := []ReleaseAsset{
		{Name: "StillPoets-0.2.0-ios-arm64-unsigned.ipa", DownloadURL: "https://example.com/a.ipa", Size: 100},
		{Name: "StillPoets-0.2.0-android-universal.apk", DownloadURL: "https://example.com/uni.apk", Size: 300},
		{Name: "StillPoets-0.2.0-android-arm64.apk", DownloadURL: "https://example.com/arm64.apk", Size: 200},
		{Name: "StillPoets-0.2.0-android-arm64.apk.sha256", DownloadURL: "https://example.com/arm64.apk.sha256", Size: 96},
	}

	got, ok := SelectAndroidAPKAsset(assets, []string{"arm64-v8a", "armeabi-v7a"})
	if !ok || got == nil || got.Name != "StillPoets-0.2.0-android-arm64.apk" {
		t.Fatalf("expected arm64 apk, got %+v (ok=%v)", got, ok)
	}

	gotUni, ok := SelectAndroidAPKAsset(assets, []string{"x86_64"})
	if !ok || gotUni == nil || gotUni.Name != "StillPoets-0.2.0-android-universal.apk" {
		t.Fatalf("expected universal fallback for x86_64, got %+v (ok=%v)", gotUni, ok)
	}

	onlyArm64 := []ReleaseAsset{
		{Name: "StillPoets-0.2.0-android-arm64.apk", DownloadURL: "https://example.com/arm64.apk", Size: 200},
	}
	if _, ok := SelectAndroidAPKAsset(onlyArm64, []string{"x86_64"}); ok {
		t.Fatalf("expected mismatch when only arm64 apk exists on x86_64 device")
	}
}

func TestParseSHA256ForFile(t *testing.T) {
	h1 := strings.Repeat("a", 64)
	h2 := strings.Repeat("b", 64)
	content := h1 + "  StillPoets-0.2.0-android-arm64.apk\n" + h2 + " *StillPoets-0.2.0-android-universal.apk\n"

	got, ok := ParseSHA256ForFile(content, "StillPoets-0.2.0-android-universal.apk")
	if !ok || got != h2 {
		t.Fatalf("expected %s, got %s (ok=%v)", h2, got, ok)
	}

	gotSingle, ok := ParseSHA256ForFile(h1+"\n", "any.apk")
	if !ok || gotSingle != h1 {
		t.Fatalf("expected single digest %s, got %s (ok=%v)", h1, gotSingle, ok)
	}
}

func TestAPKDownloaderDownloadVerifyAndReuse(t *testing.T) {
	payload := []byte(strings.Repeat("StillPoetsAPKPayload", 4096))
	sumArr := sha256.Sum256(payload)
	wantSHA := hex.EncodeToString(sumArr[:])

	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dl := &APKDownloader{
		HTTPClient:        srv.Client(),
		UserAgent:         "StillPoets-Test/0.1.3",
		AllowInsecureHTTP: true,
	}

	dir := t.TempDir()
	// 放一个旧版残留 APK，验证下载新包后会顺手清理
	stalePath := filepath.Join(dir, "StillPoets-0.1.0-android-arm64.apk")
	if err := os.WriteFile(stalePath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	asset := ReleaseAsset{
		Name:        "StillPoets-0.2.0-android-arm64.apk",
		Size:        int64(len(payload)),
		DownloadURL: srv.URL + "/StillPoets-0.2.0-android-arm64.apk",
	}

	var progressCalls int
	res, err := dl.DownloadAPK(context.Background(), dir, asset, wantSHA, func(p APKDownloadProgress) {
		progressCalls++
	})
	if err != nil {
		t.Fatalf("DownloadAPK failed: %v", err)
	}
	if res.Size != int64(len(payload)) || res.SHA256 != wantSHA || !res.VerifiedSHA || res.ReusedCache {
		t.Fatalf("unexpected download result: %+v", res)
	}
	if progressCalls == 0 {
		t.Fatalf("expected progress callbacks")
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("expected stale apk to be cleaned up")
	}

	// 二次下载同版本：应直接复用缓存而不发网络请求
	res2, err := dl.DownloadAPK(context.Background(), dir, asset, wantSHA, nil)
	if err != nil {
		t.Fatalf("second DownloadAPK failed: %v", err)
	}
	if !res2.ReusedCache || requests != 1 {
		t.Fatalf("expected cache reuse with requests=1, got reused=%v requests=%d", res2.ReusedCache, requests)
	}

	// 错误的 SHA-256 必须拦截
	badDir := t.TempDir()
	if _, err := dl.DownloadAPK(context.Background(), badDir, asset, strings.Repeat("0", 64), nil); err == nil {
		t.Fatalf("expected sha256 mismatch error")
	}
}

func TestAPKDownloaderRejectsHTTPByDefault(t *testing.T) {
	dl := NewAPKDownloader("0.1.3")
	asset := ReleaseAsset{
		Name:        "StillPoets-0.2.0-android-arm64.apk",
		Size:        10,
		DownloadURL: "http://example.com/StillPoets-0.2.0-android-arm64.apk",
	}
	if _, err := dl.DownloadAPK(context.Background(), t.TempDir(), asset, "", nil); err == nil {
		t.Fatalf("expected non-HTTPS download URL to be rejected")
	}
}
