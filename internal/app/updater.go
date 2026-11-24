package app

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// maxAPKDownloadBytes 限制单个 APK 下载体积上限（512 MiB），防止恶意或异常响应耗尽存储。
	maxAPKDownloadBytes int64 = 512 << 20
	// maxChecksumBytes 限制校验文件读取上限（256 KiB）。
	maxChecksumBytes int64 = 256 << 10
)

// APKDownloadProgress 描述应用内 APK 下载实时进度快照。
type APKDownloadProgress struct {
	Downloaded       int64
	Total            int64
	SpeedBytesPerSec float64
	ReusedCache      bool
}

// APKDownloadResult 描述一次 APK 下载与完整性校验完成后的结果。
type APKDownloadResult struct {
	FilePath    string
	AssetName   string
	Size        int64
	SHA256      string
	VerifiedSHA bool
	ReusedCache bool
}

// APKDownloader 负责安全下载 GitHub Release 中的 Android APK 并校验完整性。
type APKDownloader struct {
	HTTPClient        *http.Client
	UserAgent         string
	AllowInsecureHTTP bool // 仅限单元测试使用，生产环境必须为 false
}

// NewAPKDownloader 创建默认 APK 下载器（强制 HTTPS，禁止重定向降级至 HTTP）。
func NewAPKDownloader(appVersion string) *APKDownloader {
	ver := strings.TrimSpace(appVersion)
	if ver == "" {
		ver = DefaultAppVersion
	}
	return &APKDownloader{
		HTTPClient: &http.Client{
			Timeout: 0, // 下载超时由调用方 ctx 与传输层控制，避免慢网大包被硬截断
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				TLSHandshakeTimeout:   15 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
				IdleConnTimeout:       60 * time.Second,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return errors.New("重定向次数过多")
				}
				if len(via) > 0 && strings.EqualFold(via[0].URL.Scheme, "https") && !strings.EqualFold(req.URL.Scheme, "https") {
					return errors.New("拒绝从 HTTPS 重定向降级至不安全的 HTTP 链接")
				}
				return nil
			},
		},
		UserAgent: "StillPoets/" + ver,
	}
}

// SelectAndroidAPKAsset 从 Release 资产列表中按设备 ABI 优先级挑选最匹配的 Android APK。
func SelectAndroidAPKAsset(assets []ReleaseAsset, supportedABIs []string) (*ReleaseAsset, bool) {
	apks := make([]ReleaseAsset, 0, len(assets))
	for _, a := range assets {
		name := strings.TrimSpace(a.Name)
		dl := strings.TrimSpace(a.DownloadURL)
		if name == "" || dl == "" {
			continue
		}
		if strings.HasSuffix(strings.ToLower(name), ".apk") {
			apks = append(apks, a)
		}
	}
	if len(apks) == 0 {
		return nil, false
	}

	abis := normalizeABIs(supportedABIs)
	if len(abis) == 0 {
		abis = defaultABIsForGOARCH(runtime.GOARCH)
	}

	// 1. 按设备 ABI 优先级精确匹配
	for _, abi := range abis {
		for i := range apks {
			if apkMatchesABI(apks[i].Name, abi) {
				cp := apks[i]
				return &cp, true
			}
		}
	}

	// 2. 其次选择 universal 通用包
	for i := range apks {
		if strings.Contains(strings.ToLower(apks[i].Name), "universal") {
			cp := apks[i]
			return &cp, true
		}
	}

	// 3. 若只有一个 APK 且未标明冲突架构，直接回退使用
	if len(apks) == 1 && !hasConflictingABI(apks[0].Name, abis) {
		cp := apks[0]
		return &cp, true
	}

	// 4. 兜底返回未标明冲突架构的第一个 APK
	for i := range apks {
		if !hasConflictingABI(apks[i].Name, abis) {
			cp := apks[i]
			return &cp, true
		}
	}
	return nil, false
}

