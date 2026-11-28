package webdav

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

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
	return c.propfind(ctx, target, "0", `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/></d:prop></d:propfind>`)
}

func (c *Client) propfind(ctx context.Context, target, depth, body string) (*responseResult, error) {
	return c.do(ctx, "PROPFIND", target, []byte(body), map[string]string{
		"Depth":        depth,
		"Content-Type": "application/xml; charset=utf-8",
	}, maxControlBodySize)
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
	targetURL, err := url.Parse(target)
	if err != nil {
		return false, err
	}
	baseSegs := splitEscapedPath(targetURL.EscapedPath())
	for _, r := range ms.Responses {
		if !samePathSegments(baseSegs, hrefSegments(targetURL, r.Href)) {
			continue
		}
		if entry, ok := r.entry(); ok && entry.IsDir {
			return true, nil
		}
	}
	return false, nil
}

func (c *Client) directoryPaths(relDir string) []string {
	var dirs []string
	cur := ""
	for seg := range strings.SplitSeq(c.appRoot+"/"+strings.TrimSpace(relDir), "/") {
		if seg == "" || seg == "." || seg == ".." {
			continue
		}
		cur += seg + "/"
		dirs = append(dirs, cur)
	}
	return dirs
}

// InvalidateDir 清除目录及其子目录的确认记录。
func (c *Client) InvalidateDir(relDir string) {
	c.invalidateDir(relDir, false)
}

func (c *Client) invalidateDir(relDir string, includeParents bool) {
	dirs := c.directoryPaths(relDir)
	target := ""
	if len(dirs) > 0 {
		target = dirs[len(dirs)-1]
	}
	c.dirMu.Lock()
	defer c.dirMu.Unlock()
	for dir := range c.ensuredDirs {
		if strings.HasPrefix(dir, target) || (includeParents && strings.HasPrefix(target, dir)) {
			delete(c.ensuredDirs, dir)
		}
	}
}

// EnsureDir 逐级检查并创建 <BaseURL>/<AppFolder>/<relDir> 目录树。
// 遇到 405 Method Not Allowed 或 301 时，使用 PROPFIND (Depth: 0) 确认目标确为集合目录。
func (c *Client) EnsureDir(ctx context.Context, relDir string) error {
	c.dirMu.Lock()
	defer c.dirMu.Unlock()

	dirs := c.directoryPaths(relDir)
	if len(dirs) > 0 {
		if _, ok := c.ensuredDirs[dirs[len(dirs)-1]]; ok {
			return ctx.Err()
		}
	}
	for _, dir := range dirs {
		if _, ok := c.ensuredDirs[dir]; ok {
			continue
		}
		if err := c.ensureCollection(ctx, dir); err != nil {
			return err
		}
		c.ensuredDirs[dir] = struct{}{}
	}
	return nil
}

func (c *Client) ensureCollection(ctx context.Context, dir string) error {
	target := c.resolveURL(dir, false, true)
	res, err := c.do(ctx, "MKCOL", target, nil, nil, maxControlBodySize)
	if err != nil {
		// 301 带 Location 时，客户端会拦截改写方法的重定向。
		if errors.Is(err, ErrUnsafeRedirect) {
			if ok, verifyErr := c.isExistingCollection(ctx, target); verifyErr == nil && ok {
				return nil
			}
		}
		return fmt.Errorf("webdav: mkcol %s: %w", dir, err)
	}

	switch res.StatusCode {
	case http.StatusCreated, http.StatusOK, http.StatusNoContent:
		return nil
	case http.StatusMethodNotAllowed, http.StatusMovedPermanently:
		ok, verifyErr := c.isExistingCollection(ctx, target)
		if verifyErr != nil {
			return fmt.Errorf("webdav: verify dir %s after %d: %w", dir, res.StatusCode, verifyErr)
		}
		if !ok {
			return fmt.Errorf("webdav: path %s returned %d on MKCOL and is not a collection", dir, res.StatusCode)
		}
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrUnauthorized
	default:
		return fmt.Errorf("webdav: mkcol %s unexpected status %d", dir, res.StatusCode)
	}
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
		c.invalidateDir(path.Dir(strings.TrimSpace(relPath)), true)
		// 若前一次 MOVE 实际已在服务端成功、重试因临时文件已移走返回 404，则回读正式目标确认
		if got, etag, getErr := c.GetWithLimit(ctx, relPath, int64(len(data))+1024); getErr == nil && bytes.Equal(got, data) {
			return etag, nil
		}
		_ = c.Delete(ctx, tmpRel)
		return "", fmt.Errorf("webdav: atomic move %s unexpected status %d", relPath, res.StatusCode)
	case http.StatusConflict:
		c.invalidateDir(path.Dir(strings.TrimSpace(relPath)), true)
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
	case http.StatusNotFound, http.StatusConflict:
		c.invalidateDir(path.Dir(strings.TrimSpace(relPath)), true)
		return "", fmt.Errorf("webdav: put %s unexpected status %d", relPath, res.StatusCode)
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
		c.InvalidateDir(relPath)
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrUnauthorized
	default:
		return fmt.Errorf("webdav: delete %s unexpected status %d", relPath, res.StatusCode)
	}
}

