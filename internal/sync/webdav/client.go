package webdav

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultAppFolder       = "StillPoets"
	DefaultUserAgent       = "StillPoets/0.1.1"
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
	MaxConcurrency   int           // 默认 2，避免触发坚果云/115 429 频控
	MaxRetries       int           // 遇 429/502/503/504/临时网络错误的最大重试次数
	Timeout          time.Duration // 单次请求超时
	RetryBaseDelay   time.Duration // 退避基础延迟（单元测试可设为极小值）
	MaxRetryDelay    time.Duration // 退避与 Retry-After 最大等待上限
	MaxDownloadBytes int64         // 默认 GET 最大下载字节数，默认 64 MiB
}

// Entry 表示 PROPFIND 返回的单个远端直接子项（已排除目录自身）。
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
		cfg:     cfg,
		baseURL: parsed,
		appRoot: cfg.AppFolder,
		sem:     make(chan struct{}, cfg.MaxConcurrency),
	}
	c.http = &http.Client{
		Timeout:       cfg.Timeout,
		Transport:     transport,
		CheckRedirect: c.checkRedirect,
	}
	return c, nil
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

// CheckConnection 通过 PROPFIND (Depth: 0) 验证用户输入的 WebDAV 根目录连通性与账号凭据。
func (c *Client) CheckConnection(ctx context.Context) error {
	target := c.resolveURL("", false, true)
	res, err := c.propfindZero(ctx, target)
	if err != nil {
		return err
	}
	switch res.StatusCode {
	case http.StatusMultiStatus, http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrUnauthorized
	case http.StatusNotFound:
		return ErrNotFound
	default:
		return fmt.Errorf("webdav: check connection unexpected status %d", res.StatusCode)
	}
}

func (c *Client) propfindZero(ctx context.Context, target string) (*responseResult, error) {
	headers := map[string]string{
		"Depth":        "0",
		"Content-Type": "application/xml; charset=utf-8",
	}
	body := []byte(`<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/></d:prop></d:propfind>`)
	return c.do(ctx, "PROPFIND", target, body, headers, maxControlBodySize)
}

// isExistingCollection 通过 Depth: 0 PROPFIND 确认目标存在且确实是一个 WebDAV 集合（目录）。
func (c *Client) isExistingCollection(ctx context.Context, target string) (bool, error) {
	res, err := c.propfindZero(ctx, target)
	if err != nil {
		return false, err
	}
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return false, ErrUnauthorized
	}
	if res.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if res.StatusCode != http.StatusMultiStatus && res.StatusCode != http.StatusOK {
		return false, fmt.Errorf("webdav: verify collection status %d", res.StatusCode)
	}
	// 部分极简 WebDAV 实现对 Depth 0 返回空 body 的 207/200；若有 XML 则严格校验 collection
	if len(bytes.TrimSpace(res.Body)) == 0 {
		return true, nil
	}
	var ms multiStatusXML
	if err := xml.Unmarshal(res.Body, &ms); err != nil {
		return false, fmt.Errorf("webdav: parse depth-0 propfind xml: %w", err)
	}
	for _, r := range ms.Responses {
		if r.Status != "" && !isSuccessDAVStatus(r.Status) {
			continue
		}
		for _, ps := range r.PropStats {
			if ps.Status != "" && !isSuccessDAVStatus(ps.Status) {
				continue
			}
			if ps.Prop.ResourceType.Collection != nil {
				return true, nil
			}
		}
	}
	return false, nil
}

