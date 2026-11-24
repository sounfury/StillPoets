package views

import (
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
)

type stubAppUpdater struct {
	mu          sync.Mutex
	canInstall  bool
	abis        []string
	installed   []string
	openedPerms int
}

func (s *stubAppUpdater) CanInstallPackages() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.canInstall, nil
}

func (s *stubAppUpdater) OpenInstallPermissionSettings() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.openedPerms++
	return nil
}

func (s *stubAppUpdater) InstallAPK(apkPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.installed = append(s.installed, apkPath)
	return nil
}

func (s *stubAppUpdater) DeviceABIs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.abis...)
}

func TestUpdateModalRendersAllStages(t *testing.T) {
	ctx := t.Context()
	svc, err := app.Open(ctx, t.TempDir(), app.NewMemorySecretStore())
	if err != nil {
		t.Fatalf("app.Open: %v", err)
	}
	defer svc.Close()

	appUI := NewAppUI(svc, nil, nil)
	appUI.SetAppVersion("0.1.3")
	updater := &stubAppUpdater{
		canInstall: true,
		abis:       []string{"arm64-v8a", "armeabi-v7a"},
	}
	appUI.SetAppUpdater(updater)

	rel := app.ReleaseInfo{
		TagName:     "v0.2.0",
		Name:        "StillPoets v0.2.0",
		Body:        "## StillPoets v0.2.0\n\n### 本次更新\n- 支持 Android 应用内自动更新与进度弹窗\n- 优化桌面小组件排版",
		HTMLURL:     "https://github.com/sounfury/StillPoets/releases/tag/v0.2.0",
		PublishedAt: time.Now(),
		Assets: []app.ReleaseAsset{
			{
				Name:        "StillPoets-0.2.0-android-arm64.apk",
				Size:        18 << 20,
				DownloadURL: "https://github.com/sounfury/StillPoets/releases/download/v0.2.0/StillPoets-0.2.0-android-arm64.apk",
			},
		},
	}

	appUI.openUpdateModalForRelease(rel, false)
	if !appUI.showUpdateModal || appUI.updateAPKAsset == nil || appUI.updateAPKAsset.Name != "StillPoets-0.2.0-android-arm64.apk" {
		t.Fatalf("expected update modal open with arm64 asset, got open=%v asset=%+v", appUI.showUpdateModal, appUI.updateAPKAsset)
	}

	stages := []struct {
		name  string
		setup func()
	}{
		{"prompt", func() {
			appUI.updateStage = updateStagePrompt
		}},
		{"downloading", func() {
			appUI.updateStage = updateStageDownloading
			appUI.updateDownloaded = 9 << 20
			appUI.updateTotal = 18 << 20
			appUI.updateSpeedBPS = 3.5 * 1024 * 1024
		}},
		{"need-permission", func() {
			appUI.updateStage = updateStageNeedPermission
			appUI.updateVerifiedSHA = true
			appUI.updateAPKPath = "/tmp/fake.apk"
		}},
		{"ready-to-install", func() {
			appUI.updateStage = updateStageReadyToInstall
			appUI.updateAPKPath = "/tmp/fake.apk"
		}},
		{"error", func() {
			appUI.updateStage = updateStageError
			appUI.updateStageError = "网络连接已重置"
		}},
	}

	for _, st := range stages {
		st.setup()
		if img := ui.Render(appUI.Render, 390, 844, 2.0); img == nil {
			t.Fatalf("stage %s phone render returned nil", st.name)
		}
		if img := ui.Render(appUI.Render, 1080, 720, 2.0); img == nil {
			t.Fatalf("stage %s tablet render returned nil", st.name)
		}
	}
}

func TestOnAppActivatedResumesInstallAfterPermissionGranted(t *testing.T) {
	ctx := t.Context()
	svc, err := app.Open(ctx, t.TempDir(), app.NewMemorySecretStore())
	if err != nil {
		t.Fatalf("app.Open: %v", err)
	}
	defer svc.Close()

	appUI := NewAppUI(svc, nil, nil)
	updater := &stubAppUpdater{
		canInstall: true,
		abis:       []string{"arm64-v8a"},
	}
	appUI.SetAppUpdater(updater)

	appUI.mu.Lock()
	appUI.updateStage = updateStageNeedPermission
	appUI.updateAPKPath = "/data/user/0/app.stillpoets.client/files/StillPoets/updates/StillPoets-0.2.0-android-arm64.apk"
	appUI.showUpdateModal = true
	appUI.mu.Unlock()

	appUI.OnAppActivated()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		updater.mu.Lock()
		n := len(updater.installed)
		updater.mu.Unlock()
		appUI.mu.Lock()
		stage := appUI.updateStage
		appUI.mu.Unlock()
		if n == 1 && stage == updateStageReadyToInstall {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}

	updater.mu.Lock()
	defer updater.mu.Unlock()
	t.Fatalf("expected OnAppActivated to resume APK install, installed=%v stage=%v", updater.installed, appUI.updateStage)
}
