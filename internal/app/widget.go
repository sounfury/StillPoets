package app

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"

	"stillpoets/internal/domain"
	"stillpoets/internal/parser"
	"stillpoets/internal/storage"
	"stillpoets/internal/ui/theme"
)

const (
	widgetTimelineFilename = "widget_timeline.json"
	maxWidgetEntries       = 24
)

// RebuildWidgetTimeline 根据本机设置与本地缓存数据，预编译供 iOS WidgetKit / Android Glance
// 直接消费的 widget_timeline.json 快照（原子写入，避免小组件读到半截文件）。
func (s *Service) RebuildWidgetTimeline(ctx context.Context) (domain.WidgetTimeline, error) {
	s.widgetMu.Lock()
	defer s.widgetMu.Unlock()

	settings, err := s.store.LoadSettings(ctx)
	if err != nil {
		return domain.WidgetTimeline{}, err
	}
	pal := theme.ByID(theme.PresetID(settings.ThemePreset))
	wTheme := settings.WidgetTheme
	if wTheme.Preset == "" || wTheme.Preset == string(pal.ID) {
		// 内置预设色板升级时（例如刊物风纸白微调），自动同步最新主题色值到小组件快照
		wTheme.Preset = string(pal.ID)
		wTheme.BgColor = pal.BgHex
		wTheme.TextColor = pal.TextHex
		wTheme.AccentColor = pal.AccentHex
	} else {
		if wTheme.BgColor == "" {
			wTheme.BgColor = pal.BgHex
		}
		if wTheme.TextColor == "" {
			wTheme.TextColor = pal.TextHex
		}
		if wTheme.AccentColor == "" {
			wTheme.AccentColor = pal.AccentHex
		}
	}
	if wTheme.FontFamily == "" {
		wTheme.FontFamily = settings.FontFamily
	}
	wTheme.FontWeight = settings.EffectiveFontWeight()
	darkPal := theme.DarkMinimal()
	wTheme.FollowSystemDark = settings.ShouldFollowSystemDark()
	wTheme.DarkBgColor = darkPal.BgHex
	wTheme.DarkTextColor = darkPal.TextHex
	wTheme.DarkAccentColor = darkPal.AccentHex

	entries, err := s.buildWidgetEntries(ctx, settings)
	if err != nil {
		return domain.WidgetTimeline{}, err
	}

	timeline := domain.WidgetTimeline{
		GeneratedAt:            domain.NowMillis(),
		RefreshIntervalMinutes: domain.NormalizeWidgetRefreshMinutes(settings.WidgetRefreshMinutes),
		Theme:                  wTheme,
		Entries:                entries,
	}

	timelinePath := filepath.Join(s.dataDir, widgetTimelineFilename)
	if err := writeJSONAtomic(timelinePath, timeline); err != nil {
		return domain.WidgetTimeline{}, fmt.Errorf("app: write widget timeline: %w", err)
	}

	s.mu.Lock()
	cb := s.onWidgetUpdated
	s.mu.Unlock()
	if cb != nil {
		cb(timelinePath, s.FontsDir())
	}
	return timeline, nil
}

// ReadWidgetTimeline 读取当前磁盘上已预编译的小组件时间线快照（供 App 内设置页实时预览）。
func (s *Service) ReadWidgetTimeline() (domain.WidgetTimeline, error) {
	raw, err := os.ReadFile(s.WidgetTimelinePath())
	if err != nil {
		return domain.WidgetTimeline{}, err
	}
	var tl domain.WidgetTimeline
	if err := json.Unmarshal(raw, &tl); err != nil {
		return domain.WidgetTimeline{}, err
	}
	return tl, nil
}

// WidgetTimelinePath 返回当前数据目录内预编译小组件快照的路径。
func (s *Service) WidgetTimelinePath() string {
	return filepath.Join(s.dataDir, widgetTimelineFilename)
}

