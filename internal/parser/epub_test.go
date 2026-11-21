package parser_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"stillpoets/internal/domain"
	"stillpoets/internal/parser"
	booksync "stillpoets/internal/sync"
)

func epubBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func htmlPage(body string) string {
	return `<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><head><title></title></head><body>` + body + `</body></html>`
}
func container(opf string) string {
	return `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="` + opf + `"/></rootfiles></container>`
}
func parse(t *testing.T, files map[string]string) *parser.ParseResult {
	t.Helper()
	b := epubBytes(t, files)
	result, err := parser.ParseEPUBReader(bytes.NewReader(b), int64(len(b)), "book")
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func basicFiles(body string) map[string]string {
	return map[string]string{
		"META-INF/container.xml": container("OPS/book.opf"),
		"OPS/book.opf":           `<package xmlns="http://www.idpf.org/2007/opf"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>合成诗集</dc:title><dc:creator>作者</dc:creator></metadata><manifest><item id="chapter" href="chapter.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="chapter"/></spine></package>`,
		"OPS/chapter.xhtml":      htmlPage(body),
	}
}
func poemText(p domain.Poem) string {
	var lines []string
	for _, s := range p.Stanzas {
		lines = append(lines, s.Lines...)
	}
	return strings.Join(lines, "\n")
}

func TestEPUB3StructureAndCrossFileNotes(t *testing.T) {
	var cover bytes.Buffer
	if err := png.Encode(&cover, image.NewRGBA(image.Rect(0, 0, 64, 96))); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"META-INF/container.xml": container("OPS/book.opf"),
		"OPS/book.opf": `<package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>合成诗集</dc:title><dc:creator id="a">诗人</dc:creator><dc:contributor id="t">译者</dc:contributor><meta refines="#a" property="role">aut</meta><meta refines="#t" property="role">trl</meta></metadata><manifest>
 <item id="cover" href="cover.png" media-type="image/png" properties="cover-image"/>
 <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
 <item id="c" href="text/chapter.xhtml" media-type="application/xhtml+xml"/>
 <item id="n" href="notes.xhtml" media-type="application/xhtml+xml"/>
 </manifest><spine><itemref idref="nav"/><itemref idref="c"/><itemref idref="n"/></spine></package>`,
		"OPS/cover.png": cover.String(),
		"OPS/nav.xhtml": htmlPage(`<nav epub:type="toc"><ol><li><a href="text/chapter.xhtml#part">第一辑</a><ol><li><a href="text/chapter.xhtml#p1">第一首</a></li><li><a href="text/chapter.xhtml#p2">第二首</a></li></ol></li></ol></nav>`),
		"OPS/text/chapter.xhtml": htmlPage(`<h1 id="part">第一辑</h1><h2 id="p1">第一首<br/><span class="subtitle">副题</span></h2>
 <p class="author">单首作者</p><blockquote epub:type="epigraph"><p>题记第一段</p><p>题记第二段</p></blockquote>
 <div class="stanza"><p>第一行<a epub:type="noteref" href="../notes.xhtml#n%201">1</a><br/>　缩进行</p><p>第三行<img src="../glyph.png" width="16" height="16" alt="龘"/></p></div>
 <div class="stanza"><p>另一节①</p></div><figure><img src="../drawing.png" width="300" height="200" alt="山水"/><figcaption>画作图注</figcaption></figure><p><img role="presentation" src="../line.png"/></p>
 <p class="colophon">1988年3月26日 于湖畔</p>
 <h2 id="p2">第二首</h2><p>另一首<a href="#local">①</a></p><p>诗的正文</p><aside id="local" epub:type="footnote"><p>①本地注释<a role="doc-backlink" href="#p2">↩</a></p></aside><p class="note">[9]诗末附注</p>`),
		"OPS/notes.xhtml": htmlPage(`<h1>注释</h1><aside id="n 1" epub:type="endnote"><p>[1]跨文件尾注第一段</p><p>第二段解释<a role="doc-backlink" href="text/chapter.xhtml#p1">↩</a></p></aside>`),
	}
	result := parse(t, files)
	book := result.Book
	if book.Title != "合成诗集" || book.Author != "诗人" || book.Translator != "译者" {
		t.Fatalf("metadata: %+v", book)
	}
	if !bytes.Equal(result.Cover, cover.Bytes()) || result.CoverMediaType != "image/png" {
		t.Fatal("cover bytes/media type changed")
	}
	if book.CountPoems() != 2 || len(book.Poems) != 2 {
		t.Fatalf("entries: %+v / filtered %+v", book.Poems, result.Filtered)
	}
	p := book.Poems[0]
	if p.Title != "第一首" || p.Subtitle != "副题" || p.Author != "单首作者" || p.Section != "第一辑" {
		t.Fatalf("structure: %+v", p)
	}
	if p.Epigraph != "题记第一段\n题记第二段" || p.Colophon != "1988年3月26日 于湖畔" {
		t.Fatalf("ancillary fields: %+v", p)
	}
	want := []domain.Stanza{{Lines: []string{"第一行[1]", "　缩进行", "第三行龘"}}, {Lines: []string{"另一节①"}}}
	if !reflect.DeepEqual(p.Stanzas, want) {
		t.Fatalf("stanzas: %#v, want %#v", p.Stanzas, want)
	}
	if len(p.Notes) != 1 || p.Notes[0].Label != "[1]" || p.Notes[0].Body != "跨文件尾注第一段\n第二段解释" {
		t.Fatalf("cross file note: %+v", p.Notes)
	}
	if len(p.Images) != 1 || p.Images[0].Src != "OPS/drawing.png" || p.Images[0].Caption != "画作图注" {
		t.Fatalf("images: %+v", p.Images)
	}
	p2 := book.Poems[1]
	if len(p2.Notes) != 2 || p2.Notes[0].Body != "本地注释" || p2.Notes[1].Body != "诗末附注" {
		t.Fatalf("local/unlinked notes: %+v", p2)
	}
	if strings.Contains(poemText(p2), "注释") || strings.Contains(poemText(p), "题记") {
		t.Fatal("notes/epigraph leaked into verse")
	}
	if parser.BuildDefaultExcerpt(p) != "第一行\n缩进行\n第三行龘" {
		t.Fatalf("excerpt: %q", parser.BuildDefaultExcerpt(p))
	}
	reasons := map[string]int{}
	for _, f := range result.Filtered {
		reasons[f.Reason]++
	}
	if reasons["toc"] != 1 || reasons["notes"] != 1 || reasons["section"] != 1 {
		t.Fatalf("filtered: %+v", result.Filtered)
	}
	// Parsing the same bytes keeps entry and note identities/content stable.
	again := parse(t, files)
	again.Book.ParsedAt = book.ParsedAt
	if !reflect.DeepEqual(book, again.Book) {
		t.Fatal("nondeterministic parse")
	}
}