type multiStatusXML struct {
	XMLName   xml.Name      `xml:"multistatus"`
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
	DisplayName   string           `xml:"displayname"`
	ContentLength string           `xml:"getcontentlength"`
	ETag          string           `xml:"getetag"`
	LastModified  string           `xml:"getlastmodified"`
	ResourceType  *resourceTypeXML `xml:"resourcetype"`
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

	res, err := c.propfind(ctx, target, "1", entryPropfindBody)
	if err != nil {
		return nil, err
	}

	if res.StatusCode == http.StatusNotFound {
		c.invalidateDir(relDir, true)
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
	confirmed := false
	for _, r := range ms.Responses {
		segs := hrefSegments(targetURL, r.Href)
		if segs == nil {
			continue
		}
		entry, ok := r.entry()
		if samePathSegments(baseSegs, segs) {
			switch davStatusCode(r.Status) {
			case http.StatusNotFound:
				c.invalidateDir(relDir, true)
				return nil, ErrNotFound
			case http.StatusForbidden, http.StatusUnauthorized:
				return nil, ErrUnauthorized
			}
			if ok && !entry.IsDir && r.hasResourceType() {
				c.InvalidateDir(relDir)
				return nil, fmt.Errorf("webdav: list %s: target is not a collection", relDir)
			}
			confirmed = confirmed || (ok && entry.IsDir)
			continue
		}
		if !ok || len(segs) != len(baseSegs)+1 || !samePathSegments(baseSegs, segs[:len(baseSegs)]) {
			continue
		}
		if name := decodePathSegment(segs[len(segs)-1]); name != "" && name != "." && name != ".." && !strings.Contains(name, "/") {
			entry.Name = name
			entries = append(entries, entry)
		}
	}
	if confirmed || len(entries) > 0 {
		c.dirMu.Lock()
		for _, dir := range c.directoryPaths(relDir) {
			c.ensuredDirs[dir] = struct{}{}
		}
		c.dirMu.Unlock()
	}
	return entries, nil
}

const entryPropfindBody = `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:displayname/><d:getcontentlength/><d:getetag/><d:getlastmodified/><d:resourcetype/></d:prop></d:propfind>`

// Stat 获取指定资源的属性（Depth: 0），仅使用目标路径的响应。
func (c *Client) Stat(ctx context.Context, relPath string) (*Entry, error) {
	target := c.resolveURL(relPath, true, false)
	targetURL, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	res, err := c.propfind(ctx, target, "0", entryPropfindBody)
	if err != nil {
		return nil, err
	}
	switch res.StatusCode {
	case http.StatusNotFound:
		return nil, ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, ErrUnauthorized
	case http.StatusOK, http.StatusMultiStatus:
	default:
		return nil, fmt.Errorf("webdav: stat %s unexpected status %d", relPath, res.StatusCode)
	}
	var ms multiStatusXML
	if err := xml.Unmarshal(res.Body, &ms); err != nil {
		return nil, fmt.Errorf("webdav: parse stat %s: %w", relPath, err)
	}
	targetSegs := splitEscapedPath(targetURL.EscapedPath())
	for _, r := range ms.Responses {
		if !samePathSegments(targetSegs, hrefSegments(targetURL, r.Href)) {
			continue
		}
		entry, ok := r.entry()
		if ok || isSuccessDAVStatus(r.Status) {
			if len(targetSegs) > 0 {
				entry.Name = decodePathSegment(targetSegs[len(targetSegs)-1])
			}
			return &entry, nil
		}
		if status := davStatusCode(r.Status); status == http.StatusUnauthorized || status == http.StatusForbidden {
			return nil, ErrUnauthorized
		}
	}
	return nil, ErrNotFound
}

// entry 合并响应中成功的 propstat，剔除失败条目及属性。
func (r responseXML) entry() (Entry, bool) {
	if r.Status != "" && !isSuccessDAVStatus(r.Status) {
		return Entry{}, false
	}
	entry := Entry{Href: r.Href}
	ok := false
	for _, ps := range r.PropStats {
		if ps.Status != "" && !isSuccessDAVStatus(ps.Status) {
			continue
		}
		ok = true
		if ps.Prop.DisplayName != "" {
			entry.DisplayName = ps.Prop.DisplayName
		}
		if n, err := strconv.ParseInt(strings.TrimSpace(ps.Prop.ContentLength), 10, 64); err == nil {
			entry.ContentLength = n
		}
		if ps.Prop.ETag != "" {
			entry.ETag = ps.Prop.ETag
		}
		if ps.Prop.LastModified != "" {
			entry.LastModified = ps.Prop.LastModified
		}
		if ps.Prop.ResourceType != nil && ps.Prop.ResourceType.Collection != nil {
			entry.IsDir = true
		}
	}
	return entry, ok
}

func (r responseXML) hasResourceType() bool {
	for _, ps := range r.PropStats {
		if ps.Prop.ResourceType != nil && (ps.Status == "" || isSuccessDAVStatus(ps.Status)) {
			return true
		}
	}
	return false
}

func hrefSegments(target *url.URL, href string) []string {
	href = strings.TrimSpace(href)
	if href == "" {
		return nil
	}
	ref, err := url.Parse(href)
	if err != nil {
		return nil
	}
	return splitEscapedPath(target.ResolveReference(ref).EscapedPath())
}

func samePathSegments(left, right []string) bool {
	if right == nil || len(left) != len(right) {
		return false
	}
	for i, seg := range left {
		if decodePathSegment(seg) != decodePathSegment(right[i]) {
			return false
		}
	}
	return true
}

func decodePathSegment(seg string) string {
	if decoded, err := url.PathUnescape(seg); err == nil {
		return decoded
	}
	return seg
}

func splitEscapedPath(escapedPath string) []string {
	trimmed := strings.Trim(escapedPath, "/")
	if trimmed == "" {
		return []string{}
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
	code := davStatusCode(statusLine)
	return code >= 200 && code < 300
}

func davStatusCode(statusLine string) int {
	fields := strings.Fields(statusLine)
	if len(fields) < 2 {
		return 0
	}
	code, _ := strconv.Atoi(fields[1])
	return code
}