func (s *Service) buildWidgetEntries(ctx context.Context, settings domain.Settings) ([]domain.WidgetEntry, error) {
	switch settings.WidgetMode {
	case domain.WidgetModeQuotesRandom:
		return s.buildQuoteWidgetEntries(ctx)
	case domain.WidgetModeBookFavorites:
		return s.buildFavoriteWidgetEntries(ctx, settings.WidgetBookID)
	case domain.WidgetModeBookRandom:
		return s.buildBookWidgetEntries(ctx, settings.WidgetBookID, true)
	case domain.WidgetModeBookSequential:
		return s.buildBookWidgetEntries(ctx, settings.WidgetBookID, false)
	default:
		return s.buildGlobalRandomWidgetEntries(ctx)
	}
}

func (s *Service) buildQuoteWidgetEntries(ctx context.Context) ([]domain.WidgetEntry, error) {
	views, err := s.store.ListQuotes(ctx, storage.QuoteFilter{})
	if err != nil {
		return nil, err
	}
	entries := make([]domain.WidgetEntry, 0, len(views))
	for _, qv := range views {
		text := strings.TrimSpace(qv.Quote.Content)
		if text == "" {
			continue
		}
		entries = append(entries, domain.WidgetEntry{
			EntryType:   "quote",
			DisplayText: text,
			PoemTitle:   qv.Quote.PoemTitle,
			Author:      qv.Quote.Author,
			SourceTitle: qv.Quote.SourceBook,
			Deeplink:    qv.Deeplink, // 诗歌节选跳回原诗，独立/降级散句仅开单句卡片
		})
	}
	shuffleEntries(entries)
	return limitEntries(entries), nil
}

func (s *Service) buildFavoriteWidgetEntries(ctx context.Context, bookID string) ([]domain.WidgetEntry, error) {
	favs, err := s.store.ListFavorites(ctx, bookID)
	if err != nil {
		return nil, err
	}
	books, err := s.store.ListBooks(ctx, "")
	if err != nil {
		return nil, err
	}
	bookTitles := make(map[string]string, len(books))
	for _, b := range books {
		bookTitles[b.ID] = b.Title
	}
	entries := make([]domain.WidgetEntry, 0, len(favs))
	for _, f := range favs {
		text := strings.TrimSpace(f.Excerpt)
		largeText := ""
		// 仅当用户未手动划选特定金句行（HighlightLines 为空）时，尝试为 4×4 大号小组件展开多诗节正文
		if text == "" || strings.TrimSpace(f.HighlightLines) == "" {
			if p, err := s.store.GetPoem(ctx, f.BookID, f.PoemID); err == nil {
				if text == "" {
					text = parser.BuildDefaultExcerpt(p)
				}
				if strings.TrimSpace(f.HighlightLines) == "" {
					largeText = parser.BuildLargeExcerpt(p)
				}
			}
		}
		if text == "" {
			continue
		}
		if largeText == text {
			largeText = ""
		}
		link := fmt.Sprintf("stillpoets://poem/%s/%s", f.BookID, f.PoemID)
		if f.HighlightLines != "" {
			link += "?lines=" + f.HighlightLines + "&highlight=1"
		}
		entries = append(entries, domain.WidgetEntry{
			EntryType:   "poem",
			DisplayText: text,
			LargeText:   largeText,
			PoemTitle:   f.PoemTitle,
			Author:      f.Author,
			SourceTitle: bookTitles[f.BookID],
			Deeplink:    link,
		})
	}
	shuffleEntries(entries)
	return limitEntries(entries), nil
}

func (s *Service) buildBookWidgetEntries(ctx context.Context, bookID string, shuffle bool) ([]domain.WidgetEntry, error) {
	if bookID == "" {
		return s.buildGlobalRandomWidgetEntries(ctx)
	}
	book, err := s.store.GetBook(ctx, bookID)
	if err != nil {
		return []domain.WidgetEntry{}, nil
	}
	poems, err := s.store.ListPoems(ctx, book.ID)
	if err != nil {
		return nil, err
	}
	favByPoem, err := s.favoritesByPoem(ctx, book.ID)
	if err != nil {
		return nil, err
	}
	entries := poemsToWidgetEntries(book, poems, favByPoem)
	if shuffle {
		shuffleEntries(entries)
	}
	return limitEntries(entries), nil
}

