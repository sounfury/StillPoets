package sync

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"strconv"
	"strings"

	"stillpoets/internal/domain"
	"stillpoets/internal/sync/webdav"
)

const (
	// ProtocolSchemaVersion 标记 Per-Device 代次快照协议版本（v2 取代早期共享 manifest v1）。
	ProtocolSchemaVersion = 2
	// SPBookSchemaVersion 标记标准化诗集正文包 .spbook.json.gz 的格式版本。
	SPBookSchemaVersion = 1

	StateDir           = "state"
	BooksDir           = "books"
	InboxDir           = "inbox"
	LegacyManifestPath = "manifest.json"

	ContentTypeGzip = "application/gzip"

	// 安全体积上限，防止损坏或恶意大文件撑爆移动端内存。
	MaxSnapshotCompressedBytes int64 = 16 << 20  // 16 MiB
	MaxSnapshotRawBytes        int64 = 64 << 20  // 64 MiB
	MaxSPBookCompressedBytes   int64 = 32 << 20  // 32 MiB
	MaxSPBookRawBytes          int64 = 128 << 20 // 128 MiB

	// KeepSnapshotGenerations 每台设备在远端 state/ 目录至少保留最近 2 份已验证快照，
	// 即使新代次直传截断损坏，也能自动回退上一代有效快照。
	KeepSnapshotGenerations = 2
)

// SnapshotData 明确限定跨端同步的元数据载荷，严格排除本机设置、字体与缓存状态。
// ReadingEvents 采用可选字段（omitempty）保持与旧版 v2 快照的向前/向后兼容。
type SnapshotData struct {
	Groups        []domain.BookGroup       `json:"groups,omitempty"`
	Books         []domain.Book            `json:"books,omitempty"`
	Favorites     []domain.FavoritePoem    `json:"favorites,omitempty"`
	Quotes        []domain.Quote           `json:"quotes,omitempty"`
	Progress      []domain.ReadingProgress `json:"progress,omitempty"`
	ReadingEvents []domain.ReadingEvent    `json:"readingEvents,omitempty"`
}

// DeviceSnapshot 是单台设备发布到 state/device_<installId>_<rev>.json.gz 的完整元数据快照。
type DeviceSnapshot struct {
	SchemaVersion int          `json:"schemaVersion"`
	InstallID     string       `json:"installId"`
	Rev           uint64       `json:"rev"`
	MaxCounter    uint64       `json:"maxCounter"`
	GeneratedAt   int64        `json:"generatedAt"`
	Data          SnapshotData `json:"data"`
}

// WorkspaceInitResult 描述连接并初始化远端 WebDAV 应用目录的结果。
type WorkspaceInitResult struct {
	ExistingRemote  bool // true 表示 state/ 下已存在至少 1 份有效设备快照
	DeviceCount     int  // 远端已发现的不同 installId 数量
	LegacyWorkspace bool // true 表示检测到旧版 manifest.json 工作区，需提醒迁移或更换目录
}

// IsValidInstallID 校验 installId 仅包含字母、数字与连字符（不含下划线），保证快照文件名解析无歧义。
func IsValidInstallID(installID string) bool {
	if len(installID) == 0 || len(installID) > 64 {
		return false
	}
	for i := 0; i < len(installID); i++ {
		c := installID[i]
		isAlphaNum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !isAlphaNum && c != '-' {
			return false
		}
	}
	return true
}

// SnapshotFilename 生成规范化的设备代次快照文件名：device_<installId>_<rev>.json.gz。
func SnapshotFilename(installID string, rev uint64) (string, error) {
	if !IsValidInstallID(installID) {
		return "", fmt.Errorf("sync: invalid installId %q", installID)
	}
	if rev == 0 {
		return "", errors.New("sync: snapshot rev must be >= 1")
	}
	return fmt.Sprintf("device_%s_%d.json.gz", installID, rev), nil
}

// SnapshotPath 返回指定设备代次快照在 WebDAV 应用目录内的相对路径。
func SnapshotPath(installID string, rev uint64) (string, error) {
	name, err := SnapshotFilename(installID, rev)
	if err != nil {
		return "", err
	}
	return StateDir + "/" + name, nil
}

