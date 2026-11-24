package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultAppVersion = "0.1.5"
	GitHubRepoOwner   = "sounfury"
	GitHubRepoName    = "StillPoets"
	GitHubReleasesURL = "https://github.com/sounfury/StillPoets/releases"
	GitHubAPIBaseURL  = "https://api.github.com/repos/sounfury/StillPoets"
	ContactEmail      = "a2133266@outlook.com"
)

// ReleaseAsset 描述单个 GitHub Release 附带的安装包或构建产物。
type ReleaseAsset struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	DownloadURL string `json:"browser_download_url"`
}

// ReleaseInfo 描述一条 GitHub Release 记录（用于更新检查与更新日志展示）。
type ReleaseInfo struct {
	TagName     string         `json:"tag_name"`
	Name        string         `json:"name"`
	Body        string         `json:"body"`
	HTMLURL     string         `json:"html_url"`
	PublishedAt time.Time      `json:"published_at"`
	Prerelease  bool           `json:"prerelease"`
	Draft       bool           `json:"draft"`
	Assets      []ReleaseAsset `json:"assets"`
}

// DisplayTitle 返回 Release 的展示标题（优先 Name，回退 TagName）。
func (r ReleaseInfo) DisplayTitle() string {
	name := strings.TrimSpace(r.Name)
	if name != "" {
		return name
	}
	return strings.TrimSpace(r.TagName)
}

// UpdateCheckResult 描述一次检查 GitHub Release 是否有新包的结果。
type UpdateCheckResult struct {
	CurrentVersion string
	Latest         *ReleaseInfo
	HasUpdate      bool
	NoRelease      bool
	CheckedAt      time.Time
}

// ReleaseChecker 负责查询 GitHub Releases 以检查更新与拉取更新日志。
type ReleaseChecker struct {
	BaseAPIURL string
	HTTPClient *http.Client
	UserAgent  string
}

// NewReleaseChecker 创建指向 StillPoets GitHub 仓库的 Release 检查器。
func NewReleaseChecker(appVersion string) *ReleaseChecker {
	ver := strings.TrimSpace(appVersion)
	if ver == "" {
		ver = DefaultAppVersion
	}
	return &ReleaseChecker{
		BaseAPIURL: GitHubAPIBaseURL,
		HTTPClient: &http.Client{Timeout: 12 * time.Second},
		UserAgent:  "StillPoets/" + ver,
	}
}

// CheckLatestRelease 检查仓库最新 Release 是否比 currentVersion 更新（仅检查不自动更新）。
func (rc *ReleaseChecker) CheckLatestRelease(ctx context.Context, currentVersion string) (UpdateCheckResult, error) {
	cur := strings.TrimSpace(currentVersion)
	if cur == "" {
		cur = DefaultAppVersion
	}
	res := UpdateCheckResult{
		CurrentVersion: cur,
		CheckedAt:      time.Now(),
	}

	latest, status, err := rc.fetchSingleRelease(ctx, "/releases/latest")
	if err == nil && latest != nil {
		res.Latest = latest
		res.HasUpdate = CompareSemver(latest.TagName, cur) > 0
		return res, nil
	}
	if status != http.StatusNotFound {
		return res, err
	}

	// 若尚未发布正式 latest（例如仅有 prerelease 或尚未发版），尝试回退读取最近一条非草稿 Release。
	list, listErr := rc.FetchReleases(ctx, 5)
	if listErr != nil {
		if errors.Is(listErr, errNoReleasesFound) {
			res.NoRelease = true
			return res, nil
		}
		return res, listErr
	}
	if len(list) == 0 {
		res.NoRelease = true
		return res, nil
	}
	top := list[0]
	res.Latest = &top
	res.HasUpdate = CompareSemver(top.TagName, cur) > 0
	return res, nil
}

var errNoReleasesFound = errors.New("no releases found")

