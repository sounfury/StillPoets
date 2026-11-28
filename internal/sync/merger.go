package sync

import (
	"slices"
	"strings"

	"stillpoets/internal/domain"
)

// MergeSnapshotData 对两份元数据载荷执行确定性合并。
// 所有集合与字段级合并均满足交换律、结合律与幂等性，结果与拉取顺序无关。
func MergeSnapshotData(a, b SnapshotData) SnapshotData {
	return SnapshotData{
		Groups:        MergeGroups(a.Groups, b.Groups),
		Books:         MergeBooks(a.Books, b.Books),
		Favorites:     MergeFavorites(a.Favorites, b.Favorites),
		Quotes:        MergeQuotes(a.Quotes, b.Quotes),
		Progress:      MergeProgress(a.Progress, b.Progress),
		ReadingEvents: MergeReadingEvents(a.ReadingEvents, b.ReadingEvents),
	}
}

// MaxCounterInData 扫描快照载荷中的所有逻辑版本号，返回最大 Lamport 计数器值。
func MaxCounterInData(base uint64, d SnapshotData) uint64 {
	m := base
	for _, g := range d.Groups {
		m = domain.MaxCounterOf(m, g.NameVer, g.SortVer, g.DeletedVer)
	}
	for _, b := range d.Books {
		m = domain.MaxCounterOf(m, b.ContentVer, b.GroupVer, b.StarredVer, b.SortVer, b.DeletedVer)
	}
	for _, f := range d.Favorites {
		m = domain.MaxCounterOf(m, f.ActiveVer, f.ExcerptVer)
	}
	for _, q := range d.Quotes {
		m = domain.MaxCounterOf(m, q.BodyVer, q.FavoriteVer, q.DeletedVer)
	}
	for _, p := range d.Progress {
		m = domain.MaxCounterOf(m, p.PositionVer, p.StatusVer)
	}
	return m
}

// MergeGroups 按 groupId 合并诗集分组。
// Name 与 SortOrder 独立版本化；Deleted 对同一 groupId 永久生效（吸收状态）。
func MergeGroups(a, b []domain.BookGroup) []domain.BookGroup {
	merged := make(map[string]domain.BookGroup, len(a)+len(b))
	insert := func(in domain.BookGroup) {
		if in.ID == "" {
			return
		}
		cur, exists := merged[in.ID]
		if !exists {
			merged[in.ID] = in
			return
		}
		merged[in.ID] = mergeOneGroup(cur, in)
	}
	for _, g := range a {
		insert(g)
	}
	for _, g := range b {
		insert(g)
	}

	out := make([]domain.BookGroup, 0, len(merged))
	for _, g := range merged {
		out = append(out, g)
	}
	slices.SortFunc(out, func(x, y domain.BookGroup) int {
		if x.SortOrder != y.SortOrder {
			return x.SortOrder - y.SortOrder
		}
		return strings.Compare(x.ID, y.ID)
	})
	return out
}

func mergeOneGroup(x, y domain.BookGroup) domain.BookGroup {
	out := domain.BookGroup{
		ID:        x.ID,
		CreatedAt: minPositive(x.CreatedAt, y.CreatedAt),
	}
	out.Name, out.NameVer = mergeVersionedString(x.Name, x.NameVer, y.Name, y.NameVer)
	out.SortOrder, out.SortVer = mergeVersionedInt(x.SortOrder, x.SortVer, y.SortOrder, y.SortVer)
	out.Deleted, out.DeletedAt, out.DeletedVer = mergePermanentDelete(
		x.Deleted, x.DeletedAt, x.DeletedVer,
		y.Deleted, y.DeletedAt, y.DeletedVer,
	)
	return out
}

func mergeVersionedString(xv string, xVer domain.Version, yv string, yVer domain.Version) (string, domain.Version) {
	if cmp := xVer.Compare(yVer); cmp > 0 || (cmp == 0 && xv >= yv) {
		return xv, xVer
	}
	return yv, yVer
}

func mergeVersionedInt(xv int, xVer domain.Version, yv int, yVer domain.Version) (int, domain.Version) {
	if cmp := xVer.Compare(yVer); cmp > 0 || (cmp == 0 && xv >= yv) {
		return xv, xVer
	}
	return yv, yVer
}

