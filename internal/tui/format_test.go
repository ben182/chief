package tui

import "testing"

func TestFormatLineCount(t *testing.T) {
	tests := map[int]string{
		0:       "0",
		7:       "7",
		999:     "999",
		1000:    "1,000",
		4812:    "4,812",
		10000:   "10,000",
		999999:  "999,999",
		1000000: "1,000,000",
		-387:    "-387",
		-4812:   "-4,812",
	}
	for n, want := range tests {
		if got := formatLineCount(n); got != want {
			t.Errorf("formatLineCount(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestPluralize(t *testing.T) {
	tests := []struct {
		n    int
		noun string
		want string
	}{
		{1, "file", "1 file"},
		{0, "file", "0 files"},
		{47, "file", "47 files"},
		// Large counts carry the separator through from formatLineCount.
		{1200, "file", "1,200 files"},
	}
	for _, tc := range tests {
		if got := pluralize(tc.n, tc.noun); got != tc.want {
			t.Errorf("pluralize(%d, %q) = %q, want %q", tc.n, tc.noun, got, tc.want)
		}
	}
}
