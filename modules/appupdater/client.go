package appupdater

import (
	"errors"
	"strings"
	"sync"
)

var (
	instOnce sync.Once
	inst     Installer
	instErr  error
)

func getInstaller() (Installer, error) {
	instOnce.Do(func() {
		inst, instErr = NewInstaller()
	})
	return inst, instErr
}

// Available 报告当前运行平台是否具备原生应用内 APK 更新安装能力（仅 Android 返回 true）。
func Available() bool {
	b, err := getInstaller()
	return err == nil && b != nil
}

// CanInstallPackages 检查当前应用是否已获得安装未知来源 APK 的系统权限。
func CanInstallPackages() (bool, error) {
	b, err := getInstaller()
	if err != nil {
		return false, err
	}
	if b == nil {
		return false, errors.ErrUnsupported
	}
	return b.CanInstallPackages()
}

// OpenInstallPermissionSettings 打开系统「允许安装未知应用」设置页。
func OpenInstallPermissionSettings() error {
	b, err := getInstaller()
	if err != nil {
		return err
	}
	if b == nil {
		return errors.ErrUnsupported
	}
	return b.OpenInstallPermissionSettings()
}

// InstallAPK 拉起系统安装器安装本地 APK 文件。
func InstallAPK(apkPath string) error {
	b, err := getInstaller()
	if err != nil {
		return err
	}
	if b == nil {
		return errors.ErrUnsupported
	}
	return b.InstallAPK(apkPath)
}

// DeviceABIs 返回设备支持的 Android ABI 优先级列表；非 Android 平台返回 nil。
func DeviceABIs() []string {
	b, err := getInstaller()
	if err != nil || b == nil {
		return nil
	}
	raw, err := b.DeviceABIs()
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
