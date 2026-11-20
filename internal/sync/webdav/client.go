package webdav

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	DefaultAppFolder   = "StillPoets"
	DefaultUserAgent   = "StillPoets/0.1.0"
	DefaultConcurrency = 2
	DefaultMaxRetries  = 3
)

var (
	// ErrNotFound 表示远端文件或目录不存在（HTTP 404）。
	ErrNotFound = errors.New("webdav: resource not found")
	// ErrUnauthorized 表示 WebDAV 认证失败（HTTP 401/403）。
	ErrUnauthorized = errors.New("webdav: unauthorized")
	// ErrPreconditionFailed 表示 ETag 乐观锁校验未通过（HTTP 412）。
	ErrPreconditionFailed = errors.New("webdav: precondition failed")
)

// Config 描述 WebDAV 连接与兼容策略配置。
type Config struct {
	BaseURL          string // 用户输入的 WebDAV 地址，例如 https://dav.jianguoyun.com/dav/
	Username         string
	Password         string
	AppFolder        string        // 默认在 BaseURL 下创建的专属目录，留空则使用 StillPoets
	UserAgent        string        // 可自定义以适配特定网盘限制
	AtomicUpload     bool          // 是否先 PUT 临时文件再 MOVE 覆盖（AList/115 如不兼容可关闭）
	AllowInsecureTLS bool          // 仅允许局域网自签名 NAS 时显式开启
	MaxConcurrency   int           // 默认 2，避免触发坚果云/115 429 频控
	MaxRetries       int           // 遇 429/503/临时网络错误的最大重试次数
	Timeout          time.Duration // 单次请求超时
	RetryBaseDelay   time.Duration // 退避基础延迟（单元测试可设为极小值）
}

// Entry 表示 PROPFIND 返回的单个远端文件或目录条目。
type Entry struct {
	Href          string
	DisplayName   string
	IsDir         bool
	ContentLength int64
	ETag          string
	LastModified  string
}

// Client 是零外部依赖的轻量 WebDAV 客户端，内置并发控制、自动建目录与退避重试。
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
	if !strings.HasSuffix(parsed.Path, "/") {
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

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.AllowInsecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // 用户显式允许自签名局域网 NAS
	}

	return &Client{
		cfg:     cfg,
		baseURL: parsed,
		appRoot: cfg.AppFolder,
		http: &http.Client{
			Timeout:   cfg.Timeout,
			Transport: transport,
		},
		sem: make(chan struct{}, cfg.MaxConcurrency),
	}, nil
}

// AppFolder 返回当前使用的应用根目录名（如 StillPoets）。
func (c *Client) AppFolder() string {
	return c.appRoot
}

// resolveURL 将应用根目录内的相对路径转换为完整且正确转义的 WebDAV URL。
// 当 includeAppRoot 为 false 时，仅解析到用户输入的 BaseURL 根目录。
func (c *Client) resolveURL(relPath string, includeAppRoot bool, isDir bool) string {
	u := *c.baseURL
	basePath := strings.TrimSuffix(u.Path, "/")

	var segments []string
	if basePath != "" {
		for _, s := range strings.Split(strings.TrimPrefix(basePath, "/"), "/") {
			if s != "" {
				segments = append(segments, s)
			}
		}
	}
	if includeAppRoot && c.appRoot != "" {
		for _, s := range strings.Split(c.appRoot, "/") {
			if s != "" {
				segments = append(segments, s)
			}
		}
	}
	cleaned := strings.Trim(strings.TrimSpace(relPath), "/")
	if cleaned != "" && cleaned != "." {
		for _, s := range strings.Split(cleaned, "/") {
			if s != "" && s != "." && s != ".." {
				segments = append(segments, s)
			}
		}
	}

	escaped := make([]string, len(segments))
	for i, s := range segments {
		escaped[i] = url.PathEscape(s)
	}
	u.RawPath = "/" + strings.Join(escaped, "/")
	u.Path = "/" + strings.Join(segments, "/")
	if isDir && !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
		u.RawPath += "/"
	}
	return u.String()
}

// CheckConnection 通过 PROPFIND (Depth: 0) 验证用户输入的 WebDAV 根目录连通性与账号凭据。
func (c *Client) CheckConnection(ctx context.Context) error {
	target := c.resolveURL("", false, true)
	headers := map[string]string{
		"Depth":        "0",
		"Content-Type": "application/xml; charset=utf-8",
	}
	body := []byte(`<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/></d:prop></d:propfind>`)
	resp, err := c.do(ctx, "PROPFIND", target, body, headers)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	switch resp.StatusCode {
	case http.StatusMultiStatus, http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrUnauthorized
	case http.StatusNotFound:
		return ErrNotFound
	default:
		return fmt.Errorf("webdav: check connection unexpected status %d", resp.StatusCode)
	}
}

// EnsureDir 逐级检查并创建 <BaseURL>/<AppFolder>/<relDir> 目录树。
// 兼容返回 201 Created、405 Method Not Allowed（目录已存在）、301/200 的不同 WebDAV 服务。
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
		resp, err := c.do(ctx, "MKCOL", target, nil, nil)
		if err != nil {
			return fmt.Errorf("webdav: mkcol %s: %w", cur, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusCreated, http.StatusOK, http.StatusNoContent,
			http.StatusMethodNotAllowed, http.StatusMovedPermanently:
			// 201 表示新建成功，405/301 表示目录已存在（坚果云/Nextcloud/AList 常见行为）
			continue
		case http.StatusUnauthorized, http.StatusForbidden:
			return ErrUnauthorized
		default:
			return fmt.Errorf("webdav: mkcol %s unexpected status %d", cur, resp.StatusCode)
		}
	}
	return nil
}

