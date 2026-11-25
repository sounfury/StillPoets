package app

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

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

	dayKey := time.Now().Format("2006-01-02")
	poolRevision := widgetPoolRevision(settings)
	shuffleSeed := fmt.Sprintf("%s|%s", poolRevision, dayKey)

	entries, err := s.buildWidgetEntries(ctx, settings, shuffleSeed)
	if err != nil {
		return domain.WidgetTimeline{}, err
	}

	timeline := domain.WidgetTimeline{
		GeneratedAt:            domain.NowMillis(),
		PoolRevision:           poolRevision,
		RefreshIntervalMinutes: domain.NormalizeWidgetRefreshMinutes(settings.WidgetRefreshMinutes),
		Theme:                  wTheme,
		Entries:                entries,
	}

	timelinePath := filepath.Join(s.dataDir, widgetTimelineFilename)
	unchanged := false
	if prev, err := s.ReadWidgetTimeline(); err == nil && widgetTimelineEquivalent(prev, timeline) {
		// 诗池、样式、切换间隔与条目顺序均未变化：沿用原快照时间戳，不反复覆盖磁盘也不无故触发小组件重绘
		timeline.GeneratedAt = prev.GeneratedAt
		unchanged = true
	}

	if !unchanged {
		if err := writeJSONAtomic(timelinePath, timeline); err != nil {
			return domain.WidgetTimeline{}, fmt.Errorf("app: write widget timeline: %w", err)
		}
	}

	shouldNotify := !unchanged || !s.widgetSyncedOnce
	s.widgetSyncedOnce = true
	if shouldNotify {
		s.mu.Lock()
		cb := s.onWidgetUpdated
		s.mu.Unlock()
		if cb != nil {
			cb(timelinePath, s.FontsDir())
		}
	}
	return timeline, nil
}

// ReshuffleWidgetTimeline 递增本地换诗代次并立刻重建小组件快照（仅用于用户显式点击「刷新小组件」）。
func (s *Service) ReshuffleWidgetTimeline(ctx context.Context) (domain.WidgetTimeline, error) {
	if _, err := s.store.UpdateSettings(ctx, func(st *domain.Settings) {
		st.WidgetShuffleEpoch++
	}); err != nil {
		return domain.WidgetTimeline{}, err
	}
	return s.RebuildWidgetTimeline(ctx)
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

func (s *Service) buildWidgetEntries(ctx context.Context, settings domain.Settings, shuffleSeed string) ([]domain.WidgetEntry, error) {
	switch settings.WidgetMode {
	case domain.WidgetModeQuotesRandom:
		return s.buildQuoteWidgetEntries(ctx, shuffleSeed)
	case domain.WidgetModeBookFavorites:
		return s.buildFavoriteWidgetEntries(ctx, settings.WidgetBookID, shuffleSeed)
	case domain.WidgetModeBookRandom:
		return s.buildBookWidgetEntries(ctx, settings.WidgetBookID, true, shuffleSeed)
	case domain.WidgetModeBookSequential:
		return s.buildBookWidgetEntries(ctx, settings.WidgetBookID, false, shuffleSeed)
	default:
		return s.buildGlobalRandomWidgetEntries(ctx, shuffleSeed)
	}
}

func (s *Service) buildQuoteWidgetEntries(ctx context.Context, shuffleSeed string) ([]domain.WidgetEntry, error) {
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
	sortEntriesDeterministically(entries, shuffleSeed)
	return limitEntries(entries), nil
}

func (s *Service) buildFavoriteWidgetEntries(ctx context.Context, bookID, shuffleSeed string) ([]domain.WidgetEntry, error) {
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
	sortEntriesDeterministically(entries, shuffleSeed)
	return limitEntries(entries), nil
}

func (s *Service) buildBookWidgetEntries(ctx context.Context, bookID string, shuffle bool, shuffleSeed string) ([]domain.WidgetEntry, error) {
	if bookID == "" {
		return s.buildGlobalRandomWidgetEntries(ctx, shuffleSeed)
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
		sortEntriesDeterministically(entries, shuffleSeed)
	}
	return limitEntries(entries), nil
}

func (s *Service) buildGlobalRandomWidgetEntries(ctx context.Context, shuffleSeed string) ([]domain.WidgetEntry, error) {
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
	sortEntriesDeterministically(entries, shuffleSeed)
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

func widgetPoolRevision(settings domain.Settings) string {
	mode := settings.WidgetMode
	if mode == "" {
		mode = domain.WidgetModeGlobalRandom
	}
	return fmt.Sprintf("%s|%s|%d", mode, settings.WidgetBookID, settings.WidgetShuffleEpoch)
}

func widgetTimelineEquivalent(a, b domain.WidgetTimeline) bool {
	return a.PoolRevision == b.PoolRevision &&
		a.RefreshIntervalMinutes == b.RefreshIntervalMinutes &&
		a.Theme == b.Theme &&
		slices.Equal(a.Entries, b.Entries)
}

// sortEntriesDeterministically 使用一致性哈希按 (seed, entry) 确定性排序，替代无种子真随机洗牌：
//  1. 同一天同一诗池下，无论冷启动、WebDAV 同步或修改主题/字号/间隔触发多少次重建，条目顺序 100% 稳定，
//     确保当前 3 小时（或任意设定间隔）槽位内的诗句不会提前被洗走；
//  2. 即使当天新增/删除少量诗篇或红心，其余诗篇的相对顺序依然高度稳定，不会全盘洗牌；
//  3. 跨自然日或用户主动切池/点击「刷新小组件」时 seed 变化，平滑迎来新一批诗句。
func sortEntriesDeterministically(entries []domain.WidgetEntry, seed string) {
	if len(entries) <= 1 {
		return
	}
	type ranked struct {
		entry domain.WidgetEntry
		rank  uint64
	}
	items := make([]ranked, len(entries))
	for i, e := range entries {
		items[i] = ranked{
			entry: e,
			rank:  widgetEntryHash(seed, e),
		}
	}
	slices.SortFunc(items, func(a, b ranked) int {
		if a.rank < b.rank {
			return -1
		}
		if a.rank > b.rank {
			return 1
		}
		if c := strings.Compare(a.entry.Deeplink, b.entry.Deeplink); c != 0 {
			return c
		}
		return strings.Compare(a.entry.DisplayText, b.entry.DisplayText)
	})
	for i := range items {
		entries[i] = items[i].entry
	}
}

func widgetEntryHash(seed string, e domain.WidgetEntry) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(seed))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(e.Deeplink))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(e.DisplayText))
	return h.Sum64()
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
