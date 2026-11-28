package views

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
	"stillpoets/internal/domain"
)

func (a *AppUI) autoSelectInitialBook() {
	a.mu.Lock()
	if a.activeBookID != "" || a.deeplinkHandled {
		a.mu.Unlock()
		return
	}
	var targetBookID, targetPoemID string
	if a.continueItem != nil {
		targetBookID = a.continueItem.Book.ID
		targetPoemID = a.continueItem.Progress.LastPoemID
	} else {
		for _, b := range a.books {
			if b.LocalState == domain.LocalStateDownloaded {
				targetBookID = b.ID
				break
			}
		}
		if targetBookID == "" && len(a.books) > 0 {
			targetBookID = a.books[0].ID
		}
	}
	a.mu.Unlock()

	if targetBookID != "" {
		a.openBook(targetBookID, targetPoemID, nil, false)
	}
}

// HandleDeeplink 处理来自桌面小组件或系统路由的 stillpoets:// 协议。

// --- 异步业务动作 ---

func (a *AppUI) openBook(bookID, poemID string, highlightRanges []app.LineRange, enterMobileReader bool) {
	a.openBookLocked(bookID, poemID, highlightRanges, enterMobileReader, "", entryMainline)
}

func (a *AppUI) openBookWithSource(bookID, poemID string, highlightRanges []app.LineRange, enterMobileReader bool, source readerEntrySource) {
	a.openBookLocked(bookID, poemID, highlightRanges, enterMobileReader, "", source)
}

// openBookLocked 必须在 UI 线程上调用（来自点击事件或 dispatchUI 闭包），通过单调递增 openSeq 彻底消除切书乱序与串书污染（P0-2）。
func (a *AppUI) openBookLocked(bookID, poemID string, highlightRanges []app.LineRange, enterMobileReader bool, deeplinkURL string, source readerEntrySource) {
	a.mu.Lock()
	a.openSeq++
	reqSeq := a.openSeq
	a.activeBookID = bookID
	a.mu.Unlock()

	a.highlightRanges = slices.Clone(highlightRanges)
	a.clearPoemSelectionState()
	a.tocQuery = ""
	a.tocScroll = ui.ScrollState{}
	a.readerError = ""
	a.readerEntrySource = source
	a.peekBannerHidden = false
	a.pendingPoemID = poemID
	if enterMobileReader {
		a.readerMobile = true
	}

	request := bookOpenRequest{
		bookID: bookID, poemID: poemID, highlightRanges: highlightRanges,
		enterMobileReader: enterMobileReader, deeplinkURL: deeplinkURL, source: source, seq: reqSeq,
	}
	book, found := a.bookByID(bookID)
	if found && book.LocalState != domain.LocalStateDownloaded {
		a.downloadRequestedBook(request)
		return
	}
	go a.loadOpenedBook(request, book.Author, found)
}

func (a *AppUI) downloadBookAsync(bookID, poemID string, highlightRanges []app.LineRange, reqSeq uint64, source readerEntrySource) {
	a.mu.Lock()
	if a.downloadingBooks[bookID] {
		a.mu.Unlock()
		return
	}
	a.downloadingBooks[bookID] = true
	a.mu.Unlock()
	a.dispatchUI(func() {
		a.mu.Lock()
		match := a.activeBookID == bookID
		a.mu.Unlock()
		if match {
			a.readerError = ""
		}
	})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		err := a.svc.EnsureBookContent(ctx, bookID)
		a.mu.Lock()
		delete(a.downloadingBooks, bookID)
		stillTarget := a.openSeq == reqSeq && a.activeBookID == bookID
		a.mu.Unlock()

		a.reloadSync(nil)
		a.dispatchUI(func() {
			a.mu.Lock()
			match := a.openSeq == reqSeq && a.activeBookID == bookID
			a.mu.Unlock()
			if !match {
				return
			}
			if err != nil {
				a.readerError = err.Error()
				return
			}
			if stillTarget {
				a.openBookLocked(bookID, poemID, highlightRanges, a.readerMobile, "", source)
			}
		})
	}()
}

func (a *AppUI) recordPoemReadAsync(bookID string, poem domain.Poem, allPoems []domain.Poem) {
	if !poem.IsPoem() {
		return
	}
	totalPoems := countFormalPoems(allPoems)
	go func() {
		// 串行写阅读进度，避免快速连续翻页时旧篇目后拿锁覆盖新续读位置（P1-14）
		a.readOrderMu.Lock()
		defer a.readOrderMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = a.svc.RecordPoemRead(ctx, bookID, poem.ID, 0, totalPoems)
	}()
}

func (a *AppUI) recordPoemPeekAsync(bookID string, poem domain.Poem, allPoems []domain.Poem) {
	if !poem.IsPoem() {
		return
	}
	totalPoems := countFormalPoems(allPoems)
	go func() {
		// 与主线写进度共享串行锁，确保偶遇写入与主线写入严格按触发顺序落库
		a.readOrderMu.Lock()
		defer a.readOrderMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = a.svc.RecordPoemPeek(ctx, bookID, poem.ID, totalPoems)
	}()
}

func countFormalPoems(allPoems []domain.Poem) int {
	totalPoems := 0
	for _, p := range allPoems {
		if p.IsPoem() {
			totalPoems++
		}
	}
	return totalPoems
}

