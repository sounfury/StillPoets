package views

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"stillpoets/internal/app"
	"stillpoets/internal/ui/components"
)

func TestDeadPoetsSpecimensAndMidnightWindow(t *testing.T) {
	if strings.Contains(dpsShortDefaultQuote, "纸上有风") || strings.TrimSpace(dpsShortDefaultQuote) == "" {
		t.Fatalf("unexpected default short quote: %q", dpsShortDefaultQuote)
	}
	if !strings.Contains(dpsThoreauOpening, "我步入丛林") || !strings.Contains(dpsThoreauOpening, "发现自己从没有活过") {
		t.Fatalf("Thoreau opening quote incomplete: %q", dpsThoreauOpening)
	}

	// 排版预览条：首项纯中文，后续项全部中英对照，且支持循环索引
	if spec0 := inlinePreviewSpecimen(0); spec0.ZH != dpsShortDefaultQuote || spec0.EN != "" {
		t.Fatalf("inline specimen[0] = %+v, want default Chinese only", spec0)
	}
	for i := 1; i < len(dpsInlineSpecimens); i++ {
		sp := inlinePreviewSpecimen(i)
		if strings.TrimSpace(sp.ZH) == "" || strings.TrimSpace(sp.EN) == "" {
			t.Fatalf("inline specimen[%d] should contain both ZH and EN: %+v", i, sp)
		}
	}
	if inlinePreviewSpecimen(len(dpsInlineSpecimens)) != inlinePreviewSpecimen(0) {
		t.Fatalf("inlinePreviewSpecimen should wrap around")
	}

	// 字体二级页标本卡：首项为梭罗 7 行，次项起为中英对照
	if spec0 := fontsPreviewSpecimen(0); spec0.ZH != dpsThoreauOpening || spec0.EN != "" {
		t.Fatalf("fonts specimen[0] = %+v, want Thoreau opening", spec0)
	}
	for i := 1; i < len(dpsFontsSpecimens); i++ {
		sp := fontsPreviewSpecimen(i)
		if strings.TrimSpace(sp.ZH) == "" || strings.TrimSpace(sp.EN) == "" {
			t.Fatalf("fonts specimen[%d] should contain both ZH and EN: %+v", i, sp)
		}
	}
	if fontsPreviewSpecimen(len(dpsFontsSpecimens)) != fontsPreviewSpecimen(0) {
		t.Fatalf("fontsPreviewSpecimen should wrap around")
	}

	// 午夜洞穴时间窗口：00:00–03:59 为 true，其余为 false
	loc := time.Local
	cases := []struct {
		h, m int
		want bool
	}{
		{23, 59, false},
		{0, 0, true},
		{2, 30, true},
		{3, 59, true},
		{4, 0, false},
		{14, 0, false},
	}
	for _, tc := range cases {
		ts := time.Date(2026, 11, 24, tc.h, tc.m, 0, 0, loc)
		if got := isMidnightCaveTime(ts); got != tc.want {
			t.Fatalf("isMidnightCaveTime(%02d:%02d) = %v, want %v", tc.h, tc.m, got, tc.want)
		}
	}
}

