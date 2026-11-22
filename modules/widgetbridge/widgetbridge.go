// Package widgetbridge 负责主 App 与原生桌面小组件之间的快照路径同步、刷新触发与固定引导。
package widgetbridge

// Bridge 定义原生桌面小组件桥接契约（Android 使用 Kotlin AppWidget 实现，后续可扩展 iOS Swift WidgetKit）。
//
//mygo:native
type Bridge interface {
	// Sync 将 widget_timeline.json 路径与自定义字体目录持久化给原生小组件，并刷新桌面所有 StillPoets 小组件实例。
	Sync(timelinePath string, fontsDir string) error
	// RequestPin 请求系统桌面弹出「将 StillPoets 诗笺添加到主屏幕」引导弹窗（Android 8.0+ 支持）。
	// 若当前启动器不支持程序化固定，返回 false, nil。
	RequestPin() (bool, error)
	// ActiveWidgetCount 返回当前桌面上已添加的 StillPoets 小组件实例数量。
	ActiveWidgetCount() (int, error)
}