// ParseSnapshotFilename 严格解析 state/ 下的快照文件名。
func ParseSnapshotFilename(name string) (installID string, rev uint64, ok bool) {
	const prefix = "device_"
	const suffix = ".json.gz"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return "", 0, false
	}
	middle := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
	parts := strings.Split(middle, "_")
	if len(parts) != 2 {
		return "", 0, false
	}
	id, revStr := parts[0], parts[1]
	if !IsValidInstallID(id) || revStr == "" {
		return "", 0, false
	}
	parsedRev, err := strconv.ParseUint(revStr, 10, 64)
	if err != nil || parsedRev == 0 {
		return "", 0, false
	}
	// 拒绝前导零等非规范形式，确保文件名与数值一一对应
	if strconv.FormatUint(parsedRev, 10) != revStr {
		return "", 0, false
	}
	return id, parsedRev, true
}

// BookParsedPath 返回指定诗集默认标准化解析包的远端路径。
func BookParsedPath(bookID string) string {
	return fmt.Sprintf("%s/%s/parsed.spbook.json.gz", BooksDir, bookID)
}

// BookBuildParsedRelPath 返回不可变构建子路径（相对于 books/<bookId>/）。
func BookBuildParsedRelPath(attemptID string) string {
	return fmt.Sprintf("builds/%s/parsed.spbook.json.gz", attemptID)
}

// NormalizeBookContentPath 校验并归一化 Book.ContentPath（空串代表默认 parsed.spbook.json.gz）。
// 仅允许 books/<bookId>/ 下的相对路径，禁止路径穿越、前导斜杠或反斜杠。
func NormalizeBookContentPath(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", nil
	}
	if strings.Contains(rel, "\\") || strings.HasPrefix(rel, "/") || strings.HasSuffix(rel, "/") {
		return "", fmt.Errorf("sync: invalid book contentPath %q", rel)
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.ContainsRune(seg, '\x00') {
			return "", fmt.Errorf("sync: invalid book contentPath %q", rel)
		}
	}
	cleaned := path.Clean(rel)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("sync: invalid book contentPath %q", rel)
	}
	return cleaned, nil
}

// BookParsedPathFor 返回某本诗集实际对应的远端解析包路径（支持可选不可变 ContentPath，旧书默认走根路径）。
func BookParsedPathFor(book domain.Book) (string, error) {
	if !IsValidSafeSegment(book.ID) {
		return "", fmt.Errorf("sync: invalid book id %q", book.ID)
	}
	rel, err := NormalizeBookContentPath(book.ContentPath)
	if err != nil {
		return "", err
	}
	if rel == "" {
		return BookParsedPath(book.ID), nil
	}
	return fmt.Sprintf("%s/%s/%s", BooksDir, book.ID, rel), nil
}

// IsValidSafeSegment 校验单个远端路径段（不含路径分隔符、控制字符或相对目录段）。
func IsValidSafeSegment(seg string) bool {
	if seg == "" || seg == "." || seg == ".." || len(seg) > 255 {
		return false
	}
	if strings.ContainsAny(seg, "/\\\x00") {
		return false
	}
	return true
}

// BookRawPath 返回指定诗集原始导入文件的远端路径（保留原文件名+随机短ID）。
func BookRawPath(bookID, rawFilename string) string {
	return fmt.Sprintf("%s/%s/%s", BooksDir, bookID, rawFilename)
}

// BookCoverPath 返回指定诗集缩略封面的远端路径。
func BookCoverPath(bookID string) string {
	return fmt.Sprintf("%s/%s/cover.webp", BooksDir, bookID)
}

// EnsureWorkspace 验证 WebDAV 连接，创建 state/ 与 books/ 目录树，
// 并同时检测是否存在现有设备快照或旧版 manifest.json 工作区。
func EnsureWorkspace(ctx context.Context, client *webdav.Client) (*WorkspaceInitResult, error) {
	if err := client.CheckConnection(ctx); err != nil {
		return nil, err
	}

	for _, dir := range []string{StateDir, BooksDir, InboxDir} {
		if err := client.EnsureDir(ctx, dir); err != nil {
			return nil, fmt.Errorf("sync: ensure workspace dir %s: %w", dir, err)
		}
	}

	entries, err := client.ListDir(ctx, StateDir)
	if err != nil {
		return nil, fmt.Errorf("sync: list %s: %w", StateDir, err)
	}
	devices := make(map[string]struct{})
	for _, e := range entries {
		if e.IsDir {
			continue
		}
		if id, _, ok := ParseSnapshotFilename(e.Name); ok {
			devices[id] = struct{}{}
		}
	}

	legacyFound := false
	if _, _, err := client.GetWithLimit(ctx, LegacyManifestPath, 1<<20); err == nil {
		legacyFound = true
	} else if !errors.Is(err, webdav.ErrNotFound) {
		// 非 404 错误（如权限异常）直接暴露
		return nil, fmt.Errorf("sync: probe legacy manifest: %w", err)
	}

	return &WorkspaceInitResult{
		ExistingRemote:  len(devices) > 0,
		DeviceCount:     len(devices),
		LegacyWorkspace: legacyFound,
	}, nil
}

