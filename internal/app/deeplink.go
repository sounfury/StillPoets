package app

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// DeeplinkKind 区分小组件与外部唤起的跳转目标类型。
type DeeplinkKind string

const (
	DeeplinkNone  DeeplinkKind = ""
	DeeplinkPoem  DeeplinkKind = "poem"
	DeeplinkQuote DeeplinkKind = "quote"
)

// LineRange 表示诗篇内 1-indexed 的连续高亮行区间（含首尾）。
type LineRange struct {
	Start int
	End   int
}

// Contains 判断 1-indexed 行号是否落在该区间内。
func (r LineRange) Contains(line int) bool {
	return r.Start > 0 && line >= r.Start && line <= r.End
}

// DeeplinkTarget 描述解析后的 stillpoets:// 跳转目标。
type DeeplinkTarget struct {
	Kind       DeeplinkKind
	BookID     string
	PoemID     string
	QuoteID    string
	Highlight  bool
	LineRanges []LineRange
}

// ParseDeeplink 解析 stillpoets://poem/<bookId>/<poemId>?lines=4-6&highlight=1
// 以及 stillpoets://quote/<quoteId> 协议。
func ParseDeeplink(raw string) (DeeplinkTarget, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return DeeplinkTarget{}, fmt.Errorf("invalid deeplink: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "stillpoets") {
		return DeeplinkTarget{}, fmt.Errorf("unsupported deeplink scheme %q", u.Scheme)
	}

	parts := splitPath(u.Path)
	switch strings.ToLower(u.Host) {
	case "poem":
		if len(parts) < 2 {
			return DeeplinkTarget{}, fmt.Errorf("poem deeplink requires bookId and poemId")
		}
		q := u.Query()
		ranges := ParseLineRanges(q.Get("lines"))
		highlight := q.Get("highlight") == "1" || q.Get("highlight") == "true" || len(ranges) > 0
		return DeeplinkTarget{
			Kind:       DeeplinkPoem,
			BookID:     parts[0],
			PoemID:     parts[1],
			Highlight:  highlight,
			LineRanges: ranges,
		}, nil

	case "quote":
		if len(parts) < 1 {
			return DeeplinkTarget{}, fmt.Errorf("quote deeplink requires quoteId")
		}
		return DeeplinkTarget{
			Kind:    DeeplinkQuote,
			QuoteID: parts[0],
		}, nil

	default:
		return DeeplinkTarget{}, fmt.Errorf("unsupported deeplink host %q", u.Host)
	}
}

// ParseLineRanges 解析诸如 "4-6"、"2,5-7" 形式的 1-indexed 行范围字符串。
func ParseLineRanges(spec string) []LineRange {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	var out []LineRange
	for _, seg := range strings.Split(spec, ",") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		if a, b, ok := strings.Cut(seg, "-"); ok {
			start, err1 := strconv.Atoi(strings.TrimSpace(a))
			end, err2 := strconv.Atoi(strings.TrimSpace(b))
			if err1 == nil && err2 == nil && start > 0 {
				if end < start {
					start, end = end, start
				}
				out = append(out, LineRange{Start: start, End: end})
			}
			continue
		}
		n, err := strconv.Atoi(seg)
		if err == nil && n > 0 {
			out = append(out, LineRange{Start: n, End: n})
		}
	}
	return out
}

// FormatLineRanges 将已排序的 1-indexed 行号压缩为紧凑的区间字符串（如 "2-4,7"）。
func FormatLineRanges(lines []int) string {
	if len(lines) == 0 {
		return ""
	}
	var parts []string
	start, prev := lines[0], lines[0]
	for _, n := range lines[1:] {
		if n == prev {
			continue
		}
		if n == prev+1 {
			prev = n
			continue
		}
		parts = append(parts, formatSingleRange(start, prev))
		start, prev = n, n
	}
	parts = append(parts, formatSingleRange(start, prev))
	return strings.Join(parts, ",")
}

func formatSingleRange(start, end int) string {
	if start == end {
		return strconv.Itoa(start)
	}
	return fmt.Sprintf("%d-%d", start, end)
}

func splitPath(p string) []string {
	var parts []string
	for _, seg := range strings.Split(strings.Trim(p, "/"), "/") {
		if seg != "" {
			parts = append(parts, seg)
		}
	}
	return parts
}
