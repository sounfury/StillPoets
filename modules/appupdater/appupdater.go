// Package appupdater 提供 Android 原生应用内更新安装桥接能力。
package appupdater

// Installer 定义 Android APK 安装器原生桥接契约。
//
//mygo:native
type Installer interface {
	// CanInstallPackages 报告当前应用是否已具备安装未知来源应用权限（Android 8.0+）。
	CanInstallPackages() (bool, error)
	// OpenInstallPermissionSettings 打开系统的「允许安装未知应用」权限设置页（Android 8.0+）。
	OpenInstallPermissionSettings() error
	// InstallAPK 通过 FileProvider 拉起 Android 系统安装器安装指定路径的 APK 文件。
	InstallAPK(apkPath string) error
	// DeviceABIs 返回设备按优先级排序的 ABI 列表（逗号分隔，如 "arm64-v8a,armeabi-v7a"）。
	DeviceABIs() (string, error)
}
