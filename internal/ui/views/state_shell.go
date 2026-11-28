package views

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
	"stillpoets/internal/storage"
	"stillpoets/internal/ui/components"
	"stillpoets/internal/ui/layout"
	"stillpoets/internal/ui/theme"
)

func (a *AppUI) HandleDeeplink(rawURL string) {
	target, err := app.ParseDeeplink(rawURL)
	if err != nil {
		a.showTransientToast("无法识别的链接")
		return
	}
	switch target.Kind {
	case app.DeeplinkPoem:
		a.dispatchUI(func() {
			a.deeplinkHandled = true
			a.activeTab = components.TabShelf
			a.shelfSubTab = components.SubTabBooks
			a.shelfPager.scopeKey = ""
			a.poemPager.scopeKey = ""
			a.showQuoteDetail = false
			a.openBookLocked(target.BookID, target.PoemID, target.LineRanges, true, rawURL, entryWidget)
		})
	case app.DeeplinkQuote:
		var reqSeq uint64
		a.dispatchUI(func() {
			a.deeplinkHandled = true
			a.mu.Lock()
			a.openSeq++
			reqSeq = a.openSeq
			a.mu.Unlock()
		})
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			all, err := a.svc.ListQuotes(ctx, storage.QuoteFilter{})
			if err != nil {
				return
			}
			var found *storage.QuoteView
			for i := range all {
				if all[i].Quote.ID == target.QuoteID {
					q := all[i]
					found = &q
					break
				}
			}
			a.dispatchUI(func() {
				a.mu.Lock()
				stale := a.openSeq != reqSeq
				a.mu.Unlock()
				if stale {
					return
				}
				if found != nil {
					a.activeTab = components.TabShelf
					a.shelfSubTab = components.SubTabQuotes
					a.shelfPager.scopeKey = ""
					a.readerMobile = false
					a.detailQuote = found
					a.showQuoteDetail = true
				} else {
					a.mu.Lock()
					a.pendingDeeplink = rawURL
					a.mu.Unlock()
					a.showTransientToastOnUI("未在本地找到该散页，同步完成后将自动重试")
				}
			})
		}()
	}
}

// TriggerStartupSync 应用启动时若已启用 WebDAV，则在后台静默拉取最新元数据；完成后如有挂起的冷启动 Deeplink 则自动重试（P1-7）。
func (a *AppUI) TriggerStartupSync() {
	a.triggerAutoSync(true)
}

// TriggerForegroundSync 应用从后台切回前台时带短冷却窗口静默同步一次，确保电脑端刚整理的分组与投递的新书切回即见。
func (a *AppUI) TriggerForegroundSync() {
	a.triggerAutoSync(false)
}

