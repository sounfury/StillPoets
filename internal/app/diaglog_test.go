package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stillpoets/internal/domain"
)

func TestSanitizeLogText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "url userinfo",
			in:   "GET https://alice:supersecret@dav.example.com/dav/StillPoets/state failed",
			want: "GET https://[REDACTED]@dav.example.com/dav/StillPoets/state failed",
		},
		{
			name: "authorization header",
			in:   "Authorization: Basic YWxpY2U6c2VjcmV0 rejected",
			want: "Authorization: [REDACTED] rejected",
		},
		{
			name: "password kv",
			in:   "connect error password=my_top_secret token=abc12345",
			want: "connect error password=[REDACTED] token=[REDACTED]",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeLogText(tc.in)
			if got != tc.want {
				t.Fatalf("SanitizeLogText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestExportDiagnosticLogAndRedaction(t *testing.T) {
	ctx := t.Context()
	svc, err := Open(ctx, t.TempDir(), NewMemorySecretStore())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer svc.Close()

	// 配置模拟 WebDAV 凭据（用于验证导出日志绝不会泄露账号和密码）
	const secretUser = "poet_private_user@example.com"
	const secretPass = "UltraSecretPass#9988"
	if err := svc.SaveWebDAVConfig(ctx, WebDAVConfigInput{
		BaseURL:      "https://dav.jianguoyun.com/dav/",
		Username:     secretUser,
		Password:     secretPass,
		AppFolder:    "StillPoets",
		AtomicUpload: false,
		Enabled:      true,
	}); err != nil {
		t.Fatalf("SaveWebDAVConfig: %v", err)
	}

	// 写入包含敏感片段的运行事件，验证自动脱敏
	svc.LogEvent("sync", "upstream error from https://%s:%s@dav.jianguoyun.com/dav/ Authorization: Bearer tok_xyz", secretUser, secretPass)

	// 设置字重为 500
	if _, err := svc.UpdateSettings(ctx, func(st *domain.Settings) {
		st.FontWeight = domain.FontWeightMedium
	}); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}

	outPath, sizeBytes, err := svc.ExportDiagnosticLog(ctx, "0.2.3")
	if err != nil {
		t.Fatalf("ExportDiagnosticLog: %v", err)
	}
	if sizeBytes <= 0 {
		t.Fatalf("expected positive exported size, got %d", sizeBytes)
	}
	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", outPath, err)
	}
	content := string(raw)

	for _, mustContain := range []string{
		"StillPoets Diagnostic Report",
		"AppVersion  : 0.2.3",
		"fontWeight=500",
		"host=https://dav.jianguoyun.com",
		"hasSecretInKeychain=true",
		"[Runtime Event Logs]",
		"[REDACTED]",
	} {
		if !strings.Contains(content, mustContain) {
			t.Fatalf("diagnostic log missing %q:\n%s", mustContain, content)
		}
	}

	for _, forbidden := range []string{secretUser, secretPass, "tok_xyz"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("diagnostic log leaked sensitive value %q:\n%s", forbidden, content)
		}
	}

	// 验证旧导出文件自动修剪（最多保留 diagExportMaxKeep 份）
	exportDir := filepath.Dir(outPath)
	for i := 0; i < diagExportMaxKeep+3; i++ {
		f := filepath.Join(exportDir, "StillPoets-diagnostic-old-"+string(rune('a'+i))+".log")
		if err := os.WriteFile(f, []byte("old"), 0o600); err != nil {
			t.Fatalf("write old log: %v", err)
		}
		ts := time.Now().Add(-time.Duration(i+1) * time.Minute)
		_ = os.Chtimes(f, ts, ts)
	}
	pruneOldDiagnosticExports(exportDir, diagExportMaxKeep)
	entries, err := os.ReadDir(exportDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != diagExportMaxKeep {
		t.Fatalf("expected %d exported logs after prune, got %d", diagExportMaxKeep, len(entries))
	}
}

func TestDeleteFontAndFontWeightSettings(t *testing.T) {
	ctx := t.Context()
	dataDir := t.TempDir()
	svc, err := Open(ctx, dataDir, NewMemorySecretStore())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer svc.Close()

	// 创建模拟字体文件并导入
	srcFont := filepath.Join(t.TempDir(), "LXGWWenKai.ttf")
	if err := os.WriteFile(srcFont, []byte("fake-ttf-bytes-for-test"), 0o600); err != nil {
		t.Fatalf("write temp font: %v", err)
	}
	lf, raw, err := svc.ImportFont(srcFont)
	if err != nil {
		t.Fatalf("ImportFont: %v", err)
	}
	if lf.Family != "LXGWWenKai" || lf.Size != int64(len(raw)) {
		t.Fatalf("unexpected LocalFont: %+v", lf)
	}

	// 启用该字体与中等字重 500
	st, err := svc.UpdateSettings(ctx, func(s *domain.Settings) {
		s.FontFamily = lf.Family
		s.FontWeight = domain.FontWeightMedium
		s.WidgetTheme.FontFamily = lf.Family
		s.WidgetTheme.FontFile = lf.Filename
		s.WidgetTheme.FontWeight = domain.FontWeightMedium
	})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if st.EffectiveFontWeight() != 500 {
		t.Fatalf("expected weight 500, got %d", st.EffectiveFontWeight())
	}

	// 拒绝路径穿越文件名
	if err := svc.DeleteFont(ctx, "../stillpoets.db"); err == nil {
		t.Fatalf("expected path traversal delete to fail")
	}

	// 正常删除当前启用字体，应自动回退系统默认字体并保留字重
	if err := svc.DeleteFont(ctx, lf.Filename); err != nil {
		t.Fatalf("DeleteFont: %v", err)
	}
	if got := svc.ListLocalFonts(); len(got) != 0 {
		t.Fatalf("expected 0 fonts after delete, got %d", len(got))
	}
	reloaded, err := svc.LoadSettings(ctx)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if reloaded.FontFamily != "" || reloaded.WidgetTheme.FontFile != "" || reloaded.WidgetTheme.FontFamily != "" {
		t.Fatalf("expected font fallback to default after deleting active font, got %+v", reloaded)
	}
	if reloaded.EffectiveFontWeight() != 500 {
		t.Fatalf("expected fontWeight preserved at 500, got %d", reloaded.EffectiveFontWeight())
	}
}