func TestEPUB2NCXNoiseAndClassification(t *testing.T) {
	files := map[string]string{
		"META-INF/container.xml": container("book.opf"),
		"book.opf": `<package xmlns="http://www.idpf.org/2007/opf"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>书</dc:title><dc:creator role="aut">作者</dc:creator><dc:creator role="trl">译者</dc:creator><meta name="cover" content="img"/></metadata><manifest>
 <item id="img" href="cover.jpg" media-type="image/jpeg"/><item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
 <item id="cover" href="cover.xhtml" media-type="application/xhtml+xml"/><item id="cip" href="cip.xhtml" media-type="application/xhtml+xml"/><item id="toc" href="toc.xhtml" media-type="application/xhtml+xml"/><item id="blank" href="blank.xhtml" media-type="application/xhtml+xml"/><item id="body" href="body.xhtml" media-type="application/xhtml+xml"/></manifest><spine toc="ncx"><itemref idref="cover"/><itemref idref="cip"/><itemref idref="toc"/><itemref idref="blank"/><itemref idref="body"/></spine><guide><reference type="cover" href="cover.xhtml"/></guide></package>`,
		"cover.jpg": "original jpeg", "cover.xhtml": htmlPage(`<img src="cover.jpg"/>`), "cip.xhtml": htmlPage(`<p>图书在版编目（CIP）数据</p><p>ISBN 123456</p>`), "toc.xhtml": htmlPage(`<h1>目录</h1><p><a href="body.xhtml">正文</a></p>`), "blank.xhtml": htmlPage(`<p>　<br/>&#160;</p>`),
		"toc.ncx":    `<ncx><navMap><navPoint><navLabel><text>第一辑</text></navLabel><content src="body.xhtml#s"/><navPoint><navLabel><text>译序</text></navLabel><content src="body.xhtml#intro"/></navPoint><navPoint><navLabel><text>长歌</text></navLabel><content src="body.xhtml#poem"/></navPoint><navPoint><navLabel><text>后记</text></navLabel><content src="body.xhtml#after"/></navPoint></navPoint></navMap></ncx>`,
		"body.xhtml": htmlPage(`<h1 id="s">第一辑</h1><h2 id="intro">译序</h2><p>译者谈翻译。</p><h2 id="poem">长歌<a href="#n">[1]</a></h2><p>　　第一行</p><p>第二行</p><p>　</p><p>另一节</p><p>1989年3月26日</p><hr/><p class="note"><a id="n" href="#poem">[1]</a>同文件脚注</p><h2 id="after">后记</h2><p>最后的自然段。</p>`),
	}
	result := parse(t, files)
	if len(result.Book.Poems) != 3 || result.Book.CountPoems() != 1 {
		t.Fatalf("classification: %+v", result)
	}
	if result.Book.Translator != "译者" || result.CoverMediaType != "image/jpeg" || string(result.Cover) != "original jpeg" {
		t.Fatal("EPUB2 metadata/cover")
	}
	if result.Book.Poems[0].Kind != domain.EntryKindProse || result.Book.Poems[2].Kind != domain.EntryKindProse {
		t.Fatal("prose classification")
	}
	p := result.Book.Poems[1]
	if p.Colophon != "1989年3月26日" || len(p.Stanzas) != 2 || len(p.Notes) != 1 || p.Notes[0].Body != "同文件脚注" {
		t.Fatalf("poem: %+v", p)
	}
	if len(result.Book.Poems[2].Notes) != 0 {
		t.Fatal("referenced note attached to following prose")
	}
	if len(result.Filtered) != 5 {
		t.Fatalf("filtered: %+v", result.Filtered)
	}
}

