package theme

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// StillPoets 动效 Token：克制、温润、无浮夸过冲，贴合纸页与画册阅读气质。
var (
	// PageSpring 用于翻诗、诗集/散页横滑与抽屉落位：零过冲平滑弹簧，跟随手指初速度。
	PageSpring = ui.Animation{
		Spring: ui.SpringOptions{
			Duration: 340 * time.Millisecond,
			Bounce:   0,
		},
	}

	// ReaderSlideSpring 用于手机端全屏阅读器推入/滑出转场。
	ReaderSlideSpring = ui.Animation{
		Spring: ui.SpringOptions{
			Duration: 320 * time.Millisecond,
			Bounce:   0,
		},
	}

	// PressSpring 用于卡片/书封/导航项的轻量按压缩放反馈。
	PressSpring = ui.Animation{
		Spring: ui.SpringOptions{
			Duration: 220 * time.Millisecond,
			Bounce:   0.08,
		},
	}
)

// TabPageTransition 返回主导航（书架 / 统计 / 我的）切换时的柔和淡入微浮过渡。
func TabPageTransition() ui.ElementTransition {
	return ui.ElementTransition{
		Duration:     220 * time.Millisecond,
		Ease:         ui.EaseOut,
		ExitDuration: 150 * time.Millisecond,
		ExitEase:     ui.EaseIn,
		Enter:        &ui.Motion{Y: 8, Opacity: 0},
		Exit:         &ui.Motion{Opacity: 0},
	}
}

// PoemJumpTransition 返回目录远距离跨诗跳转时的柔和纸页淡入过渡。
func PoemJumpTransition() ui.ElementTransition {
	return ui.ElementTransition{
		Duration:     220 * time.Millisecond,
		Ease:         ui.EaseOut,
		ExitDuration: 140 * time.Millisecond,
		ExitEase:     ui.EaseIn,
		Enter:        &ui.Motion{Y: 6, Opacity: 0},
		Exit:         &ui.Motion{Opacity: 0},
	}
}

// MobileReaderTransition 返回手机端进入/退出全屏阅读器的横向推入转场。
func MobileReaderTransition() ui.ElementTransition {
	anim := ReaderSlideSpring
	return ui.ElementTransition{
		Animation:    &anim,
		ExitDuration: 240 * time.Millisecond,
		ExitEase:     ui.EaseOut,
		Enter:        &ui.Motion{XPercent: 100, Opacity: 0.92},
		Exit:         &ui.Motion{XPercent: 100, Opacity: 0.92},
	}
}

// BackdropTransition 返回弹层半透明遮罩的淡入淡出过渡。
func BackdropTransition() ui.ElementTransition {
	return ui.ElementTransition{
		Duration:     200 * time.Millisecond,
		Ease:         ui.EaseOut,
		ExitDuration: 150 * time.Millisecond,
		ExitEase:     ui.EaseIn,
		Enter:        &ui.Motion{Opacity: 0},
		Exit:         &ui.Motion{Opacity: 0},
	}
}

// DialogPanelTransition 返回居中弹窗卡片的弹性缩放上浮进场过渡。
func DialogPanelTransition() ui.ElementTransition {
	anim := ui.Animation{
		Spring: ui.SpringOptions{
			Duration: 280 * time.Millisecond,
			Bounce:   0.06,
		},
	}
	return ui.ElementTransition{
		Animation:    &anim,
		ExitDuration: 150 * time.Millisecond,
		ExitEase:     ui.EaseIn,
		Enter:        &ui.Motion{Y: 14, Scale: 0.96, Opacity: 0},
		Exit:         &ui.Motion{Y: 8, Scale: 0.97, Opacity: 0},
	}
}

// ToastTransition 返回底部瞬态云同步胶囊的浮现与淡出过渡。
func ToastTransition() ui.ElementTransition {
	anim := ui.Animation{
		Spring: ui.SpringOptions{
			Duration: 300 * time.Millisecond,
			Bounce:   0.1,
		},
	}
	return ui.ElementTransition{
		Animation:    &anim,
		ExitDuration: 180 * time.Millisecond,
		ExitEase:     ui.EaseIn,
		Enter:        &ui.Motion{Y: 16, Scale: 0.95, Opacity: 0},
		Exit:         &ui.Motion{Y: 12, Scale: 0.96, Opacity: 0},
	}
}

// BarRevealTransition 返回阅读器划选操作条等局部区域的平滑折叠展开过渡。
func BarRevealTransition() ui.ElementTransition {
	return ui.ElementTransition{
		Duration:     200 * time.Millisecond,
		Ease:         ui.EaseOut,
		ExitDuration: 150 * time.Millisecond,
		ExitEase:     ui.EaseIn,
		Enter:        &ui.Motion{Collapse: true, Opacity: 0},
		Exit:         &ui.Motion{Collapse: true, Opacity: 0},
	}
}

// ColorTransition 返回标签/芯片选中态平滑变色过渡。
func ColorTransition() ui.ElementTransition {
	return ui.ElementTransition{
		Duration: 180 * time.Millisecond,
		Ease:     ui.EaseOut,
		Colors:   true,
	}
}

// ApplyPressScale 在元素按下时施加温润的微缩触控反馈（基于纯绘制层 Pose，零布局抖动）。
func ApplyPressScale(e ui.Element, pressedScale float32) {
	p := ui.Pose{Scale: 1}
	if e.Pressed() {
		p.Scale = pressedScale
	}
	e.AnimatePose(p, PressSpring)
}
