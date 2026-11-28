package parser

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/html"

	"stillpoets/internal/domain"
)

type textBlock struct {
	node   *html.Node
	text   string
	style  map[string]string
	refs   []noteReference
	images []domain.ImageRef
	note   *noteTarget
}

func (p *publication) blocks(d *document) ([]textBlock, map[*html.Node]int) {
	var blocks []textBlock
	positions := map[*html.Node]int{}
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		positions[n] = len(blocks)
		if n.Data == "head" || n.Data == "script" || n.Data == "style" {
			return
		}
		if t, ok := p.notes[n]; ok {
			blocks = append(blocks, textBlock{node: n, note: &t})
			return
		}
		style := d.style(n)
		if attr(n, "hidden") != "" || style["display"] == "none" || style["visibility"] == "hidden" {
			if heading(n) > 0 && attr(n, "title") != "" {
				blocks = append(blocks, textBlock{node: n, text: attr(n, "title"), style: style})
			}
			return
		}
		// Repeated download banners are outside publication content.
		if n.Data == "div" && len([]rune(nodeText(n))) < 500 && strings.Contains(nodeText(n), "下载") {
			links := 0
			walk(n, func(x *html.Node) {
				if x.Data == "a" && strings.HasPrefix(attr(x, "href"), "http") {
					links++
				}
			})
			if links >= 2 {
				return
			}
		}
		hasBlocks := false
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if isBlock(c) || c.Data == "figure" {
				hasBlocks = true
				break
			}
		}
		if !hasBlocks && (isBlock(n) || n.Data == "img" || n.Data == "image" || n.Type == html.TextNode && strings.TrimSpace(n.Data) != "") {
			b := textBlock{node: n, style: style}
			p.render(d, n, &b)
			walk(n, func(x *html.Node) { positions[x] = len(blocks) })
			blocks = append(blocks, b)
			return
		}
		stanza := semantic(n, "stanza", "verse-group") || (n.Data == "div" || n.Data == "section") && marginTop(style) >= 2
		if stanza {
			blocks = append(blocks, textBlock{})
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
		if stanza {
			blocks = append(blocks, textBlock{})
		}
	}
	var body *html.Node
	walk(d.root, func(n *html.Node) {
		if n.Data == "body" {
			body = n
		}
	})
	if body != nil {
		visit(body)
	}
	return blocks, positions
}
func (p *publication) render(d *document, n *html.Node, b *textBlock) {
	var out strings.Builder
	preserve := n.Data == "pre" || b.style["white-space"] == "pre" || b.style["white-space"] == "pre-wrap"
	var visit func(*html.Node)
	visit = func(x *html.Node) {
		if x != n {
			if _, ok := p.notes[x]; ok {
				return
			}
			if attr(x, "hidden") != "" || d.style(x)["display"] == "none" {
				return
			}
		}
		if r, ok := p.refs[x]; ok {
			b.refs = append(b.refs, r)
			out.WriteString(r.label)
			return
		}
		if x.Data == "img" || x.Data == "image" {
			glyph, img := p.parseImage(d, x)
			out.WriteString(glyph)
			if img != nil {
				b.images = append(b.images, *img)
			}
			return
		}
		if x.Type == html.TextNode {
			text := x.Data
			if !preserve {
				text = htmlWhitespace.ReplaceAllString(text, " ")
			}
			out.WriteString(text)
		}
		if x.Data == "br" {
			out.WriteByte('\n')
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(n)
	if preserve {
		b.text = strings.Trim(strings.ReplaceAll(out.String(), "\r", ""), "\n")
	} else {
		b.text = normalizeText(out.String())
	}
}

var htmlWhitespace = regexp.MustCompile(`[ \t\r\n]+`)

func normalizeText(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r", ""), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		// ASCII source indentation is formatting; NBSP/full-width spaces carry verse indentation.
		line = strings.Trim(line, " \t")
		line = strings.TrimRightFunc(line, unicode.IsSpace)
		if strings.TrimSpace(line) == "" {
			out = append(out, "")
			continue
		}
		var b strings.Builder
		space := false
		for _, r := range line {
			if r == ' ' || r == '\t' {
				if !space {
					b.WriteRune(' ')
				}
				space = true
			} else {
				b.WriteRune(r)
				space = false
			}
		}
		out = append(out, b.String())
	}
	return strings.Trim(strings.Join(out, "\n"), "\n")
}
func (p *publication) parseImage(d *document, n *html.Node) (string, *domain.ImageRef) {
	href := attr(n, "src")
	if href == "" {
		href = attr(n, "href")
	}
	src, _ := resolve(d.src, href)
	if src == "" || href == "" {
		return "", nil
	}
	alt := strings.TrimSpace(attr(n, "alt"))
	label := strings.ToLower(alt + " " + attr(n, "class") + " " + attr(n, "role"))
	for _, word := range []string{"logo", "qrcode", "qr code", "separator", "ornament", "decoration", "presentation", "装饰", "分隔"} {
		if strings.Contains(label, word) {
			return "", nil
		}
	}
	style := d.style(n)
	width := imageDimension(attr(n, "width"))
	height := imageDimension(attr(n, "height"))
	if width == 0 {
		width = imageDimension(style["width"])
	}
	if height == 0 {
		height = imageDimension(style["height"])
	}
	if len([]rune(alt)) > 0 && len([]rune(alt)) <= 4 && (width == 0 || height == 0) {
		if data, err := p.read(src); err == nil {
			if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
				width = cfg.Width
				height = cfg.Height
			}
		}
	}
	if alt != "" && len([]rune(alt)) <= 4 && ((width > 0 && width <= 48 && height > 0 && height <= 48) || semantic(n, "gaiji", "glyph")) {
		return alt, nil
	}
	if width > 0 && height > 0 && (width <= 32 && height <= 32 || float64(height)/float64(width) < 0.08) {
		return "", nil
	}
	img := &domain.ImageRef{Src: src, Alt: alt, Width: width, Height: height}
	for parent := n.Parent; parent != nil; parent = parent.Parent {
		if parent.Data == "figure" {
			walk(parent, func(x *html.Node) {
				if x.Data == "figcaption" {
					img.Caption = strings.TrimSpace(nodeText(x))
				}
			})
			break
		}
	}
	return "", img
}
func imageDimension(v string) int {
	v = strings.TrimSuffix(v, "px")
	if strings.HasSuffix(v, "em") {
		return int(emLength(v) * 16)
	}
	n, _ := strconv.Atoi(v)
	return n
}

