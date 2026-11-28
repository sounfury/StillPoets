package parser

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"stillpoets/internal/domain"
)

var inlineNoteRefs = regexp.MustCompile(`\[[0-9]+\]|［[0-9]+］|【[0-9]+】|[①-⑳㉑-㉟㊱-㊿⁰¹²³⁴⁵⁶⁷⁸⁹]+`)

// StripNoteRefs removes conventional inline note markers while preserving verse numbers.
func StripNoteRefs(s string) string { return inlineNoteRefs.ReplaceAllString(s, "") }

// BuildDefaultExcerpt selects up to four lines of the first stanza (at most 160 runes).
// Prose and ancillary fields are excluded from widget candidates.
func BuildDefaultExcerpt(p domain.Poem) string {
	if !p.IsPoem() {
		return ""
	}
	for _, stanza := range p.Stanzas {
		var lines []string
		used := 0
		for _, line := range stanza.Lines {
			line = excerptLine(line)
			if line == "" {
				continue
			}
			if len(lines) > 0 && used+utf8.RuneCountInString(line)+1 > 160 {
				break
			}
			if utf8.RuneCountInString(line) > 160 {
				line = string([]rune(line)[:159]) + "…"
			}
			lines = append(lines, line)
			used += utf8.RuneCountInString(line) + 1
			if len(lines) == 4 {
				break
			}
		}
		if len(lines) > 0 {
			return strings.Join(lines, "\n")
		}
	}
	return ""
}

// BuildLargeExcerpt selects multiple stanzas (preserving blank lines between stanzas,
// up to 14 lines and 420 runes) so 4×4 / large widgets read like a full book page
// instead of leaving large empty margins around 2–3 short lines.
func BuildLargeExcerpt(p domain.Poem) string {
	if !p.IsPoem() {
		return ""
	}
	var out []string
	used := 0
	for _, stanza := range p.Stanzas {
		lines := excerptStanzaLines(stanza)
		if len(lines) == 0 {
			continue
		}
		if len(out) > 0 {
			// Leave room for both the stanza separator and the first line.
			if len(out)+2 > largeExcerptLines || used+1+utf8.RuneCountInString(lines[0])+1 > largeExcerptRunes {
				break
			}
			out = append(out, "")
			used++
		}
		var reachedLimit bool
		out, used, reachedLimit = appendExcerptStanza(out, used, lines)
		if reachedLimit {
			break
		}
	}
	return strings.Join(out, "\n")
}

const (
	largeExcerptLines = 14
	largeExcerptRunes = 420
)

func excerptLine(line string) string {
	return strings.TrimSpace(StripNoteRefs(line))
}

func excerptStanzaLines(stanza domain.Stanza) []string {
	var lines []string
	for _, rawLine := range stanza.Lines {
		if line := excerptLine(rawLine); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func appendExcerptStanza(out []string, used int, lines []string) ([]string, int, bool) {
	for _, line := range lines {
		count := utf8.RuneCountInString(line)
		if len(out) >= largeExcerptLines || len(out) > 0 && used+count+1 > largeExcerptRunes {
			return out, used, true
		}
		if count > largeExcerptRunes {
			line = string([]rune(line)[:largeExcerptRunes-1]) + "…"
			count = largeExcerptRunes
		}
		out = append(out, line)
		used += count + 1
	}
	return out, used, false
}
