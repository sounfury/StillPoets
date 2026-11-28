package parser

import (
	"math"
	"regexp"
	"strings"

	"golang.org/x/net/html"

	"stillpoets/internal/domain"
)

var numberedPart = regexp.MustCompile(`^(?:[0-9一二三四五六七八九十百IVXLCDM]+|[0-9]+[.．、：:].*|[（(][0-9一二三四五六七八九十]+[)）])$`)

func (p *publication) finishEntry(d *document, blocks []textBlock, t tocEntry, src string, result *ParseResult, section *string) {
	title, header := entryTitle(d, blocks, t.title)
	if reason := filterReason(title, entryBody(blocks), blocks); reason != "" {
		result.Filtered = append(result.Filtered, FilteredItem{src, title, reason})
		return
	}
	if t.parent {
		*section = joinText(t.section, title, " · ")
	}
	entrySection := t.section
	if entrySection == "" {
		entrySection = *section
	}
	poem := domain.Poem{Title: StripNoteRefs(title), Section: entrySection, Stanzas: []domain.Stanza{}}
	if header >= 0 {
		applyEntryHeading(&poem, blocks[header].text)
	}
	content := p.extractEntryContent(&poem, blocks, header)
	poem.Kind = classify(title, content)
	if header < 0 && t.title == "" {
		poem.Kind = domain.EntryKindProse
	}
	content = extractColophon(&poem, content)
	buildEntryStanzas(&poem, content)
	retainUnreferencedNotes(&poem)
	if len(poem.Stanzas) == 0 {
		result.Filtered = append(result.Filtered, FilteredItem{src, title, emptyEntryReason(t, &poem, blocks)})
		return
	}
	result.Book.Poems = append(result.Book.Poems, poem)
}

func entryTitle(d *document, blocks []textBlock, title string) (string, int) {
	header := firstHeading(blocks)
	if header >= 0 && title == "" {
		title = headingTitle(blocks[header])
	}
	if title == "" {
		walk(d.root, func(n *html.Node) {
			if n.Data == "title" && title == "" {
				title = strings.TrimSpace(nodeText(n))
			}
		})
	}
	if title == "" || title == "未知" {
		title = "无题"
	}
	return title, header
}

func headingTitle(b textBlock) string {
	first, _, _ := strings.Cut(b.text, "\n")
	return StripNoteRefs(strings.TrimSpace(first))
}

func entryBody(blocks []textBlock) string {
	var body strings.Builder
	for _, b := range blocks {
		body.WriteString(b.text)
		body.WriteByte('\n')
	}
	return body.String()
}

func applyEntryHeading(poem *domain.Poem, text string) {
	parts := strings.Split(text, "\n")
	if len(parts) <= 1 {
		return
	}
	if first := strings.TrimSpace(StripNoteRefs(parts[0])); first != "" && strings.HasPrefix(poem.Title, first) {
		poem.Title = first
	}
	poem.Subtitle = strings.TrimSpace(StripNoteRefs(strings.Join(parts[1:], "\n")))
}

func (p *publication) extractEntryContent(poem *domain.Poem, blocks []textBlock, header int) []textBlock {
	var content []textBlock
	var epigraphCandidates []textBlock
	for i, b := range blocks {
		if p.extractEntryAncillary(poem, b, i == header) {
			continue
		}
		if !hasBody(content) && isEpigraph(b) {
			epigraphCandidates = append(epigraphCandidates, b)
			continue
		}
		content = append(content, b)
	}
	// Heuristic epigraphs (e.g. a leading <blockquote>) only count as epigraphs
	// when body text follows; otherwise the blockquote IS the poem itself.
	if hasBody(content) {
		for _, eb := range epigraphCandidates {
			poem.Epigraph = joinText(poem.Epigraph, eb.text, "\n")
		}
	} else if len(epigraphCandidates) > 0 {
		content = append(epigraphCandidates, content...)
	}
	return content
}

// extractEntryAncillary reports whether the block is excluded from the body.
func (p *publication) extractEntryAncillary(poem *domain.Poem, b textBlock, isHeader bool) bool {
	for _, ref := range b.refs {
		appendNote(poem, p.notes[ref.target], ref.label)
	}
	if b.note != nil {
		if !b.note.referenced {
			appendNote(poem, *b.note, "")
		}
		return true
	}
	poem.Images = append(poem.Images, b.images...)
	if isHeader {
		return true
	}
	if b.node != nil && b.node.Data == "figcaption" {
		return true
	}
	if semanticNode(b.node, "subtitle") {
		poem.Subtitle = joinText(poem.Subtitle, b.text, "\n")
		return true
	}
	if semanticNode(b.node, "author", "byline") {
		poem.Author = strings.TrimSpace(b.text)
		return true
	}
	if ancestorSemantic(b.node, "epigraph", "dedication") {
		poem.Epigraph = joinText(poem.Epigraph, b.text, "\n")
		return true
	}
	return false
}

