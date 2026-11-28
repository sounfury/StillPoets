package views

import (
	"context"
	"fmt"
	"github.com/egoist/mygo/ui"
	"slices"
	"strings"
	"time"

	"stillpoets/internal/app"
	"stillpoets/internal/domain"
	"stillpoets/internal/storage"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/theme"
	"stillpoets/modules/widgetbridge"
)

func (a *AppUI) reloadSync(ctx context.Context) {
	// 始终确保本地 SQLite 刷新使用未过期的独立上下文，防止上游网络同步超时后把本机书架/状态冲刷为空。
	if ctx == nil || ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
	}

	settings, settingsErr := a.svc.LoadSettings(ctx)
	if settingsErr == nil {
		if settings.FontSize <= 0 {
			settings.FontSize = 18
		}
		if settings.LineHeight <= 0 {
			settings.LineHeight = 1.85
		}
	}
	groups, groupsErr := a.svc.ListGroups(ctx)
	books, booksErr := a.svc.ListBooks(ctx, "")
	progressList, progressErr := a.svc.ListProgress(ctx)
	var pMap map[string]domain.ReadingProgress
	if progressErr == nil {
		pMap = make(map[string]domain.ReadingProgress, len(progressList))
		for _, p := range progressList {
			pMap[p.BookID] = p
		}
	}
	cont, contErr := a.svc.GetContinueReading(ctx)
	today, todayErr := a.svc.PickTodayReread(ctx)

	var (
		qFilter          storage.QuoteFilter
		curBookID        string
		snapOpenSeq      uint64
		snapQuoteSeq     uint64
		snapStatsSeq     uint64
		snapDaySeq       uint64
		statsPeriod      storage.StatsPeriod
		statsHeatmapYear int
		statsAuthorMode  storage.AuthorWeightMode
		selectedDate     string
	)
	a.mu.Lock()
	qFilter = storage.QuoteFilter{
		Kind:       a.quoteKind,
		SourceBook: a.quoteBook,
		Author:     a.quoteAuthor,
	}
	curBookID = a.activeBookID
	snapOpenSeq = a.openSeq
	snapQuoteSeq = a.quoteFilterSeq
	snapStatsSeq = a.statsSeq
	snapDaySeq = a.statsDaySeq
	statsPeriod = a.statsPeriod
	statsHeatmapYear = a.statsHeatmapYear
	statsAuthorMode = a.statsAuthorMode
	selectedDate = a.statsSelectedDate
	a.mu.Unlock()

	quotes, quotesErr := a.svc.ListQuotes(ctx, qFilter)
	facets, facetsErr := a.svc.ListQuoteFacets(ctx)
	stats, statsErr := a.svc.GetStatsOverview(ctx)
	dash, dashErr := a.svc.GetStatsDashboard(ctx, storage.StatsQuery{
		Period:     statsPeriod,
		Year:       statsHeatmapYear,
		AuthorMode: statsAuthorMode,
	})
	effectiveDate := selectedDate
	if effectiveDate == "" && dashErr == nil && dash.Heatmap.LatestActiveDate != "" {
		effectiveDate = dash.Heatmap.LatestActiveDate
	}
	var (
		dayEvents []storage.ReadingEventItem
		dayErr    error
	)
	if effectiveDate != "" {
		dayEvents, dayErr = a.svc.ListReadingEventsByDate(ctx, effectiveDate)
	}
	syncOv, syncOvErr := a.svc.GetSyncOverview(ctx)
	localFonts := a.svc.ListLocalFonts()
	cacheSize := a.svc.CacheSizeBytes()
	diagLogSize := a.svc.DiagnosticLogSizeBytes()
	widgetTL, widgetErr := a.svc.ReadWidgetTimeline()
	activeWidgets := widgetbridge.ActiveWidgetCount()
	hasSecret := a.svc.HasWebDAVPassword()

	var (
		poems    []domain.Poem
		poemsErr error
		favMap   map[string]domain.FavoritePoem
		favsErr  error
	)
	if curBookID != "" {
		poems, poemsErr = a.svc.ListPoems(ctx, curBookID)
		var favs []domain.FavoritePoem
		favs, favsErr = a.svc.ListFavorites(ctx, curBookID)
		if favsErr == nil {
			favMap = make(map[string]domain.FavoritePoem, len(favs))
			for _, f := range favs {
				favMap[f.PoemID] = f
			}
		}
	}

	a.dispatchUI(func() {
		if settingsErr == nil {
			a.settings = settings
			a.fontSizeSlider = float64(settings.FontSize)
			a.lineHeightSlider = float64(settings.LineHeight)
		} else if a.settings.FontSize <= 0 {
			a.settings = domain.Settings{ThemePreset: string(theme.PresetArtbook), FontSize: 18, LineHeight: 1.85}
			a.fontSizeSlider = 18
			a.lineHeightSlider = 1.85
		}
		a.syncNativeAppearance(a.palette())
		if groupsErr == nil {
			a.groups = groups
		}
		if booksErr == nil {
			a.books = books
		}
		if progressErr == nil {
			a.progressMap = pMap
		}
		if contErr == nil {
			a.continueItem = cont
		}
		if todayErr == nil {
			a.todayReread = today
		}
		a.mu.Lock()
		quoteMatch := a.quoteFilterSeq == snapQuoteSeq
		openMatch := a.openSeq == snapOpenSeq && a.activeBookID == curBookID
		statsMatch := a.statsSeq == snapStatsSeq
		dayMatch := a.statsDaySeq == snapDaySeq
		a.mu.Unlock()
		if quoteMatch {
			if quotesErr == nil {
				a.quotes = quotes
			}
			if facetsErr == nil {
				a.quoteFacets = facets
			}
		}
		if statsErr == nil {
			a.stats = stats
		}
		if statsMatch && dashErr == nil {
			a.statsDashboard = dash
			if a.detailFinishedBook != nil {
				var refreshed *storage.FinishedBookItem
				for i := range dash.FinishedBooks {
					if dash.FinishedBooks[i].Book.ID == a.detailFinishedBook.Book.ID {
						cp := dash.FinishedBooks[i]
						refreshed = &cp
						break
					}
				}
				if refreshed != nil {
					a.detailFinishedBook = refreshed
				} else {
					a.showFinishedModal = false
					a.detailFinishedBook = nil
				}
			}
		}
		if dayMatch && dayErr == nil && effectiveDate != "" {
			if a.statsSelectedDate == "" {
				a.statsSelectedDate = effectiveDate
			}
			a.statsDayEvents = dayEvents
			a.statsDayLoading = false
		}
		if syncOvErr == nil {
			a.syncOverview = syncOv
		}
		a.localFonts = localFonts
		a.cacheSizeBytes = cacheSize
		a.diagLogSizeBytes = diagLogSize
		a.activeWidgetCount = activeWidgets
		if widgetErr == nil {
			cp := widgetTL
			a.widgetPreview = &cp
			a.widgetCount = len(widgetTL.Entries)
		}
		a.davHasSecret = hasSecret

		// 若当前在读书已被其他设备删除，退出全屏阅读层避免滞留无返回按钮的空态（P1-7）
		if booksErr == nil && a.activeBookID != "" {
			stillExists := false
			for _, b := range a.books {
				if b.ID == a.activeBookID {
					stillExists = true
					break
				}
			}
			if !stillExists {
				a.mu.Lock()
				a.openSeq++
				a.activeBookID = ""
				a.mu.Unlock()
				a.activePoems = nil
				a.activePoemIdx = 0
				a.activeFavorites = map[string]domain.FavoritePoem{}
				a.readerMobile = false
				a.readerError = ""
				a.readerEntrySource = entryMainline
				a.peekBannerHidden = false
				a.pendingPoemID = ""
				return
			}
		}

		if curBookID != "" && openMatch {
			if poemsErr == nil {
				a.activePoems = poems
				if a.activePoemIdx >= len(poems) {
					a.activePoemIdx = max(0, len(poems)-1)
				}
			}
			if favsErr == nil {
				a.activeFavorites = favMap
			}
		}
	})
}

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

	var targetBook *domain.Book
	for i := range a.books {
		if a.books[i].ID == bookID {
			cp := a.books[i]
			targetBook = &cp
			break
		}
	}

	if targetBook != nil && targetBook.LocalState != domain.LocalStateDownloaded {
		a.downloadBookAsync(bookID, poemID, highlightRanges, reqSeq, source)
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if targetBook == nil {
			b, err := a.svc.Store().GetBook(ctx, bookID)
			if err != nil {
				if deeplinkURL != "" {
					a.mu.Lock()
					a.pendingDeeplink = deeplinkURL
					a.mu.Unlock()
				}
				a.dispatchUI(func() {
					a.mu.Lock()
					stale := a.openSeq != reqSeq
					a.mu.Unlock()
					if !stale {
						a.readerError = "暂未在本地找到该诗集，请等待云同步完成后重试"
					}
				})
				return
			}
			if b.LocalState != domain.LocalStateDownloaded {
				a.downloadBookAsync(bookID, poemID, highlightRanges, reqSeq, source)
				return
			}
		}
		poems, err := a.svc.ListPoems(ctx, bookID)
		if err != nil {
			a.dispatchUI(func() {
				a.mu.Lock()
				stale := a.openSeq != reqSeq || a.activeBookID != bookID
				a.mu.Unlock()
				if !stale {
					a.readerError = fmt.Sprintf("加载诗集失败：%v", err)
				}
			})
			return
		}
		favs, _ := a.svc.ListFavorites(ctx, bookID)
		favMap := make(map[string]domain.FavoritePoem, len(favs))
		for _, f := range favs {
			favMap[f.PoemID] = f
		}
		prog, progErr := a.svc.GetProgress(ctx, bookID)

		// 定位目标诗篇（优先显式 poemID，其次续读 LastPoemID，其次首篇正式诗）
		idx := 0
		wantPoem := poemID
		if wantPoem == "" {
			wantPoem = prog.LastPoemID
		}
		found := false
		if wantPoem != "" {
			for i, p := range poems {
				if p.ID == wantPoem {
					idx = i
					found = true
					break
				}
			}
		}
		if !found {
			for i, p := range poems {
				if p.IsPoem() {
					idx = i
					break
				}
			}
		}

		// 若偶遇直达的恰好就是本书当前主线续读位置，则自然回归主线阅读，避免冗余提示条。
		effectiveSource := source
		if effectiveSource.isPeek() && len(poems) > 0 && idx < len(poems) && prog.LastPoemID != "" && poems[idx].ID == prog.LastPoemID {
			effectiveSource = entryMainline
		}

		var (
			readPoem      *domain.Poem
			bookAuthor    string
			wasReadBefore bool
			startDwell    bool
		)
		if targetBook != nil {
			bookAuthor = targetBook.Author
		}
		a.dispatchUI(func() {
			a.mu.Lock()
			stale := a.openSeq != reqSeq || a.activeBookID != bookID
			a.mu.Unlock()
			if stale {
				return
			}
			if progErr == nil {
				a.progressMap[bookID] = prog
			}
			a.activePoems = poems
			a.activePoemIdx = idx
			a.activeFavorites = favMap
			a.poemPager.scopeKey = ""
			a.readerError = ""
			a.readerEntrySource = effectiveSource
			a.peekBannerHidden = false
			a.pendingPoemID = ""
			if bookAuthor == "" {
				if bk, ok := a.activeBook(); ok {
					bookAuthor = bk.Author
				}
			}
			if len(poems) > 0 && idx < len(poems) {
				cp := poems[idx]
				wasReadBefore = slices.Contains(prog.ReadPoemIDs, cp.ID)
				if enterMobileReader {
					readPoem = &cp
					startDwell = true
				} else if a.lastIsTablet && a.activeTab == components.TabShelf && a.shelfSubTab == components.SubTabBooks {
					readPoem = &cp
					startDwell = true
				}
			}
		})

		if readPoem != nil {
			if startDwell {
				a.startPoemDwell(bookID, *readPoem, bookAuthor, wasReadBefore)
			}
			if enterMobileReader {
				if effectiveSource.isPeek() {
					a.recordPoemPeekAsync(bookID, *readPoem, poems)
				} else {
					a.recordPoemReadAsync(bookID, *readPoem, poems)
				}
			}
		}
	}()
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