func TestHeadingFallbackSpacingAndGlyph(t *testing.T) {
	files := basicFiles(`<style>p{margin-top:0;text-indent:0}.verse{margin-top:0}.gap{margin-top:2.5em}.indent{ text-indent:2em }.a{ text-indent:1em }.b{text-indent:3em}</style>
 <h1>一朵花开了</h1><p>首行</p><p class="gap indent">次节首行</p><p>尾行<img alt="𰻞" width="1em" height="1em" src="glyph.png"/></p><h1>另一首</h1><p class="a b">缩进三格</p><p class="colophon">写于河畔</p>`)
	result := parse(t, files)
	if result.Book.CountPoems() != 2 {
		t.Fatalf("heading split: %+v", result.Book.Poems)
	}
	p := result.Book.Poems[0]
	if len(p.Stanzas) != 2 || p.Stanzas[1].Lines[0] != "　　次节首行" || p.Stanzas[1].Lines[1] != "尾行𰻞" {
		t.Fatalf("spacing/glyph: %+v", p)
	}
	if result.Book.Poems[1].Stanzas[0].Lines[0] != "　　　缩进三格" || result.Book.Poems[1].Colophon != "写于河畔" {
		t.Fatalf("CSS source order/colophon: %+v", result.Book.Poems[1])
	}
}

func TestHTMLWhitespaceAndPreformattedVerse(t *testing.T) {
	result := parse(t, basicFiles("<h1>诗</h1><p>首行\n  续文<br/>　下一行</p><pre>  保留两格\n\\t符号\n\t保留制表符\n\n下一节</pre>"))
	p := result.Book.Poems[0]
	want := []domain.Stanza{{Lines: []string{"首行 续文", "　下一行", "  保留两格", `\t符号`, "\t保留制表符"}}, {Lines: []string{"下一节"}}}
	if !reflect.DeepEqual(p.Stanzas, want) {
		t.Fatalf("HTML/pre whitespace: %#v", p.Stanzas)
	}
}

func TestHeadingGroupsWithoutTOC(t *testing.T) {
	result := parse(t, basicFiles(`<h1>第一辑</h1><h2>第一首</h2><p>一行诗</p><h2>第二首</h2><p>另一行</p><h1>第二辑</h1><h2>第三首</h2><p>第三行</p>`))
	if result.Book.CountPoems() != 3 {
		t.Fatalf("inferred groups: %+v", result.Book.Poems)
	}
	for i, want := range []string{"第一辑", "第一辑", "第二辑"} {
		if result.Book.Poems[i].Section != want {
			t.Fatalf("section %d: %+v", i, result.Book.Poems[i])
		}
	}
	if len(result.Filtered) != 2 {
		t.Fatalf("section headings: %+v", result.Filtered)
	}
}

