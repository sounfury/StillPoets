package layout

import "github.com/egoist/mygo/ui"

const (
	// TabletBreakpointDIP 定义触发平板横屏工作区（左窄导航轨 + 双栏）的最小宽度。
	TabletBreakpointDIP float32 = 760

	// NavRailWidthDIP 是平板最左侧窄导航轨的固定宽度。
	NavRailWidthDIP float32 = 72

	// MasterPaneWidthDIP 是平板书架主栏宽度（保留右侧充裕的诗歌阅读呼吸感）。
	MasterPaneWidthDIP float32 = 348

	// ReadingMaxWidthDIP 限制整诗沉浸阅读区的黄金列宽。
	ReadingMaxWidthDIP float32 = 620
)

// IsTablet 判断当前窗口是否处于平板/宽屏形态。
func IsTablet(c *ui.Context) bool {
	w, _ := c.Size()
	return w >= TabletBreakpointDIP
}

// BookGridColumns 返回当前视口下书架封面的推荐列数（手机 3 列，平板左主栏 2 列）。
func BookGridColumns(c *ui.Context) int {
	if IsTablet(c) {
		return 2
	}
	return 3
}

// MasonryColumns 返回散页纸卡瀑布流的列数（手机 2 列，平板 3~4 列）。
func MasonryColumns(c *ui.Context) int {
	w, _ := c.Size()
	switch {
	case w >= 1080:
		return 4
	case w >= TabletBreakpointDIP:
		return 3
	default:
		return 2
	}
}

// MasterDetail 根据视口宽度在平板双栏与手机单栏间自适应切换。
func MasterDetail(c *ui.Context, masterWidth float32, borderColor ui.Color, master func(), detail func()) {
	if !IsTablet(c) {
		master()
		return
	}
	ui.Row(c).Fill().Children(func() {
		ui.Column(c.Key("master")).Width(masterWidth).Shrink(0).FillHeight().
			BorderColor(borderColor).BorderWidth(0, 1, 0, 0).
			Children(master)
		ui.Column(c.Key("detail")).Grow(1).FillHeight().Children(detail)
	})
}
