package views

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
	"stillpoets/internal/domain"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/theme"
)

func (a *AppUI) clearBookCacheAsync(bookID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.svc.ClearBookCache(ctx, bookID); err != nil {
			a.showTransientToast("无法清理：" + err.Error())
			return
		}
		a.reloadSync(nil)
		a.showTransientToast("已清理本书离线缓存")
	}()
}

func (a *AppUI) deleteBookAsync(bookID string) {
	if a.activeBookID == bookID {
		a.mu.Lock()
		a.openSeq++
		a.activeBookID = ""
		a.mu.Unlock()
		a.readerMobile = false
		a.activePoems = nil
		a.readerEntrySource = entryMainline
		a.peekBannerHidden = false
		a.pendingPoemID = ""
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.svc.DeleteBook(ctx, bookID); err != nil {
			a.showTransientToast("删除诗集失败：" + err.Error())
			return
		}
		a.reloadSync(nil)
		a.showTransientToast("已删除诗集（相关诗歌节选已保留文字）")
	}()
}

func (a *AppUI) renderTOCModal(c *ui.Context, pal theme.Palette) {
	components.AnimatedSheet(c, &a.showTOCModal, pal, []float32{0.84}, "诗集目录", func() {
		book, ok := a.activeBook()
		if !ok {
			return
		}
		prog := a.progressMap[book.ID]
		isFinished := prog.Status == domain.ReadStatusFinished

		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, book.Title).FontSize(17).Bold().TextColor(pal.TextPrimary)
				ui.Textf(c, "%s · 共 %d 篇", book.Author, len(a.activePoems)).FontSize(12).TextColor(pal.TextMuted)
			})
			closeBtn := ui.Box(c.Key("toc-close")).Padding(4).Cursor(ui.CursorPointer)
			theme.ApplyPressScale(closeBtn, 0.90)
			closeBtn.Children(func() { ui.Icon(c, components.IconClose).Width(16).Height(16).TextColor(pal.TextMuted) })
			if closeBtn.Clicked() {
				a.showTOCModal = false
			}
		})

		// 书本快捷管理行（完读 / 移入分组 / 清理缓存 / 删除）
		ui.Row(c.Key("toc-book-actions")).Gap(8).Wrap().Children(func() {
			finLabel := "标记已完读"
			if isFinished {
				finLabel = "已完读 ✓"
			}
			if ui.Button(c, finLabel).Clicked() {
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					_, _ = a.svc.MarkBookFinished(ctx, book.ID, !isFinished)
					a.reloadSync(ctx)
				}()
			}
			if len(a.activeFavorites) > 0 {
				if ui.Button(c, "精选合辑卡").Clicked() {
					favs := make([]domain.FavoritePoem, 0, len(a.activeFavorites))
					for _, fv := range a.activeFavorites {
						favs = append(favs, fv)
					}
					a.openCollectionShareCard(book, favs)
				}
			}
			if ui.Button(c, "清理离线正文").Clicked() {
				a.showTOCModal = false
				a.clearBookCacheAsync(book.ID)
			}
			if ui.Button(c, "删除诗集").Clicked() {
				a.showTOCModal = false
				a.deleteBookAsync(book.ID)
			}
		})

		// 移入分组切换
		if len(a.groups) > 0 {
			ui.Row(c.Key("toc-group-assign")).Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "分组：").FontSize(12).TextColor(pal.TextMuted)
				renderAssign := func(gid, label string) {
					active := book.GroupID == gid
					bg, fg, bd := pal.Background, pal.TextSecondary, pal.Border
					if active {
						bg, fg, bd = pal.Accent, pal.AccentText, pal.Accent
					}
					chip := ui.Box(c.Key("asg-"+gid)).Padding(4, 9).Radius(999).Background(bg).Border(1, bd).Cursor(ui.CursorPointer).
						Transition(theme.ColorTransition())
					chip.Children(func() { ui.Text(c, label).FontSize(11).TextColor(fg) })
					if chip.Clicked() {
						go func() {
							ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							defer cancel()
							_ = a.svc.SetBookGroup(ctx, book.ID, gid)
							a.reloadSync(ctx)
						}()
					}
				}
				renderAssign("", "未分组")
				for _, g := range a.groups {
					renderAssign(g.ID, g.Name)
				}
			})
		}

		// 本书目录搜索（实时匹配诗题与诗句全文）
		if ui.SearchField(c.Key("toc-search-input"), &a.tocQuery).Placeholder("搜本书诗题或诗句全文…").Changed() {
			a.tocScroll.Y = 0
		}

		tocEntries, isSearching := filterTOCPoems(a.activePoems, a.tocQuery)
		if isSearching {
			ui.Textf(c, "找到 %d 篇（按标题与正文匹配）", len(tocEntries)).
				FontSize(11.5).TextColor(pal.TextMuted)
		}

		ui.Divider(c)

		// 诗集目录列表（Basis(0) 确保严格约束在抽屉剩余高度内，底部留白防止末项贴边）
		ui.Scroll(c.Key("toc-list-scroll")).TrackScroll(&a.tocScroll).Grow(1).Basis(0).FillWidth().Children(func() {
			ui.Column(c).Gap(4).FillWidth().Padding(0, 0, 24, 0).Children(func() {
				if isSearching && len(tocEntries) == 0 {
					ui.Textf(c, "未在《%s》中找到匹配的诗题或诗句", book.Title).
						FontSize(13).TextColor(pal.TextMuted).Padding(18, 6)
					return
				}
				for _, entry := range tocEntries {
					entry := entry
					i, p := entry.index, entry.poem
					isCurrent := i == a.activePoemIdx
					isMainline := prog.LastPoemID != "" && p.ID == prog.LastPoemID
					_, isFav := a.activeFavorites[p.ID]
					bg := ui.Transparent
					if isCurrent {
						bg = pal.HighlightBg
					}
					row := ui.Column(c.Key(fmt.Sprintf("toc-item-%d", i))).
						FillWidth().
						Padding(8, 10).
						Gap(3).
						Radius(6).
						Background(bg).
						Cursor(ui.CursorPointer)
					row.Children(func() {
						ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Textf(c, "%d.", i+1).FontSize(12).TextColor(pal.TextMuted).Width(32)
							titleEl := ui.Text(c, p.Title).FontSize(13).MaxLines(1).Grow(1)
							if isCurrent {
								titleEl.Bold().TextColor(pal.Accent)
							} else {
								titleEl.TextColor(pal.TextPrimary)
							}
							if isMainline {
								ui.Box(c).Padding(2, 6).Radius(999).Border(1, pal.Accent).Children(func() {
									ui.Text(c, "续读").FontSize(10).TextColor(pal.Accent)
								})
							}
							if !p.IsPoem() {
								ui.Text(c, "附文").FontSize(10).TextColor(pal.TextMuted)
							}
							if isFav {
								ui.Icon(c, components.IconHeartFilled).Width(13).Height(13).TextColor(pal.Accent)
							}
						})
						if entry.snippet != "" {
							ui.Text(c, entry.snippet).
								FontSize(11.5).
								TextColor(pal.TextSecondary).
								MaxLines(1).
								Padding(0, 0, 0, 40)
						}
					})
					if row.Clicked() {
						a.showTOCModal = false
						if a.readerEntrySource.isPeek() && isMainline {
							a.returnToMainlineProgress(book.ID)
						} else {
							a.navigatePoem(book.ID, i)
						}
						if entry.matchLine > 0 {
							a.highlightRanges = []app.LineRange{{Start: entry.matchLine, End: entry.matchLine}}
						}
					}
				}
			})
		})
	})
}