// MergeBooks 按 bookId 合并纯诗集元数据（不混入本机 LocalState / LocalContentHash）。
// GroupID、IsStarred、SortOrder、Content 描述组各自独立版本化；删除对同一 bookId 永久生效。
func MergeBooks(a, b []domain.Book) []domain.Book {
	merged := make(map[string]domain.Book, len(a)+len(b))
	insert := func(in domain.Book) {
		if in.ID == "" {
			return
		}
		// 剥离本机状态，确保纯元数据合并与设备缓存无关
		in.LocalState = ""
		in.LocalContentHash = ""
		cur, exists := merged[in.ID]
		if !exists {
			merged[in.ID] = in
			return
		}
		merged[in.ID] = mergeOneBook(cur, in)
	}
	for _, item := range a {
		insert(item)
	}
	for _, item := range b {
		insert(item)
	}

	out := make([]domain.Book, 0, len(merged))
	for _, item := range merged {
		out = append(out, item)
	}
	slices.SortFunc(out, func(x, y domain.Book) int {
		if x.SortOrder != y.SortOrder {
			return x.SortOrder - y.SortOrder
		}
		return strings.Compare(x.ID, y.ID)
	})
	return out
}

func mergeOneBook(x, y domain.Book) domain.Book {
	out := domain.Book{
		ID:        x.ID,
		CreatedAt: minPositive(x.CreatedAt, y.CreatedAt),
	}

	// 导入内容描述组
	useXContent := false
	if cmp := x.ContentVer.Compare(y.ContentVer); cmp > 0 {
		useXContent = true
	} else if cmp == 0 {
		if x.ContentHash != y.ContentHash {
			useXContent = x.ContentHash > y.ContentHash
		} else if x.ContentPath != y.ContentPath {
			useXContent = x.ContentPath > y.ContentPath
		} else {
			useXContent = x.Title >= y.Title
		}
	}
	if useXContent {
		out.Title = x.Title
		out.Author = x.Author
		out.DynastyOrGenre = x.DynastyOrGenre
		out.CoverHash = x.CoverHash
		out.ContentHash = x.ContentHash
		out.ContentSize = x.ContentSize
		out.ContentPath = x.ContentPath
		out.RawFilename = x.RawFilename
		out.RawSize = x.RawSize
		out.PoemCount = x.PoemCount
		out.ContentVer = x.ContentVer
	} else {
		out.Title = y.Title
		out.Author = y.Author
		out.DynastyOrGenre = y.DynastyOrGenre
		out.CoverHash = y.CoverHash
		out.ContentHash = y.ContentHash
		out.ContentSize = y.ContentSize
		out.ContentPath = y.ContentPath
		out.RawFilename = y.RawFilename
		out.RawSize = y.RawSize
		out.PoemCount = y.PoemCount
		out.ContentVer = y.ContentVer
	}

	// 独立可编辑字段：GroupID / IsStarred / SortOrder
	out.GroupID, out.GroupVer = mergeVersionedString(x.GroupID, x.GroupVer, y.GroupID, y.GroupVer)

	if cmp := x.StarredVer.Compare(y.StarredVer); cmp > 0 || (cmp == 0 && (x.IsStarred || !y.IsStarred)) {
		out.IsStarred, out.StarredVer = x.IsStarred, x.StarredVer
	} else {
		out.IsStarred, out.StarredVer = y.IsStarred, y.StarredVer
	}

	out.SortOrder, out.SortVer = mergeVersionedInt(x.SortOrder, x.SortVer, y.SortOrder, y.SortVer)

	out.Deleted, out.DeletedAt, out.DeletedVer = mergePermanentDelete(
		x.Deleted, x.DeletedAt, x.DeletedVer,
		y.Deleted, y.DeletedAt, y.DeletedVer,
	)
	return out
}