// EnsureDir 逐级检查并创建 <BaseURL>/<AppFolder>/<relDir> 目录树。
// 遇到 405 Method Not Allowed 或 301 时，使用 PROPFIND (Depth: 0) 确认目标确为集合目录。
func (c *Client) EnsureDir(ctx context.Context, relDir string) error {
	var parts []string
	if c.appRoot != "" {
		parts = append(parts, strings.Split(c.appRoot, "/")...)
	}
	trimmed := strings.Trim(strings.TrimSpace(relDir), "/")
	if trimmed != "" && trimmed != "." {
		parts = append(parts, strings.Split(trimmed, "/")...)
	}

	cur := ""
	for _, seg := range parts {
		if seg == "" || seg == "." || seg == ".." {
			continue
		}
		if cur == "" {
			cur = seg
		} else {
			cur = cur + "/" + seg
		}
		target := c.resolveURL(cur, false, true)
		res, err := c.do(ctx, "MKCOL", target, nil, nil, maxControlBodySize)
		if err != nil {
			// 若 MKCOL 遇 301 被安全重定向策略拦截，尝试确认目标是否已是集合目录
			if errors.Is(err, ErrUnsafeRedirect) {
				ok, verifyErr := c.isExistingCollection(ctx, target)
				if verifyErr == nil && ok {
					continue
				}
			}
			return fmt.Errorf("webdav: mkcol %s: %w", cur, err)
		}

		switch res.StatusCode {
		case http.StatusCreated, http.StatusOK, http.StatusNoContent:
			continue
		case http.StatusMethodNotAllowed, http.StatusMovedPermanently:
			ok, verifyErr := c.isExistingCollection(ctx, target)
			if verifyErr != nil {
				return fmt.Errorf("webdav: verify dir %s after %d: %w", cur, res.StatusCode, verifyErr)
			}
			if !ok {
				return fmt.Errorf("webdav: path %s returned %d on MKCOL and is not a collection", cur, res.StatusCode)
			}
			continue
		case http.StatusUnauthorized, http.StatusForbidden:
			return ErrUnauthorized
		default:
			return fmt.Errorf("webdav: mkcol %s unexpected status %d", cur, res.StatusCode)
		}
	}
	return nil
}

// GetWithLimit 下载应用目录内的文件（限制最大读取字节数），显式请求 Accept-Encoding: identity 保留原始字节。
func (c *Client) GetWithLimit(ctx context.Context, relPath string, maxBytes int64) ([]byte, string, error) {
	if maxBytes <= 0 {
		maxBytes = c.cfg.MaxDownloadBytes
	}
	target := c.resolveURL(relPath, true, false)
	headers := map[string]string{
		"Accept-Encoding": "identity",
	}
	res, err := c.do(ctx, http.MethodGet, target, nil, headers, maxBytes)
	if err != nil {
		return nil, "", err
	}

	switch res.StatusCode {
	case http.StatusOK:
		return res.Body, res.Header.Get("ETag"), nil
	case http.StatusNotFound:
		return nil, "", ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, "", ErrUnauthorized
	default:
		return nil, "", fmt.Errorf("webdav: get %s unexpected status %d", relPath, res.StatusCode)
	}
}

// GetWithETag 下载应用目录内的文件并返回内容与 ETag。
func (c *Client) GetWithETag(ctx context.Context, relPath string) ([]byte, string, error) {
	return c.GetWithLimit(ctx, relPath, c.cfg.MaxDownloadBytes)
}

// Get 下载应用目录内的文件。
func (c *Client) Get(ctx context.Context, relPath string) ([]byte, error) {
	data, _, err := c.GetWithETag(ctx, relPath)
	return data, err
}

// Put 上传文件到应用目录内。
// 若启用了 AtomicUpload，则先上传至临时文件再通过 MOVE 覆盖正式文件；
// 否则直接 PUT（适配不支持覆盖式 MOVE 的 AList/115 挂载点，配合代次快照保证断电安全）。
func (c *Client) Put(ctx context.Context, relPath string, data []byte, contentType string) (string, error) {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if !c.cfg.AtomicUpload {
		return c.putDirect(ctx, relPath, data, contentType, "")
	}

	var rnd [4]byte
	_, _ = rand.Read(rnd[:])
	dir := path.Dir(strings.TrimPrefix(relPath, "/"))
	base := path.Base(relPath)
	tmpName := fmt.Sprintf(".%s.%s.tmp", base, hex.EncodeToString(rnd[:]))
	tmpRel := tmpName
	if dir != "." && dir != "/" {
		tmpRel = dir + "/" + tmpName
	}

	if _, err := c.putDirect(ctx, tmpRel, data, contentType, ""); err != nil {
		return "", fmt.Errorf("webdav: atomic upload temp put: %w", err)
	}

	srcURL := c.resolveURL(tmpRel, true, false)
	dstURL := c.resolveURL(relPath, true, false)
	res, err := c.do(ctx, "MOVE", srcURL, nil, map[string]string{
		"Destination": dstURL,
		"Overwrite":   "T",
	}, maxControlBodySize)
	if err != nil {
		// 若 MOVE 因响应丢失产生歧义，先回读目标核验是否已实际提交成功（P1-9）
		if got, etag, getErr := c.GetWithLimit(ctx, relPath, int64(len(data))+1024); getErr == nil && bytes.Equal(got, data) {
			return etag, nil
		}
		_ = c.Delete(ctx, tmpRel)
		return "", fmt.Errorf("webdav: atomic upload move: %w", err)
	}

	switch res.StatusCode {
	case http.StatusCreated, http.StatusNoContent, http.StatusOK:
		return res.Header.Get("ETag"), nil
	case http.StatusNotFound:
		// 若前一次 MOVE 实际已在服务端成功、重试因临时文件已移走返回 404，则回读正式目标确认
		if got, etag, getErr := c.GetWithLimit(ctx, relPath, int64(len(data))+1024); getErr == nil && bytes.Equal(got, data) {
			return etag, nil
		}
		_ = c.Delete(ctx, tmpRel)
		return "", fmt.Errorf("webdav: atomic move %s unexpected status %d", relPath, res.StatusCode)
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", ErrUnauthorized
	default:
		_ = c.Delete(ctx, tmpRel)
		return "", fmt.Errorf("webdav: atomic move %s unexpected status %d", relPath, res.StatusCode)
	}
}

