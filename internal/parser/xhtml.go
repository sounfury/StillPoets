package parser

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
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

type documentBlocks struct {
	blocks    []textBlock
	positions map[*html.Node]int
}

func (p *publication) blocks(d *document) ([]textBlock, map[*html.Node]int) {
	out := documentBlocks{positions: map[*html.Node]int{}}
	var body *html.Node
	walk(d.root, func(n *html.Node) {
		if n.Data == "body" {
			body = n
		}
	})
	if body != nil {
		p.collectBlocks(d, body, &out)
	}
	return out.blocks, out.positions
}

func (p *publication) collectBlocks(d *document, n *html.Node, out *documentBlocks) {
	out.positions[n] = len(out.blocks)
	switch n.Data {
	case "head", "script", "style":
		return
	}
	if t, ok := p.notes[n]; ok {
		out.blocks = append(out.blocks, textBlock{node: n, note: &t})
		return
	}
	style := d.style(n)
	if attr(n, "hidden") != "" || style["display"] == "none" || style["visibility"] == "hidden" {
		if title := attr(n, "title"); heading(n) > 0 && title != "" {
			out.blocks = append(out.blocks, textBlock{node: n, text: title, style: style})
		}
		return
	}
	if isDownloadBanner(n) {
		return
	}
	if isRenderableBlock(n) {
		b := textBlock{node: n, style: style}
		p.render(d, n, &b)
		walk(n, func(x *html.Node) { out.positions[x] = len(out.blocks) })
		out.blocks = append(out.blocks, b)
		return
	}
	p.collectBlockChildren(d, n, style, out)
}

func (p *publication) collectBlockChildren(d *document, n *html.Node, style map[string]string, out *documentBlocks) {
	stanza := semantic(n, "stanza", "verse-group") || (n.Data == "div" || n.Data == "section") && marginTop(style) >= 2
	if stanza {
		out.blocks = append(out.blocks, textBlock{})
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		p.collectBlocks(d, c, out)
	}
	if stanza {
		out.blocks = append(out.blocks, textBlock{})
	}
}

// Repeated download banners are outside publication content.
func isDownloadBanner(n *html.Node) bool {
	if n.Data != "div" {
		return false
	}
	text := nodeText(n)
	if len([]rune(text)) >= 500 || !strings.Contains(text, "下载") {
		return false
	}
	links := 0
	walk(n, func(x *html.Node) {
		if x.Data == "a" && strings.HasPrefix(attr(x, "href"), "http") {
			links++
		}
	})
	return links >= 2
}

func isRenderableBlock(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if isBlock(c) || c.Data == "figure" {
			return false
		}
	}
	return isBlock(n) || n.Data == "img" || n.Data == "image" || n.Type == html.TextNode && strings.TrimSpace(n.Data) != ""
}

type blockRenderer struct {
	document *document
	block    *textBlock
	root     *html.Node
	preserve bool
	text     strings.Builder
}

func (p *publication) render(d *document, n *html.Node, b *textBlock) {
	r := blockRenderer{
		document: d, block: b, root: n,
		preserve: n.Data == "pre" || b.style["white-space"] == "pre" || b.style["white-space"] == "pre-wrap",
	}
	p.renderBlockNode(&r, n)
	if r.preserve {
		b.text = strings.Trim(strings.ReplaceAll(r.text.String(), "\r", ""), "\n")
	} else {
		b.text = normalizeText(r.text.String())
	}
}

func (p *publication) renderBlockNode(r *blockRenderer, n *html.Node) {
	if n != r.root && p.skipInlineNode(r.document, n) {
		return
	}
	if ref, ok := p.refs[n]; ok {
		r.block.refs = append(r.block.refs, ref)
		r.text.WriteString(ref.label)
		return
	}
	switch n.Data {
	case "img", "image":
		glyph, img := p.parseImage(r.document, n)
		r.text.WriteString(glyph)
		if img != nil {
			r.block.images = append(r.block.images, *img)
		}
		return
	}
	if n.Type == html.TextNode {
		text := n.Data
		if !r.preserve {
			text = htmlWhitespace.ReplaceAllString(text, " ")
		}
		r.text.WriteString(text)
	}
	if n.Data == "br" {
		r.text.WriteByte('\n')
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		p.renderBlockNode(r, c)
	}
}