// startPoemDwell 开始追踪一首正式诗篇的有效停留时长：
// 1. 若切离前一篇已停留 >= poemDwellMinLeave，自动补记前一篇为有效读诗事件；
// 2. 当前篇目停留满 poemDwellConfirmDelay（5 秒）自动落库一条单日去重读诗事件。
func (a *AppUI) startPoemDwell(bookID string, poem domain.Poem, bookAuthor string, wasReadBefore bool) {
	var commitPrev *poemDwellSession

	a.mu.Lock()
	if a.dwell != nil {
		if a.dwell.bookID == bookID && a.dwell.poemID == poem.ID {
			a.mu.Unlock()
			return
		}
		prev := a.dwell
		if prev.timer != nil {
			prev.timer.Stop()
			prev.timer = nil
		}
		if !prev.committed && time.Since(prev.startedAt) >= poemDwellMinLeave {
			prev.committed = true
			commitPrev = prev
		}
		a.dwell = nil
	}

	if bookID != "" && poem.ID != "" && poem.IsPoem() {
		author := strings.TrimSpace(poem.Author)
		if author == "" {
			author = strings.TrimSpace(bookAuthor)
		}
		sess := &poemDwellSession{
			bookID:        bookID,
			poemID:        poem.ID,
			poemTitle:     strings.TrimSpace(poem.Title),
			author:        author,
			wasReadBefore: wasReadBefore,
			startedAt:     time.Now(),
		}
		sess.timer = time.AfterFunc(poemDwellConfirmDelay, func() {
			a.commitPoemDwellIfActive(sess)
		})
		a.dwell = sess
	}
	a.mu.Unlock()

	if commitPrev != nil {
		a.commitPoemDwellAsync(commitPrev)
	}
}

// flushCurrentPoemDwell 结束或立即确认当前读诗停留会话。
// force=true 用于用户显式点亮 ❤️ / 划句留散页等深度阅读行为；force=false 仅当停留 >= poemDwellMinLeave 时落库。
func (a *AppUI) flushCurrentPoemDwell(force bool) {
	var toCommit *poemDwellSession

	a.mu.Lock()
	sess := a.dwell
	if sess != nil {
		if sess.timer != nil {
			sess.timer.Stop()
			sess.timer = nil
		}
		if !sess.committed && (force || time.Since(sess.startedAt) >= poemDwellMinLeave) {
			sess.committed = true
			toCommit = sess
		}
		if !force {
			a.dwell = nil
		}
	}
	a.mu.Unlock()

	if toCommit != nil {
		a.commitPoemDwellAsync(toCommit)
	}
}

func (a *AppUI) commitPoemDwellIfActive(target *poemDwellSession) {
	a.mu.Lock()
	if a.dwell != target || target.committed {
		a.mu.Unlock()
		return
	}
	target.committed = true
	target.timer = nil
	a.mu.Unlock()

	a.commitPoemDwellAsync(target)
}

func (a *AppUI) commitPoemDwellAsync(sess *poemDwellSession) {
	if sess == nil || sess.bookID == "" || sess.poemID == "" {
		return
	}
	go func() {
		a.readOrderMu.Lock()
		defer a.readOrderMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _, _ = a.svc.RecordReadingEvent(ctx, sess.bookID, sess.poemID, sess.poemTitle, sess.author, sess.wasReadBefore)
	}()
}

func (a *AppUI) triggerImportEPUB() {
	if a.pickFile == nil {
		a.showTransientToastOnUI("当前环境未注册文件选择器")
		return
	}
	a.showAddModal = false
	groupID := a.selectedGrp
	a.mu.Lock()
	startSeq := a.openSeq
	a.mu.Unlock()
	go func() {
		filePath, err := a.pickFile("选择 EPUB 诗集文件", []string{"epub"})
		if err != nil || strings.TrimSpace(filePath) == "" {
			return
		}
		a.showTransientToast("正在解析诗集…")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := a.svc.ImportEPUB(ctx, filePath, groupID)
		if err != nil {
			a.showTransientToast("导入失败：" + err.Error())
			return
		}
		a.reloadSync(nil)
		a.dispatchUI(func() {
			a.mu.Lock()
			userNavigated := a.openSeq != startSeq
			a.mu.Unlock()
			if !userNavigated {
				a.openBookLocked(res.Book.ID, "", nil, false, "", entryMainline)
			}
		})
		a.showTransientToast(fmt.Sprintf("已导入《%s》(%d 首)", res.Book.Title, res.Book.PoemCount))
	}()
}

func (a *AppUI) triggerImportFont() {
	if a.pickFile == nil {
		a.showTransientToastOnUI("当前环境未注册文件选择器")
		return
	}
	go func() {
		filePath, err := a.pickFile("选择自定义字体 (.ttf / .otf)", []string{"ttf", "otf"})
		if err != nil || strings.TrimSpace(filePath) == "" {
			return
		}
		lf, raw, err := a.svc.ImportFont(filePath)
		if err != nil {
			a.showTransientToast("字体导入失败：" + err.Error())
			return
		}
		if a.registerFn != nil {
			if err := a.registerFn(raw, lf.Family); err != nil {
				a.showTransientToast("字体注册失败：" + err.Error())
				return
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = a.svc.UpdateSettings(ctx, func(st *domain.Settings) {
			st.FontFamily = lf.Family
			st.WidgetTheme.FontFamily = lf.Family
			st.WidgetTheme.FontFile = lf.Filename
		})
		a.reloadSync(nil)
		a.showTransientToast("已启用字体：" + lf.Family)
	}()
}

func (a *AppUI) activeBook() (domain.Book, bool) {
	for _, b := range a.books {
		if b.ID == a.activeBookID {
			return b, true
		}
	}
	return domain.Book{}, false
}

func (a *AppUI) filteredBooks() []domain.Book {
	if a.selectedGrp == "" {
		return a.books
	}
	out := make([]domain.Book, 0, len(a.books))
	for _, b := range a.books {
		if b.GroupID == a.selectedGrp {
			out = append(out, b)
		}
	}
	return out
}
