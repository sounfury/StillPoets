package components

import "github.com/egoist/mygo/ui"

// 极简线条 SVG 图标集合（严格对齐 V3 原型视觉权重：1.6px 细描边、无冗余填充）。
var (
	IconSearch = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="7"/><line x1="20" y1="20" x2="16.2" y2="16.2"/></svg>`))

	IconPlus = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>`))

	IconShelf = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="4" width="6.5" height="16" rx="1"/><rect x="13.5" y="4" width="6.5" height="16" rx="1"/></svg>`))

	IconStats = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"><line x1="6" y1="18" x2="6" y2="13"/><line x1="12" y1="18" x2="12" y2="9"/><line x1="18" y1="18" x2="18" y2="5"/></svg>`))

	IconProfile = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="8.5" r="3.5"/><path d="M5 19.5c1.5-3.4 4.1-5 7-5s5.5 1.6 7 5"/></svg>`))

	IconBookmark = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M6 4.5h12v16l-6-4-6 4z"/></svg>`))

	IconHeart = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20s-7-4.6-8.6-8.6C2.3 8.5 4.2 5.5 7.3 5.5c1.9 0 3.5 1 4.7 2.5 1.2-1.5 2.8-2.5 4.7-2.5 3.1 0 5 3 3.9 5.9C19 15.4 12 20 12 20z"/></svg>`))

	IconHeartFilled = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><path d="M12 20.5s-7.2-4.7-8.9-8.9C1.9 8.5 4 5.2 7.3 5.2c1.9 0 3.6 1 4.7 2.6 1.1-1.6 2.8-2.6 4.7-2.6 3.3 0 5.4 3.3 4.2 6.4-1.7 4.2-8.9 8.9-8.9 8.9z"/></svg>`))

	IconCloudCheck = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M7.5 18h9.2a4 4 0 0 0 .8-7.9A5.5 5.5 0 0 0 6.8 9.2 3.8 3.8 0 0 0 7.5 18z"/><path d="M9.5 13.8l1.8 1.8 3.6-3.6"/></svg>`))

	IconCloud = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M7.5 18h9.2a4 4 0 0 0 .8-7.9A5.5 5.5 0 0 0 6.8 9.2 3.8 3.8 0 0 0 7.5 18z"/></svg>`))

	IconList = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"><line x1="8" y1="6.5" x2="19" y2="6.5"/><line x1="8" y1="12" x2="19" y2="12"/><line x1="8" y1="17.5" x2="19" y2="17.5"/><circle cx="4.5" cy="6.5" r="0.9" fill="currentColor"/><circle cx="4.5" cy="12" r="0.9" fill="currentColor"/><circle cx="4.5" cy="17.5" r="0.9" fill="currentColor"/></svg>`))

	IconArrowRight = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><line x1="5" y1="12" x2="19" y2="12"/><polyline points="13 6 19 12 13 18"/></svg>`))

	IconChevronRight = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 5 16 12 9 19"/></svg>`))

	IconBack = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><polyline points="15 5 8 12 15 19"/></svg>`))

	IconClose = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"><line x1="6" y1="6" x2="18" y2="18"/><line x1="18" y1="6" x2="6" y2="18"/></svg>`))

	IconCheck = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><polyline points="5 12.5 10 17.5 19 7.5"/></svg>`))

	IconTrash = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><polyline points="4 7 20 7"/><path d="M9.5 7V5a1.5 1.5 0 0 1 1.5-1.5h2A1.5 1.5 0 0 1 14.5 5v2"/><path d="M6.5 7l.8 11.5a1.8 1.8 0 0 0 1.8 1.5h5.8a1.8 1.8 0 0 0 1.8-1.5L17.5 7"/></svg>`))

	IconRefresh = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M20 11a8 8 0 0 0-14.5-4.5L4 8"/><polyline points="4 4.5 4 8 7.5 8"/><path d="M4 13a8 8 0 0 0 14.5 4.5L20 16"/><polyline points="20 19.5 20 16 16.5 16"/></svg>`))

	IconAlertCircle = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="8.5"/><line x1="12" y1="8.2" x2="12" y2="12.8"/><circle cx="12" cy="16" r="0.8" fill="currentColor" stroke="none"/></svg>`))

	IconMail = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="3.5" y="5.5" width="17" height="13" rx="2"/><polyline points="4.5 7 12 13 19.5 7"/></svg>`))

	IconExternalLink = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M14 5h5v5"/><line x1="10" y1="14" x2="19" y2="5"/><path d="M18 13.5V18a1.5 1.5 0 0 1-1.5 1.5h-10A1.5 1.5 0 0 1 5 18V8A1.5 1.5 0 0 1 6.5 6.5H11"/></svg>`))

	IconFileText = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M7 4.5h6.5L18 9v10.5a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1v-14a1 1 0 0 1 1-1z"/><polyline points="13.5 4.5 13.5 9 18 9"/><line x1="9" y1="13" x2="15" y2="13"/><line x1="9" y1="16.5" x2="13.5" y2="16.5"/></svg>`))

	IconDownload = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M12 4.5v10.5"/><polyline points="8 11.5 12 15.5 16 11.5"/><path d="M5 18.5h14"/></svg>`))
)
