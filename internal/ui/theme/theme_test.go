package theme

import "testing"

func TestResolveFollowSystemDark(t *testing.T) {
	cases := []struct {
		name             string
		preferred        PresetID
		followSystemDark bool
		systemDark       bool
		wantID           PresetID
	}{
		{
			name:             "light system keeps artbook",
			preferred:        PresetArtbook,
			followSystemDark: true,
			systemDark:       false,
			wantID:           PresetArtbook,
		},
		{
			name:             "dark system switches artbook to dark_minimal",
			preferred:        PresetArtbook,
			followSystemDark: true,
			systemDark:       true,
			wantID:           PresetDarkMinimal,
		},
		{
			name:             "dark system switches editorial to dark_minimal",
			preferred:        PresetEditorial,
			followSystemDark: true,
			systemDark:       true,
			wantID:           PresetDarkMinimal,
		},
		{
			name:             "dark system with follow disabled keeps editorial",
			preferred:        PresetEditorial,
			followSystemDark: false,
			systemDark:       true,
			wantID:           PresetEditorial,
		},
		{
			name:             "light system with manual dark_minimal stays dark",
			preferred:        PresetDarkMinimal,
			followSystemDark: false,
			systemDark:       false,
			wantID:           PresetDarkMinimal,
		},
		{
			name:             "empty preset falls back to artbook in light and dark_minimal in dark",
			preferred:        "",
			followSystemDark: true,
			systemDark:       true,
			wantID:           PresetDarkMinimal,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Resolve(tc.preferred, tc.followSystemDark, tc.systemDark)
			if got.ID != tc.wantID {
				t.Fatalf("Resolve(%q, follow=%v, dark=%v) = %q, want %q",
					tc.preferred, tc.followSystemDark, tc.systemDark, got.ID, tc.wantID)
			}
		})
	}
}

func TestQuoteSwatchAiryRatioAndNonAdjacency(t *testing.T) {
	for _, pal := range Presets() {
		filled := 0
		const total = 20
		for i := 0; i < total; i++ {
			sw := pal.QuoteSwatchFor(i, false)
			if sw.IsFilled {
				filled++
			}
			// 独立散句始终保持纸白/细线卡
			if st := pal.QuoteSwatchFor(i, true); st.IsFilled {
				t.Fatalf("%s: standalone quote at %d should never use filled swatch", pal.ID, i)
			}
		}
		// 保持 20% 彩色点缀（80% 纸白/浅米留白卡）
		if filled != 4 {
			t.Fatalf("%s: expected 4/20 (20%%) colored quote cards, got %d/%d", pal.ID, filled, total)
		}
	}
}

func TestPaperGrainAndPoetryTypography(t *testing.T) {
	if got := Artbook().PaperGrainOpacity(); got < 0.03 || got > 0.05 {
		t.Fatalf("Artbook paper grain opacity = %v, want in [0.03, 0.05]", got)
	}
	if got := DarkMinimal().PaperGrainOpacity(); got < 0.03 || got > 0.05 {
		t.Fatalf("DarkMinimal paper grain opacity = %v, want in [0.03, 0.05]", got)
	}
	if got := Editorial().PaperGrainOpacity(); got != 0 {
		t.Fatalf("Editorial paper grain opacity = %v, want 0 to preserve crisp #FFFFFF paper", got)
	}
	if got := PoetryFont(""); got != FontSerif {
		t.Fatalf("PoetryFont empty = %q, want %q", got, FontSerif)
	}
	if got := PoetryFont("LXGW WenKai"); got != "LXGW WenKai, "+FontSerif {
		t.Fatalf("PoetryFont custom = %q", got)
	}
}