// FetchReleases 拉取仓库最近的 GitHub Releases 列表（过滤草稿），用于更新日志页面展示。
func (rc *ReleaseChecker) FetchReleases(ctx context.Context, limit int) ([]ReleaseInfo, error) {
	if limit <= 0 || limit > 30 {
		limit = 10
	}
	endpoint := fmt.Sprintf("%s/releases?per_page=%d", strings.TrimRight(rc.apiBase(), "/"), limit)
	body, status, err := rc.doGET(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, errNoReleasesFound
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("GitHub API 返回 HTTP %d", status)
	}
	var raw []ReleaseInfo
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析 Release 列表失败：%w", err)
	}
	out := make([]ReleaseInfo, 0, len(raw))
	for _, item := range raw {
		if item.Draft {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func (rc *ReleaseChecker) fetchSingleRelease(ctx context.Context, suffix string) (*ReleaseInfo, int, error) {
	endpoint := strings.TrimRight(rc.apiBase(), "/") + suffix
	body, status, err := rc.doGET(ctx, endpoint)
	if err != nil {
		return nil, status, err
	}
	if status == http.StatusNotFound {
		return nil, status, errNoReleasesFound
	}
	if status < 200 || status >= 300 {
		return nil, status, fmt.Errorf("GitHub API 返回 HTTP %d", status)
	}
	var info ReleaseInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, status, fmt.Errorf("解析 Release 响应失败：%w", err)
	}
	return &info, status, nil
}

func (rc *ReleaseChecker) apiBase() string {
	if rc != nil && strings.TrimSpace(rc.BaseAPIURL) != "" {
		return rc.BaseAPIURL
	}
	return GitHubAPIBaseURL
}

func (rc *ReleaseChecker) doGET(ctx context.Context, endpoint string) ([]byte, int, error) {
	client := rc.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 12 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	ua := rc.UserAgent
	if ua == "" {
		ua = "StillPoets/" + DefaultAppVersion
	}
	req.Header.Set("User-Agent", ua)

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("网络请求失败：%w", err)
	}
	defer resp.Body.Close()

	const maxRespBytes = 2 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRespBytes))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("读取响应失败：%w", err)
	}
	return raw, resp.StatusCode, nil
}

// CompareSemver 比较两个语义化版本字符串（兼容前缀 v 与预发布后缀，如 v0.2.0 > 0.1.9 > 0.1.9-beta.1）。
// 返回 >0 表示 a > b，0 表示相等，<0 表示 a < b。
func CompareSemver(a, b string) int {
	pa := parseSemver(a)
	pb := parseSemver(b)
	for i := 0; i < 3; i++ {
		if pa.nums[i] != pb.nums[i] {
			if pa.nums[i] > pb.nums[i] {
				return 1
			}
			return -1
		}
	}
	// 正式版 > 同号预发布版
	switch {
	case pa.pre == "" && pb.pre == "":
		return 0
	case pa.pre == "" && pb.pre != "":
		return 1
	case pa.pre != "" && pb.pre == "":
		return -1
	default:
		return strings.Compare(pa.pre, pb.pre)
	}
}

type semverParts struct {
	nums [3]int
	pre  string
}

func parseSemver(raw string) semverParts {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	if idx := strings.IndexByte(s, '+'); idx >= 0 {
		s = s[:idx]
	}
	var out semverParts
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		out.pre = s[idx+1:]
		s = s[:idx]
	}
	segs := strings.Split(s, ".")
	for i := 0; i < 3 && i < len(segs); i++ {
		if n, err := strconv.Atoi(strings.TrimSpace(segs[i])); err == nil && n >= 0 {
			out.nums[i] = n
		}
	}
	return out
}

// CheckLatestRelease 是基于默认 GitHub 仓库的便捷检查入口。
func (s *Service) CheckLatestRelease(ctx context.Context, currentVersion string) (UpdateCheckResult, error) {
	checker := NewReleaseChecker(currentVersion)
	res, err := checker.CheckLatestRelease(ctx, currentVersion)
	if err != nil {
		s.LogEvent("update", "check latest release failed: %v", err)
		return res, err
	}
	if res.NoRelease {
		s.LogEvent("update", "check latest release: repository has no releases yet (current=%s)", res.CurrentVersion)
	} else if res.Latest != nil {
		s.LogEvent("update", "check latest release: current=%s latest=%s hasUpdate=%v", res.CurrentVersion, res.Latest.TagName, res.HasUpdate)
	}
	return res, nil
}

// FetchReleases 拉取仓库最近的 Release 更新日志并记录事件。
func (s *Service) FetchReleases(ctx context.Context, currentVersion string, limit int) ([]ReleaseInfo, error) {
	checker := NewReleaseChecker(currentVersion)
	list, err := checker.FetchReleases(ctx, limit)
	if err != nil {
		if errors.Is(err, errNoReleasesFound) {
			s.LogEvent("update", "fetch releases: no releases found")
			return nil, nil
		}
		s.LogEvent("update", "fetch releases failed: %v", err)
		return nil, err
	}
	s.LogEvent("update", "fetched %d releases from GitHub", len(list))
	return list, nil
}
