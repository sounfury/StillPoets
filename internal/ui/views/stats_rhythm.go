package views

import (
	"math"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/storage"
	"stillpoets/internal/ui/theme"
)

func renderRhythmDonut(c *ui.Context, pal theme.Palette, rhythm storage.ReadingRhythm, slotColors [5]ui.Color) {
	ui.Box(c.Key("rhythm-donut")).Width(122).Height(122).Center().Draw(func(p *ui.Painter, r ui.Rect) {
		cx := r.X + r.W/2
		cy := r.Y + r.H/2
		radius := min(r.W, r.H)/2 - 11
		const strokeW = float32(13)

		if rhythm.TotalEvents == 0 {
			ring := (&ui.Path{}).Circle(cx, cy, radius)
			p.StrokePath(ring, strokeW, pal.Border)
			return
		}

		nonZero := 0
		for _, sl := range rhythm.Slots {
			if sl.Count > 0 {
				nonZero++
			}
		}
		gap := float32(0)
		if nonZero > 1 {
			gap = 0.01
		}
		cursor := float32(0)
		for i, sl := range rhythm.Slots {
			if sl.Count <= 0 {
				continue
			}
			span := float32(sl.Ratio)
			from := cursor + gap/2
			to := cursor + span - gap/2
			if to <= from {
				to = from + 0.006
			}
			arc := (&ui.Path{}).Circle(cx, cy, radius).Trim(from, min(1, to))
			p.StrokePath(arc, strokeW, slotColors[i])
			cursor += span
		}
	}).Children(func() {
		ui.Column(c).Center().Gap(1).Children(func() {
			ui.Textf(c, "%d", rhythm.TotalEvents).
				Font(theme.FontSerif).FontFeatures("tnum").FontSize(22).Bold().TextColor(pal.TextPrimary)
			ui.Text(c, "次读诗").Font(theme.FontSans).FontSize(10.5).TextColor(pal.TextMuted)
		})
	})
}

func renderRhythmBreakdown(c *ui.Context, pal theme.Palette, rhythm storage.ReadingRhythm, slotColors [5]ui.Color) {
	ui.Column(c.Key("rhythm-slot-list")).Grow(1).FillWidth().Gap(9).Children(func() {
		for i, sl := range rhythm.Slots {
			pct := int(math.Round(sl.Ratio * 100))
			ui.Column(c.Key("rh-slot-" + sl.Key)).FillWidth().Gap(4).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(7).Children(func() {
					ui.Box(c).Width(8).Height(8).Radius(999).Background(slotColors[i])
					ui.Text(c, sl.Label).Font(theme.FontSerif).FontSize(12.5).Bold().TextColor(pal.TextPrimary)
					ui.Text(c, sl.RangeText).Font(theme.FontMono).FontFeatures("tnum").FontSize(10.5).TextColor(pal.TextMuted).Grow(1)
					ui.Textf(c, "%d 首 · %d%%", sl.Count, pct).Font(theme.FontMono).FontFeatures("tnum").FontSize(11).TextColor(pal.TextSecondary)
				})
				ui.Progress(c, sl.Ratio).Height(4).FillWidth().TextColor(slotColors[i]).Background(pal.SurfaceMuted)
			})
		}
	})
}
