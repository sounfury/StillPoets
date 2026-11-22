package widgetbridge

import (
	"errors"
	"sync"
)

var (
	bridgeOnce sync.Once
	bridgeInst Bridge
	bridgeErr  error
)

func getBridge() (Bridge, error) {
	bridgeOnce.Do(func() {
		bridgeInst, bridgeErr = NewBridge()
	})
	return bridgeInst, bridgeErr
}

// Available 报告当前运行平台是否具备原生桌面小组件桥接能力。
func Available() bool {
	b, err := getBridge()
	return err == nil && b != nil
}

// Reload 将最新快照路径和字体目录同步给原生侧，并触发所有桌面小组件刷新。
// 在无原生小组件实现的平台（如桌面单测）上静默跳过。
func Reload(timelinePath, fontsDir string) {
	b, err := getBridge()
	if err != nil || b == nil {
		return
	}
	_ = b.Sync(timelinePath, fontsDir)
}

// RequestPin 请求系统桌面将 StillPoets 小组件固定到主屏幕。
// 若平台不支持则返回 errors.ErrUnsupported；若桌面启动器不支持固定则返回 false, nil。
func RequestPin() (bool, error) {
	b, err := getBridge()
	if err != nil {
		return false, err
	}
	if b == nil {
		return false, errors.ErrUnsupported
	}
	return b.RequestPin()
}

// ActiveWidgetCount 返回当前桌面上已添加的小组件数量；非移动平台返回 0。
func ActiveWidgetCount() int {
	b, err := getBridge()
	if err != nil || b == nil {
		return 0
	}
	n, err := b.ActiveWidgetCount()
	if err != nil {
		return 0
	}
	return n
}