func TestDeadPoetsEasterEggsUIInteraction(t *testing.T) {
	ctx := t.Context()
	svc, err := app.Open(ctx, t.TempDir(), app.NewMemorySecretStore())
	if err != nil {
		t.Fatalf("app.Open: %v", err)
	}
	defer svc.Close()

	appUI := NewAppUI(svc, nil, nil)
	appUI.SetAppVersion("0.1.3")
	appUI.reloadSync(ctx)

	// 1. 白天空书库显示「今日重读」+ 及时行乐经典台词
	appUI.nowFn = func() time.Time { return time.Date(2026, 11, 24, 15, 0, 0, 0, time.Local) }
	appUI.activeTab = components.TabShelf
	appUI.shelfSubTab = components.SubTabBooks
	tt := ui.NewTester(appUI.Render, 390, 844)
	tt.Frame()
	if _, ok := tt.Find("今日重读  ·  静候第一卷诗集"); !ok {
		t.Fatalf("expected daytime empty hero label")
	}
	if _, ok := tt.Find(dpsEmptyHeroQuote); !ok {
		t.Fatalf("expected Dead Poets empty hero quote")
	}

	// 2. 午夜洞穴（01:30）：空书库变为「午夜洞穴」+ 梦中自由；空散页附微光引文
	appUI.nowFn = func() time.Time { return time.Date(2026, 11, 24, 1, 30, 0, 0, time.Local) }
	tt.Frame()
	if _, ok := tt.Find("午夜洞穴  ·  静候第一卷诗集"); !ok {
		t.Fatalf("expected midnight cave hero label at 01:30")
	}
	if _, ok := tt.Find(dpsMidnightHeroQuote); !ok {
		t.Fatalf("expected midnight cave hero quote at 01:30")
	}

	appUI.shelfSubTab = components.SubTabQuotes
	tt.Frame()
	if _, ok := tt.Find(dpsMidnightQuietLine); !ok {
		t.Fatalf("expected midnight cave quiet line in empty quotes")
	}

	// 3. 「我的」主页：排版预览条轻点切换中英对照
	appUI.activeTab = components.TabProfile
	appUI.profileSub = profileSubpageNone
	tt.Frame()
	if _, ok := tt.Find(dpsShortDefaultQuote); !ok {
		t.Fatalf("expected short Dead Poets default quote in profile preview")
	}
	if err := tt.Click(dpsShortDefaultQuote); err != nil {
		t.Fatalf("click inline preview quote: %v", err)
	}
	tt.Frame()
	if appUI.inlineSpecimenIdx != 1 {
		t.Fatalf("inlineSpecimenIdx = %d, want 1", appUI.inlineSpecimenIdx)
	}
	if _, ok := tt.Find(dpsInlineSpecimens[1].EN); !ok {
		t.Fatalf("expected bilingual English line after tapping inline preview")
	}

	// 4. 字体二级页：默认梭罗 7 行，轻点切换至梭罗中英对照
	appUI.openProfileSubpage(profileSubpageFonts)
	tt.Frame()
	advanceFrames(tt, 450*time.Millisecond)
	if _, ok := tt.Find(dpsThoreauOpening); !ok {
		t.Fatalf("expected Thoreau opening quote in typography subpage")
	}
	if err := tt.Click(dpsThoreauOpening); err != nil {
		t.Fatalf("click fonts specimen box: %v", err)
	}
	tt.Frame()
	if appUI.fontsSpecimenIdx != 1 {
		t.Fatalf("fontsSpecimenIdx = %d, want 1", appUI.fontsSpecimenIdx)
	}
	if _, ok := tt.Find(dpsThoreauOpeningEN); !ok {
		t.Fatalf("expected Thoreau English text after tapping fonts specimen")
	}

	// 5. 「关于 StillPoets」页：展示梭罗开场誓词扉页题记
	appUI.openProfileSubpage(profileSubpageAbout)
	tt.Frame()
	advanceFrames(tt, 450*time.Millisecond)
	if _, ok := tt.Find(dpsThoreauOpening); !ok {
		t.Fatalf("expected Thoreau epigraph in About page")
	}

	// 6. 平板空阅读器右栏：展示梭罗 7 行与午夜洞穴署名
	appUI.activeTab = components.TabShelf
	appUI.shelfSubTab = components.SubTabBooks
	ttTablet := ui.NewTester(appUI.Render, 1080, 720)
	ttTablet.Frame()
	if _, ok := ttTablet.Find(dpsThoreauOpening); !ok {
		t.Fatalf("expected Thoreau opening quote in tablet empty reader pane")
	}
	if _, ok := ttTablet.Find("—— " + dpsThoreauAttribution + " · " + dpsMidnightCaveLabel); !ok {
		t.Fatalf("expected midnight cave attribution in tablet empty reader pane")
	}
}