func (a *AppUI) triggerAutoSync(ignoreCooldown bool) {
	if !a.svc.LoadWebDAVProfile().Enabled {
		return
	}
	now := time.Now()
	a.mu.Lock()
	if a.autoSyncRunning {
		a.mu.Unlock()
		return
	}
	if !ignoreCooldown && !a.lastAutoSyncAt.IsZero() && now.Sub(a.lastAutoSyncAt) < foregroundSyncCooldown {
		a.mu.Unlock()
		return
	}
	a.autoSyncRunning = true
	a.lastAutoSyncAt = now
	a.mu.Unlock()

	go func() {
		defer func() {
			a.mu.Lock()
			a.autoSyncRunning = false
			a.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		_, err := a.svc.SyncNow(ctx, false)
		if err == nil {
			a.mu.Lock()
			pending := a.pendingDeeplink
			a.pendingDeeplink = ""
			a.mu.Unlock()
			if pending != "" {
				a.HandleDeeplink(pending)
			}
		}
	}()
}

// Render 是 MyGo 原生窗口的根视图函数。
func (a *AppUI) Render(c *ui.Context) {
	a.mu.Lock()
	headless := a.uiDispatch == nil
	a.mu.Unlock()
	if headless {
		a.uiThreadMu.Lock()
		defer a.uiThreadMu.Unlock()
	}

	if sysTheme := c.Theme(); sysTheme != nil {
		a.systemDark = sysTheme.Dark
	}
	pal := a.palette()
	c.SetTheme(pal.UITheme())
	c.Root().Background(pal.Background)
	a.syncNativeAppearance(pal)

	isTablet := layout.IsTablet(c)
	if a.lastIsTablet && !isTablet && a.statsHeatmapYear == 0 {
		// 宽屏切回窄屏时热力图 MaxX 曾被归零，自动重停靠到最右侧（近期）
		a.statsHeatmapScroll.X = math.MaxFloat32
	}
	a.lastIsTablet = isTablet

	if isTablet {
		a.renderTabletShell(c, pal)
	} else {
		a.renderPhoneShell(c, pal)
	}

	// 瞬态云同步轻胶囊（浮于底部，成功约 2.2 秒后自动淡出，平时零占位）
	a.renderSyncToastOverlay(c, pal, isTablet)

	// 所有轻弹窗与抽屉
	a.renderModals(c, pal)
}

func (a *AppUI) selectShelfSubTab(sub components.ShelfSubTab) {
	if a.shelfSubTab == sub {
		return
	}
	if sub != components.SubTabBooks {
		a.exitShelfOrganize()
	}
	a.shelfSubTab = sub
	if sub == components.SubTabQuotes {
		go a.reloadQuotesOnly()
	}
}

func (a *AppUI) switchMainTab(tab components.NavTab, clearSelection bool) {
	if a.activeTab == components.TabShelf && tab != components.TabShelf {
		a.flushCurrentPoemDwell(false)
		a.exitShelfOrganize()
		if clearSelection {
			a.clearPoemSelectionState()
		}
	}
	if tab == components.TabStats && a.activeTab != components.TabStats {
		a.refreshStatsDashboardAsync()
	}
	a.activeTab = tab
	a.readerMobile = false
	a.profileSub = profileSubpageNone
	a.showChangelogModal = false
	a.confirmDisconnect = false
	a.confirmDeleteFont = ""
}

func (a *AppUI) renderActiveTabSurface(c *ui.Context, pal theme.Palette, prefix string, isTablet bool) {
	tabKey := fmt.Sprintf("%s-tab-%d", prefix, a.activeTab)
	tabBox := ui.Box(c.Key(tabKey)).
		Absolute().
		Fill().
		Background(pal.Background).
		Transition(theme.TabPageTransition())
	if grain := pal.PaperGrainOpacity(); grain > 0 {
		isDark := pal.IsDark()
		tabBox.Draw(func(p *ui.Painter, r ui.Rect) {
			theme.DrawPaperTexture(p, r, 0, isDark, grain)
		})
	}
	tabBox.Children(func() {
		switch a.activeTab {
		case components.TabShelf:
			if isTablet {
				a.renderTabletShelfWorkspace(c, pal)
			} else {
				a.renderShelfPane(c, pal, false)
			}
		case components.TabStats:
			a.renderStatsPage(c, pal, isTablet)
		case components.TabProfile:
			a.renderProfilePage(c, pal, isTablet)
		}
	})
}

func (a *AppUI) renderTabletShell(c *ui.Context, pal theme.Palette) {
	ui.Row(c.Key("tablet-root")).Fill().Children(func() {
		components.TabletNavRail(c, pal, a.activeTab, func(tab components.NavTab) {
			a.switchMainTab(tab, true)
		})

		ui.Box(c.Key("tablet-content")).Grow(1).FillHeight().Clip().Children(func() {
			a.renderActiveTabSurface(c, pal, "tablet", true)
		})
	})
}

func (a *AppUI) useFloatingBottomNav() bool {
	if a.settings.FloatingBottomBar != nil {
		return *a.settings.FloatingBottomBar
	}
	return components.DefaultFloatingBottomNav()
}

func (a *AppUI) renderPhoneShell(c *ui.Context, pal theme.Palette) {
	floatingNav := a.useFloatingBottomNav()
	renderBody := func() {
		a.renderActiveTabSurface(c, pal, "phone", false)
	}

	ui.Box(c.Key("phone-stage")).Fill().Clip().Children(func() {
		if floatingNav {
			// iOS 液态玻璃悬浮底栏：正文全屏铺底以便滚动内容从胶囊下方透出折射
			ui.Box(c.Key("phone-body")).Fill().Clip().Children(renderBody)
			components.FloatingBottomNavBar(c, pal, a.activeTab, func(tab components.NavTab) {
				a.switchMainTab(tab, false)
			})
		} else {
			ui.Column(c.Key("phone-root")).Fill().Children(func() {
				ui.Box(c.Key("phone-body")).Grow(1).FillWidth().Clip().Children(renderBody)
				components.BottomNavBar(c, pal, a.activeTab, func(tab components.NavTab) {
					a.switchMainTab(tab, false)
				})
			})
		}

		// 手机端设置二级页面以右侧推入转场叠在底栏之上，返回时保留设置主页滚动位置
		if a.activeTab == components.TabProfile && a.profileSub != profileSubpageNone {
			a.renderProfileSubpageLayer(c, pal, false)
		}

		// 手机端全屏阅读器以右侧推入转场叠在书架之上，返回时保留书架滚动位置
		if a.readerMobile && a.activeBookID != "" {
			ui.Box(c.Key("phone-reader-layer")).
				Absolute().
				Fill().
				Background(pal.ReaderPaper).
				Transition(theme.MobileReaderTransition()).
				Children(func() {
					a.renderReaderPane(c, pal, false)
				})
		}
	})
}

func (a *AppUI) renderSyncToastOverlay(c *ui.Context, pal theme.Palette, isTablet bool) {
	a.mu.Lock()
	msg := a.toastMessage
	phase := a.toastPhase
	a.mu.Unlock()
	if msg == "" {
		return
	}
	w, h := c.Size()
	if w <= 0 || h <= 0 {
		return
	}
	_, _, safeBottom, _ := c.SafeArea()
	bottom := float32(76) + safeBottom
	if a.useFloatingBottomNav() {
		bottom = 84 + safeBottom
	}
	if !isTablet && a.activeTab == components.TabProfile && a.profileSub != profileSubpageNone {
		bottom = 24 + safeBottom
	}
	left := float32(0)
	width := w
	if isTablet {
		bottom = 20 + safeBottom
		left = layout.NavRailWidthDIP
		width = layout.MasterPaneWidthDIP
	}
	toastKind := components.ToastInfo
	switch phase {
	case app.SyncToastSyncing:
		toastKind = components.ToastLoading
	case app.SyncToastPartial, app.SyncToastError:
		toastKind = components.ToastWarning
	}
	ui.Overlay(c, func() {
		ui.Row(c.Key("toast-overlay")).
			Absolute().
			Left(left).
			Bottom(bottom).
			Width(width).
			Justify(ui.Center).
			Transition(theme.ToastTransition()).
			Children(func() {
				components.SyncToastPill(c, pal, toastKind, msg, func() {
					a.activeTab = components.TabProfile
				})
			})
	})
}
