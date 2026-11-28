package components

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/mobile"

	"stillpoets/internal/ui/theme"
)

// DialogOptions 封装居中弹层的标识与最大尺寸。
type DialogOptions struct {
	Key       string
	MaxWidth  float32
	MaxHeight float32
}

// AnimatedDialog 渲染带遮罩淡入淡出与卡片弹性缩放上浮的居中弹层。
// 点击遮罩或按 Escape 会将 *open 设为 false。
func AnimatedDialog(c *ui.Context, open *bool, pal theme.Palette, opts DialogOptions, content func()) {
	if !*open {
		return
	}
	key, maxWidth, maxHeight := opts.Key, opts.MaxWidth, opts.MaxHeight
	winW, winH := c.Size()
	w := maxWidth
	if winW > 0 && winW-32 < w {
		w = winW - 32
	}
	if w < 260 {
		w = 260
	}
	mh := maxHeight
	if winH > 0 && (mh <= 0 || winH-48 < mh) {
		mh = winH - 48
	}

	ui.Overlay(c, func() {
		back := ui.Box(c.Key("dlg-back-" + key)).
			Absolute().
			Left(0).Top(0).Right(0).Bottom(0).
			Center().
			Modal().
			Background(ui.RGBA(0, 0, 0, 0.42)).
			Transition(theme.BackdropTransition())

		if back.Clicked() || back.OverlayShortcut(0, ui.KeyEscape) {
			*open = false
		}

		back.Children(func() {
			panel := ui.Column(c.Key("dlg-panel-"+key)).
				Width(w).
				MaxHeight(mh).
				Padding(20).
				Gap(12).
				Radius(14).
				Background(pal.Surface).
				Border(1, pal.Border).
				Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, 0.26)).
				Role(ui.RoleDialog).
				Transition(theme.DialogPanelTransition())

			// 消费卡片内点击，避免事件冒泡到遮罩关闭弹窗
			panel.Clicked()
			panel.Children(content)
		})
	})
}

// AnimatedSheet 渲染原生移动端手势底部抽屉（支持弹簧弹出、下拉关闭与平板居中限宽）。
// 注意：MyGo mobile.Sheet 的多段 detents 是固定按最大高度布局再向下平移偏移量实现的，
// 半高驻留时会把内部滚动区底部推出屏幕之外；因此统一收敛为单段可见高度，配合 Basis(0) 确保长列表完整滚到底。
func AnimatedSheet(c *ui.Context, open *bool, pal theme.Palette, detents []float32, label string, content func()) {
	bg := pal.Surface
	effective := detents
	if len(detents) > 1 {
		best := detents[0]
		for _, d := range detents[1:] {
			if d > best {
				best = d
			}
		}
		effective = []float32{best}
	}
	mobile.Sheet(c, open, mobile.SheetOptions{
		Detents:    effective,
		Background: &bg,
		Label:      label,
	}, func() {
		ui.Column(c).FillWidth().Grow(1).Basis(0).AlignItems(ui.Center).Padding(4, 18, 16, 18).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(560).Grow(1).Basis(0).Gap(12).Children(content)
		})
	})
}
