package views

import (
	"context"
	"fmt"
	"slices"
	"time"

	"stillpoets/internal/app"
	"stillpoets/internal/domain"
	"stillpoets/internal/ui/components"
)

type bookOpenRequest struct {
	bookID, poemID    string
	highlightRanges   []app.LineRange
	enterMobileReader bool
	deeplinkURL       string
	source            readerEntrySource
	seq               uint64
}

func (a *AppUI) downloadRequestedBook(req bookOpenRequest) {
	a.downloadBookAsync(req.bookID, req.poemID, req.highlightRanges, req.seq, req.source)
}

func (a *AppUI) checkOpenedBookContent(ctx context.Context, req bookOpenRequest) bool {
	book, err := a.svc.Store().GetBook(ctx, req.bookID)
	if err != nil {
		if req.deeplinkURL != "" {
			a.mu.Lock()
			a.pendingDeeplink = req.deeplinkURL
			a.mu.Unlock()
		}
		a.dispatchUI(func() {
			a.mu.Lock()
			current := a.openSeq == req.seq
			a.mu.Unlock()
			if current {
				a.readerError = "暂未在本地找到该诗集，请等待云同步完成后重试"
			}
		})
		return false
	}
	if book.LocalState != domain.LocalStateDownloaded {
		a.downloadRequestedBook(req)
		return false
	}
	return true
}

func (a *AppUI) isCurrentBookOpen(req bookOpenRequest) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.openSeq == req.seq && a.activeBookID == req.bookID
}

type openedBook struct {
	poems       []domain.Poem
	favorites   map[string]domain.FavoritePoem
	progress    domain.ReadingProgress
	progressErr error
	index       int
	source      readerEntrySource
	author      string
}

func (a *AppUI) loadOpenedBook(req bookOpenRequest, author string, knownBook bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !knownBook && !a.checkOpenedBookContent(ctx, req) {
		return
	}
	poems, err := a.svc.ListPoems(ctx, req.bookID)
	if err != nil {
		a.dispatchUI(func() {
			if a.isCurrentBookOpen(req) {
				a.readerError = fmt.Sprintf("加载诗集失败：%v", err)
			}
		})
		return
	}
	favorites, _ := a.svc.ListFavorites(ctx, req.bookID)
	indexedFavorites := indexReaderFavorites(favorites)
	progress, progressErr := a.svc.GetProgress(ctx, req.bookID)
	idx := readerOpenPoemIndex(poems, req.poemID, progress.LastPoemID)
	source := req.source
	// 偶遇目标就是当前续读位置时，直接回归主线。
	if source.isPeek() && idx < len(poems) && progress.LastPoemID != "" && poems[idx].ID == progress.LastPoemID {
		source = entryMainline
	}
	book := openedBook{
		poems: poems, favorites: indexedFavorites, progress: progress, progressErr: progressErr,
		index: idx, source: source, author: author,
	}
	a.dispatchUI(func() {
		a.applyOpenedBook(req, book)
	})
}

// 优先定位显式诗篇，再尝试续读位置，默认定位首篇正式诗。
func readerOpenPoemIndex(poems []domain.Poem, poemID, lastPoemID string) int {
	if poemID == "" {
		poemID = lastPoemID
	}
	if poemID != "" {
		for i, poem := range poems {
			if poem.ID == poemID {
				return i
			}
		}
	}
	for i, poem := range poems {
		if poem.IsPoem() {
			return i
		}
	}
	return 0
}

func (a *AppUI) applyOpenedBook(req bookOpenRequest, book openedBook) {
	if !a.isCurrentBookOpen(req) {
		return
	}
	if book.progressErr == nil {
		a.progressMap[req.bookID] = book.progress
	}
	a.activePoems = book.poems
	a.activePoemIdx = book.index
	a.activeFavorites = book.favorites
	a.poemPager.scopeKey = ""
	a.readerError = ""
	a.readerEntrySource = book.source
	a.peekBannerHidden = false
	a.pendingPoemID = ""

	// UI 调度可能异步，停留与进度记录须等该请求在 UI 线程通过代次校验。
	a.trackOpenedPoem(req, book)
}

func (a *AppUI) trackOpenedPoem(req bookOpenRequest, book openedBook) {
	if book.index >= len(book.poems) {
		return
	}
	visibleInShelf := a.lastIsTablet && a.activeTab == components.TabShelf && a.shelfSubTab == components.SubTabBooks
	if !req.enterMobileReader && !visibleInShelf {
		return
	}
	author := book.author
	if author == "" {
		if active, ok := a.activeBook(); ok {
			author = active.Author
		}
	}
	poem := book.poems[book.index]
	a.startPoemDwell(req.bookID, poem, author, slices.Contains(book.progress.ReadPoemIDs, poem.ID))
	if req.enterMobileReader {
		if book.source.isPeek() {
			a.recordPoemPeekAsync(req.bookID, poem, book.poems)
		} else {
			a.recordPoemReadAsync(req.bookID, poem, book.poems)
		}
	}
}
