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
	settings, err := s.store.LoadSettings(ctx)
	if err != nil {
		return domain.WidgetTimeline{}, err
	}
	pal := theme.ByID(theme.PresetID(settings.ThemePreset))
	wTheme := settings.WidgetTheme
	if wTheme.Preset == "" {
		wTheme.Preset = string(pal.ID)
	}
	if wTheme.BgColor == "" {
		wTheme.BgColor = pal.BgHex
	}
	if wTheme.TextColor == "" {
		wTheme.TextColor = pal.TextHex
	}
	if wTheme.AccentColor == "" {
		wTheme.AccentColor = pal.AccentHex
	}
	if wTheme.FontFamily == "" {
		wTheme.FontFamily = settings.FontFamily
	}

	entries, err := s.buildWidgetEntries(ctx, settings)
	if err != nil {
		return domain.WidgetTimeline{}, err
	}

	timeline := domain.WidgetTimeline{
		GeneratedAt: domain.NowMillis(),
		Theme:       wTheme,
		Entries:     entries,
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
		if text == "" {
			if p, err := s.store.GetPoem(ctx, f.BookID, f.PoemID); err == nil {
				text = parser.BuildDefaultExcerpt(p)
			}
		}
		if text == "" {
			continue
		}
		link := fmt.Sprintf("stillpoets://poem/%s/%s", f.BookID, f.PoemID)
		if f.HighlightLines != "" {
			link += "?lines=" + f.HighlightLines + "&highlight=1"
		}
		entries = append(entries, domain.WidgetEntry{
			EntryType:   "poem",
			DisplayText: text,
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
	lines := ""
	if hasFav && strings.TrimSpace(fav.Excerpt) != "" {
		text = strings.TrimSpace(fav.Excerpt)
		lines = fav.HighlightLines
	} else {
		text = parser.BuildDefaultExcerpt(p)
	}
	if text == "" {
		return domain.WidgetEntry{}, false
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
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func stableDayHash(dayKey string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(dayKey))
	return h.Sum64()
}
