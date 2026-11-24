package app

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	diagLogMaxBytes     = 512 * 1024 // 单文件最大 512 KiB，超出自动轮转保留 .1 备份
	diagExportMaxKeep   = 5          // 导出目录最多保留最近 5 份诊断日志，避免堆积
	diagExportTailBytes = 512 * 1024
)

var (
	reURLUserInfo = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)([^/\s]+)@`)
	reAuthHeader  = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)(?:basic|bearer|digest)\s+[^\s,;"]+`)
	reSecretKV    = regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|apikey|api_key)\s*[:=]\s*([^\s&,;"]+)`)
)

// SanitizeLogText 清除日志文本中的 URL 鉴权信息、Authorization 头与密码键值对，遵守凭据安全铁律。
func SanitizeLogText(s string) string {
	if s == "" {
		return ""
	}
	s = reURLUserInfo.ReplaceAllString(s, "${1}[REDACTED]@")
	s = reAuthHeader.ReplaceAllString(s, "${1}[REDACTED]")
	s = reSecretKV.ReplaceAllString(s, "${1}=[REDACTED]")
	return s
}

func sanitizeBaseURLForDiag(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "(未配置)"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "[invalid-url]"
	}
	return u.Scheme + "://" + u.Host
}

type diagLogger struct {
	mu      sync.Mutex
	logDir  string
	logPath string
}

func newDiagLogger(dataDir string) *diagLogger {
	dir := filepath.Join(dataDir, "logs")
	_ = os.MkdirAll(dir, 0o755)
	return &diagLogger{
		logDir:  dir,
		logPath: filepath.Join(dir, "app.log"),
	}
}

