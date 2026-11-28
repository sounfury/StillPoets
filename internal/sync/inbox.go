package sync

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"stillpoets/internal/domain"
)

const (
	// InboxSchemaVersion 标记 inbox/*.ready.json 清单格式版本。
	InboxSchemaVersion = 1
	// InboxParserRuleVersion 标记当前客户端支持的诗集解析规则版本，用于稳定跨端篇序。
	InboxParserRuleVersion = 1
	// InboxReadySuffix 标记已完成原件上传并可供任意移动设备消费的清单后缀。
	InboxReadySuffix = ".ready.json"

	// MaxInboxReadyBytes 限制单份 ready.json 清单体积，避免损坏大文件被误读。
	MaxInboxReadyBytes int64 = 64 << 10 // 64 KiB
	// MaxInboxEPUBBytes 限制单次通过 inbox 消费的 EPUB 体积，保护移动端内存安全。
	MaxInboxEPUBBytes int64 = 64 << 20 // 64 MiB
)

// InboxItem 描述浏览器/外部工具投递到 WebDAV inbox/<importId>.ready.json 的待解析 EPUB 任务。
// 浏览器先将原件上传至 books/<bookId>/<rawFilename>，校验完成后最后写入本清单；
// 任意先同步的移动设备均可读取本清单、验证原件哈希后完成解析并发布书目。
type InboxItem struct {
	SchemaVersion     int            `json:"schemaVersion"`
	ImportID          string         `json:"importId"`
	BookID            string         `json:"bookId"`
	OriginalFilename  string         `json:"originalFilename"`
	RawFilename       string         `json:"rawFilename"`
	RawSize           int64          `json:"rawSize"`
	RawSHA256         string         `json:"rawSha256"`
	GroupID           string         `json:"groupId,omitempty"`
	InitialGroupVer   domain.Version `json:"initialGroupVer,omitempty"`
	InitialStarredVer domain.Version `json:"initialStarredVer,omitempty"`
	InitialSortVer    domain.Version `json:"initialSortVer,omitempty"`
	ParserRuleVersion int            `json:"parserRuleVersion"`
	SubmittedAt       int64          `json:"submittedAt"`
	Source            string         `json:"source,omitempty"`
}

// InboxReadyFilename 返回规范化的投递清单文件名。
func InboxReadyFilename(importID string) (string, error) {
	if !IsValidSafeSegment(importID) {
		return "", fmt.Errorf("sync: invalid inbox importId %q", importID)
	}
	return importID + InboxReadySuffix, nil
}

// InboxReadyPath 返回 inbox 清单在 WebDAV 工作区内的相对路径。
func InboxReadyPath(importID string) (string, error) {
	name, err := InboxReadyFilename(importID)
	if err != nil {
		return "", err
	}
	return InboxDir + "/" + name, nil
}

// ParseInboxReadyFilename 从 inbox/ 下的文件名解析 importID。
func ParseInboxReadyFilename(name string) (string, bool) {
	if !strings.HasSuffix(name, InboxReadySuffix) {
		return "", false
	}
	id := strings.TrimSuffix(name, InboxReadySuffix)
	if !IsValidSafeSegment(id) {
		return "", false
	}
	return id, true
}

// EncodeInboxItem 校验并序列化 inbox 清单。
func EncodeInboxItem(item InboxItem) ([]byte, error) {
	if item.SchemaVersion == 0 {
		item.SchemaVersion = InboxSchemaVersion
	}
	if item.ParserRuleVersion == 0 {
		item.ParserRuleVersion = InboxParserRuleVersion
	}
	if item.BookID == "" {
		item.BookID = item.ImportID
	}
	if item.ImportID == "" {
		item.ImportID = item.BookID
	}
	item.RawSHA256 = strings.ToLower(strings.TrimSpace(item.RawSHA256))
	if err := ValidateInboxItem(item, item.ImportID); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(item)
	if err != nil {
		return nil, fmt.Errorf("sync: marshal inbox item: %w", err)
	}
	if int64(len(raw)) > MaxInboxReadyBytes {
		return nil, fmt.Errorf("sync: inbox item exceeds max bytes (%d > %d)", len(raw), MaxInboxReadyBytes)
	}
	return raw, nil
}

// DecodeInboxItem 解码并严格校验 inbox/*.ready.json 清单。
func DecodeInboxItem(raw []byte, expectedImportID string) (*InboxItem, error) {
	if int64(len(raw)) > MaxInboxReadyBytes {
		return nil, fmt.Errorf("sync: inbox item exceeds limit (%d > %d)", len(raw), MaxInboxReadyBytes)
	}
	var item InboxItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, fmt.Errorf("sync: unmarshal inbox item: %w", err)
	}
	item.RawSHA256 = strings.ToLower(strings.TrimSpace(item.RawSHA256))
	if err := ValidateInboxItem(item, expectedImportID); err != nil {
		return nil, err
	}
	return &item, nil
}

// ValidateInboxItem 对 inbox 清单做路径安全、版本与哈希格式校验。
func ValidateInboxItem(item InboxItem, expectedImportID string) error {
	if item.SchemaVersion != InboxSchemaVersion {
		return fmt.Errorf("sync: unsupported inbox schemaVersion %d (want %d)", item.SchemaVersion, InboxSchemaVersion)
	}
	if item.ParserRuleVersion <= 0 || item.ParserRuleVersion > InboxParserRuleVersion {
		return fmt.Errorf("sync: unsupported inbox parserRuleVersion %d (max %d)", item.ParserRuleVersion, InboxParserRuleVersion)
	}
	if !IsValidSafeSegment(item.ImportID) || !IsValidSafeSegment(item.BookID) {
		return errors.New("sync: invalid inbox importId or bookId")
	}
	if item.ImportID != item.BookID {
		return fmt.Errorf("sync: inbox importId %q does not match bookId %q", item.ImportID, item.BookID)
	}
	if expectedImportID != "" && item.ImportID != expectedImportID {
		return fmt.Errorf("sync: inbox importId mismatch: got %q, want %q", item.ImportID, expectedImportID)
	}
	if !IsValidSafeSegment(item.RawFilename) {
		return fmt.Errorf("sync: invalid inbox rawFilename %q", item.RawFilename)
	}
	if item.RawSize <= 0 || item.RawSize > MaxInboxEPUBBytes {
		return fmt.Errorf("sync: invalid inbox rawSize %d", item.RawSize)
	}
	if !isHexSHA256(item.RawSHA256) {
		return fmt.Errorf("sync: invalid inbox rawSha256 %q", item.RawSHA256)
	}
	if item.SubmittedAt <= 0 {
		return errors.New("sync: inbox submittedAt must be positive")
	}
	return nil
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
