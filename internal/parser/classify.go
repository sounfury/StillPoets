package parser

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"

	"stillpoets/internal/domain"
)

var proseTitle = regexp.MustCompile(`译序|译者序|序言|前言|导读|后记|补记|年表|年谱|生平|简介|颁奖词|受奖词|致辞|演讲|出版说明|编者按|译者的话|译者札记|(?:^|[）》])序(?:$|[（(])`)
var colophonDate = regexp.MustCompile(`^[（(\s]*(?:[12][0-9]{3}|[一二][〇○零一二三四五六七八九]{3})(?:年|[./-])[0-9〇○零一二三四五六七八九十./年月日—－至、;；\s]*(?:于|写于|作于|在)?[^，。！？!?]{0,16}[）)]?$`)
var publicationYears = regexp.MustCompile(`^[（(][12][0-9]{3}(?:[；;，、—－ -]+[12][0-9]{3})*[）)]$`)

func isColophon(s string) bool {
	return utf8.RuneCountInString(s) <= 60 && (colophonDate.MatchString(s) || publicationYears.MatchString(s) || regexpPlace.MatchString(s))
}

var regexpPlace = regexp.MustCompile(`^(?:写于|作于|改于|定稿于|于)[^，。！？!?]{1,20}$`)

func classify(title string, blocks []textBlock) domain.EntryKind {
	if proseTitle.MatchString(title) {
		return domain.EntryKindProse
	}
	if hasProseStructure(blocks) {
		return domain.EntryKindProse
	}
	return domain.EntryKindPoem
}

func hasProseStructure(blocks []textBlock) bool {
	count, long, verse := 0, 0, false
	totalChars, longChars := 0, 0
	for _, b := range blocks {
		if b.note != nil || b.node != nil && heading(b.node) > 0 {
			continue
		}
		s := strings.TrimSpace(b.text)
		if s == "" {
			continue
		}
		count++
		n := utf8.RuneCountInString(s)
		verseBlock := isVerseBlock(b.node)
		totalChars += n
		if n > 90 && !verseBlock {
			long++
			longChars += n
		}
		if verseBlock || strings.Contains(b.text, "\n") {
			verse = true
		}
	}
	return count > 0 && (longChars*2 >= totalChars || !verse && long*2 >= count)
}

func isVerseBlock(n *html.Node) bool {
	return semanticNode(n, "poetry", "verse", "line", "stanza") || n != nil && n.Data == "pre"
}

func filterReason(title, body string, blocks []textBlock) string {
	if reason := filteredTitle(title); reason != "" {
		return reason
	}
	if reason := publishingPageReason(body); reason != "" {
		return reason
	}
	if isLinkDirectory(blocks) {
		return "toc"
	}
	return ""
}

func filteredTitle(title string) string {
	label := strings.ToLower(strings.TrimSpace(title))
	switch label {
	case "cover", "封面", "扉页", "title page":
		return "cover"
	case "目录", "目錄", "contents", "table of contents":
		return "toc"
	case "版权", "版权页", "版权信息", "copyright":
		return "copyright"
	}
	if strings.Contains(label, "目录") && (strings.Contains(label, "英文") || strings.Contains(label, "英语")) {
		return "toc"
	}
	return ""
}

func publishingPageReason(body string) string {
	// CIP and production credits identify publishing pages even without a heading.
	if strings.Contains(body, "图书在版编目") || strings.Contains(body, "CIP数据核字") || strings.Contains(body, "ISBN") && (strings.Contains(body, "版权所有") || strings.Contains(body, "出版发行") || strings.Contains(body, "责任编辑")) {
		return "copyright"
	}
	if utf8.RuneCountInString(body) < 500 && strings.Contains(body, "出版社") && strings.Contains(body, "电子书") && strings.Contains(body, "致力于") {
		return "publishing"
	}
	if strings.Contains(body, "电子书制作") || strings.Contains(body, "书籍设计") && strings.Contains(body, "策划人") {
		return "publishing"
	}
	return ""
}

func isLinkDirectory(blocks []textBlock) bool {
	linkText, total := 0, 0
	for _, b := range blocks {
		total += utf8.RuneCountInString(strings.TrimSpace(b.text))
		if b.node != nil {
			walk(b.node, func(n *html.Node) {
				if n.Data == "a" {
					linkText += utf8.RuneCountInString(strings.TrimSpace(nodeText(n)))
				}
			})
		}
	}
	return total > 0 && linkText*10 >= total*8 && linkText > 20
}