func normalizeABIs(abis []string) []string {
	out := make([]string, 0, len(abis))
	for _, raw := range abis {
		s := strings.ToLower(strings.TrimSpace(raw))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func defaultABIsForGOARCH(goarch string) []string {
	switch strings.ToLower(strings.TrimSpace(goarch)) {
	case "arm64":
		return []string{"arm64-v8a"}
	case "arm":
		return []string{"armeabi-v7a"}
	case "amd64":
		return []string{"x86_64"}
	case "386":
		return []string{"x86"}
	default:
		return []string{"arm64-v8a"}
	}
}

func apkMatchesABI(filename, abi string) bool {
	lower := strings.ToLower(filename)
	switch abi {
	case "arm64-v8a", "arm64", "aarch64":
		return strings.Contains(lower, "arm64") || strings.Contains(lower, "aarch64")
	case "armeabi-v7a", "armeabi", "arm", "armv7":
		if strings.Contains(lower, "arm64") || strings.Contains(lower, "aarch64") {
			return false
		}
		return strings.Contains(lower, "armeabi") || strings.Contains(lower, "armv7") ||
			strings.Contains(lower, "-arm.") || strings.Contains(lower, "_arm.") || strings.Contains(lower, "-arm-")
	case "x86_64", "amd64":
		return strings.Contains(lower, "x86_64") || strings.Contains(lower, "amd64")
	case "x86", "386":
		return strings.Contains(lower, "x86") && !strings.Contains(lower, "x86_64")
	default:
		return false
	}
}

func hasConflictingABI(filename string, deviceABIs []string) bool {
	if len(deviceABIs) == 0 {
		return false
	}
	known := []string{"arm64-v8a", "armeabi-v7a", "x86_64", "x86"}
	tagged := ""
	for _, k := range known {
		if apkMatchesABI(filename, k) {
			tagged = k
			break
		}
	}
	if tagged == "" {
		return false
	}
	for _, d := range deviceABIs {
		if apkMatchesABI(filename, d) {
			return false
		}
	}
	return true
}

// FindChecksumAsset 查找与目标 APK 匹配的 SHA-256 校验文件资产（优先 <apk>.sha256，其次 SHA256SUMS.txt）。
func FindChecksumAsset(assets []ReleaseAsset, apkName string) (*ReleaseAsset, bool) {
	wantExact := strings.ToLower(strings.TrimSpace(apkName) + ".sha256")
	for i := range assets {
		if strings.TrimSpace(assets[i].DownloadURL) == "" {
			continue
		}
		if strings.ToLower(strings.TrimSpace(assets[i].Name)) == wantExact {
			cp := assets[i]
			return &cp, true
		}
	}
	for i := range assets {
		if strings.TrimSpace(assets[i].DownloadURL) == "" {
			continue
		}
		lower := strings.ToLower(strings.TrimSpace(assets[i].Name))
		if lower == "sha256sums.txt" || lower == "sha256sums" || lower == "checksums.txt" {
			cp := assets[i]
			return &cp, true
		}
	}
	return nil, false
}

// ParseSHA256ForFile 从 sha256sum 输出或纯十六进制摘要文本中提取指定文件名的 SHA-256。
func ParseSHA256ForFile(content, targetFilename string) (string, bool) {
	want := strings.ToLower(filepath.Base(strings.TrimSpace(targetFilename)))
	scanner := bufio.NewScanner(strings.NewReader(content))
	var singleHash string
	validLines := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		digest := strings.ToLower(fields[0])
		if !isHexSHA256(digest) {
			continue
		}
		validLines++
		if len(fields) == 1 {
			singleHash = digest
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		name = strings.ToLower(filepath.Base(name))
		if name == want {
			return digest, true
		}
	}
	if validLines == 1 && singleHash != "" {
		return singleHash, true
	}
	return "", false
}

func isHexSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// FetchExpectedSHA256 下载并解析 Release 中附带的 SHA-256 校验值；若 Release 未提供校验文件则返回 "", false, nil。
func (d *APKDownloader) FetchExpectedSHA256(ctx context.Context, assets []ReleaseAsset, apkName string) (string, bool, error) {
	csAsset, ok := FindChecksumAsset(assets, apkName)
	if !ok || csAsset == nil {
		return "", false, nil
	}
	if err := d.validateURL(csAsset.DownloadURL); err != nil {
		return "", false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, csAsset.DownloadURL, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("User-Agent", d.userAgent())

	resp, err := d.client().Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", false, fmt.Errorf("校验文件返回 HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxChecksumBytes))
	if err != nil {
		return "", false, err
	}
	digest, found := ParseSHA256ForFile(string(raw), apkName)
	if !found {
		return "", false, fmt.Errorf("校验文件 %s 中未找到 %s 的 SHA-256 摘要", csAsset.Name, apkName)
	}
	return digest, true, nil
}

// DownloadAPK 流式下载 APK 至 destDir，并按期望大小/SHA-256 校验通过后原子落盘。
// 若本地缓存已存在同名且完整吻合的 APK，则直接复用避免重复下载。
func (d *APKDownloader) DownloadAPK(
	ctx context.Context,
	destDir string,
	asset ReleaseAsset,
	expectedSHA256 string,
	onProgress func(APKDownloadProgress),
) (APKDownloadResult, error) {
	var res APKDownloadResult
	if err := d.validateURL(asset.DownloadURL); err != nil {
		return res, err
	}
	safeName := sanitizeAPKFilename(asset.Name)
	if safeName == "" {
		return res, errors.New("无效的 APK 文件名")
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return res, fmt.Errorf("创建更新缓存目录失败：%w", err)
	}

	destPath := filepath.Join(destDir, safeName)
	expectedSHA := strings.ToLower(strings.TrimSpace(expectedSHA256))

	// 检查已有缓存是否可安全复用
	if cached, ok := tryReuseCachedAPK(destPath, asset.Size, expectedSHA); ok {
		if onProgress != nil {
			onProgress(APKDownloadProgress{
				Downloaded:  cached.Size,
				Total:       cached.Size,
				ReusedCache: true,
			})
		}
		cached.AssetName = safeName
		return cached, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.DownloadURL, nil)
	if err != nil {
		return res, err
	}
	req.Header.Set("User-Agent", d.userAgent())
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := d.client().Do(req)
	if err != nil {
		return res, fmt.Errorf("下载请求失败：%w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return res, fmt.Errorf("下载失败：服务器返回 HTTP %d", resp.StatusCode)
	}

	total := asset.Size
	if total <= 0 && resp.ContentLength > 0 {
		total = resp.ContentLength
	}
	if total > maxAPKDownloadBytes {
		return res, fmt.Errorf("安装包体积超出安全限制（%d 字节）", total)
	}

	tmpPath := fmt.Sprintf("%s.part.%d", destPath, time.Now().UnixNano())
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return res, fmt.Errorf("创建临时文件失败：%w", err)
	}
	cleanupTmp := func() {
		_ = f.Close()
		_ = os.Remove(tmpPath)
	}

	hasher := sha256.New()
	writer := io.MultiWriter(f, hasher)
	buf := make([]byte, 32*1024)
	var downloaded int64
	started := time.Now()
	lastReport := started
	var lastBytes int64

	if onProgress != nil {
		onProgress(APKDownloadProgress{Downloaded: 0, Total: total})
	}

	limited := io.LimitReader(resp.Body, maxAPKDownloadBytes+1)
	for {
		if err := ctx.Err(); err != nil {
			cleanupTmp()
			return res, err
		}
		n, readErr := limited.Read(buf)
		if n > 0 {
			downloaded += int64(n)
			if downloaded > maxAPKDownloadBytes {
				cleanupTmp()
				return res, errors.New("安装包体积超出安全限制")
			}
			if _, werr := writer.Write(buf[:n]); werr != nil {
				cleanupTmp()
				return res, fmt.Errorf("写入安装包失败：%w", werr)
			}
			now := time.Now()
			if onProgress != nil && (now.Sub(lastReport) >= 120*time.Millisecond || downloaded-lastBytes >= 256*1024) {
				elapsed := now.Sub(started).Seconds()
				speed := 0.0
				if elapsed > 0.05 {
					speed = float64(downloaded) / elapsed
				}
				onProgress(APKDownloadProgress{
					Downloaded:       downloaded,
					Total:            total,
					SpeedBytesPerSec: speed,
				})
				lastReport = now
				lastBytes = downloaded
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			cleanupTmp()
			if errors.Is(ctx.Err(), context.Canceled) {
				return res, context.Canceled
			}
			return res, fmt.Errorf("下载中断：%w", readErr)
		}
	}

	if err := f.Sync(); err != nil {
		cleanupTmp()
		return res, fmt.Errorf("落盘失败：%w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return res, fmt.Errorf("关闭文件失败：%w", err)
	}

	if downloaded == 0 {
		_ = os.Remove(tmpPath)
		return res, errors.New("下载的安装包为空文件")
	}
	if asset.Size > 0 && downloaded != asset.Size {
		_ = os.Remove(tmpPath)
		return res, fmt.Errorf("安装包大小校验失败：期望 %d 字节，实际 %d 字节", asset.Size, downloaded)
	}

	actualSHA := hex.EncodeToString(hasher.Sum(nil))
	if expectedSHA != "" && !strings.EqualFold(actualSHA, expectedSHA) {
		_ = os.Remove(tmpPath)
		return res, errors.New("安装包 SHA-256 完整性校验失败，请重试或前往 GitHub Releases 手动下载")
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return res, fmt.Errorf("保存安装包失败：%w", err)
	}

	// 清理同目录下的旧版缓存包
	CleanupStaleUpdateAPKs(destDir, safeName)

	elapsed := time.Since(started).Seconds()
	speed := 0.0
	if elapsed > 0.05 {
		speed = float64(downloaded) / elapsed
	}
	if onProgress != nil {
		onProgress(APKDownloadProgress{
			Downloaded:       downloaded,
			Total:            downloaded,
			SpeedBytesPerSec: speed,
		})
	}

	return APKDownloadResult{
		FilePath:    destPath,
		AssetName:   safeName,
		Size:        downloaded,
		SHA256:      actualSHA,
		VerifiedSHA: expectedSHA != "",
		ReusedCache: false,
	}, nil
}

// CleanupStaleUpdateAPKs 清理更新缓存目录中的历史安装包与未完成临时文件。
func CleanupStaleUpdateAPKs(destDir, keepFilename string) {
	entries, err := os.ReadDir(destDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if keepFilename != "" && name == keepFilename {
			continue
		}
		lower := strings.ToLower(name)
		if strings.HasSuffix(lower, ".apk") || strings.Contains(lower, ".apk.part.") {
			_ = os.Remove(filepath.Join(destDir, name))
		}
	}
}

func tryReuseCachedAPK(path string, expectedSize int64, expectedSHA string) (APKDownloadResult, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() <= 0 {
		return APKDownloadResult{}, false
	}
	if expectedSize > 0 && info.Size() != expectedSize {
		return APKDownloadResult{}, false
	}
	if expectedSHA == "" && expectedSize <= 0 {
		// 既无大小约束又无哈希约束时不盲目复用旧缓存
		return APKDownloadResult{}, false
	}
	f, err := os.Open(path)
	if err != nil {
		return APKDownloadResult{}, false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return APKDownloadResult{}, false
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if expectedSHA != "" && !strings.EqualFold(sum, expectedSHA) {
		return APKDownloadResult{}, false
	}
	return APKDownloadResult{
		FilePath:    path,
		Size:        info.Size(),
		SHA256:      sum,
		VerifiedSHA: expectedSHA != "",
		ReusedCache: true,
	}, true
}

func sanitizeAPKFilename(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "" || base == "." || base == ".." || strings.ContainsAny(base, `/\`) {
		return ""
	}
	if !strings.HasSuffix(strings.ToLower(base), ".apk") {
		return ""
	}
	return base
}

func (d *APKDownloader) validateURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return errors.New("无效的下载链接")
	}
	if strings.EqualFold(u.Scheme, "https") {
		return nil
	}
	if d.AllowInsecureHTTP && strings.EqualFold(u.Scheme, "http") {
		return nil
	}
	return fmt.Errorf("仅允许通过 HTTPS 下载更新包（收到 %q）", u.Scheme)
}

func (d *APKDownloader) client() *http.Client {
	if d != nil && d.HTTPClient != nil {
		return d.HTTPClient
	}
	return NewAPKDownloader(DefaultAppVersion).HTTPClient
}

func (d *APKDownloader) userAgent() string {
	if d != nil && strings.TrimSpace(d.UserAgent) != "" {
		return d.UserAgent
	}
	return "StillPoets/" + DefaultAppVersion
}

// UpdatesDir 返回存放应用内更新 APK 的私有目录（位于 dataDir 内，受 Android FileProvider 覆盖）。
func (s *Service) UpdatesDir() string {
	return filepath.Join(s.dataDir, "updates")
}

// CleanupUpdateCache 清理不再需要的历史安装包缓存（当当前版本已更新时可安全清空）。
func (s *Service) CleanupUpdateCache() {
	CleanupStaleUpdateAPKs(s.UpdatesDir(), "")
}

// DownloadReleaseAPK 为指定 Release 下载适配当前设备的 Android APK，并自动尝试执行 SHA-256 校验。
func (s *Service) DownloadReleaseAPK(
	ctx context.Context,
	currentVersion string,
	rel ReleaseInfo,
	supportedABIs []string,
	onProgress func(APKDownloadProgress),
) (APKDownloadResult, error) {
	asset, ok := SelectAndroidAPKAsset(rel.Assets, supportedABIs)
	if !ok || asset == nil {
		return APKDownloadResult{}, errors.New("该版本未附带适配当前设备的 Android 安装包")
	}
	dl := NewAPKDownloader(currentVersion)
	expectedSHA, hasSHA, shaErr := dl.FetchExpectedSHA256(ctx, rel.Assets, asset.Name)
	if shaErr != nil {
		// 仅当 Release 附带了校验文件但校验文件读取失败时记录日志；若无明确摘要则退化为大小校验
		s.LogEvent("update", "fetch apk sha256 skipped/failed (%s): %v", asset.Name, shaErr)
	}
	if !hasSHA {
		expectedSHA = ""
	}
	s.LogEvent("update", "download apk start: tag=%s asset=%s size=%d hasSHA=%v", rel.TagName, asset.Name, asset.Size, hasSHA)
	res, err := dl.DownloadAPK(ctx, s.UpdatesDir(), *asset, expectedSHA, onProgress)
	if err != nil {
		s.LogEvent("update", "download apk failed: tag=%s err=%v", rel.TagName, err)
		return res, err
	}
	s.LogEvent("update", "download apk ready: asset=%s size=%d verifiedSHA=%v reused=%v", res.AssetName, res.Size, res.VerifiedSHA, res.ReusedCache)
	return res, nil
}
