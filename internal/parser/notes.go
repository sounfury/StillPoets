package parser

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/html"

	"stillpoets/internal/domain"
)

type noteTarget struct {
	id, label, body string
	referenced      bool
}
type noteReference struct {
	target *html.Node
	label  string
}

var noteMarker = regexp.MustCompile(`^(?:\[[0-9]+\]|［[0-9]+］|【[0-9]+】|[①-⑳㉑-㉟㊱-㊿]|[0-9]+)$`)
var leadingNoteMarker = regexp.MustCompile(`^\s*(\[[0-9]+\]|［[0-9]+］|【[0-9]+】|[①-⑳㉑-㉟㊱-㊿])\s*`)

func noteLabel(s string) string {
	s = strings.TrimSpace(s)
	if regexpDigits.MatchString(s) {
		return "[" + s + "]"
	}
	return s
}

var regexpDigits = regexp.MustCompile(`^[0-9]+$`)

func noteElement(n *html.Node) bool {
	return semantic(n, "footnote", "endnote", "note", "fnote") || n.Data == "p" && textNotePrefix.MatchString(strings.TrimSpace(nodeText(n)))
}

var textNotePrefix = regexp.MustCompile(`^(?:注|附注|作者注|译者注)[：:]`)

func (p *publication) withinNote(n *html.Node) bool {
	for x := n; x != nil; x = x.Parent {
		if _, ok := p.notes[x]; ok {
			return true
		}
	}
	return false
}
func (p *publication) noteContainer(n *html.Node) *html.Node {
	for x := n; x != nil; x = x.Parent {
		if _, ok := p.notes[x]; ok {
			return x
		}
	}
	for x := n; x != nil; x = x.Parent {
		if isBlock(x) {
			return x
		}
	}
	return nil
}
func (p *publication) addNote(d *document, n *html.Node, id string) {
	if n == nil {
		return
	}
	if _, ok := p.notes[n]; ok {
		return
	}
	label := ""
	body := strings.TrimSpace(noteText(n))
	if m := leadingNoteMarker.FindStringSubmatch(body); m != nil {
		label = noteLabel(m[1])
		body = strings.TrimSpace(body[len(m[0]):])
	}
	if m := textNotePrefix.FindString(body); m != "" {
		label = strings.TrimRight(m, "：:")
		body = strings.TrimSpace(strings.TrimPrefix(body, m))
	}
	if id == "" {
		id = attr(n, "id")
	}
	if id == "" {
		id = fmt.Sprintf("note-%d", len(p.notes)+1)
	}
	if body != "" {
		p.notes[n] = noteTarget{id: d.src + "#" + id, label: label, body: body}
	}
}
func (p *publication) indexNotes() {
	// ZIP/manifest order, rather than map iteration, keeps anonymous note IDs stable.
	var docs []*document
	var names []string
	for name := range p.docs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		docs = append(docs, p.docs[name])
	}
	for _, d := range docs {
		walk(d.root, func(n *html.Node) { p.indexNoteElement(d, n) })
	}
	for _, d := range docs {
		walk(d.root, func(n *html.Node) { p.indexNoteReference(d, n) })
	}
	// A backlink may have looked like a reference before its enclosing note was found.
	for n := range p.refs {
		if p.withinNote(n) {
			delete(p.refs, n)
		} else {
			t := p.notes[p.refs[n].target]
			t.referenced = true
			p.notes[p.refs[n].target] = t
		}
	}
}
func (p *publication) indexNoteElement(d *document, n *html.Node) {
	if !noteElement(n) || p.withinNote(n) {
		return
	}
	// A footnote container (e.g. <ol id="fn-list"> or <div class="footnotes">)
	// with child list items or multiple paragraphs holds separate notes.
	var children []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Data == "p" || c.Data == "li" {
			children = append(children, c)
		}
	}
	splitList := (n.Data == "ol" || n.Data == "ul") && len(children) > 0
	splitMulti := len(children) > 1 && (attr(n, "id") == "" || hasChildID(children))
	if splitList || splitMulti || len(children) == 1 && attr(n, "id") == "" {
		for _, c := range children {
			p.addNote(d, c, firstID(c))
		}
	} else {
		p.addNote(d, n, firstID(n))
	}
}

func (p *publication) indexNoteReference(d *document, n *html.Node) {
	if n.Data != "a" || p.withinNote(n) {
		return
	}
	label := strings.TrimSpace(nodeText(n))
	if !semantic(n, "noteref") && !noteMarker.MatchString(label) {
		return
	}
	src, id := resolve(d.src, attr(n, "href"))
	targetDoc := p.docs[src]
	if targetDoc == nil || id == "" {
		return
	}
	target := targetDoc.ids[id]
	if target == nil {
		return
	}
	container := p.noteContainer(target)
	if container == nil || heading(container) > 0 {
		return
	}
	// A numeric link is a note only when its destination contains explanatory text.
	if len([]rune(strings.TrimSpace(noteText(container)))) <= len([]rune(label)) {
		return
	}
	p.addNote(targetDoc, container, id)
	if _, ok := p.notes[container]; ok {
		p.refs[n] = noteReference{container, noteLabel(label)}
	}
}

func hasChildID(nodes []*html.Node) bool {
	for _, n := range nodes {
		if firstID(n) != "" {
			return true
		}
	}
	return false
}
func firstID(n *html.Node) string {
	id := attr(n, "id")
	if id != "" {
		return id
	}
	walk(n, func(x *html.Node) {
		if id == "" {
			id = attr(x, "id")
		}
	})
	return id
}
func noteText(n *html.Node) string {
	var b strings.Builder
	var visit func(*html.Node)
	visit = func(x *html.Node) {
		if x.Data == "a" && (semantic(x, "backlink") || attr(x, "role") == "doc-backlink") {
			return
		}
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
		if x.Data == "br" {
			b.WriteByte('\n')
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
		if x != n && isBlock(x) {
			b.WriteByte('\n')
		}
	}
	visit(n)
	return b.String()
}
func appendNote(poem *domain.Poem, t noteTarget, label string) {
	for _, n := range poem.Notes {
		if n.ID == t.id {
			return
		}
	}
	if label == "" {
		label = t.label
	}
	poem.Notes = append(poem.Notes, domain.Note{ID: t.id, Label: label, Body: t.body})
}
