package storage

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"stillpoets/internal/domain"
)

func buildHeatmap(events []domain.ReadingEvent, year int, now time.Time) HeatmapData {
	loc := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	todayStr := today.Format("2006-01-02")

	type dayAgg struct {
		total   int
		initial int
		reread  int
	}
	byDate := make(map[string]*dayAgg)
	yearSet := map[int]struct{}{now.Year(): {}}
	for _, ev := range events {
		if len(ev.ReadDate) >= 4 {
			if y, err := strconv.Atoi(ev.ReadDate[:4]); err == nil && y > 1970 {
				yearSet[y] = struct{}{}
			}
		}
		agg := byDate[ev.ReadDate]
		if agg == nil {
			agg = &dayAgg{}
			byDate[ev.ReadDate] = agg
		}
		agg.total++
		if ev.IsReread {
			agg.reread++
		} else {
			agg.initial++
		}
	}

	availYears := make([]int, 0, len(yearSet))
	for y := range yearSet {
		availYears = append(availYears, y)
	}
	slices.Sort(availYears)

	var (
		rangeStart time.Time
		rangeEnd   time.Time
		gridStart  time.Time
		gridEnd    time.Time
		modeLabel  string
	)
	if year <= 0 {
		// 默认近 52 周滚动视图，右端自然对齐本周日
		offset := (int(today.Weekday()) + 6) % 7
		thisMonday := today.AddDate(0, 0, -offset)
		gridStart = thisMonday.AddDate(0, 0, -51*7)
		gridEnd = thisMonday.AddDate(0, 0, 6)
		rangeStart = gridStart
		rangeEnd = today
		modeLabel = "近一年"
	} else {
		rangeStart = time.Date(year, 1, 1, 0, 0, 0, 0, loc)
		rangeEnd = time.Date(year, 12, 31, 0, 0, 0, 0, loc)
		startOffset := (int(rangeStart.Weekday()) + 6) % 7
		gridStart = rangeStart.AddDate(0, 0, -startOffset)
		endOffset := (7 - ((int(rangeEnd.Weekday()) + 6) % 7) - 1)
		gridEnd = rangeEnd.AddDate(0, 0, endOffset)
		modeLabel = fmt.Sprintf("%d 年", year)
	}

	hm := HeatmapData{
		Year:           year,
		ModeLabel:      modeLabel,
		AvailableYears: availYears,
	}

	lastLabeledMonth := time.Month(0)
	for weekStart := gridStart; !weekStart.After(gridEnd); weekStart = weekStart.AddDate(0, 0, 7) {
		var w HeatmapWeek
		for d := 0; d < 7; d++ {
			cur := weekStart.AddDate(0, 0, d)
			dateStr := cur.Format("2006-01-02")
			outOfRange := cur.Before(rangeStart) || cur.After(rangeEnd)
			inFuture := cur.After(today)

			// 标注每个自然月首次出现的列
			if !outOfRange && (cur.Day() == 1 || (len(hm.Weeks) == 0 && d == 0)) && cur.Month() != lastLabeledMonth {
				w.MonthLabel = fmt.Sprintf("%d月", int(cur.Month()))
				lastLabeledMonth = cur.Month()
			}

			cell := HeatmapDay{
				Date:       dateStr,
				InFuture:   inFuture,
				OutOfRange: outOfRange,
			}
			if agg := byDate[dateStr]; !outOfRange && !inFuture && agg != nil && agg.total > 0 {
				cell.Count = agg.total
				cell.InitialCount = agg.initial
				cell.RereadCount = agg.reread
				cell.Level = heatmapLevel(agg.total)
				hm.ActiveDays++
				hm.TotalEvents += agg.total
				if agg.total > hm.MaxDayCount {
					hm.MaxDayCount = agg.total
				}
				if dateStr <= todayStr && dateStr > hm.LatestActiveDate {
					hm.LatestActiveDate = dateStr
				}
			}
			w.Days[d] = cell
		}
		hm.Weeks = append(hm.Weeks, w)
	}
	return hm
}

