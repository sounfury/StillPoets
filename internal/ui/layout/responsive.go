package layout

import "github.com/egoist/mygo/ui"

const (
	// TabletBreakpointDIP 定义触发平板双栏布局的最小视口宽度（DIP）。
	// 面向 Android 平板优先适配，横竖屏均能自然切换单栏与双栏。
	TabletBreakpointDIP float32 = 720

	// ReadingMaxWidthDIP 限制平板宽屏下诗歌正文的最大列宽，保证优雅留白。
	ReadingMaxWidthDIP float32 = 640
)

// IsTablet 判断当前窗口是否处于平板/宽屏形态。
func IsTablet(c *ui.Context) bool {
	w, _ := c.Size()
	return w >= TabletBreakpointDIP
}

// MasterDetail 根据设备视口宽度自动切换布局：
// - 平板宽屏（>= 720 DIP）：左右双栏（左侧目录/列表 + 右侧正文/详情）
// - 手机窄屏（< 720 DIP）：只渲染主栏（详情通过原生导航栈 Push 展示）
func MasterDetail(c *ui.Context, masterWidth float32, master func(), detail func()) {
	if !IsTablet(c) {
		master()
		return
	}
	ui.Row(c).Fill().Children(func() {
		ui.Column(c.Key("master")).Width(masterWidth).Shrink(0).Children(master)
		ui.Divider(c)
		ui.Column(c.Key("detail")).Grow(1).Children(detail)
	})
}
