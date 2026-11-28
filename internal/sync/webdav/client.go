package webdav

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultAppFolder       = "StillPoets"
	DefaultUserAgent       = "StillPoets/0.1.3"
	DefaultConcurrency     = 2
	DefaultMaxRetries      = 3
	DefaultMaxRetryDelay   = 60 * time.Second
	DefaultMaxDownloadSize = 64 << 20 // 64 MiB
	maxControlBodySize     = 4 << 20  // 4 MiB for PROPFIND XML / error bodies
)

var (
	// ErrNotFound 表示远端文件或目录不存在（HTTP 404）。
	ErrNotFound = errors.New("webdav: resource not found")
	// ErrUnauthorized 表示 WebDAV 认证失败（HTTP 401/403）。
	ErrUnauthorized = errors.New("webdav: unauthorized")
	// ErrPreconditionFailed 表示 ETag 乐观锁校验未通过（HTTP 412）。
	ErrPreconditionFailed = errors.New("webdav: precondition failed")
	// ErrResponseTooLarge 表示远端响应体超过安全读取上限。
	ErrResponseTooLarge = errors.New("webdav: response body exceeds limit")
	// ErrUnsafeRedirect 表示遇到不安全或会改写 DAV 方法的 HTTP 重定向。
	ErrUnsafeRedirect = errors.New("webdav: unsafe redirect blocked")
	// ErrRateLimited 表示服务端通过 Retry-After 要求长退避，超过本轮最大重试等待上限。
	ErrRateLimited = errors.New("webdav: server rate limited")
)

// Config 描述 WebDAV 连接与兼容策略配置。
type Config struct {
	BaseURL                     string // 用户输入的 WebDAV 地址，例如 https://dav.jianguoyun.com/dav/
	Username                    string
	Password                    string
	AppFolder                   string        // 默认在 BaseURL 下创建的专属目录，留空则使用 StillPoets
	UserAgent                   string        // 可自定义以适配特定网盘限制
	AtomicUpload                bool          // 是否先 PUT 临时文件再 MOVE 覆盖（AList/115 如不兼容可关闭）
	AllowCrossOriginGetRedirect bool          // 允许 GET 跟随跨域 HTTPS 直链重定向（如 OpenList/115 302），并剥离凭据头
	AllowInsecureTLS            bool          // 仅允许局域网自签名 NAS 时显式开启
	MaxConcurrency              int           // 默认 2，避免触发坚果云/115 429 频控
	MaxRetries                  int           // 遇 429/502/503/504/临时网络错误的最大重试次数
	Timeout                     time.Duration // 单次请求超时
	RetryBaseDelay              time.Duration // 退避基础延迟（单元测试可设为极小值）
	MaxRetryDelay               time.Duration // 退避与 Retry-After 最大等待上限
	MaxDownloadBytes            int64         // 默认 GET 最大下载字节数，默认 64 MiB
}

// Entry 表示 PROPFIND 返回的单个远端资源。
type Entry struct {
	Name          string // 从 href 解析并单次解码后的直接子项文件名（优先用于协议识别）
	Href          string
	DisplayName   string // 仅用于 UI 展示
	IsDir         bool
	ContentLength int64
	ETag          string
	LastModified  string
}

type responseResult struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// Client 是零外部依赖的轻量 WebDAV 客户端，内置并发控制、安全重定向、自动建目录与退避重试。
type Client struct {
	cfg     Config
	baseURL *url.URL
	appRoot string
	http    *http.Client
	sem     chan struct{}

	dirMu       sync.Mutex
	ensuredDirs map[string]struct{}
}