func (p *publication) skipInlineNode(d *document, n *html.Node) bool {
	_, isNote := p.notes[n]
	return isNote || attr(n, "hidden") != "" || d.style(n)["display"] == "none"
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
	if isDecorativeImage(n, alt) {
		return "", nil
	}
	shortAlt := alt != "" && len([]rune(alt)) <= 4
	width, height := p.imageSize(d, n, src, shortAlt)
	if shortAlt && (fitsImageBox(width, height, 48) || semantic(n, "gaiji", "glyph")) {
		return alt, nil
	}
	if fitsImageBox(width, height, 32) || width > 0 && height > 0 && float64(height)/float64(width) < 0.08 {
		return "", nil
	}
	return "", &domain.ImageRef{Src: src, Alt: alt, Width: width, Height: height, Caption: imageCaption(n)}
}

func isDecorativeImage(n *html.Node, alt string) bool {
	label := strings.ToLower(alt + " " + attr(n, "class") + " " + attr(n, "role"))
	for _, word := range []string{"logo", "qrcode", "qr code", "separator", "ornament", "decoration", "presentation", "装饰", "分隔"} {
		if strings.Contains(label, word) {
			return true
		}
	}
	return false
}

func fitsImageBox(width, height, maxSize int) bool {
	return width > 0 && width <= maxSize && height > 0 && height <= maxSize
}

func (p *publication) imageSize(d *document, n *html.Node, src string, shortAlt bool) (int, int) {
	style := d.style(n)
	width := imageDimensionWithStyle(n, style, "width")
	height := imageDimensionWithStyle(n, style, "height")
	if !shortAlt || width != 0 && height != 0 {
		return width, height
	}
	data, err := p.read(src)
	if err != nil {
		return width, height
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		return cfg.Width, cfg.Height
	}
	return width, height
}

func imageDimensionWithStyle(n *html.Node, style map[string]string, axis string) int {
	if size := imageDimension(attr(n, axis)); size != 0 {
		return size
	}
	return imageDimension(style[axis])
}

func imageCaption(n *html.Node) string {
	var caption string
	for parent := n.Parent; parent != nil; parent = parent.Parent {
		if parent.Data != "figure" {
			continue
		}
		walk(parent, func(x *html.Node) {
			if x.Data == "figcaption" {
				caption = strings.TrimSpace(nodeText(x))
			}
		})
		return caption
	}
	return caption
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
	if reason := documentFilterReason(d); reason != "" {
		result.Filtered = append(result.Filtered, FilteredItem{Src: d.src, Reason: reason})
		return
	}
	blocks, positions := p.blocks(d)
	boundaries, fileEntry := p.tocBoundaries(d, positions)
	if len(boundaries) == 0 {
		// Without TOC anchors, only the document's top heading level introduces entries.
		minHeading := topHeadingLevel(blocks)
		applyTopHeadingBoundaries(blocks, minHeading, fileEntry, boundaries)
		if fileEntry.src == "" || fileEntry.parent {
			applySubHeadingBoundaries(blocks, minHeading, fileEntry, boundaries)
		}
	}
	p.emitDocumentEntries(d, result, section, blocks, boundaries, fileEntry)
}

func documentFilterReason(d *document) string {
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
	return semanticReason
}

func (p *publication) tocBoundaries(d *document, positions map[*html.Node]int) (map[int]tocEntry, tocEntry) {
	boundaries := map[int]tocEntry{}
	fileEntry := tocEntry{}
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
			}
		}
	}
	return boundaries, fileEntry
}

func topHeadingLevel(blocks []textBlock) int {
	minHeading := 7
	for _, b := range blocks {
		if b.node != nil {
			if h := heading(b.node); h > 0 && h < minHeading {
				minHeading = h
			}
		}
	}
	return minHeading
}

func (p *publication) emitDocumentEntries(d *document, result *ParseResult, section *string, blocks []textBlock, boundaries map[int]tocEntry, fileEntry tocEntry) {
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
		title := headingTitle(b)
		t := tocEntry{title: title, section: fileEntry.section}
		if i == first {
			t = fileEntry
			if t.title == "" {
				t.title = title
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
				boundaries[j] = tocEntry{title: headingTitle(next), section: joinText(fileEntry.section, parent.title, " · ")}
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

func semanticNode(n *html.Node, words ...string) bool { return n != nil && semantic(n, words...) }