func TestMixedProseAndQuotedVerse(t *testing.T) {
	result := parse(t, basicFiles(`<h1>评论</h1><p>`+strings.Repeat("评论者讨论诗歌的表达和结构。", 20)+`</p><p class="poetry">一行引诗</p><p class="poetry">第二行引诗</p>`))
	if len(result.Book.Poems) != 1 || result.Book.Poems[0].Kind != domain.EntryKindProse || len(result.Book.Poems[0].Stanzas) != 3 {
		t.Fatalf("quoted verse changed prose classification: %+v", result.Book.Poems)
	}
}

func TestNotesOutsideSpineAndUnreferencedNotes(t *testing.T) {
	files := basicFiles(`<h1>诗</h1><p>首行<a href="notes.xhtml#n">[1]</a>又见<a href="notes.xhtml#n">[1]</a></p><p>注：没有超链接的诗末附注。</p>`)
	files["OPS/book.opf"] = strings.Replace(files["OPS/book.opf"], "</manifest>", `<item id="notes" href="notes.xhtml" media-type="application/xhtml+xml"/></manifest>`, 1)
	files["OPS/notes.xhtml"] = htmlPage(`<p id="n">[1]未列入 spine 的注释。</p>`)
	result := parse(t, files)
	if len(result.Book.Poems) != 1 || len(result.Book.Poems[0].Notes) != 2 || result.Book.Poems[0].Notes[0].Body != "未列入 spine 的注释。" || result.Book.Poems[0].Notes[1].Label != "注" {
		t.Fatalf("notes: %+v", result.Book.Poems)
	}
	result = parse(t, basicFiles(`<h1>尾注</h1><aside id="unused" epub:type="endnote">[1]没有引用的注释仍应保留。</aside>`))
	if len(result.Book.Poems) != 1 || result.Book.Poems[0].Kind != domain.EntryKindProse || !strings.Contains(poemText(result.Book.Poems[0]), "没有引用的注释仍应保留") {
		t.Fatalf("unreferenced notes lost: %+v", result)
	}
	result = parse(t, basicFiles(`<h1>诗</h1><p>首行</p><p>没有链接的末尾诗句</p>`))
	if len(result.Book.Poems[0].Notes) != 0 {
		t.Fatal("plain verse became a note")
	}
}

func TestEPUB3SemanticCover(t *testing.T) {
	files := basicFiles(`<img src="cover.png"/>`)
	files["OPS/chapter.xhtml"] = strings.Replace(files["OPS/chapter.xhtml"], "<body>", `<body epub:type="cover">`, 1)
	result := parse(t, files)
	if len(result.Book.Poems) != 0 || len(result.Filtered) != 1 || result.Filtered[0].Reason != "cover" {
		t.Fatalf("semantic cover: %+v", result)
	}
}

func TestStripNoteRefsAndExcerpt(t *testing.T) {
	if got := parser.StripNoteRefs("第1行[12]，①花［2］②③叶¹；2024"); got != "第1行，花叶；2024" {
		t.Fatal(got)
	}
	p := domain.Poem{Epigraph: "题记", Colophon: "落款", Stanzas: []domain.Stanza{{Lines: []string{"[1]", "一①", "二[2]", "三", "四", "五"}}, {Lines: []string{"后节"}}}}
	if got := parser.BuildDefaultExcerpt(p); got != "一\n二\n三\n四" {
		t.Fatal(got)
	}
	p.Kind = domain.EntryKindProse
	if parser.BuildDefaultExcerpt(p) != "" {
		t.Fatal("prose excerpt")
	}
	p.Kind = domain.EntryKindPoem
	p.Stanzas = []domain.Stanza{{Lines: []string{strings.Repeat("诗", 200)}}}
	if len([]rune(parser.BuildDefaultExcerpt(p))) != 160 {
		t.Fatal("excerpt size")
	}
}