func (s *Service) buildGlobalRandomWidgetEntries(ctx context.Context) ([]domain.WidgetEntry, error) {
	books, err := s.store.ListBooks(ctx, "")
	if err != nil {
		return nil, err
	}
	favByKey, err := s.favoritesByKey(ctx)
	if err != nil {
		return nil, err
	}
	var entries []domain.WidgetEntry
	for _, book := range books {
		if book.LocalState != domain.LocalStateDownloaded {
			continue
		}
		poems, err := s.store.ListPoems(ctx, book.ID)
		if err != nil {
			continue
		}
		for _, p := range poems {
			if !p.IsPoem() {
				continue
			}
			fav, hasFav := favByKey[book.ID+"/"+p.ID]
			entry, ok := buildSinglePoemWidgetEntry(book, p, fav, hasFav)
			if ok {
				entries = append(entries, entry)
			}
		}
	}
	shuffleEntries(entries)
	return limitEntries(entries), nil
}

func poemsToWidgetEntries(book domain.Book, poems []domain.Poem, favByPoem map[string]domain.FavoritePoem) []domain.WidgetEntry {
	entries := make([]domain.WidgetEntry, 0, len(poems))
	for _, p := range poems {
		if !p.IsPoem() {
			continue
		}
		fav, hasFav := favByPoem[p.ID]
		if entry, ok := buildSinglePoemWidgetEntry(book, p, fav, hasFav); ok {
			entries = append(entries, entry)
		}
	}
	return entries
}

func buildSinglePoemWidgetEntry(book domain.Book, p domain.Poem, fav domain.FavoritePoem, hasFav bool) (domain.WidgetEntry, bool) {
	text := ""
	largeText := ""
	lines := ""
	manualSelection := hasFav && strings.TrimSpace(fav.Excerpt) != "" && strings.TrimSpace(fav.HighlightLines) != ""
	if manualSelection {
		text = strings.TrimSpace(fav.Excerpt)
		lines = fav.HighlightLines
	} else {
		if hasFav && strings.TrimSpace(fav.Excerpt) != "" {
			text = strings.TrimSpace(fav.Excerpt)
		} else {
			text = parser.BuildDefaultExcerpt(p)
		}
		largeText = parser.BuildLargeExcerpt(p)
	}
	if text == "" {
		return domain.WidgetEntry{}, false
	}
	if largeText == text {
		largeText = ""
	}
	author := p.Author
	if author == "" {
		author = book.Author
	}
	link := fmt.Sprintf("stillpoets://poem/%s/%s", book.ID, p.ID)
	if lines != "" {
		link += "?lines=" + lines + "&highlight=1"
	}
	return domain.WidgetEntry{
		EntryType:   "poem",
		DisplayText: text,
		LargeText:   largeText,
		PoemTitle:   p.Title,
		Author:      author,
		SourceTitle: book.Title,
		Deeplink:    link,
	}, true
}

func (s *Service) favoritesByPoem(ctx context.Context, bookID string) (map[string]domain.FavoritePoem, error) {
	favs, err := s.store.ListFavorites(ctx, bookID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]domain.FavoritePoem, len(favs))
	for _, f := range favs {
		out[f.PoemID] = f
	}
	return out, nil
}

func (s *Service) favoritesByKey(ctx context.Context) (map[string]domain.FavoritePoem, error) {
	favs, err := s.store.ListFavorites(ctx, "")
	if err != nil {
		return nil, err
	}
	out := make(map[string]domain.FavoritePoem, len(favs))
	for _, f := range favs {
		out[f.BookID+"/"+f.PoemID] = f
	}
	return out, nil
}

func shuffleEntries(entries []domain.WidgetEntry) {
	if len(entries) <= 1 {
		return
	}
	rand.Shuffle(len(entries), func(i, j int) {
		entries[i], entries[j] = entries[j], entries[i]
	})
}

func limitEntries(entries []domain.WidgetEntry) []domain.WidgetEntry {
	if len(entries) > maxWidgetEntries {
		return entries[:maxWidgetEntries]
	}
	return entries
}

func writeJSONAtomic(target string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// 使用唯一临时文件避免并发写入同一 .tmp 互相覆盖或重命名失败（P1-5）
	f, err := os.CreateTemp(dir, filepath.Base(target)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := f.Name()
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func stableDayHash(dayKey string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(dayKey))
	return h.Sum64()
}