// MergeFavorites 按复合主键 (BookID, PoemID) 合并诗篇精选池 ❤️。
// 精选开关（Active/ActiveVer）与节选内容（Excerpt/HighlightLines/IsCustomQuote/ExcerptVer）独立合并：
// 编辑节选不会恢复已取消的收藏，只有显式重新点亮才会提升 ActiveVer。
func MergeFavorites(a, b []domain.FavoritePoem) []domain.FavoritePoem {
	keyOf := func(f domain.FavoritePoem) string {
		return f.BookID + "\x00" + f.PoemID
	}
	merged := make(map[string]domain.FavoritePoem, len(a)+len(b))
	insert := func(in domain.FavoritePoem) {
		if in.BookID == "" || in.PoemID == "" {
			return
		}
		k := keyOf(in)
		cur, exists := merged[k]
		if !exists {
			merged[k] = in
			return
		}
		merged[k] = mergeOneFavorite(cur, in)
	}
	for _, f := range a {
		insert(f)
	}
	for _, f := range b {
		insert(f)
	}

	out := make([]domain.FavoritePoem, 0, len(merged))
	for _, f := range merged {
		out = append(out, f)
	}
	slices.SortFunc(out, func(x, y domain.FavoritePoem) int {
		if x.BookID != y.BookID {
			return strings.Compare(x.BookID, y.BookID)
		}
		return strings.Compare(x.PoemID, y.PoemID)
	})
	return out
}

func mergeOneFavorite(x, y domain.FavoritePoem) domain.FavoritePoem {
	out := domain.FavoritePoem{
		ID:        pickStableID(x.ID, y.ID),
		BookID:    x.BookID,
		PoemID:    x.PoemID,
		CreatedAt: minPositive(x.CreatedAt, y.CreatedAt),
	}

	// 1. 收藏成员状态（Active / ActiveVer）
	if cmp := x.ActiveVer.Compare(y.ActiveVer); cmp > 0 || (cmp == 0 && (!x.Active || y.Active)) {
		out.Active = x.Active
		out.ActiveVer = x.ActiveVer
		out.DeletedAt = x.DeletedAt
	} else {
		out.Active = y.Active
		out.ActiveVer = y.ActiveVer
		out.DeletedAt = y.DeletedAt
	}

	// 2. 节选与高亮行（ExcerptVer 独立比较，永不改写 Active）
	if cmp := x.ExcerptVer.Compare(y.ExcerptVer); cmp > 0 || (cmp == 0 && x.Excerpt >= y.Excerpt) {
		out.Excerpt = x.Excerpt
		out.HighlightLines = x.HighlightLines
		out.IsCustomQuote = x.IsCustomQuote
		out.ExcerptVer = x.ExcerptVer
	} else {
		out.Excerpt = y.Excerpt
		out.HighlightLines = y.HighlightLines
		out.IsCustomQuote = y.IsCustomQuote
		out.ExcerptVer = y.ExcerptVer
	}

	// 3. 冗余展示字段（诗题/作者）从版本较新的一方取值，缺省时用另一方补齐
	xMax := maxVer(x.ActiveVer, x.ExcerptVer)
	yMax := maxVer(y.ActiveVer, y.ExcerptVer)
	if cmp := xMax.Compare(yMax); cmp > 0 || (cmp == 0 && x.PoemTitle >= y.PoemTitle) {
		out.PoemTitle = firstNonEmpty(x.PoemTitle, y.PoemTitle)
		out.Author = firstNonEmpty(x.Author, y.Author)
	} else {
		out.PoemTitle = firstNonEmpty(y.PoemTitle, x.PoemTitle)
		out.Author = firstNonEmpty(y.Author, x.Author)
	}
	return out
}

// MergeQuotes 按 quoteId 合并入口 B 独立句子库。
// BodyVer 与 FavoriteVer 独立合并；删除对同一 quoteId 永久生效（吸收状态），普通编辑不可复活。
func MergeQuotes(a, b []domain.Quote) []domain.Quote {
	merged := make(map[string]domain.Quote, len(a)+len(b))
	insert := func(in domain.Quote) {
		if in.ID == "" {
			return
		}
		in.Tags = normalizeStringSet(in.Tags)
		cur, exists := merged[in.ID]
		if !exists {
			merged[in.ID] = in
			return
		}
		merged[in.ID] = mergeOneQuote(cur, in)
	}
	for _, q := range a {
		insert(q)
	}
	for _, q := range b {
		insert(q)
	}

	out := make([]domain.Quote, 0, len(merged))
	for _, q := range merged {
		out = append(out, q)
	}
	slices.SortFunc(out, func(x, y domain.Quote) int {
		return strings.Compare(x.ID, y.ID)
	})
	return out
}

