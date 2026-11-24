package parser

import (
	"strings"
	"testing"

	"stillpoets/internal/domain"
)

func TestBuildLargeExcerptMultiStanzaAndLimits(t *testing.T) {
	p := domain.Poem{
		ID:    "p1",
		Title: "面朝大海，春暖花开",
		Kind:  domain.EntryKindPoem,
		Stanzas: []domain.Stanza{
			{Lines: []string{"从明天起，做一个幸福的人", "喂马、劈柴，周游世界[1]"}},
			{Lines: []string{"从明天起，关心粮食和蔬菜", "我有一所房子，面朝大海，春暖花开"}},
			{Lines: []string{"从明天起，和每一个亲人通信", "告诉他们我的幸福", "那幸福的闪电告诉我的", "我将告诉每一个人"}},
		},
	}

	// 紧凑摘要仍保持首节（2 行），兼容 2×2 / 4×2 卡片
	compact := BuildDefaultExcerpt(p)
	if compact != "从明天起，做一个幸福的人\n喂马、劈柴，周游世界" {
		t.Fatalf("unexpected BuildDefaultExcerpt: %q", compact)
	}

	// 大尺寸 4×4 摘要应跨诗节展开、保留节间空行、剥离注脚
	large := BuildLargeExcerpt(p)
	want := strings.Join([]string{
		"从明天起，做一个幸福的人",
		"喂马、劈柴，周游世界",
		"",
		"从明天起，关心粮食和蔬菜",
		"我有一所房子，面朝大海，春暖花开",
		"",
		"从明天起，和每一个亲人通信",
		"告诉他们我的幸福",
		"那幸福的闪电告诉我的",
		"我将告诉每一个人",
	}, "\n")
	if large != want {
		t.Fatalf("BuildLargeExcerpt mismatch:\ngot:\n%s\nwant:\n%s", large, want)
	}

	// 验证超长诗篇被安全截断在 14 行以内且末尾无悬空空行
	var longStanzas []domain.Stanza
	for s := 0; s < 8; s++ {
		longStanzas = append(longStanzas, domain.Stanza{
			Lines: []string{"第一行诗句", "第二行诗句", "第三行诗句"},
		})
	}
	longLarge := BuildLargeExcerpt(domain.Poem{
		ID:      "p-long",
		Kind:    domain.EntryKindPoem,
		Stanzas: longStanzas,
	})
	lines := strings.Split(longLarge, "\n")
	if len(lines) > 14 {
		t.Fatalf("expected at most 14 lines, got %d", len(lines))
	}
	if lines[len(lines)-1] == "" {
		t.Fatalf("expected no trailing blank line in large excerpt: %q", longLarge)
	}

	// 附文不生成小组件正文
	if got := BuildLargeExcerpt(domain.Poem{ID: "front", Kind: domain.EntryKindProse, Stanzas: p.Stanzas}); got != "" {
		t.Fatalf("expected empty large excerpt for non-poem, got %q", got)
	}
}
