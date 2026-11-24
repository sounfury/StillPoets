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

// BuildLargeExcerpt selects multiple stanzas (preserving blank lines between stanzas,
// up to 14 lines and 420 runes) so 4×4 / large widgets read like a full book page
// instead of leaving large empty margins around 2–3 short lines.
func BuildLargeExcerpt(p domain.Poem) string {
	if !p.IsPoem() {
		return ""
	}
	const (
		maxLines = 14
		maxRunes = 420
	)
	var out []string
	used := 0

	for _, stanza := range p.Stanzas {
		var stanzaLines []string
		for _, rawLine := range stanza.Lines {
			line := strings.TrimSpace(StripNoteRefs(rawLine))
			if line == "" {
				continue
			}
			stanzaLines = append(stanzaLines, line)
		}
		if len(stanzaLines) == 0 {
			continue
		}

		// 跨诗节时插入一行空行作为诗节呼吸分隔
		if len(out) > 0 {
			// 若剩余行数仅够空行不够下一节首行，直接结束，避免末尾悬空空行
			if len(out)+2 > maxLines {
				break
			}
			firstLen := utf8.RuneCountInString(stanzaLines[0])
			if used+1+firstLen+1 > maxRunes {
				break
			}
			out = append(out, "")
			used++
		}

		reachedLimit := false
		for _, line := range stanzaLines {
			if len(out) >= maxLines {
				reachedLimit = true
				break
			}
			rCount := utf8.RuneCountInString(line)
			if len(out) > 0 && used+rCount+1 > maxRunes {
				reachedLimit = true
				break
			}
			if rCount > maxRunes {
				line = string([]rune(line)[:maxRunes-1]) + "…"
				rCount = maxRunes
			}
			out = append(out, line)
			used += rCount + 1
		}
		if reachedLimit {
			break
		}
	}

	// 兜底剔除可能存在的尾空行
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}