func mergeOneQuote(x, y domain.Quote) domain.Quote {
	out := domain.Quote{
		ID:        x.ID,
		CreatedAt: minPositive(x.CreatedAt, y.CreatedAt),
	}

	if cmp := x.BodyVer.Compare(y.BodyVer); cmp > 0 || (cmp == 0 && x.Content >= y.Content) {
		out.Kind = x.Kind
		out.Content = x.Content
		out.Author = x.Author
		out.SourceBook = x.SourceBook
		out.BookID = x.BookID
		out.PoemID = x.PoemID
		out.PoemTitle = x.PoemTitle
		out.HighlightLines = x.HighlightLines
		out.ImportSource = x.ImportSource
		out.Tags = slices.Clone(x.Tags)
		out.BodyVer = x.BodyVer
	} else {
		out.Kind = y.Kind
		out.Content = y.Content
		out.Author = y.Author
		out.SourceBook = y.SourceBook
		out.BookID = y.BookID
		out.PoemID = y.PoemID
		out.PoemTitle = y.PoemTitle
		out.HighlightLines = y.HighlightLines
		out.ImportSource = y.ImportSource
		out.Tags = slices.Clone(y.Tags)
		out.BodyVer = y.BodyVer
	}

	if cmp := x.FavoriteVer.Compare(y.FavoriteVer); cmp > 0 || (cmp == 0 && (x.IsFavorite || !y.IsFavorite)) {
		out.IsFavorite, out.FavoriteVer = x.IsFavorite, x.FavoriteVer
	} else {
		out.IsFavorite, out.FavoriteVer = y.IsFavorite, y.FavoriteVer
	}

	out.Deleted, out.DeletedAt, out.DeletedVer = mergePermanentDelete(
		x.Deleted, x.DeletedAt, x.DeletedVer,
		y.Deleted, y.DeletedAt, y.DeletedVer,
	)
	return out
}

// MergeProgress 按 bookId 合并阅读进度：
// 1. LastPoemID（PositionVer）独立合并；
// 2. Status + FinishedAt（StatusVer）独立合并，翻阅旧诗不会倒退完读状态；
// 3. ReadPoemIDs 按并集去重排序合并，双端精确统计已读诗篇数。
func MergeProgress(a, b []domain.ReadingProgress) []domain.ReadingProgress {
	merged := make(map[string]domain.ReadingProgress, len(a)+len(b))
	insert := func(in domain.ReadingProgress) {
		if in.BookID == "" {
			return
		}
		in.LocalScrollRatio = 0
		in.ReadPoemIDs = normalizeStringSet(in.ReadPoemIDs)
		cur, exists := merged[in.BookID]
		if !exists {
			merged[in.BookID] = in
			return
		}
		merged[in.BookID] = mergeOneProgress(cur, in)
	}
	for _, p := range a {
		insert(p)
	}
	for _, p := range b {
		insert(p)
	}

	out := make([]domain.ReadingProgress, 0, len(merged))
	for _, p := range merged {
		out = append(out, p)
	}
	slices.SortFunc(out, func(x, y domain.ReadingProgress) int {
		return strings.Compare(x.BookID, y.BookID)
	})
	return out
}

func mergeOneProgress(x, y domain.ReadingProgress) domain.ReadingProgress {
	out := domain.ReadingProgress{
		BookID: x.BookID,
	}

	if cmp := x.PositionVer.Compare(y.PositionVer); cmp > 0 || (cmp == 0 && x.LastPoemID >= y.LastPoemID) {
		out.LastPoemID = x.LastPoemID
		out.PositionVer = x.PositionVer
	} else {
		out.LastPoemID = y.LastPoemID
		out.PositionVer = y.PositionVer
	}

	useXStatus := false
	if cmp := x.StatusVer.Compare(y.StatusVer); cmp > 0 {
		useXStatus = true
	} else if cmp == 0 {
		if x.FinishedAt != y.FinishedAt {
			useXStatus = x.FinishedAt > y.FinishedAt
		} else {
			useXStatus = string(x.Status) >= string(y.Status)
		}
	}
	if useXStatus {
		out.Status = x.Status
		out.FinishedAt = x.FinishedAt
		out.StatusVer = x.StatusVer
	} else {
		out.Status = y.Status
		out.FinishedAt = y.FinishedAt
		out.StatusVer = y.StatusVer
	}

	// 已读 poemId 集合取并集（去重+有序）
	combined := make([]string, 0, len(x.ReadPoemIDs)+len(y.ReadPoemIDs))
	combined = append(combined, x.ReadPoemIDs...)
	combined = append(combined, y.ReadPoemIDs...)
	out.ReadPoemIDs = normalizeStringSet(combined)
	return out
}

