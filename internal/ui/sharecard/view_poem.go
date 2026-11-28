package sharecard

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/ui/theme"
)

func renderPoemHeader(c *ui.Context, data CardData, pal CardPalette, poetryFont string) {
	ui.Column(c.Key("sc-poem-header")).FillWidth().Gap(6).Children(func() {
		title := strings.TrimSpace(data.PoemTitle)
		if title == "" {
			title = "无题"
		}
		ui.Text(c, title).
			Font(poetryFont).FontSize(20).Bold().LineHeight(1.38).TextColor(pal.TextPrimary)

		if sub := strings.TrimSpace(data.PoemSubtitle); sub != "" {
			ui.Text(c, sub).
				Font(theme.FontNote).FontSize(12.5).LineHeight(1.5).TextColor(pal.TextSecondary)
		}

		meta := []string{}
		if a := strings.TrimSpace(data.Author); a != "" {
			meta = append(meta, a)
		}
		if sec := strings.TrimSpace(data.PoemSection); sec != "" {
			meta = append(meta, sec)
		}
		if len(meta) > 0 {
			ui.Text(c, strings.Join(meta, " · ")).
				Font(theme.FontNote).FontSize(12).TextColor(pal.TextMuted)
		}
	})
}

func renderPoemVerses(c *ui.Context, data CardData, pal CardPalette, poetryFont string, weight int) {
	// 完整诗节正文
	ui.Column(c.Key("sc-poem-stanzas")).FillWidth().Gap(14).Padding(4, 0, 2, 0).Children(func() {
		if len(data.Stanzas) == 0 && strings.TrimSpace(data.QuoteText) != "" {
			ui.Text(c, strings.TrimSpace(data.QuoteText)).
				Font(poetryFont).FontSize(15.5).FontWeight(weight).LineHeight(1.85).TextColor(pal.TextPrimary)
			return
		}
		for si, stanza := range data.Stanzas {
			lines := make([]string, 0, len(stanza.Lines))
			for _, line := range stanza.Lines {
				if trimmed := strings.TrimRight(line, " \t"); trimmed != "" {
					lines = append(lines, trimmed)
				}
			}
			if len(lines) == 0 {
				continue
			}
			ui.Text(c.Key(fmt.Sprintf("sc-stanza-%d", si)), strings.Join(lines, "\n")).
				Font(poetryFont).
				FontSize(15.2).
				FontWeight(weight).
				LineHeight(1.86).
				TextColor(pal.TextPrimary)
		}
	})
}

func renderPoemColophon(c *ui.Context, data CardData, pal CardPalette) {
	// 落款与原书出处
	colophon := strings.TrimSpace(data.Colophon)
	bookTitle := strings.TrimSpace(data.BookTitle)
	if bookTitle == "" {
		bookTitle = strings.TrimSpace(data.SourceBook)
	}
	if colophon != "" || bookTitle != "" {
		ui.Column(c.Key("sc-poem-tail")).FillWidth().Gap(4).Padding(6, 0, 0, 0).AlignItems(ui.End).Children(func() {
			if colophon != "" {
				ui.Text(c, colophon).Font(theme.FontNote).FontSize(11).Italic().TextColor(pal.TextMuted)
			}
			if bookTitle != "" {
				ui.Text(c, "选自《"+bookTitle+"》").Font(theme.FontNote).FontSize(11).TextColor(pal.TextSecondary)
			}
		})
	}
}