// NewClient 校验配置并创建 WebDAV 客户端。
func NewClient(cfg Config) (*Client, error) {
	rawBase := strings.TrimSpace(cfg.BaseURL)
	if rawBase == "" {
		return nil, errors.New("webdav: base URL is required")
	}
	parsed, err := url.Parse(rawBase)
	if err != nil {
		return nil, fmt.Errorf("webdav: invalid base URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("webdav: unsupported scheme %q, only http/https allowed", parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, errors.New("webdav: base URL host is empty")
	}
	if parsed.User != nil {
		return nil, errors.New("webdav: base URL must not embed credentials")
	}
	// 保留原始已转义路径，同时确保以 "/" 结尾
	if parsed.RawPath != "" {
		if !strings.HasSuffix(parsed.RawPath, "/") {
			parsed.RawPath += "/"
		}
		if !strings.HasSuffix(parsed.Path, "/") {
			parsed.Path += "/"
		}
	} else if !strings.HasSuffix(parsed.Path, "/") {
		parsed.Path += "/"
	}

	if strings.TrimSpace(cfg.AppFolder) == "" {
		cfg.AppFolder = DefaultAppFolder
	}
	cfg.AppFolder = strings.Trim(strings.TrimSpace(cfg.AppFolder), "/")
	if strings.TrimSpace(cfg.UserAgent) == "" {
		cfg.UserAgent = DefaultUserAgent
	}
	if cfg.MaxConcurrency <= 0 {
		cfg.MaxConcurrency = DefaultConcurrency
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = DefaultMaxRetries
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.RetryBaseDelay <= 0 {
		cfg.RetryBaseDelay = 400 * time.Millisecond
	}
	if cfg.MaxRetryDelay <= 0 {
		cfg.MaxRetryDelay = DefaultMaxRetryDelay
	}
	if cfg.MaxDownloadBytes <= 0 {
		cfg.MaxDownloadBytes = DefaultMaxDownloadSize
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	// 禁用自动透明 gzip 解压，避免代理对 .gz 文件加 Content-Encoding: gzip 时破坏原始字节与哈希校验
	transport.DisableCompression = true
	if cfg.AllowInsecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // 用户显式允许自签名局域网 NAS
	}

	c := &Client{
		cfg:         cfg,
		baseURL:     parsed,
		appRoot:     cfg.AppFolder,
		sem:         make(chan struct{}, cfg.MaxConcurrency),
		ensuredDirs: make(map[string]struct{}),
	}
	c.http = &http.Client{
		Timeout:       cfg.Timeout,
		Transport:     transport,
		CheckRedirect: c.checkRedirect,
	}
	return c, nil
}

// CloseIdleConnections 关闭连接池中空闲的 HTTP 连接。
func (c *Client) CloseIdleConnections() {
	c.http.CloseIdleConnections()
}

// checkRedirect 仅允许同源且不改写 DAV 方法的重定向，防止 PUT 301/302 变 GET 误报成功或泄露 Basic Auth。
// 当启用 AllowCrossOriginGetRedirect 时，仅允许 GET 跳转到 https:// 目标，且强制剥离 Authorization/Cookie/Referer。
func (c *Client) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return fmt.Errorf("%w: too many redirects", ErrUnsafeRedirect)
	}
	prev := via[len(via)-1]

	// 禁止重定向改写 HTTP 方法（例如 PUT/PROPFIND/MKCOL 遇到 301/302/303 被降级为 GET）
	if req.Method != prev.Method {
		return fmt.Errorf("%w: method changed from %s to %s", ErrUnsafeRedirect, prev.Method, req.Method)
	}

	sameOrigin := strings.EqualFold(req.URL.Scheme, c.baseURL.Scheme) && strings.EqualFold(req.URL.Host, c.baseURL.Host)
	if !sameOrigin {
		// 仅允许显式开启时的 GET -> HTTPS 跨域直链（OpenList/115 302），且剥离所有敏感请求头
		if c.cfg.AllowCrossOriginGetRedirect && prev.Method == http.MethodGet && strings.EqualFold(req.URL.Scheme, "https") {
			req.Header.Del("Authorization")
			req.Header.Del("Cookie")
			req.Header.Del("Referer")
			req.Header.Set("Accept-Encoding", "identity")
			req.Header.Set("User-Agent", c.cfg.UserAgent)
			return nil
		}
		return fmt.Errorf("%w: cross-origin redirect to %s://%s", ErrUnsafeRedirect, req.URL.Scheme, req.URL.Host)
	}

	// 对非只读方法，只允许 307/308（规范保证保留方法与请求体）
	if prev.Method != http.MethodGet && prev.Method != http.MethodHead && req.Response != nil {
		code := req.Response.StatusCode
		if code != http.StatusTemporaryRedirect && code != http.StatusPermanentRedirect {
			return fmt.Errorf("%w: status %d on %s", ErrUnsafeRedirect, code, prev.Method)
		}
	}

	if c.cfg.Username != "" || c.cfg.Password != "" {
		req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	return nil
}

// AppFolder 返回当前使用的应用根目录名（如 StillPoets）。
func (c *Client) AppFolder() string {
	return c.appRoot
}

// resolveURL 将应用根目录内的相对路径转换为完整且正确转义的 WebDAV URL。
// 严格保留 BaseURL 中原有的已转义路径段（如 %2F），仅对新增路径段执行 PathEscape。
func (c *Client) resolveURL(relPath string, includeAppRoot bool, isDir bool) string {
	u := *c.baseURL

	var rawSegments []string
	baseEscaped := strings.TrimSuffix(u.EscapedPath(), "/")
	if baseEscaped != "" {
		for _, s := range strings.Split(strings.TrimPrefix(baseEscaped, "/"), "/") {
			if s != "" {
				rawSegments = append(rawSegments, s)
			}
		}
	}
	if includeAppRoot && c.appRoot != "" {
		for _, s := range strings.Split(c.appRoot, "/") {
			if s != "" {
				rawSegments = append(rawSegments, url.PathEscape(s))
			}
		}
	}
	cleaned := strings.Trim(strings.TrimSpace(relPath), "/")
	if cleaned != "" && cleaned != "." {
		for _, s := range strings.Split(cleaned, "/") {
			if s != "" && s != "." && s != ".." {
				rawSegments = append(rawSegments, url.PathEscape(s))
			}
		}
	}

	rawPath := "/" + strings.Join(rawSegments, "/")
	if isDir && !strings.HasSuffix(rawPath, "/") {
		rawPath += "/"
	}

	decodedSegments := make([]string, len(rawSegments))
	for i, seg := range rawSegments {
		if unesc, err := url.PathUnescape(seg); err == nil {
			decodedSegments[i] = unesc
		} else {
			decodedSegments[i] = seg
		}
	}
	plainPath := "/" + strings.Join(decodedSegments, "/")
	if isDir && !strings.HasSuffix(plainPath, "/") {
		plainPath += "/"
	}

	u.Path = plainPath
	u.RawPath = rawPath
	return u.String()
}

// do 统一执行带并发信号量限制（覆盖完整响应体读取）、BasicAuth、User-Agent、
// Retry-After 感知与有上限指数退避+抖动重试的 HTTP 请求。
func (c *Client) do(ctx context.Context, method, targetURL string, body []byte, headers map[string]string, maxBodyBytes int64) (*responseResult, error) {
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	var lastErr error
	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, targetURL, bodyReader)
		if err != nil {
			return nil, err
		}
		if c.cfg.Username != "" || c.cfg.Password != "" {
			req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
		}
		req.Header.Set("User-Agent", c.cfg.UserAgent)
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			// 不安全重定向或上下文取消不重试
			if errors.Is(err, ErrUnsafeRedirect) || ctx.Err() != nil || attempt == c.cfg.MaxRetries {
				return nil, err
			}
			lastErr = err
			delay := c.computeBackoff(attempt, 0)
			if err := sleepWithContext(ctx, delay); err != nil {
				return nil, err
			}
			continue
		}

		if shouldRetryStatus(resp.StatusCode) && attempt < c.cfg.MaxRetries {
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			resp.Body.Close()
			// 若服务端显式要求的 Retry-After 超过剩余上下文预算或生产级上限（60s），不截短提前撞限流，直接上报限流错误（P1-9）
			if err := checkRetryAfterBudget(ctx, retryAfter, c.cfg.MaxRetryDelay, method, targetURL); err != nil {
				return nil, err
			}
			lastErr = fmt.Errorf("webdav: status %d on %s %s", resp.StatusCode, method, targetURL)

			delay := c.computeBackoff(attempt, retryAfter)
			if err := sleepWithContext(ctx, delay); err != nil {
				return nil, err
			}
			continue
		}

		readBody, readErr := readLimitedBody(resp.Body, maxBodyBytes)
		resp.Body.Close()
		if readErr != nil {
			if errors.Is(readErr, ErrResponseTooLarge) || ctx.Err() != nil || attempt == c.cfg.MaxRetries {
				return nil, readErr
			}
			lastErr = readErr
			delay := c.computeBackoff(attempt, 0)
			if err := sleepWithContext(ctx, delay); err != nil {
				return nil, err
			}
			continue
		}
		return &responseResult{
			StatusCode: resp.StatusCode,
			Header:     resp.Header.Clone(),
			Body:       readBody,
		}, nil
	}
	return nil, lastErr
}