// MergeReadingEvents 按 (BookID, PoemID, ReadDate) 单日去重并确定性合并跨端有效读诗事件。
// 同一天同一首诗在多台设备均被阅读时，按 (ReadAt ASC, ID ASC) 选取最早发生的那条为主记录，
// 并按序补全缺失的 PoemTitle / Author / DeviceID，严格满足交换律、结合律与幂等性。
func MergeReadingEvents(a, b []domain.ReadingEvent) []domain.ReadingEvent {
	type dayKey struct {
		bookID   string
		poemID   string
		readDate string
	}
	merged := make(map[dayKey]domain.ReadingEvent, len(a)+len(b))
	insert := func(in domain.ReadingEvent) {
		in.BookID = strings.TrimSpace(in.BookID)
		in.PoemID = strings.TrimSpace(in.PoemID)
		in.ReadDate = strings.TrimSpace(in.ReadDate)
		if in.BookID == "" || in.PoemID == "" || in.ReadDate == "" {
			return
		}
		if in.ReadHour < 0 || in.ReadHour > 23 {
			in.ReadHour = 0
		}
		key := dayKey{bookID: in.BookID, poemID: in.PoemID, readDate: in.ReadDate}
		cur, exists := merged[key]
		if !exists {
			merged[key] = in
			return
		}
		merged[key] = mergeOneReadingEvent(cur, in)
	}
	for _, ev := range a {
		insert(ev)
	}
	for _, ev := range b {
		insert(ev)
	}

	out := make([]domain.ReadingEvent, 0, len(merged))
	for _, ev := range merged {
		out = append(out, ev)
	}
	slices.SortFunc(out, compareReadingEvents)
	return out
}

func compareReadingEvents(x, y domain.ReadingEvent) int {
	if cmp := strings.Compare(x.ReadDate, y.ReadDate); cmp != 0 {
		return cmp
	}
	if x.ReadAt != y.ReadAt {
		if x.ReadAt < y.ReadAt {
			return -1
		}
		return 1
	}
	if cmp := strings.Compare(x.BookID, y.BookID); cmp != 0 {
		return cmp
	}
	if cmp := strings.Compare(x.PoemID, y.PoemID); cmp != 0 {
		return cmp
	}
	return strings.Compare(x.ID, y.ID)
}

func mergeOneReadingEvent(x, y domain.ReadingEvent) domain.ReadingEvent {
	winner, loser := x, y
	if readingEventPrecedes(y, x) {
		winner, loser = y, x
	}
	winner.PoemTitle = firstNonEmpty(winner.PoemTitle, loser.PoemTitle)
	winner.Author = firstNonEmpty(winner.Author, loser.Author)
	winner.DeviceID = firstNonEmpty(winner.DeviceID, loser.DeviceID)
	winner.ID = firstNonEmpty(winner.ID, loser.ID)
	return winner
}

func readingEventPrecedes(a, b domain.ReadingEvent) bool {
	if a.ReadAt > 0 && b.ReadAt > 0 && a.ReadAt != b.ReadAt {
		return a.ReadAt < b.ReadAt
	}
	if (a.ReadAt > 0) != (b.ReadAt > 0) {
		return a.ReadAt > 0
	}
	return a.ID < b.ID
}

// mergePermanentDelete 实现永久删除（吸收状态）：只要任意一方已删除，合并结果即永久保持 Deleted=true。
func mergePermanentDelete(
	xDel bool, xAt int64, xVer domain.Version,
	yDel bool, yAt int64, yVer domain.Version,
) (bool, int64, domain.Version) {
	switch {
	case !xDel && !yDel:
		return false, 0, domain.Version{}
	case xDel && !yDel:
		return true, xAt, xVer
	case !xDel && yDel:
		return true, yAt, yVer
	default:
		if cmp := xVer.Compare(yVer); cmp > 0 || (cmp == 0 && xAt >= yAt) {
			return true, xAt, xVer
		}
		return true, yAt, yVer
	}
}

func minPositive(a, b int64) int64 {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}

func maxVer(a, b domain.Version) domain.Version {
	if a.Compare(b) >= 0 {
		return a
	}
	return b
}

func pickStableID(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	if a <= b {
		return a
	}
	return b
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func normalizeStringSet(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, s := range items {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	slices.Sort(out)
	return out
}
