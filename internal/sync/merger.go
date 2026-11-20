package sync

import (
	"slices"
	"strings"

	"stillpoets/internal/domain"
)

// MergeBooks 按 bookId 合并本地与远端诗集元数据。
// 规则：
// 1. 两端各自新增的诗集均保留（并集）；远端新增诗集本地状态默认标记为 remote（按需下载）。
// 2. 同 ID 诗集以 UpdatedAt 较新的元数据胜出；若本地原本已下载但远端 ContentHash 变更，则将本地状态置为 stale。
// 3. LocalState 仅属于本机状态，不被远端覆盖为 remote。
func MergeBooks(local, remote []domain.Book) []domain.Book {
	merged := make(map[string]domain.Book, len(local)+len(remote))
	for _, b := range local {
		if b.LocalState == "" {
			b.LocalState = domain.LocalStateRemote
		}
		merged[b.ID] = b
	}

	for _, r := range remote {
		cur, exists := merged[r.ID]
		if !exists {
			r.LocalState = domain.LocalStateRemote
			merged[r.ID] = r
			continue
		}

		localState := cur.LocalState
		localHash := cur.ContentHash

		if r.UpdatedAt > cur.UpdatedAt || (r.UpdatedAt == cur.UpdatedAt && r.DeletedAt > cur.DeletedAt) {
			next := r
			// 保留本机下载状态；若正文包哈希变化且本机已下载，则标记为 stale 提示下次打开重新拉取
			if localState == domain.LocalStateDownloaded && localHash != "" && r.ContentHash != "" && localHash != r.ContentHash {
				next.LocalState = domain.LocalStateStale
			} else {
				next.LocalState = localState
			}
			merged[r.ID] = next
		}
	}

	out := make([]domain.Book, 0, len(merged))
	for _, b := range merged {
		out = append(out, b)
	}
	slices.SortFunc(out, func(a, b domain.Book) int {
		if a.SortOrder != b.SortOrder {
			return a.SortOrder - b.SortOrder
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

// MergeQuotes 按 quoteId 做并集合并，保留两端各自新增的独立句子，同 ID 按 UpdatedAt 最新胜出。
func MergeQuotes(local, remote []domain.Quote) []domain.Quote {
	merged := make(map[string]domain.Quote, len(local)+len(remote))
	for _, q := range local {
		merged[q.ID] = q
	}
	for _, r := range remote {
		cur, exists := merged[r.ID]
		if !exists || r.UpdatedAt > cur.UpdatedAt || (r.UpdatedAt == cur.UpdatedAt && r.DeletedAt > cur.DeletedAt) {
			merged[r.ID] = r
		}
	}

	out := make([]domain.Quote, 0, len(merged))
	for _, q := range merged {
		out = append(out, q)
	}
	slices.SortFunc(out, func(a, b domain.Quote) int {
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

// MergeFavorites 按 (bookId, poemId) 合并诗篇级精选池 ❤️，保留最新的点亮/取消及手动选句意图。
func MergeFavorites(local, remote []domain.FavoritePoem) []domain.FavoritePoem {
	keyOf := func(f domain.FavoritePoem) string {
		return f.BookID + "::" + f.PoemID
	}
	merged := make(map[string]domain.FavoritePoem, len(local)+len(remote))
	for _, f := range local {
		merged[keyOf(f)] = f
	}
	for _, r := range remote {
		k := keyOf(r)
		cur, exists := merged[k]
		if !exists || r.UpdatedAt > cur.UpdatedAt || (r.UpdatedAt == cur.UpdatedAt && r.DeletedAt > cur.DeletedAt) {
			merged[k] = r
		}
	}

	out := make([]domain.FavoritePoem, 0, len(merged))
	for _, f := range merged {
		out = append(out, f)
	}
	slices.SortFunc(out, func(a, b domain.FavoritePoem) int {
		return strings.Compare(keyOf(a), keyOf(b))
	})
	return out
}

// MergeProgress 按 bookId 合并阅读进度与完读标记（LWW）。
func MergeProgress(local, remote []domain.ReadingProgress) []domain.ReadingProgress {
	merged := make(map[string]domain.ReadingProgress, len(local)+len(remote))
	for _, p := range local {
		merged[p.BookID] = p
	}
	for _, r := range remote {
		cur, exists := merged[r.BookID]
		if !exists || r.UpdatedAt > cur.UpdatedAt {
			merged[r.BookID] = r
		}
	}

	out := make([]domain.ReadingProgress, 0, len(merged))
	for _, p := range merged {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b domain.ReadingProgress) int {
		return strings.Compare(a.BookID, b.BookID)
	})
	return out
}