// EncodeSnapshot 将设备快照序列化并 gzip 压缩，自动校正 SchemaVersion 与 MaxCounter。
func EncodeSnapshot(snap *DeviceSnapshot) ([]byte, error) {
	if snap == nil {
		return nil, errors.New("sync: snapshot is nil")
	}
	if !IsValidInstallID(snap.InstallID) {
		return nil, fmt.Errorf("sync: invalid snapshot installId %q", snap.InstallID)
	}
	if snap.Rev == 0 {
		return nil, errors.New("sync: snapshot rev must be >= 1")
	}

	cp := *snap
	cp.SchemaVersion = ProtocolSchemaVersion
	cp.MaxCounter = MaxCounterInData(cp.MaxCounter, cp.Data)
	if cp.GeneratedAt == 0 {
		cp.GeneratedAt = domain.NowMillis()
	}

	rawJSON, err := json.Marshal(&cp)
	if err != nil {
		return nil, fmt.Errorf("sync: marshal snapshot: %w", err)
	}
	if int64(len(rawJSON)) > MaxSnapshotRawBytes {
		return nil, fmt.Errorf("sync: snapshot raw json exceeds limit (%d > %d)", len(rawJSON), MaxSnapshotRawBytes)
	}

	compressed, err := gzipBytes(rawJSON)
	if err != nil {
		return nil, fmt.Errorf("sync: gzip snapshot: %w", err)
	}
	if int64(len(compressed)) > MaxSnapshotCompressedBytes {
		return nil, fmt.Errorf("sync: snapshot gzip exceeds limit (%d > %d)", len(compressed), MaxSnapshotCompressedBytes)
	}
	return compressed, nil
}

// DecodeSnapshot 解压并完整校验设备快照（检查体积上限、gzip 完整 EOF、协议版本、installId 与 rev）。
func DecodeSnapshot(compressed []byte, expectedInstallID string, expectedRev uint64) (*DeviceSnapshot, error) {
	if int64(len(compressed)) > MaxSnapshotCompressedBytes {
		return nil, fmt.Errorf("sync: snapshot compressed size exceeds limit (%d > %d)", len(compressed), MaxSnapshotCompressedBytes)
	}
	rawJSON, err := gunzipBytesLimited(compressed, MaxSnapshotRawBytes)
	if err != nil {
		return nil, fmt.Errorf("sync: gunzip snapshot: %w", err)
	}

	var snap DeviceSnapshot
	if err := json.Unmarshal(rawJSON, &snap); err != nil {
		return nil, fmt.Errorf("sync: unmarshal snapshot: %w", err)
	}
	if snap.SchemaVersion != ProtocolSchemaVersion {
		return nil, fmt.Errorf("sync: unsupported snapshot schemaVersion %d (want %d)", snap.SchemaVersion, ProtocolSchemaVersion)
	}
	if expectedInstallID != "" && snap.InstallID != expectedInstallID {
		return nil, fmt.Errorf("sync: snapshot installId mismatch: got %q, want %q", snap.InstallID, expectedInstallID)
	}
	if !IsValidInstallID(snap.InstallID) {
		return nil, fmt.Errorf("sync: snapshot invalid installId %q", snap.InstallID)
	}
	if expectedRev > 0 && snap.Rev != expectedRev {
		return nil, fmt.Errorf("sync: snapshot rev mismatch: got %d, want %d", snap.Rev, expectedRev)
	}
	// 拒绝超出 SQLite int64 上限的异常 Lamport 计数，防止单个坏快照污染本地逻辑时钟（P1-13）
	if snap.MaxCounter > math.MaxInt64 {
		return nil, fmt.Errorf("sync: snapshot maxCounter %d exceeds int64 limit", snap.MaxCounter)
	}
	if dataMax := MaxCounterInData(0, snap.Data); dataMax > math.MaxInt64 {
		return nil, fmt.Errorf("sync: snapshot data lamport counter %d exceeds int64 limit", dataMax)
	}
	if snap.Rev == 0 {
		return nil, errors.New("sync: snapshot rev is 0")
	}
	return &snap, nil
}