func checkRetryAfterBudget(ctx context.Context, retryAfter, maxRetryDelay time.Duration, method, targetURL string) error {
	if retryAfter <= 0 {
		return nil
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < retryAfter {
		return fmt.Errorf("%w: retry after %s exceeds context budget on %s %s", ErrRateLimited, retryAfter.Round(time.Second), method, targetURL)
	}
	if retryAfter > DefaultMaxRetryDelay && maxRetryDelay >= DefaultMaxRetryDelay {
		return fmt.Errorf("%w: retry after %s on %s %s", ErrRateLimited, retryAfter.Round(time.Second), method, targetURL)
	}
	return nil
}

func readLimitedBody(r io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		return io.ReadAll(r)
	}
	lr := io.LimitReader(r, limit+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: > %d bytes", ErrResponseTooLarge, limit)
	}
	return data, nil
}

func (c *Client) computeBackoff(attempt int, retryAfter time.Duration) time.Duration {
	delay := retryAfter
	if delay <= 0 {
		delay = c.cfg.RetryBaseDelay * time.Duration(1<<attempt)
		// 添加 0~25% 随机抖动，避免双端同时惊群重试
		jitterMax := delay / 4
		if jitterMax > 0 {
			delay += randomDuration(jitterMax)
		}
	}
	if c.cfg.MaxRetryDelay > 0 && delay > c.cfg.MaxRetryDelay {
		delay = c.cfg.MaxRetryDelay
	}
	return delay
}

func parseRetryAfter(raw string, now time.Time) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if secs, err := strconv.Atoi(raw); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if ts, err := http.ParseTime(raw); err == nil {
		d := ts.Sub(now)
		if d > 0 {
			return d
		}
	}
	return 0
}

func randomDuration(max time.Duration) time.Duration {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0
	}
	n := binary.LittleEndian.Uint64(b[:])
	return time.Duration(n % uint64(max))
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func shouldRetryStatus(code int) bool {
	return code == http.StatusTooManyRequests ||
		code == http.StatusServiceUnavailable ||
		code == http.StatusBadGateway ||
		code == http.StatusGatewayTimeout
}
