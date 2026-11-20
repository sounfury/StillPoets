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

	"stillpoets/internal/domain"
	"stillpoets/internal/sync/webdav"
)

const (
	// ProtocolSchemaVersion 标记远端 StillPoets 目录协议版本，便于后续平滑迁移。
	ProtocolSchemaVersion = 1

	ManifestPath    = "manifest.json"
	MetaDir         = "meta"
	BooksDir        = "books"
	FontsDir        = "assets/fonts"
	BackupsDir      = "backups"
	MetaLibraryPath = "meta/library.json"
	MetaQuotesPath  = "meta/quotes.json"
	MetaFavsPath    = "meta/favorites.json"
	MetaNotesPath   = "meta/annotations.json"
	MetaProgPath    = "meta/progress.json"
	MetaSettingPath = "meta/settings.json"
)

// Manifest 是 <base_url>/StillPoets/manifest.json 的结构定义，
// 用于快速检测各个元数据分片是否发生变化。
type Manifest struct {
	SchemaVersion int                  `json:"schemaVersion"`
	UpdatedAt     int64                `json:"updatedAt"`
	DeviceID      string               `json:"deviceId"`
	Slices        map[string]SliceMeta `json:"slices"` // key 如 "library" / "quotes" / "favorites" / "progress"
}

// SliceMeta 记录单个元数据分片的内容哈希与版本戳。
type SliceMeta struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	UpdatedAt int64  `json:"updatedAt"`
}

// WorkspaceInitResult 描述连接并初始化远端 WebDAV 应用目录的结果。
type WorkspaceInitResult struct {
	ExistingRemote bool      // true 表示检测到已有 manifest.json，应先拉取合并而非覆盖
	Manifest       *Manifest // 当 ExistingRemote 为 true 时非空
}

// BookParsedPath 返回指定诗集标准化解析包的远端路径。
func BookParsedPath(bookID string) string {
	return fmt.Sprintf("%s/%s/parsed.spbook.json.gz", BooksDir, bookID)
}

// BookRawPath 返回指定诗集原始导入文件的远端路径（保留原文件名+随机短ID）。
func BookRawPath(bookID, rawFilename string) string {
	return fmt.Sprintf("%s/%s/%s", BooksDir, bookID, rawFilename)
}

// BookCoverPath 返回指定诗集缩略封面的远端路径。
func BookCoverPath(bookID string) string {
	return fmt.Sprintf("%s/%s/cover.webp", BooksDir, bookID)
}

// StatsYearPath 返回按年分片的读诗事件日志路径（V1.1 启用）。
func StatsYearPath(year int) string {
	return fmt.Sprintf("%s/stats-%04d.json", MetaDir, year)
}

// EnsureWorkspace 验证 WebDAV 连接，在用户地址下自动创建应用专属目录树，
// 并检测是否已存在远端云端书库（manifest.json）。
func EnsureWorkspace(ctx context.Context, client *webdav.Client) (*WorkspaceInitResult, error) {
	if err := client.CheckConnection(ctx); err != nil {
		return nil, err
	}

	requiredDirs := []string{MetaDir, BooksDir, FontsDir, BackupsDir}
	for _, dir := range requiredDirs {
		if err := client.EnsureDir(ctx, dir); err != nil {
			return nil, fmt.Errorf("sync: ensure workspace dir %s: %w", dir, err)
		}
	}

	raw, err := client.Get(ctx, ManifestPath)
	if err != nil {
		if errors.Is(err, webdav.ErrNotFound) {
			return &WorkspaceInitResult{ExistingRemote: false}, nil
		}
		return nil, fmt.Errorf("sync: check remote manifest: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("sync: parse remote manifest: %w", err)
	}
	return &WorkspaceInitResult{
		ExistingRemote: true,
		Manifest:       &manifest,
	}, nil
}

// EncodeSPBook 将标准化诗集压缩为 .spbook.json.gz 并返回字节流与其 SHA-256 哈希。
func EncodeSPBook(book *domain.SPBook) ([]byte, string, error) {
	if book == nil {
		return nil, "", errors.New("sync: spbook is nil")
	}
	if book.SchemaVersion == 0 {
		book.SchemaVersion = ProtocolSchemaVersion
	}
	rawJSON, err := json.Marshal(book)
	if err != nil {
		return nil, "", fmt.Errorf("sync: marshal spbook: %w", err)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(rawJSON); err != nil {
		_ = gz.Close()
		return nil, "", fmt.Errorf("sync: gzip spbook: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, "", fmt.Errorf("sync: close gzip: %w", err)
	}

	compressed := buf.Bytes()
	sum := sha256.Sum256(compressed)
	return compressed, hex.EncodeToString(sum[:]), nil
}

// DecodeSPBook 校验 SHA-256（若提供 expectedSHA256）并解压解析 .spbook.json.gz。
func DecodeSPBook(compressed []byte, expectedSHA256 string) (*domain.SPBook, error) {
	if expectedSHA256 != "" {
		sum := sha256.Sum256(compressed)
		got := hex.EncodeToString(sum[:])
		if got != expectedSHA256 {
			return nil, fmt.Errorf("sync: spbook hash mismatch: got %s, want %s", got, expectedSHA256)
		}
	}

	gr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("sync: open gzip: %w", err)
	}
	defer gr.Close()

	rawJSON, err := io.ReadAll(gr)
	if err != nil {
		return nil, fmt.Errorf("sync: gunzip spbook: %w", err)
	}
	var book domain.SPBook
	if err := json.Unmarshal(rawJSON, &book); err != nil {
		return nil, fmt.Errorf("sync: unmarshal spbook: %w", err)
	}
	return &book, nil
}