// putDirect 执行单次直接 PUT 请求，可选附带 If-Match 乐观锁。
func (c *Client) putDirect(ctx context.Context, relPath string, data []byte, contentType, ifMatch string) (string, error) {
	target := c.resolveURL(relPath, true, false)
	headers := map[string]string{
		"Content-Type": contentType,
	}
	if ifMatch != "" {
		headers["If-Match"] = ifMatch
	}
	res, err := c.do(ctx, http.MethodPut, target, data, headers, maxControlBodySize)
	if err != nil {
		return "", err
	}

	switch res.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		return res.Header.Get("ETag"), nil
	case http.StatusPreconditionFailed:
		return "", ErrPreconditionFailed
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", ErrUnauthorized
	default:
		return "", fmt.Errorf("webdav: put %s unexpected status %d", relPath, res.StatusCode)
	}
}

// Delete 删除应用目录内的指定文件或目录；若资源本就不存在则返回 nil。
func (c *Client) Delete(ctx context.Context, relPath string) error {
	target := c.resolveURL(relPath, true, false)
	res, err := c.do(ctx, http.MethodDelete, target, nil, nil, maxControlBodySize)
	if err != nil {
		return err
	}

	switch res.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusAccepted, http.StatusNotFound:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrUnauthorized
	default:
		return fmt.Errorf("webdav: delete %s unexpected status %d", relPath, res.StatusCode)
	}
}

type multiStatusXML struct {
	Responses []responseXML `xml:"response"`
}

type responseXML struct {
	Href      string        `xml:"href"`
	Status    string        `xml:"status"`
	PropStats []propStatXML `xml:"propstat"`
}

type propStatXML struct {
	Prop   propXML `xml:"prop"`
	Status string  `xml:"status"`
}

type propXML struct {
	DisplayName   string          `xml:"displayname"`
	ContentLength string          `xml:"getcontentlength"`
	ETag          string          `xml:"getetag"`
	LastModified  string          `xml:"getlastmodified"`
	ResourceType  resourceTypeXML `xml:"resourcetype"`
}

type resourceTypeXML struct {
	Collection *struct{} `xml:"collection"`
}

// ListDir 列出应用目录内某个子目录的直接子项（Depth: 1）。
// 仅采纳 2xx 状态属性，按 href 解析路径并自动排除目录自身与非直接子项。
func (c *Client) ListDir(ctx context.Context, relDir string) ([]Entry, error) {
	target := c.resolveURL(relDir, true, true)
	targetURL, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("webdav: parse list url: %w", err)
	}

	body := []byte(`<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:displayname/><d:getcontentlength/><d:getetag/><d:getlastmodified/><d:resourcetype/></d:prop></d:propfind>`)
	res, err := c.do(ctx, "PROPFIND", target, body, map[string]string{
		"Depth":        "1",
		"Content-Type": "application/xml; charset=utf-8",
	}, maxControlBodySize)
	if err != nil {
		return nil, err
	}

	if res.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, ErrUnauthorized
	}
	if res.StatusCode != http.StatusMultiStatus && res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("webdav: list %s unexpected status %d", relDir, res.StatusCode)
	}

	var ms multiStatusXML
	if err := xml.Unmarshal(res.Body, &ms); err != nil {
		return nil, fmt.Errorf("webdav: parse propfind xml: %w", err)
	}

	baseSegs := splitEscapedPath(targetURL.EscapedPath())
	entries := make([]Entry, 0, len(ms.Responses))
	for _, r := range ms.Responses {
		// 若 response 级带有非 2xx 状态码（例如 403/404），直接忽略
		if r.Status != "" && !isSuccessDAVStatus(r.Status) {
			continue
		}

		// 合并所有 2xx propstat 的属性；若无任何有效 propstat（全为 403/404），忽略该条目
		var (
			hasOKProp     bool
			displayName   string
			contentLength int64
			etag          string
			lastModified  string
			isDir         bool
		)
		for _, ps := range r.PropStats {
			if ps.Status != "" && !isSuccessDAVStatus(ps.Status) {
				continue
			}
			hasOKProp = true
			if ps.Prop.DisplayName != "" {
				displayName = ps.Prop.DisplayName
			}
			if cl := strings.TrimSpace(ps.Prop.ContentLength); cl != "" {
				if n, err := strconv.ParseInt(cl, 10, 64); err == nil {
					contentLength = n
				}
			}
			if ps.Prop.ETag != "" {
				etag = ps.Prop.ETag
			}
			if ps.Prop.LastModified != "" {
				lastModified = ps.Prop.LastModified
			}
			if ps.Prop.ResourceType.Collection != nil {
				isDir = true
			}
		}
		if !hasOKProp {
			continue
		}

		childName, ok := extractDirectChildName(targetURL, baseSegs, r.Href)
		if !ok {
			// 目录自身或非直接子项
			continue
		}

		entries = append(entries, Entry{
			Name:          childName,
			Href:          r.Href,
			DisplayName:   displayName,
			IsDir:         isDir,
			ContentLength: contentLength,
			ETag:          etag,
			LastModified:  lastModified,
		})
	}
	return entries, nil
}