func (p *publication) parseDocument(d *document, result *ParseResult, section *string) {
	semanticReason := ""
	walk(d.root, func(n *html.Node) {
		if n.Data == "body" {
			switch {
			case semantic(n, "cover", "titlepage"):
				semanticReason = "cover"
			case semantic(n, "copyright-page"):
				semanticReason = "copyright"
			case semantic(n, "toc"):
				semanticReason = "toc"
			}
		}
	})
	if semanticReason != "" {
		result.Filtered = append(result.Filtered, FilteredItem{Src: d.src, Reason: semanticReason})
		return
	}
	blocks, positions := p.blocks(d)
	boundaries := map[int]tocEntry{}
	fileEntry := tocEntry{}
	anchored := false
	for _, t := range p.toc {
		src, id := resolve("", t.src)
		if src != d.src {
			continue
		}
		if id == "" {
			if fileEntry.src == "" {
				fileEntry = t
			}
			continue
		}
		if n := d.ids[id]; n != nil {
			if i, ok := positions[n]; ok {
				boundaries[i] = t
				anchored = true
			}
		}
	}
	minHeading := 7
	for _, b := range blocks {
		if b.node != nil {
			if h := heading(b.node); h > 0 && h < minHeading {
				minHeading = h
			}
		}
	}
	// Without TOC anchors, only the document's top heading level introduces entries.
	if !anchored {
		applyTopHeadingBoundaries(blocks, minHeading, fileEntry, boundaries)
	}
	if !anchored && (fileEntry.src == "" || fileEntry.parent) {
		applySubHeadingBoundaries(blocks, minHeading, fileEntry, boundaries)
	}
	var current []textBlock
	currentTOC := fileEntry
	start := 0
	flush := func(end int) {
		if len(current) == 0 {
			return
		}
		src := d.src
		if start > 0 && start < len(blocks) && blocks[start].node != nil {
			src += fragment(attr(blocks[start].node, "id"))
		}
		p.finishEntry(d, current, currentTOC, src, result, section)
		current = nil
		start = end
	}
	for i, b := range blocks {
		if t, ok := boundaries[i]; ok {
			flush(i)
			currentTOC = t
			start = i
		}
		current = append(current, b)
	}
	flush(len(blocks))
}
func applyTopHeadingBoundaries(blocks []textBlock, minHeading int, fileEntry tocEntry, boundaries map[int]tocEntry) {
	first := firstHeading(blocks)
	for i, b := range blocks {
		if b.node == nil || heading(b.node) != minHeading || numberedPart.MatchString(strings.TrimSpace(b.text)) {
			continue
		}
		headingTitle := StripNoteRefs(strings.TrimSpace(strings.Split(b.text, "\n")[0]))
		t := tocEntry{title: headingTitle, section: fileEntry.section}
		if i == first {
			t = fileEntry
			if t.title == "" {
				t.title = headingTitle
			}
		}
		boundaries[i] = t
	}
}

