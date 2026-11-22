package domain

import (
	"strings"
	"testing"
)

func TestBuildRawFilename(t *testing.T) {
	tests := []struct {
		name     string
		orig     string
		shortID  string
		expected string
	}{
		{
			name:     "chinese epub filename",
			orig:     "海子诗全集.epub",
			shortID:  "a1b2c3",
			expected: "海子诗全集_a1b2c3.epub",
		},
		{
			name:     "sanitize unsafe chars and uppercase ext",
			orig:     `path/to/里尔克:杜伊诺哀歌?*.EPUB`,
			shortID:  "ff00aa",
			expected: "里尔克_杜伊诺哀歌___ff00aa.epub",
		},
		{
			name:     "empty fallback",
			orig:     "   ",
			shortID:  "123456",
			expected: "poems_123456.epub",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildRawFilename(tc.orig, tc.shortID)
			if got != tc.expected {
				t.Fatalf("BuildRawFilename(%q, %q) = %q, want %q", tc.orig, tc.shortID, got, tc.expected)
			}
		})
	}

	longName := strings.Repeat("海", 80) + ".epub"
	gotLong := BuildRawFilename(longName, "abcdef")
	if !strings.HasSuffix(gotLong, "_abcdef.epub") {
		t.Fatalf("expected suffix _abcdef.epub, got %q", gotLong)
	}
	if len([]rune(gotLong)) > 65 {
		t.Fatalf("expected truncated filename, got rune len %d", len([]rune(gotLong)))
	}
}

func TestNormalizeWidgetRefreshMinutes(t *testing.T) {
	tests := []struct {
		in   int
		want int
	}{
		{-10, DefaultWidgetRefreshMinutes},
		{0, DefaultWidgetRefreshMinutes},
		{5, MinWidgetRefreshMinutes},
		{15, 15},
		{30, 30},
		{60, 60},
		{180, 180},
		{1440, 1440},
		{3000, MaxWidgetRefreshMinutes},
	}
	for _, tc := range tests {
		if got := NormalizeWidgetRefreshMinutes(tc.in); got != tc.want {
			t.Fatalf("NormalizeWidgetRefreshMinutes(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