func heatmapLevel(count int) int {
	switch {
	case count <= 0:
		return 0
	case count <= 2:
		return 1
	case count <= 5:
		return 2
	case count <= 10:
		return 3
	default:
		return 4
	}
}

func buildReadingRhythm(events []domain.ReadingEvent) ReadingRhythm {
	r := ReadingRhythm{
		TotalEvents: len(events),
		Slots: [5]TimeSlotStat{
			{Key: "dawn", Label: "清晨", RangeText: "05:00–09:00"},
			{Key: "morning", Label: "上午", RangeText: "09:00–12:00"},
			{Key: "afternoon", Label: "午后", RangeText: "12:00–18:00"},
			{Key: "evening", Label: "入夜", RangeText: "18:00–23:00"},
			{Key: "late_night", Label: "深夜", RangeText: "23:00–05:00"},
		},
	}
	if len(events) == 0 {
		r.Insight = "开始静静读几首诗后，这里会浮现你的昼夜读诗节律。"
		return r
	}

	for _, ev := range events {
		h := ev.ReadHour
		if h < 0 || h > 23 {
			h = 0
		}
		r.Hourly[h]++
		r.Slots[timeSlotIndex(h)].Count++
	}

	topSlotIdx := 0
	for i := range r.Slots {
		r.Slots[i].Ratio = float64(r.Slots[i].Count) / float64(len(events))
		if r.Slots[i].Count > r.Slots[topSlotIdx].Count {
			topSlotIdx = i
		}
	}
	for h, cnt := range r.Hourly {
		if cnt > r.PeakHourCount {
			r.PeakHour = h
			r.PeakHourCount = cnt
		}
	}

	// 计算最集中的连续 4 小时窗口，生成克制的个人节律洞察文案
	bestStart, bestSum := 0, -1
	for start := 0; start < 24; start++ {
		sum := 0
		for k := 0; k < 4; k++ {
			sum += r.Hourly[(start+k)%24]
		}
		if sum > bestSum {
			bestSum = sum
			bestStart = start
		}
	}
	bestEnd := (bestStart + 4) % 24
	pct := int(math.Round(float64(bestSum) * 100 / float64(len(events))))
	r.Insight = fmt.Sprintf("你偏爱在%s时分读诗，%02d:00–%02d:00 贡献了 %d%% 的读诗时刻。",
		r.Slots[topSlotIdx].Label, bestStart, bestEnd, pct)
	return r
}

func timeSlotIndex(hour int) int {
	switch {
	case hour >= 5 && hour < 9:
		return 0 // 清晨
	case hour >= 9 && hour < 12:
		return 1 // 上午
	case hour >= 12 && hour < 18:
		return 2 // 午后
	case hour >= 18 && hour < 23:
		return 3 // 入夜
	default:
		return 4 // 深夜 23..04
	}
}

type authorCloudInput struct {
	windowEvents        []domain.ReadingEvent
	historicalReadPairs map[poemPair]struct{}
	includeHistorical   bool
	windowFavorites     []domain.FavoritePoem
	windowQuotes        []domain.Quote
	bookMap             map[string]domain.Book
	poemAuthors         map[poemPair]string
	mode                AuthorWeightMode
}