func applySubHeadingBoundaries(blocks []textBlock, minHeading int, fileEntry tocEntry, boundaries map[int]tocEntry) {
	for i, b := range blocks {
		if b.node == nil || heading(b.node) != minHeading {
			continue
		}
		childLevel := nextSubHeadingLevel(blocks[i+1:], minHeading)
		if childLevel == 0 {
			continue
		}
		parent := boundaries[i]
		parent.parent = true
		boundaries[i] = parent
		for j := i + 1; j < len(blocks); j++ {
			next := blocks[j]
			if next.node == nil {
				continue
			}
			level := heading(next.node)
			if level > 0 && level <= minHeading {
				break
			}
			if level == childLevel && !numberedPart.MatchString(strings.TrimSpace(next.text)) {
				boundaries[j] = tocEntry{title: StripNoteRefs(strings.TrimSpace(strings.Split(next.text, "\n")[0])), section: joinText(fileEntry.section, parent.title, " · ")}
			}
		}
	}
}

func nextSubHeadingLevel(rest []textBlock, minHeading int) int {
	for _, next := range rest {
		if strings.TrimSpace(next.text) == "" {
			continue
		}
		if next.node != nil && heading(next.node) > minHeading && !numberedPart.MatchString(strings.TrimSpace(next.text)) {
			return heading(next.node)
		}
		break
	}
	return 0
}

func firstHeading(bs []textBlock) int {
	for i, b := range bs {
		if b.node != nil && heading(b.node) > 0 {
			return i
		}
	}
	return -1
}

var numberedPart = regexp.MustCompile(`^(?:[0-9一二三四五六七八九十百IVXLCDM]+|[0-9]+[.．、：:].*|[（(][0-9一二三四五六七八九十]+[)）])$`)

