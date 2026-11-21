// Package parser converts EPUB publications into the shared SPBook reading model.
package parser

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"strings"

	"golang.org/x/net/html"

	"stillpoets/internal/domain"
)

// ParseResult includes original cover bytes; image conversion belongs to the importer.
type ParseResult struct {
	Book           *domain.SPBook
	Cover          []byte
	CoverMediaType string
	Filtered       []FilteredItem
}

// FilteredItem identifies a discarded document or fragment for import preview.
type FilteredItem struct{ Src, Title, Reason string }

type manifestItem struct {
	ID         string `xml:"id,attr"`
	Href       string `xml:"href,attr"`
	MediaType  string `xml:"media-type,attr"`
	Properties string `xml:"properties,attr"`
}
type metadataValue struct {
	ID   string `xml:"id,attr"`
	Role string `xml:"role,attr"`
	Text string `xml:",chardata"`
}
type packageDocument struct {
	Metadata struct {
		Titles       []string        `xml:"title"`
		Creators     []metadataValue `xml:"creator"`
		Contributors []metadataValue `xml:"contributor"`
		Meta         []struct {
			Name     string `xml:"name,attr"`
			Content  string `xml:"content,attr"`
			Property string `xml:"property,attr"`
			Refines  string `xml:"refines,attr"`
			Text     string `xml:",chardata"`
		} `xml:"meta"`
	} `xml:"metadata"`
	Manifest []manifestItem `xml:"manifest>item"`
	Spine    struct {
		TOC   string `xml:"toc,attr"`
		Items []struct {
			IDRef string `xml:"idref,attr"`
		} `xml:"itemref"`
	} `xml:"spine"`
	Guide []struct {
		Type string `xml:"type,attr"`
		Href string `xml:"href,attr"`
	} `xml:"guide>reference"`
}
type tocEntry struct {
	src, title, section string
	parent              bool
}
type document struct {
	src  string
	root *html.Node
	ids  map[string]*html.Node
	css  styleSheet
}
type publication struct {
	files map[string]*zip.File
	docs  map[string]*document
	toc   []tocEntry
	notes map[*html.Node]noteTarget
	refs  map[*html.Node]noteReference
}