func TestCRRegressionEdgeCases(t *testing.T) {
	files := map[string]string{
		"META-INF/container.xml": container("OPS/book.opf"),
		"OPS/book.opf": `<package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>边界回归</dc:title><dc:creator>诗人</dc:creator></metadata><manifest>
 <item id="svgcover" href="cover.svg" media-type="image/svg+xml"/>
 <item id="ch" href="ch.xhtml" media-type="application/xhtml+xml"/>
 <item id="sec" href="sec.xhtml" media-type="application/xhtml+xml"/>
 <item id="fn" href="fn.xhtml" media-type="application/xhtml+xml"/>
 </manifest><spine><itemref idref="svgcover"/><itemref idref="sec"/><itemref idref="ch"/><itemref idref="fn"/></spine></package>`,
		"OPS/cover.svg": `<svg xmlns="http://www.w3.org/2000/svg"></svg>`,
		"OPS/sec.xhtml": htmlPage(`<h1>第一辑</h1><p><img src="banner.png" width="600" height="200" alt="辑图"/></p>`),
		"OPS/ch.xhtml": `<html xmlns="http://www.w3.org/1999/xhtml"><head><link rel="stylesheet" href="missing.css"/><style>@media print { p { display: none; } } .gap { MARGIN-TOP: 2em !important ; }</style></head><body>
 <h1>全引用诗</h1>
 <blockquote><p>整首都在引用块里<a href="fn.xhtml#f1">[1]</a></p><p class="gap">第二诗节</p></blockquote>
 </body></html>`,
		"OPS/fn.xhtml": htmlPage(`<ol id="fn-list" class="footnotes"><li id="f1">[1]第一条注释</li><li id="f2">[2]第二条注释</li></ol>`),
	}
	result := parse(t, files)
	if result.Book.CountPoems() != 1 {
		t.Fatalf("want 1 poem, got %d (all=%+v filtered=%+v)", result.Book.CountPoems(), result.Book.Poems, result.Filtered)
	}
	p := result.Book.Poems[0]
	if p.Title != "全引用诗" || p.Epigraph != "" {
		t.Fatalf("blockquote poem misclassified as epigraph: %+v", p)
	}
	if len(p.Stanzas) != 2 || p.Stanzas[0].Lines[0] != "整首都在引用块里[1]" || p.Stanzas[1].Lines[0] != "第二诗节" {
		t.Fatalf("stanzas damaged by @media or !important: %+v", p.Stanzas)
	}
	if len(p.Notes) != 1 || p.Notes[0].Body != "第一条注释" {
		t.Fatalf("expected only #f1 from <ol id=fn-list>, got %+v", p.Notes)
	}
	hasNonHTML, hasImgOnly := false, false
	for _, f := range result.Filtered {
		if f.Reason == "non-html" {
			hasNonHTML = true
		}
		if f.Reason == "image-only" {
			hasImgOnly = true
		}
	}
	if !hasNonHTML || !hasImgOnly {
		t.Fatalf("expected non-html and image-only filtered items, got %+v", result.Filtered)
	}
}