func (p *publication) finishEntry(d *document, blocks []textBlock, t tocEntry, src string, result *ParseResult, section *string) {
	title := t.title
	header := -1
	for i, b := range blocks {
		if b.node != nil && heading(b.node) > 0 {
			header = i
			if title == "" {
				title = StripNoteRefs(strings.TrimSpace(strings.Split(b.text, "\n")[0]))
			}
			break
		}
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
	var body strings.Builder
	for _, b := range blocks {
		body.WriteString(b.text)
		body.WriteByte('\n')
	}
	if reason := filterReason(title, body.String(), blocks); reason != "" {
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
		parts := strings.Split(blocks[header].text, "\n")
		if len(parts) > 1 {
			if first := strings.TrimSpace(StripNoteRefs(parts[0])); first != "" && strings.HasPrefix(poem.Title, first) {
				poem.Title = first
			}
			poem.Subtitle = strings.TrimSpace(StripNoteRefs(strings.Join(parts[1:], "\n")))
		}
	}
	var content []textBlock
	var epigraphCandidates []textBlock
	for i, b := range blocks {
		for _, ref := range b.refs {
			appendNote(&poem, p.notes[ref.target], ref.label)
		}
		if b.note != nil {
			if !b.note.referenced {
				appendNote(&poem, *b.note, "")
			}
			continue
		}
		poem.Images = append(poem.Images, b.images...)
		if i == header {
			continue
		}
		if b.node != nil && b.node.Data == "figcaption" {
			continue
		}
		if semanticNode(b.node, "subtitle") {
			poem.Subtitle = joinText(poem.Subtitle, b.text, "\n")
			continue
		}
		if semanticNode(b.node, "author", "byline") {
			poem.Author = strings.TrimSpace(b.text)
			continue
		}
		if ancestorSemantic(b.node, "epigraph", "dedication") {
			poem.Epigraph = joinText(poem.Epigraph, b.text, "\n")
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
	poem.Kind = classify(title, content)
	if header < 0 && t.title == "" {
		poem.Kind = domain.EntryKindProse
	}
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
		indent := int(math.Round(emLength(b.style["text-indent"])))
		if indent > 12 {
			indent = 12
		}
		if indent < 0 {
			indent = 0
		}
		for _, s := range strings.Split(b.text, "\n") {
			if strings.TrimSpace(s) == "" {
				flush()
				continue
			}
			if indent > 0 && !strings.HasPrefix(s, "　") && !strings.HasPrefix(s, "\u00a0") {
				s = strings.Repeat("　", indent) + s
			}
			lines = append(lines, s)
		}
	}
	flush()
	// Unreferenced endnotes remain readable; only fully inlined note pages disappear.
	if len(poem.Stanzas) == 0 && len(poem.Notes) > 0 {
		poem.Kind = domain.EntryKindProse
		for _, note := range poem.Notes {
			poem.Stanzas = append(poem.Stanzas, domain.Stanza{Lines: []string{joinText(note.Label, note.Body, " ")}})
		}
		poem.Notes = nil
	}
	// Section dividers and image-only pages without readable text must not emit
	// blank entries into the V1.0 reading TOC.
	if len(poem.Stanzas) == 0 {
		reason := "blank"
		for _, b := range blocks {
			if b.note != nil {
				reason = "notes"
			}
		}
		if t.parent {
			reason = "section"
		} else if len(poem.Images) > 0 {
			reason = "image-only"
		}
		result.Filtered = append(result.Filtered, FilteredItem{src, title, reason})
		return
	}
	result.Book.Poems = append(result.Book.Poems, poem)
}
func semanticNode(n *html.Node, words ...string) bool { return n != nil && semantic(n, words...) }
func isEpigraph(b textBlock) bool {
	s := strings.TrimSpace(b.text)
	if b.node != nil && (strings.HasPrefix(s, "（") || strings.HasPrefix(s, "(")) {
		emphasized := false
		walk(b.node, func(n *html.Node) {
			if n.Data == "i" || n.Data == "em" || semantic(n, "kaiti") {
				emphasized = true
			}
		})
		if emphasized {
			return true
		}
	}
	for x := b.node; x != nil; x = x.Parent {
		if x.Data == "blockquote" {
			return true
		}
	}
	if b.style["text-align"] == "center" && emLength(b.style["font-size"]) > 0 && emLength(b.style["font-size"]) < 1 && (strings.HasPrefix(s, "赠") || strings.HasPrefix(s, "献给")) {
		return true
	}
	if strings.HasPrefix(s, "题记：") || strings.HasPrefix(s, "题记:") {
		return true
	}
	return b.node != nil && (b.node.Data == "blockquote" || b.style["font-family"] == `"kaiti"` || b.style["font-family"] == "kaiti" || semanticNode(b.node, "kaiti")) && (strings.HasPrefix(s, "“") || strings.HasPrefix(s, "\""))
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
