package loop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProgress(t *testing.T, patterns []string, entries int) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("## Codebase Patterns\n")
	for _, p := range patterns {
		b.WriteString("- " + p + "\n")
	}
	b.WriteString("\n")
	for i := 1; i <= entries; i++ {
		fmt.Fprintf(&b, "## [2026-09-25] - US-%03d\n- did a thing\n---\n", i)
	}
	path := filepath.Join(t.TempDir(), "progress.md")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The agent is told which lines hold the patterns and the last entries, so it
// reads them rather than guessing a limit and having the read refused.
func TestProgressGuidePointsAtThePatternsAndTheLastEntries(t *testing.T) {
	path := writeProgress(t, []string{"use X for Y", "never Z"}, 3)
	got := progressGuide(path)

	for _, want := range []string{
		"(13 lines)",
		"`## Codebase Patterns`: lines 1–4",
		"`## [2026-09-25] - US-002`: lines 8–10",
		"`## [2026-09-25] - US-003`: lines 11–13",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("guide lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "US-001") {
		t.Errorf("guide points at more than the last two entries:\n%s", got)
	}
	if strings.Contains(got, "budget") {
		t.Errorf("a small patterns section was flagged as over budget:\n%s", got)
	}
}

// A section too large for one read is handed out in pieces that each fit, and
// one over its budget is to be condensed before the story starts — the
// ghost-writing run's grew to 94 KB, lines of four thousand characters, and
// every iteration fought the Read tool's limit to get through it.
func TestProgressGuideSplitsAndFlagsAnOversizedPatternsSection(t *testing.T) {
	long := strings.Repeat("a pattern described at far too much length ", 90) // ~4 KB a line
	patterns := make([]string, 25)
	for i := range patterns {
		patterns[i] = long
	}
	got := progressGuide(writeProgress(t, patterns, 1))

	line := ""
	for _, l := range strings.Split(got, "\n") {
		if strings.Contains(l, "Codebase Patterns`:") {
			line = l
		}
	}
	if strings.Count(line, "lines ") < 3 {
		t.Errorf("a ~100 KB section was not split into reads that fit: %q", line)
	}
	if !strings.Contains(got, "over its 24 KB budget") || !strings.Contains(got, "Before you") {
		t.Errorf("an oversized section was not flagged for condensing:\n%s", got)
	}
}

func TestProgressGuideIsEmptyWithoutAFile(t *testing.T) {
	if got := progressGuide(filepath.Join(t.TempDir(), "progress.md")); got != "" {
		t.Errorf("guide for a missing file = %q, want nothing", got)
	}
}