// extractDirectChildName 将 response href 解析为相对于列表目录的直接子项名称（单次解码）。
// 若 href 指向目录自身、其他目录或更深层子路径，返回 ("", false)。
func extractDirectChildName(listURL *url.URL, baseSegs []string, rawHref string) (string, bool) {
	trimmed := strings.TrimSpace(rawHref)
	if trimmed == "" {
		return "", false
	}
	ref, err := url.Parse(trimmed)
	if err != nil {
		return "", false
	}
	resolved := listURL.ResolveReference(ref)
	segs := splitEscapedPath(resolved.EscapedPath())

	// 必须严格多 1 层路径段，且前缀与列表目录逐段一致（忽略大小写差异按解码段比较）
	if len(segs) != len(baseSegs)+1 {
		return "", false
	}
	for i := range baseSegs {
		a, errA := url.PathUnescape(baseSegs[i])
		b, errB := url.PathUnescape(segs[i])
		if errA != nil {
			a = baseSegs[i]
		}
		if errB != nil {
			b = segs[i]
		}
		if a != b {
			return "", false
		}
	}

	last, err := url.PathUnescape(segs[len(segs)-1])
	if err != nil {
		last = segs[len(segs)-1]
	}
	if last == "" || last == "." || last == ".." || strings.Contains(last, "/") {
		return "", false
	}
	return last, true
}

func splitEscapedPath(escapedPath string) []string {
	trimmed := strings.Trim(escapedPath, "/")
	if trimmed == "" {
		return nil
	}
	raw := strings.Split(trimmed, "/")
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// isSuccessDAVStatus 解析形如 "HTTP/1.1 200 OK" 的 WebDAV 状态行，判断是否为 2xx。
func isSuccessDAVStatus(statusLine string) bool {
	fields := strings.Fields(statusLine)
	if len(fields) < 2 {
		return false
	}
	code, err := strconv.Atoi(fields[1])
	if err != nil {
		return false
	}
	return code >= 200 && code < 300
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
		if err == nil {
			if shouldRetryStatus(resp.StatusCode) && attempt < c.cfg.MaxRetries {
				retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
				resp.Body.Close()
				// 若服务端显式要求的 Retry-After 超过剩余上下文预算或生产级上限（60s），不截短提前撞限流，直接上报限流错误（P1-9）
				if retryAfter > 0 {
					if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < retryAfter {
						return nil, fmt.Errorf("%w: retry after %s exceeds context budget on %s %s", ErrRateLimited, retryAfter.Round(time.Second), method, targetURL)
					}
					if retryAfter > DefaultMaxRetryDelay && c.cfg.MaxRetryDelay >= DefaultMaxRetryDelay {
						return nil, fmt.Errorf("%w: retry after %s on %s %s", ErrRateLimited, retryAfter.Round(time.Second), method, targetURL)
					}
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

		// 不安全重定向或上下文取消不重试
		if errors.Is(err, ErrUnsafeRedirect) || ctx.Err() != nil || attempt == c.cfg.MaxRetries {
			return nil, err
		}
		lastErr = err

		delay := c.computeBackoff(attempt, 0)
		if err := sleepWithContext(ctx, delay); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
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