func (l *diagLogger) Logf(category, format string, args ...any) {
	if l == nil {
		return
	}
	msg := SanitizeLogText(fmt.Sprintf(format, args...))
	msg = strings.ReplaceAll(msg, "\r\n", " | ")
	msg = strings.ReplaceAll(msg, "\n", " | ")
	cat := strings.TrimSpace(category)
	if cat == "" {
		cat = "app"
	}
	line := fmt.Sprintf("%s [%s] %s\n", time.Now().Format("2006-01-02 15:04:05.000 -0700"), cat, msg)

	l.mu.Lock()
	defer l.mu.Unlock()

	if info, err := os.Stat(l.logPath); err == nil && info.Size() >= diagLogMaxBytes {
		backup := l.logPath + ".1"
		_ = os.Remove(backup)
		_ = os.Rename(l.logPath, backup)
	}
	_ = os.MkdirAll(l.logDir, 0o755)
	f, err := os.OpenFile(l.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	_, _ = f.WriteString(line)
	_ = f.Close()
}

func (l *diagLogger) ReadCombinedTail(maxBytes int) []byte {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	var buf bytes.Buffer
	if prev, err := os.ReadFile(l.logPath + ".1"); err == nil && len(prev) > 0 {
		buf.Write(prev)
		if prev[len(prev)-1] != '\n' {
			buf.WriteByte('\n')
		}
	}
	if cur, err := os.ReadFile(l.logPath); err == nil && len(cur) > 0 {
		buf.Write(cur)
	}
	raw := buf.Bytes()
	if maxBytes > 0 && len(raw) > maxBytes {
		raw = raw[len(raw)-maxBytes:]
		if idx := bytes.IndexByte(raw, '\n'); idx >= 0 && idx+1 < len(raw) {
			raw = raw[idx+1:]
		}
	}
	return raw
}

func (l *diagLogger) TotalSizeBytes() int64 {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var total int64
	for _, p := range []string{l.logPath, l.logPath + ".1"} {
		if info, err := os.Stat(p); err == nil {
			total += info.Size()
		}
	}
	return total
}

// LogEvent 向应用持久化诊断日志写入一条已自动脱敏的事件记录。
func (s *Service) LogEvent(category, format string, args ...any) {
	if s == nil || s.diag == nil {
		return
	}
	s.diag.Logf(category, format, args...)
}

// DiagnosticLogSizeBytes 返回当前运行期诊断日志占用的磁盘字节数。
func (s *Service) DiagnosticLogSizeBytes() int64 {
	if s == nil || s.diag == nil {
		return 0
	}
	return s.diag.TotalSizeBytes()
}

// ExportDiagnosticLog 生成一份包含运行环境、脱敏配置、同步/书架摘要与近期运行日志的完整诊断文件，
// 保存于 <dataDir>/logs/exports/ 并返回文件绝对路径与字节大小。严禁写入 WebDAV 用户名、密码或任何凭据。
func (s *Service) ExportDiagnosticLog(ctx context.Context, appVersion string) (string, int64, error) {
	if s == nil {
		return "", 0, fmt.Errorf("app: service is nil")
	}
	if ctx == nil || ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
	}

	ver := strings.TrimSpace(appVersion)
	if ver == "" {
		ver = DefaultAppVersion
	}

	s.LogEvent("diag", "export diagnostic log requested (version=%s)", ver)

	var b strings.Builder
	now := time.Now()
	fmt.Fprintf(&b, "============================================================\n")
	fmt.Fprintf(&b, "StillPoets Diagnostic Report\n")
	fmt.Fprintf(&b, "GeneratedAt : %s\n", now.Format(time.RFC3339))
	fmt.Fprintf(&b, "AppVersion  : %s\n", ver)
	fmt.Fprintf(&b, "Platform    : %s/%s (Go %s, CPUs=%d)\n", runtime.GOOS, runtime.GOARCH, runtime.Version(), runtime.NumCPU())
	fmt.Fprintf(&b, "============================================================\n\n")

	// 1. 本机外观与阅读配置（无敏感信息）
	b.WriteString("[Local Settings]\n")
	if st, err := s.store.LoadSettings(ctx); err == nil {
		fontName := st.FontFamily
		if fontName == "" {
			fontName = "(system-default)"
		}
		fmt.Fprintf(&b, "themePreset=%s followSystemDark=%v useOriginalCover=%v\n",
			st.ThemePreset, st.ShouldFollowSystemDark(), st.UseOriginalCover)
		fmt.Fprintf(&b, "fontFamily=%s fontSize=%.1f fontWeight=%d lineHeight=%.2f\n",
			fontName, st.FontSize, st.EffectiveFontWeight(), st.LineHeight)
		fmt.Fprintf(&b, "widgetMode=%s widgetRefreshMinutes=%d\n",
			st.WidgetMode, st.WidgetRefreshMinutes)
	} else {
		fmt.Fprintf(&b, "error=%s\n", SanitizeLogText(err.Error()))
	}

	fonts := s.ListLocalFonts()
	fmt.Fprintf(&b, "localFontsCount=%d cacheSizeBytes=%d\n", len(fonts), s.CacheSizeBytes())
	for _, lf := range fonts {
		fmt.Fprintf(&b, "  - font=%s file=%s sizeBytes=%d\n", lf.Family, lf.Filename, lf.Size)
	}
	b.WriteString("\n")

	// 2. 书架与内容统计（仅数量，不导出私密诗句内容）
	b.WriteString("[Library Summary]\n")
	if stats, err := s.GetStatsOverview(ctx); err == nil {
		fmt.Fprintf(&b, "books=%d offlineCached=%d finished=%d poemsRead=%d favorites=%d quotes=%d\n",
			stats.BookCount, stats.OfflineBookCount, stats.FinishedBookCount,
			stats.ReadPoemCount, stats.FavoriteCount, stats.QuoteCount)
	} else {
		fmt.Fprintf(&b, "error=%s\n", SanitizeLogText(err.Error()))
	}
	b.WriteString("\n")

	// 3. WebDAV 同步状态（严格脱敏：仅保留 host 与目录名，严禁记录账号/密码）
	b.WriteString("[WebDAV Sync Summary (Redacted)]\n")
	prof := s.LoadWebDAVProfile()
	fmt.Fprintf(&b, "enabled=%v host=%s appFolder=%s atomicUpload=%v hasSecretInKeychain=%v\n",
		prof.Enabled, sanitizeBaseURLForDiag(prof.BaseURL), prof.AppFolder, prof.AtomicUpload, s.HasWebDAVPassword())
	if ov, err := s.GetSyncOverview(ctx); err == nil {
		lastSync := "never"
		if ov.LastSyncAt > 0 {
			lastSync = time.UnixMilli(ov.LastSyncAt).Format(time.RFC3339)
		}
		fmt.Fprintf(&b, "installId=%s localRev=%d dirty=%v lastSyncAt=%s pendingUploads=%d remoteDevices=%v\n",
			ov.InstallID, ov.LocalRev, ov.Dirty, lastSync, ov.PendingCount, ov.RemoteDevices)
	} else {
		fmt.Fprintf(&b, "overviewError=%s\n", SanitizeLogText(err.Error()))
	}
	if pending, err := s.store.ListPendingUploads(ctx); err == nil && len(pending) > 0 {
		for _, p := range pending {
			fmt.Fprintf(&b, "  - pending bookId=%s state=%s lastError=%s\n",
				p.BookID, p.State, SanitizeLogText(p.LastError))
		}
	}
	b.WriteString("\n")

	// 4. 运行期事件日志
	b.WriteString("[Runtime Event Logs]\n")
	tail := s.diag.ReadCombinedTail(diagExportTailBytes)
	if len(tail) == 0 {
		b.WriteString("(no runtime log entries recorded yet)\n")
	} else {
		b.Write(tail)
		if tail[len(tail)-1] != '\n' {
			b.WriteByte('\n')
		}
	}

	exportDir := filepath.Join(s.dataDir, "logs", "exports")
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		return "", 0, fmt.Errorf("创建日志导出目录失败：%w", err)
	}
	filename := fmt.Sprintf("StillPoets-diagnostic-%s.log", now.Format("20060102-150405"))
	outPath := filepath.Join(exportDir, filename)
	payload := []byte(b.String())
	if err := os.WriteFile(outPath, payload, 0o600); err != nil {
		return "", 0, fmt.Errorf("写入诊断日志文件失败：%w", err)
	}

	pruneOldDiagnosticExports(exportDir, diagExportMaxKeep)
	return outPath, int64(len(payload)), nil
}

func pruneOldDiagnosticExports(exportDir string, keep int) {
	entries, err := os.ReadDir(exportDir)
	if err != nil || len(entries) <= keep {
		return
	}
	type fileItem struct {
		path    string
		modTime time.Time
	}
	items := make([]fileItem, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, fileItem{
			path:    filepath.Join(exportDir, e.Name()),
			modTime: info.ModTime(),
		})
	}
	if len(items) <= keep {
		return
	}
	slices.SortFunc(items, func(a, b fileItem) int {
		if a.modTime.After(b.modTime) {
			return -1
		}
		if a.modTime.Before(b.modTime) {
			return 1
		}
		return strings.Compare(b.path, a.path)
	})
	for _, old := range items[keep:] {
		_ = os.Remove(old.path)
	}
}
