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
			line = strings.TrimSpace(StripNoteRefs(line))
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