// GetWithETag 下载应用目录内的文件并返回内容与 ETag。
func (c *Client) GetWithETag(ctx context.Context, relPath string) ([]byte, string, error) {
	target := c.resolveURL(relPath, true, false)
	resp, err := c.do(ctx, http.MethodGet, target, nil, nil)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, "", fmt.Errorf("webdav: read %s: %w", relPath, err)
		}
		return data, resp.Header.Get("ETag"), nil
	case http.StatusNotFound:
		return nil, "", ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, "", ErrUnauthorized
	default:
		return nil, "", fmt.Errorf("webdav: get %s unexpected status %d", relPath, resp.StatusCode)
	}
}

// Get 下载应用目录内的文件。
func (c *Client) Get(ctx context.Context, relPath string) ([]byte, error) {
	data, _, err := c.GetWithETag(ctx, relPath)
	return data, err
}

// Put 上传文件到应用目录内。
// 若启用了 AtomicUpload，则先上传至临时文件再通过 MOVE 覆盖正式文件；
// 否则直接 PUT（适配不支持覆盖式 MOVE 的 AList/115 挂载点）。
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
	resp, err := c.do(ctx, "MOVE", srcURL, nil, map[string]string{
		"Destination": dstURL,
		"Overwrite":   "T",
	})
	if err != nil {
		_ = c.Delete(ctx, tmpRel)
		return "", fmt.Errorf("webdav: atomic upload move: %w", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusCreated, http.StatusNoContent, http.StatusOK:
		return resp.Header.Get("ETag"), nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", ErrUnauthorized
	default:
		_ = c.Delete(ctx, tmpRel)
		return "", fmt.Errorf("webdav: atomic move %s unexpected status %d", relPath, resp.StatusCode)
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
	resp, err := c.do(ctx, http.MethodPut, target, data, headers)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		return resp.Header.Get("ETag"), nil
	case http.StatusPreconditionFailed:
		return "", ErrPreconditionFailed
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", ErrUnauthorized
	default:
		return "", fmt.Errorf("webdav: put %s unexpected status %d", relPath, resp.StatusCode)
	}
}

// Delete 删除应用目录内的指定文件或目录；若资源本就不存在则返回 nil。
func (c *Client) Delete(ctx context.Context, relPath string) error {
	target := c.resolveURL(relPath, true, false)
	resp, err := c.do(ctx, http.MethodDelete, target, nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusAccepted, http.StatusNotFound:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrUnauthorized
	default:
		return fmt.Errorf("webdav: delete %s unexpected status %d", relPath, resp.StatusCode)
	}
}

type multiStatusXML struct {
	Responses []responseXML `xml:"response"`
}

type responseXML struct {
	Href     string      `xml:"href"`
	PropStat propStatXML `xml:"propstat"`
}

type propStatXML struct {
	Prop propXML `xml:"prop"`
}

type propXML struct {
	DisplayName   string           `xml:"displayname"`
	ContentLength int64            `xml:"getcontentlength"`
	ETag          string           `xml:"getetag"`
	LastModified  string           `xml:"getlastmodified"`
	ResourceType  resourceTypeXML  `xml:"resourcetype"`
}

type resourceTypeXML struct {
	Collection *struct{} `xml:"collection"`
}

// ListDir 列出应用目录内某个子目录的直接子项（Depth: 1）。
func (c *Client) ListDir(ctx context.Context, relDir string) ([]Entry, error) {
	target := c.resolveURL(relDir, true, true)
	body := []byte(`<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:displayname/><d:getcontentlength/><d:getetag/><d:getlastmodified/><d:resourcetype/></d:prop></d:propfind>`)
	resp, err := c.do(ctx, "PROPFIND", target, body, map[string]string{
		"Depth":        "1",
		"Content-Type": "application/xml; charset=utf-8",
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode != http.StatusMultiStatus && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("webdav: list %s unexpected status %d", relDir, resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var ms multiStatusXML
	if err := xml.Unmarshal(raw, &ms); err != nil {
		return nil, fmt.Errorf("webdav: parse propfind xml: %w", err)
	}

	entries := make([]Entry, 0, len(ms.Responses))
	for _, r := range ms.Responses {
		entries = append(entries, Entry{
			Href:          r.Href,
			DisplayName:   r.PropStat.Prop.DisplayName,
			IsDir:         r.PropStat.Prop.ResourceType.Collection != nil,
			ContentLength: r.PropStat.Prop.ContentLength,
			ETag:          r.PropStat.Prop.ETag,
			LastModified:  r.PropStat.Prop.LastModified,
		})
	}
	return entries, nil
}

// do 统一执行带并发信号量限制、BasicAuth、User-Agent 与 429/503 指数退避重试的 HTTP 请求。
func (c *Client) do(ctx context.Context, method, targetURL string, body []byte, headers map[string]string) (*http.Response, error) {
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
			if !shouldRetryStatus(resp.StatusCode) || attempt == c.cfg.MaxRetries {
				return resp, nil
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("webdav: status %d on %s %s", resp.StatusCode, method, targetURL)
		} else {
			if ctx.Err() != nil || attempt == c.cfg.MaxRetries {
				return nil, err
			}
			lastErr = err
		}

		backoff := c.cfg.RetryBaseDelay * time.Duration(1<<attempt)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, lastErr
}

func shouldRetryStatus(code int) bool {
	return code == http.StatusTooManyRequests ||
		code == http.StatusServiceUnavailable ||
		code == http.StatusBadGateway ||
		code == http.StatusGatewayTimeout
}