func extractColophon(poem *domain.Poem, content []textBlock) []textBlock {
	// Trailing blank blocks never obscure a date or explicit colophon.
	for len(content) > 0 {
		b := content[len(content)-1]
		s := strings.TrimSpace(b.text)
		if s == "" {
			content = content[:len(content)-1]
			continue
		}
		if ancestorSemantic(b.node, "colophon", "dateline") || isColophon(s) {
			poem.Colophon = joinText(s, poem.Colophon, "\n")
			content = content[:len(content)-1]
			continue
		}
		break
	}
	return content
}

func buildEntryStanzas(poem *domain.Poem, content []textBlock) {
	var lines []string
	flush := func() {
		if len(lines) > 0 {
			poem.Stanzas = append(poem.Stanzas, domain.Stanza{Lines: lines})
			lines = nil
		}
	}
	for _, b := range content {
		if strings.TrimSpace(b.text) == "" {
			flush()
			continue
		}
		if poem.Kind == domain.EntryKindProse {
			flush()
			poem.Stanzas = append(poem.Stanzas, domain.Stanza{Lines: []string{strings.TrimSpace(b.text)}})
			continue
		}
		if marginTop(b.style) >= 2 || semanticNode(b.node, "stanza") {
			flush()
		}
		indent := min(12, max(0, int(math.Round(emLength(b.style["text-indent"])))))
		for _, s := range strings.Split(b.text, "\n") {
			if strings.TrimSpace(s) == "" {
				flush()
				continue
			}
			lines = append(lines, indentVerseLine(s, indent))
		}
	}
	flush()
}

func indentVerseLine(line string, indent int) string {
	if indent > 0 && !strings.HasPrefix(line, "　") && !strings.HasPrefix(line, "\u00a0") {
		return strings.Repeat("　", indent) + line
	}
	return line
}

func retainUnreferencedNotes(poem *domain.Poem) {
	// Unreferenced endnotes remain readable; only fully inlined note pages disappear.
	if len(poem.Stanzas) == 0 && len(poem.Notes) > 0 {
		poem.Kind = domain.EntryKindProse
		for _, note := range poem.Notes {
			poem.Stanzas = append(poem.Stanzas, domain.Stanza{Lines: []string{joinText(note.Label, note.Body, " ")}})
		}
		poem.Notes = nil
	}
}

func emptyEntryReason(t tocEntry, poem *domain.Poem, blocks []textBlock) string {
	if t.parent {
		return "section"
	}
	if len(poem.Images) > 0 {
		return "image-only"
	}
	for _, b := range blocks {
		if b.note != nil {
			return "notes"
		}
	}
	return "blank"
}

func isEpigraph(b textBlock) bool {
	s := strings.TrimSpace(b.text)
	if b.node != nil && (strings.HasPrefix(s, "（") || strings.HasPrefix(s, "(")) && hasEpigraphEmphasis(b.node) {
		return true
	}
	for x := b.node; x != nil; x = x.Parent {
		if x.Data == "blockquote" {
			return true
		}
	}
	if isCenteredDedication(b, s) || strings.HasPrefix(s, "题记：") || strings.HasPrefix(s, "题记:") {
		return true
	}
	return isKaitiEpigraph(b) && (strings.HasPrefix(s, "“") || strings.HasPrefix(s, "\""))
}

func hasEpigraphEmphasis(node *html.Node) bool {
	emphasized := false
	walk(node, func(n *html.Node) {
		if n.Data == "i" || n.Data == "em" || semantic(n, "kaiti") {
			emphasized = true
		}
	})
	return emphasized
}

func isCenteredDedication(b textBlock, text string) bool {
	size := emLength(b.style["font-size"])
	return b.style["text-align"] == "center" && size > 0 && size < 1 && (strings.HasPrefix(text, "赠") || strings.HasPrefix(text, "献给"))
}

func isKaitiEpigraph(b textBlock) bool {
	return b.node != nil && (b.style["font-family"] == `"kaiti"` || b.style["font-family"] == "kaiti" || semantic(b.node, "kaiti"))
}

func ancestorSemantic(n *html.Node, words ...string) bool {
	for x := n; x != nil && x.Data != "body"; x = x.Parent {
		if semantic(x, words...) {
			return true
		}
	}
	return false
}

func hasBody(blocks []textBlock) bool {
	for _, b := range blocks {
		if strings.TrimSpace(b.text) != "" {
			return true
		}
	}
	return false
}