func TestInvalidEPUB(t *testing.T) {
	for _, test := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"container missing", map[string]string{}, "container.xml"},
		{"rootfile missing", map[string]string{"META-INF/container.xml": `<container/>`}, "no rootfile"},
		{"bad XML", map[string]string{"META-INF/container.xml": "<"}, "container.xml"},
		{"spine missing manifest entry", map[string]string{"META-INF/container.xml": container("b.opf"), "b.opf": `<package><spine><itemref idref="missing"/></spine></package>`}, "missing from manifest"},
	} {
		t.Run(test.name, func(t *testing.T) {
			b := epubBytes(t, test.files)
			_, err := parser.ParseEPUBReader(bytes.NewReader(b), int64(len(b)), "id")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	if _, err := parser.ParseEPUBReader(bytes.NewReader([]byte("bad")), 3, "id"); err == nil {
		t.Fatal("invalid ZIP accepted")
	}
	if _, err := parser.ParseEPUBReader(bytes.NewReader(nil), 0, ""); err == nil {
		t.Fatal("empty ID accepted")
	}
	if _, err := parser.ParseEPUBFile(filepath.Join(t.TempDir(), "absent.epub"), "id"); err == nil {
		t.Fatal("absent file accepted")
	}
}

func TestRealBooks(t *testing.T) {
	dir := filepath.Join("..", "..", "test-books")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skip("test-books is not installed")
	}
	tests := []struct {
		name                    string
		min, max                int
		notes, colophons, prose bool
	}{
		{"草叶集", 380, 460, true, true, true}, {"恶之花", 140, 180, true, false, true}, {"海子的诗", 100, 110, true, false, false}, {"叶芝诗集", 330, 420, false, true, true},
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.epub"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filename := ""
			for _, f := range files {
				if strings.HasPrefix(filepath.Base(f), test.name) {
					filename = f
					break
				}
			}
			if filename == "" {
				t.Fatalf("missing real fixture %s", test.name)
			}
			result, err := parser.ParseEPUBFile(filename, "real-book")
			if err != nil {
				t.Fatal(err)
			}
			book := result.Book
			count := book.CountPoems()
			if count < test.min || count > test.max {
				t.Fatalf("implausible poem count %d (want %d..%d)", count, test.min, test.max)
			}
			if len(result.Cover) == 0 || !strings.HasPrefix(result.CoverMediaType, "image/") {
				t.Fatal("missing cover")
			}
			if book.Title == "" || book.Author == "" {
				t.Fatal("missing metadata")
			}
			notes, colophons, prose, epigraphs := 0, 0, 0, 0
			for i, p := range book.Poems {
				if p.ID != fmt.Sprintf("real-book:%04d", i+1) || p.OrderIndex != i || p.BookID != book.BookID {
					t.Fatalf("entry identity: %+v", p)
				}
				notes += len(p.Notes)
				if p.Colophon != "" {
					colophons++
				}
				if p.Kind == domain.EntryKindProse {
					prose++
				}
				if p.Epigraph != "" {
					epigraphs++
				}
				if p.IsPoem() && (strings.Contains(p.Title, "版权") || (p.Title == "目录" || strings.Contains(p.Title, "英文目录")) || strings.Contains(poemText(p), "CIP数据核字")) {
					t.Fatalf("noise in poem %s", p.Title)
				}
				if p.IsPoem() && strings.Contains(p.Title, "后记") {
					t.Fatalf("afterword classified as poem %s", p.Title)
				}
				if (strings.Contains(p.Title, "译序") || strings.Contains(p.Title, "前言") || strings.Contains(p.Title, "后记")) && p.Kind != domain.EntryKindProse {
					t.Fatalf("prose classified as poem %s", p.Title)
				}
			}
			if test.notes && notes == 0 {
				t.Fatal("no notes extracted")
			}
			if test.colophons && colophons == 0 {
				t.Fatal("no source colophons extracted")
			}
			if test.prose && prose == 0 {
				t.Fatal("no prose extracted")
			}
			// These source editions actually omit the corresponding information.
			if test.name == "叶芝诗集" && notes != 0 {
				t.Fatalf("empty source spans became fabricated notes: %d", notes)
			}
			if test.name == "海子的诗" && colophons != 0 {
				t.Fatalf("source verse was mistaken for colophon: %d", colophons)
			}
			if test.name == "海子的诗" {
				found := false
				for _, p := range book.Poems {
					if p.Title == "阿尔的太阳" {
						found = true
						if p.Subtitle != "——给我的瘦哥哥" || p.Epigraph == "" || len(p.Notes) != 2 || strings.Contains(parser.BuildDefaultExcerpt(p), "不信仰太阳") {
							t.Fatalf("source epigraph/subtitle/notes: %+v", p)
						}
					}
				}
				if !found {
					t.Fatal("source poem missing")
				}
			}
			if test.name == "恶之花" {
				for _, p := range book.Poems {
					if p.Title == "告读者" && (p.Section != "忧郁和理想" || len(p.Stanzas) != 10 || len(p.Notes) != 2) {
						t.Fatalf("source stanza/section/notes: %+v", p)
					}
				}
			}
			encoded, hash, err := booksync.EncodeSPBook(book)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := booksync.DecodeSPBook(encoded, hash, book.BookID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(book, decoded) {
				t.Fatal("SPBook round trip changed content")
			}
			t.Logf("STATS title=%q poems=%d prose=%d filtered=%d notes=%d colophons=%d epigraphs=%d cover=%d/%s", book.Title, count, prose, len(result.Filtered), notes, colophons, epigraphs, len(result.Cover), result.CoverMediaType)
			for _, p := range book.Poems {
				if p.IsPoem() {
					t.Logf("SAMPLE title=%q section=%q stanzas=%d excerpt=%q", p.Title, p.Section, len(p.Stanzas), parser.BuildDefaultExcerpt(p))
					break
				}
			}
			for _, p := range book.Poems {
				if len(p.Notes) > 0 {
					t.Logf("NOTE title=%q label=%q body=%q", p.Title, p.Notes[0].Label, p.Notes[0].Body)
					break
				}
			}
			for _, p := range book.Poems {
				if p.Colophon != "" {
					t.Logf("COLOPHON title=%q value=%q", p.Title, p.Colophon)
					break
				}
			}
			var proseTitles []string
			for _, p := range book.Poems {
				if p.Kind == domain.EntryKindProse {
					proseTitles = append(proseTitles, p.Title)
				}
			}
			t.Logf("PROSE %q", proseTitles)
		})
	}
}
