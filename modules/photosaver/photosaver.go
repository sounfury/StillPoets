// Package photosaver 提供将高清诗歌卡片 PNG 图片一键保存到 iOS / Android 系统相册的原生桥接能力。
package photosaver

// Saver 定义跨平台系统相册写入契约：
//   - Android 10+ 通过 MediaStore 零权限直存 Pictures/<albumName>/；
//   - iOS 通过 PHPhotoLibrary (.addOnly) 写入系统相册。
//
//mygo:native
type Saver interface {
	// SaveImage 将本地图片文件写入系统相册（albumName 为空时默认使用 "StillPoets"）。
	SaveImage(filePath string, albumName string) error
}