type poemMatchResult struct {
	matched    bool
	titleMatch bool
	snippet    string
	matchLine  int // 1-based line number in poem stanzas when matched in body
}

type tocSearchEntry struct {
	index     int
	poem      domain.Poem
	snippet   string
	matchLine int
}

func splitSearchTerms(raw string) []string {
	return strings.Fields(strings.ToLower(strings.TrimSpace(raw)))
}

func containsAllTerms(haystackLower string, terms []string) bool {
	for _, t := range terms {
		if !strings.Contains(haystackLower, t) {
			return false
		}
	}
	return len(terms) > 0
}

func containsAnyTerm(haystack string, terms []string) bool {
	for _, t := range terms {
		if strings.Contains(haystack, t) {
			return true
		}
	}
	return false
}

func matchPoemQuery(p domain.Poem, terms []string) poemMatchResult {
	if len(terms) == 0 {
		return poemMatchResult{matched: true}
	}

	titleHaystack := strings.ToLower(strings.Join([]string{p.Title, p.Subtitle, p.Section, p.Author}, " "))
	titleMatch := containsAllTerms(titleHaystack, terms)

	// 优先在正文诗行中定位具体命中行（支持单行命中与全文跨行命中）
	var (
		allMatchedLine    int
		allMatchedText    string
		firstTermLine     int
		firstTermLineText string
		lineNum           int
		bodyBuilder       strings.Builder
	)
	bodyBuilder.WriteString(titleHaystack)
	bodyBuilder.WriteByte(' ')
	if p.Epigraph != "" {
		bodyBuilder.WriteString(strings.ToLower(p.Epigraph))
		bodyBuilder.WriteByte(' ')
	}
	for _, st := range p.Stanzas {
		for _, rawLine := range st.Lines {
			lineNum++
			trimmed := strings.TrimSpace(rawLine)
			if trimmed == "" {
				continue
			}
			lower := strings.ToLower(trimmed)
			bodyBuilder.WriteString(lower)
			bodyBuilder.WriteByte(' ')
			if allMatchedLine == 0 && containsAllTerms(lower, terms) {
				allMatchedLine = lineNum
				allMatchedText = trimmed
			}
			if firstTermLine == 0 && containsAnyTerm(lower, terms) {
				firstTermLine = lineNum
				firstTermLineText = trimmed
			}
		}
	}
	if p.Colophon != "" {
		bodyBuilder.WriteString(strings.ToLower(p.Colophon))
		bodyBuilder.WriteByte(' ')
	}
	for _, n := range p.Notes {
		if n.Body != "" {
			bodyBuilder.WriteString(strings.ToLower(n.Body))
			bodyBuilder.WriteByte(' ')
		}
	}

	if !titleMatch && !containsAllTerms(bodyBuilder.String(), terms) {
		return poemMatchResult{}
	}

	res := poemMatchResult{
		matched:    true,
		titleMatch: titleMatch,
	}
	switch {
	case allMatchedLine > 0:
		res.matchLine = allMatchedLine
		res.snippet = allMatchedText
	case firstTermLine > 0:
		res.matchLine = firstTermLine
		res.snippet = firstTermLineText
	case !titleMatch && strings.TrimSpace(p.Epigraph) != "" && containsAllTerms(strings.ToLower(p.Epigraph), terms):
		res.snippet = strings.TrimSpace(p.Epigraph)
	case !titleMatch && strings.TrimSpace(p.Colophon) != "" && containsAllTerms(strings.ToLower(p.Colophon), terms):
		res.snippet = strings.TrimSpace(p.Colophon)
	}
	return res
}

func filterTOCPoems(poems []domain.Poem, query string) ([]tocSearchEntry, bool) {
	terms := splitSearchTerms(query)
	if len(terms) == 0 {
		out := make([]tocSearchEntry, len(poems))
		for i, p := range poems {
			out[i] = tocSearchEntry{index: i, poem: p}
		}
		return out, false
	}

	var titleHits, bodyHits []tocSearchEntry
	for i, p := range poems {
		m := matchPoemQuery(p, terms)
		if !m.matched {
			continue
		}
		item := tocSearchEntry{
			index:     i,
			poem:      p,
			snippet:   m.snippet,
			matchLine: m.matchLine,
		}
		if m.titleMatch {
			// 标题已直接命中且正文未命中具体行时，不重复占两行
			if m.matchLine == 0 {
				item.snippet = ""
			}
			titleHits = append(titleHits, item)
		} else {
			bodyHits = append(bodyHits, item)
		}
	}
	return append(titleHits, bodyHits...), true
}

func (a *AppUI) bookTitleByID(bookID string) string {
	for _, b := range a.books {
		if b.ID == bookID {
			return b.Title
		}
	}
	return ""
}