// EncodeSPBook 将标准化诗集压缩为 .spbook.json.gz 并返回字节流与其 SHA-256 哈希。
func EncodeSPBook(book *domain.SPBook) ([]byte, string, error) {
	if book == nil {
		return nil, "", errors.New("sync: spbook is nil")
	}
	cp := *book
	if cp.SchemaVersion == 0 {
		cp.SchemaVersion = SPBookSchemaVersion
	}
	rawJSON, err := json.Marshal(&cp)
	if err != nil {
		return nil, "", fmt.Errorf("sync: marshal spbook: %w", err)
	}
	if int64(len(rawJSON)) > MaxSPBookRawBytes {
		return nil, "", fmt.Errorf("sync: spbook raw json exceeds limit (%d > %d)", len(rawJSON), MaxSPBookRawBytes)
	}

	compressed, err := gzipBytes(rawJSON)
	if err != nil {
		return nil, "", fmt.Errorf("sync: gzip spbook: %w", err)
	}
	if int64(len(compressed)) > MaxSPBookCompressedBytes {
		return nil, "", fmt.Errorf("sync: spbook gzip exceeds limit (%d > %d)", len(compressed), MaxSPBookCompressedBytes)
	}
	sum := sha256.Sum256(compressed)
	return compressed, hex.EncodeToString(sum[:]), nil
}

// DecodeSPBook 校验体积、SHA-256、gzip 完整性、SchemaVersion 与 BookID，返回解析后的 SPBook。
func DecodeSPBook(compressed []byte, expectedSHA256, expectedBookID string) (*domain.SPBook, error) {
	if int64(len(compressed)) > MaxSPBookCompressedBytes {
		return nil, fmt.Errorf("sync: spbook compressed size exceeds limit (%d > %d)", len(compressed), MaxSPBookCompressedBytes)
	}
	if expectedSHA256 != "" {
		sum := sha256.Sum256(compressed)
		got := hex.EncodeToString(sum[:])
		if got != expectedSHA256 {
			return nil, fmt.Errorf("sync: spbook hash mismatch: got %s, want %s", got, expectedSHA256)
		}
	}

	rawJSON, err := gunzipBytesLimited(compressed, MaxSPBookRawBytes)
	if err != nil {
		return nil, fmt.Errorf("sync: gunzip spbook: %w", err)
	}
	var book domain.SPBook
	if err := json.Unmarshal(rawJSON, &book); err != nil {
		return nil, fmt.Errorf("sync: unmarshal spbook: %w", err)
	}
	if book.SchemaVersion != SPBookSchemaVersion {
		return nil, fmt.Errorf("sync: unsupported spbook schemaVersion %d (want %d)", book.SchemaVersion, SPBookSchemaVersion)
	}
	if expectedBookID != "" && book.BookID != expectedBookID {
		return nil, fmt.Errorf("sync: spbook bookId mismatch: got %q, want %q", book.BookID, expectedBookID)
	}
	return &book, nil
}

func gzipBytes(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(raw); err != nil {
		_ = gz.Close()
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// gunzipBytesLimited 读取并严格校验单成员 gzip 流直至真实 EOF，拒绝截断流或超限流。
func gunzipBytesLimited(compressed []byte, maxRawBytes int64) ([]byte, error) {
	br := bytes.NewReader(compressed)
	gr, err := gzip.NewReader(br)
	if err != nil {
		return nil, err
	}
	defer gr.Close()

	lr := io.LimitReader(gr, maxRawBytes+1)
	raw, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxRawBytes {
		return nil, fmt.Errorf("decompressed size exceeds limit %d", maxRawBytes)
	}
	if err := gr.Close(); err != nil {
		return nil, err
	}
	return raw, nil
}
