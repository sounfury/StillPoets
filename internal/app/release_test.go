package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v0.2.0", "0.1.0", 1},
		{"0.1.0", "v0.1.0", 0},
		{"v0.1.10", "v0.1.2", 1},
		{"v0.1.0-beta.1", "v0.1.0", -1},
		{"v0.1.0", "v0.1.0-rc.1", 1},
		{"v0.1.0-beta.2", "v0.1.0-beta.1", 1},
		{"v1.0.0", "v2.0.0", -1},
	}
	for _, tc := range cases {
		got := CompareSemver(tc.a, tc.b)
		if got != tc.want {
			t.Fatalf("CompareSemver(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestReleaseCheckerUpdateAndChangelog(t *testing.T) {
	pubTime := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)

	t.Run("new release available and draft filtered", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/releases/latest":
				_ = json.NewEncoder(w).Encode(ReleaseInfo{
					TagName:     "v0.2.0",
					Name:        "StillPoets v0.2.0",
					Body:        "- 新增设置页二级页面\n- 支持分析日志导出",
					HTMLURL:     "https://github.com/sounfury/StillPoets/releases/tag/v0.2.0",
					PublishedAt: pubTime,
					Assets: []ReleaseAsset{
						{Name: "StillPoets-0.2.0-android-arm64.apk", Size: 18 << 20},
					},
				})
			case "/releases":
				_ = json.NewEncoder(w).Encode([]ReleaseInfo{
					{TagName: "v0.3.0-draft", Draft: true},
					{TagName: "v0.2.0", Name: "v0.2.0", Body: "新版本", PublishedAt: pubTime},
					{TagName: "v0.1.0", Name: "v0.1.0", Body: "首发版本", PublishedAt: pubTime.Add(-24 * time.Hour)},
				})
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		rc := &ReleaseChecker{
			BaseAPIURL: srv.URL,
			HTTPClient: srv.Client(),
			UserAgent:  "StillPoets/0.1.0",
		}

		res, err := rc.CheckLatestRelease(t.Context(), "0.1.0")
		if err != nil {
			t.Fatalf("CheckLatestRelease: %v", err)
		}
		if !res.HasUpdate || res.Latest == nil || res.Latest.TagName != "v0.2.0" {
			t.Fatalf("expected update to v0.2.0, got %+v", res)
		}
		if len(res.Latest.Assets) != 1 || res.Latest.Assets[0].Name != "StillPoets-0.2.0-android-arm64.apk" {
			t.Fatalf("unexpected assets: %+v", res.Latest.Assets)
		}

		// 若当前已经是 0.2.0 则无更新
		resSame, err := rc.CheckLatestRelease(t.Context(), "0.2.0")
		if err != nil {
			t.Fatalf("CheckLatestRelease same: %v", err)
		}
		if resSame.HasUpdate {
			t.Fatalf("expected HasUpdate=false for same version")
		}

		// 拉取更新日志列表应自动过滤 draft
		list, err := rc.FetchReleases(t.Context(), 10)
		if err != nil {
			t.Fatalf("FetchReleases: %v", err)
		}
		if len(list) != 2 || list[0].TagName != "v0.2.0" || list[1].TagName != "v0.1.0" {
			t.Fatalf("unexpected releases list: %+v", list)
		}
	})

	t.Run("no releases yet returns NoRelease", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/releases/latest":
				http.NotFound(w, r)
			case "/releases":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`[]`))
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		rc := &ReleaseChecker{BaseAPIURL: srv.URL, HTTPClient: srv.Client()}
		res, err := rc.CheckLatestRelease(t.Context(), "0.1.0")
		if err != nil {
			t.Fatalf("CheckLatestRelease: %v", err)
		}
		if !res.NoRelease || res.HasUpdate {
			t.Fatalf("expected NoRelease=true HasUpdate=false, got %+v", res)
		}
	})
}
