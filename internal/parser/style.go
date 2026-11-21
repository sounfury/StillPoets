package parser

import (
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// Only simple publisher selectors are needed for spacing, indentation and hidden text.
// Complex selectors and @-rule blocks (@media/@page/@font-face) are ignored.
type styleRule struct {
	tag     string
	id      string
	classes []string
	score   int
	values  map[string]string
}
type styleSheet []styleRule

var cssComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
var simpleSelector = regexp.MustCompile(`^(?:[a-zA-Z][\w-]*)?(?:[.#][\w-]+)*$`)
var cssToken = regexp.MustCompile(`[.#][\w-]+`)
var lengthValue = regexp.MustCompile(`^(-?[0-9]+(?:\.[0-9]+)?)(em|rem|px|pt)?$`)

func declarations(s string) map[string]string {
	m := map[string]string{}
	for _, v := range strings.Split(s, ";") {
		k, x, ok := strings.Cut(v, ":")
		if !ok {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(k))
		if key == "" {
			continue
		}
		val := strings.TrimSpace(x)
		if len(val) >= len("!important") && strings.EqualFold(val[len(val)-len("!important"):], "!important") {
			val = strings.TrimSpace(val[:len(val)-len("!important")])
		}
		m[key] = val
	}
	return m
}

func parseSimpleSelector(selector string) (styleRule, bool) {
	selector = strings.TrimSpace(selector)
	if selector == "" || !simpleSelector.MatchString(selector) {
		return styleRule{}, false
	}
	rule := styleRule{tag: strings.ToLower(cssToken.ReplaceAllString(selector, ""))}
	if rule.tag != "" {
		rule.score++
	}
	for _, tok := range cssToken.FindAllString(selector, -1) {
		if tok[0] == '#' {
			if rule.id != "" && rule.id != tok[1:] {
				return styleRule{}, false
			}
			rule.id = tok[1:]
			rule.score += 100
		} else {
			rule.classes = append(rule.classes, tok[1:])
			rule.score += 10
		}
	}
	return rule, true
}

func (s *styleSheet) add(text string) {
	cleaned := cssComment.ReplaceAllString(text, "")
	i := 0
	for i < len(cleaned) {
		// Skip leading whitespace and stray semicolons.
		for i < len(cleaned) && (cleaned[i] == ' ' || cleaned[i] == '\t' || cleaned[i] == '\n' || cleaned[i] == '\r' || cleaned[i] == ';') {
			i++
		}
		if i >= len(cleaned) {
			break
		}
		// Skip @-rules (@media, @page, @font-face, @import, @charset, etc.) so
		// print/media-specific rules cannot leak into base structural styles.
		if cleaned[i] == '@' {
			for i < len(cleaned) && cleaned[i] != ';' && cleaned[i] != '{' {
				i++
			}
			if i >= len(cleaned) {
				break
			}
			if cleaned[i] == ';' {
				i++
				continue
			}
			depth := 0
			for i < len(cleaned) {
				if cleaned[i] == '{' {
					depth++
				} else if cleaned[i] == '}' {
					depth--
					if depth == 0 {
						i++
						break
					}
				}
				i++
			}
			continue
		}
		brace := strings.IndexByte(cleaned[i:], '{')
		if brace < 0 {
			break
		}
		selList := cleaned[i : i+brace]
		bodyStart := i + brace + 1
		bodyEnd := strings.IndexByte(cleaned[bodyStart:], '}')
		if bodyEnd < 0 {
			break
		}
		decls := declarations(cleaned[bodyStart : bodyStart+bodyEnd])
		i = bodyStart + bodyEnd + 1
		if len(decls) == 0 {
			continue
		}
		for _, sel := range strings.Split(selList, ",") {
			if rule, ok := parseSimpleSelector(sel); ok {
				rule.values = decls
				*s = append(*s, rule)
			}
		}
	}
}

func (p *publication) loadStyles(d *document) {
	walk(d.root, func(n *html.Node) {
		if n.Data == "style" {
			d.css.add(nodeText(n))
		}
		if n.Data == "link" && hasToken(attr(n, "rel"), "stylesheet") {
			src, _ := resolve(d.src, attr(n, "href"))
			if src == "" {
				return
			}
			// Missing stylesheets in imperfect EPUBs must not abort text parsing.
			if b, err := p.read(src); err == nil {
				d.css.add(string(b))
			}
		}
	})
}

func (d *document) style(n *html.Node) map[string]string {
	out := map[string]string{}
	scores := map[string]int{}
	nodeID := attr(n, "id")
	nodeClass := attr(n, "class")
	for _, rule := range d.css {
		if rule.tag != "" && rule.tag != n.Data {
			continue
		}
		if rule.id != "" && nodeID != rule.id {
			continue
		}
		match := true
		for _, cls := range rule.classes {
			if !hasToken(nodeClass, cls) {
				match = false
				break
			}
		}
		if match {
			for k, v := range rule.values {
				if rule.score >= scores[k] {
					out[k] = v
					scores[k] = rule.score
				}
			}
		}
	}
	if inline := attr(n, "style"); inline != "" {
		for k, v := range declarations(inline) {
			out[k] = v
		}
	}
	return out
}

func emLength(s string) float64 {
	m := lengthValue.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	switch m[2] {
	case "px":
		v /= 16
	case "pt":
		v /= 12
	}
	return v
}

func marginTop(style map[string]string) float64 {
	if v, ok := style["margin-top"]; ok {
		return emLength(v)
	}
	v := strings.Fields(style["margin"])
	if len(v) > 0 {
		return emLength(v[0])
	}
	return 0
}

func semantic(n *html.Node, words ...string) bool {
	for _, key := range []string{"type", "role", "class"} {
		value := strings.ToLower(attr(n, key))
		if value == "" {
			continue
		}
		for _, word := range words {
			if hasToken(value, word) || hasToken(value, "doc-"+word) {
				return true
			}
		}
	}
	return false
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key || key == "type" && a.Key == "epub:type" {
			return a.Val
		}
	}
	return ""
}

func walk(n *html.Node, f func(*html.Node)) {
	f(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, f)
	}
}

func nodeText(n *html.Node) string {
	var b strings.Builder
	walk(n, func(x *html.Node) {
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
	})
	return b.String()
}

func heading(n *html.Node) int {
	if len(n.Data) == 2 && n.Data[0] == 'h' && n.Data[1] >= '1' && n.Data[1] <= '6' {
		return int(n.Data[1] - '0')
	}
	return 0
}

func isBlock(n *html.Node) bool {
	switch n.Data {
	case "p", "div", "li", "aside", "section", "blockquote", "pre", "figcaption", "dt", "dd", "td", "h1", "h2", "h3", "h4", "h5", "h6":
		return true
	}
	return false
}
