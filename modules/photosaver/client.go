package photosaver

import (
	"errors"
	"strings"
	"sync"
)

var (
	saverOnce sync.Once
	saverInst Saver
	saverErr  error
)

func getSaver() (Saver, error) {
	saverOnce.Do(func() {
		saverInst, saverErr = NewSaver()
	})
	return saverInst, saverErr
}

// Available 报告当前平台是否支持直接写入系统相册（iOS / Android 返回 true，桌面端返回 false）。
func Available() bool {
	s, err := getSaver()
	return err == nil && s != nil
}

// SaveImage 将指定路径的图片保存到系统相册。
// 若当前平台不支持（如 Linux/macOS/Windows 桌面预览），返回 errors.ErrUnsupported。
func SaveImage(filePath, albumName string) error {
	s, err := getSaver()
	if err != nil {
		return err
	}
	if s == nil {
		return errors.ErrUnsupported
	}
	album := strings.TrimSpace(albumName)
	if album == "" {
		album = "StillPoets"
	}
	return s.SaveImage(filePath, album)
}