// ParseEPUBFile parses a local EPUB without extracting its ZIP to disk.
func ParseEPUBFile(filename, bookID string) (*ParseResult, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("parser: open EPUB: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return ParseEPUBReader(f, info.Size(), bookID)
}

// ParseEPUBReader parses an EPUB in spine order. IDs derive from bookID and entry order.
func ParseEPUBReader(r io.ReaderAt, size int64, bookID string) (*ParseResult, error) {
	if strings.TrimSpace(bookID) == "" {
		return nil, fmt.Errorf("parser: bookID is required")
	}
	z, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("parser: read ZIP: %w", err)
	}
	pub := &publication{files: map[string]*zip.File{}, docs: map[string]*document{}, notes: map[*html.Node]noteTarget{}, refs: map[*html.Node]noteReference{}}
	for _, f := range z.File {
		pub.files[path.Clean(strings.ReplaceAll(f.Name, "\\", "/"))] = f
	}
	data, err := pub.read("META-INF/container.xml")
	if err != nil {
		return nil, err
	}
	var container struct {
		Roots []struct {
			Path string `xml:"full-path,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	if err = xml.Unmarshal(data, &container); err != nil {
		return nil, fmt.Errorf("parser: container.xml: %w", err)
	}
	if len(container.Roots) == 0 {
		return nil, fmt.Errorf("parser: container has no rootfile")
	}
	opfPath := container.Roots[0].Path
	data, err = pub.read(opfPath)
	if err != nil {
		return nil, err
	}
	var opf packageDocument
	if err = xml.Unmarshal(data, &opf); err != nil {
		return nil, fmt.Errorf("parser: OPF: %w", err)
	}
	result := &ParseResult{Book: &domain.SPBook{SchemaVersion: 1, BookID: bookID, SourceFormat: "epub", ParsedAt: domain.NowMillis(), Poems: []domain.Poem{}}}
	book := result.Book
	if len(opf.Metadata.Titles) > 0 {
		book.Title = strings.TrimSpace(opf.Metadata.Titles[0])
	}
	roles := map[string]string{}
	coverID := ""
	for _, m := range opf.Metadata.Meta {
		if m.Name == "cover" {
			coverID = m.Content
		}
		if m.Property == "role" {
			roles[strings.TrimPrefix(m.Refines, "#")] = strings.TrimSpace(m.Text)
		}
	}
	for _, v := range append(opf.Metadata.Creators, opf.Metadata.Contributors...) {
		role := v.Role
		if roles[v.ID] != "" {
			role = roles[v.ID]
		}
		name := strings.TrimSpace(v.Text)
		if role == "trl" {
			book.Translator = joinText(book.Translator, name, " / ")
		} else if role == "aut" || role == "" && containsCreator(opf.Metadata.Creators, v) {
			book.Author = joinText(book.Author, name, " / ")
		}
	}
	items := map[string]manifestItem{}
	var ncx, nav string
	for _, item := range opf.Manifest {
		item.Href, _ = resolve(opfPath, item.Href)
		items[item.ID] = item
		if (item.ID == coverID || hasToken(item.Properties, "cover-image")) && strings.HasPrefix(item.MediaType, "image/") && len(result.Cover) == 0 {
			if coverBytes, readErr := pub.read(item.Href); readErr == nil {
				result.Cover = coverBytes
				result.CoverMediaType = item.MediaType
			}
		}
		if item.ID == opf.Spine.TOC || item.MediaType == "application/x-dtbncx+xml" && ncx == "" {
			ncx = item.Href
		}
		if hasToken(item.Properties, "nav") {
			nav = item.Href
		}
	}
	// The manifest includes note documents that may be absent from the reading spine.
	var textBytes int
	for _, item := range opf.Manifest {
		if item.MediaType != "application/xhtml+xml" && item.MediaType != "text/html" {
			continue
		}
		src, _ := resolve(opfPath, item.Href)
		data, err = pub.read(src)
		if err != nil {
			return nil, err
		}
		textBytes += len(data)
		if textBytes > 128<<20 {
			return nil, fmt.Errorf("parser: EPUB text exceeds 128 MiB")
		}
		root, e := html.Parse(bytes.NewReader(data))
		if e != nil {
			return nil, fmt.Errorf("parser: %s: %w", src, e)
		}
		d := &document{src: src, root: root, ids: map[string]*html.Node{}, css: styleSheet{}}
		walk(root, func(n *html.Node) {
			if id := attr(n, "id"); id != "" {
				d.ids[id] = n
			}
			if id := attr(n, "name"); n.Data == "a" && id != "" {
				d.ids[id] = n
			}
		})
		pub.loadStyles(d)
		pub.docs[src] = d
	}
	if nav != "" {
		pub.toc = parseNAV(pub.docs[nav])
	}
	if len(pub.toc) == 0 && ncx != "" {
		data, err = pub.read(ncx)
		if err != nil {
			return nil, err
		}
		pub.toc, err = parseNCX(data, ncx)
		if err != nil {
			return nil, err
		}
	}
	guide := map[string]string{}
	for _, g := range opf.Guide {
		src, _ := resolve(opfPath, g.Href)
		guide[src] = g.Type
	}
	pub.indexNotes()
	section := ""
	for _, ref := range opf.Spine.Items {
		item, ok := items[ref.IDRef]
		if !ok {
			return nil, fmt.Errorf("parser: spine item %q missing from manifest", ref.IDRef)
		}
		d := pub.docs[item.Href]
		if d == nil {
			// EPUB3 allows non-HTML spine items (such as SVG cover/illustration pages).
			result.Filtered = append(result.Filtered, FilteredItem{Src: item.Href, Reason: "non-html"})
			continue
		}
		reason := ""
		switch guide[d.src] {
		case "cover", "other.titlepage", "title-page":
			reason = "cover"
		case "toc":
			reason = "toc"
		case "copyright-page":
			reason = "copyright"
		}
		if d.src == nav {
			reason = "toc"
		}
		if reason != "" {
			result.Filtered = append(result.Filtered, FilteredItem{Src: d.src, Reason: reason})
			continue
		}
		pub.parseDocument(d, result, &section)
	}
	for i := range book.Poems {
		p := &book.Poems[i]
		p.BookID = bookID
		p.OrderIndex = i
		p.ID = fmt.Sprintf("%s:%04d", bookID, i+1)
	}
	return result, nil
}

func containsCreator(vs []metadataValue, v metadataValue) bool {
	for _, x := range vs {
		if x == v {
			return true
		}
	}
	return false
}
func (p *publication) read(src string) ([]byte, error) {
	f := p.files[src]
	if f == nil {
		return nil, fmt.Errorf("parser: missing EPUB resource %q", src)
	}
	const limit = 32 << 20
	if f.UncompressedSize64 > limit {
		return nil, fmt.Errorf("parser: resource %s exceeds 32 MiB", src)
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("parser: read %s: %w", src, err)
	}
	if len(b) > limit {
		return nil, fmt.Errorf("parser: resource %s exceeds 32 MiB", src)
	}
	return b, nil
}

// resolve handles URL escaping and fragments without allowing external resources.
func resolve(base, href string) (string, string) {
	u, err := url.Parse(href)
	if err != nil || u.IsAbs() || u.Host != "" {
		return "", ""
	}
	src := base
	if u.Path != "" {
		src = path.Clean(path.Join(path.Dir(base), u.Path))
	}
	return src, u.Fragment
}
func hasToken(s, t string) bool {
	for _, v := range strings.Fields(s) {
		if v == t {
			return true
		}
	}
	return false
}
func joinText(a, b, sep string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + sep + b
}

type navPoint struct {
	Label   string `xml:"navLabel>text"`
	Content struct {
		Src string `xml:"src,attr"`
	} `xml:"content"`
	Children []navPoint `xml:"navPoint"`
}

func parseNCX(data []byte, src string) ([]tocEntry, error) {
	var ncx struct {
		Points []navPoint `xml:"navMap>navPoint"`
	}
	if err := xml.Unmarshal(data, &ncx); err != nil {
		return nil, fmt.Errorf("parser: NCX: %w", err)
	}
	var entries []tocEntry
	var visit func([]navPoint, string)
	visit = func(points []navPoint, section string) {
		for _, n := range points {
			file, id := resolve(src, n.Content.Src)
			title := strings.TrimSpace(n.Label)
			entries = append(entries, tocEntry{file + fragment(id), title, section, len(n.Children) > 0})
			next := section
			if len(n.Children) > 0 {
				next = joinText(section, title, " · ")
			}
			visit(n.Children, next)
		}
	}
	visit(ncx.Points, "")
	return entries, nil
}
func parseNAV(d *document) []tocEntry {
	if d == nil {
		return nil
	}
	var entries []tocEntry
	walk(d.root, func(n *html.Node) {
		if n.Data != "nav" || !hasToken(attr(n, "type"), "toc") && attr(n, "role") != "doc-toc" {
			return
		}
		var visit func(*html.Node, string)
		visit = func(node *html.Node, section string) {
			for c := node.FirstChild; c != nil; c = c.NextSibling {
				if c.Data != "li" {
					visit(c, section)
					continue
				}
				var link, list *html.Node
				for x := c.FirstChild; x != nil; x = x.NextSibling {
					if x.Data == "a" || x.Data == "span" {
						link = x
					}
					if x.Data == "ol" {
						list = x
					}
				}
				next := section
				if link != nil {
					src, id := resolve(d.src, attr(link, "href"))
					title := strings.TrimSpace(nodeText(link))
					if attr(link, "href") != "" {
						entries = append(entries, tocEntry{src + fragment(id), title, section, list != nil})
					}
					if list != nil {
						next = joinText(section, title, " · ")
					}
				}
				if list != nil {
					visit(list, next)
				}
			}
		}
		visit(n, "")
	})
	return entries
}
func fragment(id string) string {
	if id == "" {
		return ""
	}
	return "#" + id
}