func buildAuthorCloud(in authorCloudInput) []AuthorStat {
	windowEvents := in.windowEvents
	historicalReadPairs := in.historicalReadPairs
	includeHistorical := in.includeHistorical
	windowFavorites := in.windowFavorites
	windowQuotes := in.windowQuotes
	bookMap := in.bookMap
	poemAuthors := in.poemAuthors
	mode := in.mode
	type acc struct {
		readPairs map[poemPair]struct{}
		books     map[string]struct{}
		favCount  int
		quoteCnt  int
	}
	byAuthor := make(map[string]*acc)
	ensure := func(name string) *acc {
		a := byAuthor[name]
		if a == nil {
			a = &acc{
				readPairs: make(map[poemPair]struct{}),
				books:     make(map[string]struct{}),
			}
			byAuthor[name] = a
		}
		return a
	}

	for _, ev := range windowEvents {
		b := bookMap[ev.BookID]
		poet := resolvePoemAuthor(ev.Author, poemAuthors[poemPair{bookID: ev.BookID, poemID: ev.PoemID}], b.Author)
		if poet == "" {
			continue
		}
		a := ensure(poet)
		a.readPairs[poemPair{bookID: ev.BookID, poemID: ev.PoemID}] = struct{}{}
		a.books[ev.BookID] = struct{}{}
	}

	if includeHistorical {
		for pair := range historicalReadPairs {
			b, ok := bookMap[pair.bookID]
			if !ok {
				continue
			}
			poet := resolvePoemAuthor("", poemAuthors[pair], b.Author)
			if poet == "" {
				continue
			}
			a := ensure(poet)
			a.readPairs[pair] = struct{}{}
			a.books[pair.bookID] = struct{}{}
		}
	}

	for _, f := range windowFavorites {
		b := bookMap[f.BookID]
		poet := resolvePoemAuthor(f.Author, poemAuthors[poemPair{bookID: f.BookID, poemID: f.PoemID}], b.Author)
		if poet == "" {
			continue
		}
		a := ensure(poet)
		a.favCount++
		a.books[f.BookID] = struct{}{}
	}

	for _, q := range windowQuotes {
		poet := normalizePoetName(q.Author)
		if poet == "" {
			continue
		}
		a := ensure(poet)
		a.quoteCnt++
		if q.BookID != "" {
			if _, ok := bookMap[q.BookID]; ok {
				a.books[q.BookID] = struct{}{}
			}
		}
	}

	list := make([]AuthorStat, 0, len(byAuthor))
	for name, a := range byAuthor {
		readCnt := len(a.readPairs)
		combined := readCnt + a.favCount*3 + a.quoteCnt*2
		var score int
		switch mode {
		case AuthorWeightRead:
			score = readCnt
		case AuthorWeightCollection:
			score = a.favCount + a.quoteCnt
		default:
			score = combined
		}
		if score <= 0 {
			continue
		}
		list = append(list, AuthorStat{
			Author:        name,
			ReadCount:     readCnt,
			FavoriteCount: a.favCount,
			QuoteCount:    a.quoteCnt,
			BookCount:     len(a.books),
			CombinedScore: combined,
			Score:         score,
		})
	}

	slices.SortFunc(list, func(a, b AuthorStat) int {
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		if a.CombinedScore != b.CombinedScore {
			return b.CombinedScore - a.CombinedScore
		}
		return strings.Compare(a.Author, b.Author)
	})
	if len(list) > 36 {
		list = list[:36]
	}

	// 按对数分档映射到 1..5 视觉阶梯，拉开高频与低频诗人的排版层次感
	if len(list) > 0 {
		maxScore := float64(list[0].Score)
		minScore := float64(list[len(list)-1].Score)
		logMax := math.Log1p(maxScore)
		logMin := math.Log1p(minScore)
		for i := range list {
			if logMax <= logMin {
				list[i].Tier = 4
				continue
			}
			norm := (math.Log1p(float64(list[i].Score)) - logMin) / (logMax - logMin)
			tier := 1 + int(math.Round(norm*4))
			list[i].Tier = max(1, min(5, tier))
		}
	}
	return list
}

func resolvePoemAuthor(explicitAuthor, cachedPoemAuthor, bookAuthor string) string {
	for _, cand := range []string{explicitAuthor, cachedPoemAuthor, bookAuthor} {
		if n := normalizePoetName(cand); n != "" {
			return n
		}
	}
	return ""
}

func normalizePoetName(raw string) string {
	s := strings.TrimSpace(raw)
	switch s {
	case "", "佚名", "未知", "无名氏", "Unknown", "unknown":
		return ""
	default:
		return s
	}
}
