package tui

import (
	"errors"
	"strings"
	"testing"
)

// loadedDiffViewer builds a viewer holding a fixed diff, bypassing git so the
// scroll and render logic can be tested without a repository.
func loadedDiffViewer(lines []string, width, height int) *DiffViewer {
	return &DiffViewer{
		lines:  lines,
		width:  width,
		height: height,
		loaded: true,
	}
}

func diffLines(n int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = " context line"
	}
	return lines
}

func TestDiffViewerScrollDownStopsAtMaxOffset(t *testing.T) {
	d := loadedDiffViewer(diffLines(30), 80, 10)

	for i := 0; i < 100; i++ {
		d.ScrollDown()
	}

	// 30 lines in a 10-line viewport leaves 20 lines of scroll.
	if d.offset != 20 {
		t.Errorf("expected offset to stop at 20, got %d", d.offset)
	}
}

func TestDiffViewerScrollUpStopsAtTop(t *testing.T) {
	d := loadedDiffViewer(diffLines(30), 80, 10)
	d.offset = 3

	for i := 0; i < 10; i++ {
		d.ScrollUp()
	}

	if d.offset != 0 {
		t.Errorf("expected offset to stop at 0, got %d", d.offset)
	}
}

func TestDiffViewerPageDownAndUpMoveHalfAPage(t *testing.T) {
	d := loadedDiffViewer(diffLines(100), 80, 20)

	d.PageDown()
	if d.offset != 10 {
		t.Errorf("expected offset 10 after PageDown, got %d", d.offset)
	}

	d.PageUp()
	if d.offset != 0 {
		t.Errorf("expected offset 0 after PageUp, got %d", d.offset)
	}
}

func TestDiffViewerPageDownClampsToMaxOffset(t *testing.T) {
	d := loadedDiffViewer(diffLines(25), 80, 20)

	for i := 0; i < 5; i++ {
		d.PageDown()
	}

	if d.offset != 5 {
		t.Errorf("expected offset clamped to 5, got %d", d.offset)
	}
}

func TestDiffViewerPageUpClampsToTop(t *testing.T) {
	d := loadedDiffViewer(diffLines(100), 80, 20)
	d.offset = 5

	d.PageUp()

	if d.offset != 0 {
		t.Errorf("expected offset clamped to 0, got %d", d.offset)
	}
}

func TestDiffViewerScrollToTopAndBottom(t *testing.T) {
	d := loadedDiffViewer(diffLines(40), 80, 10)

	d.ScrollToBottom()
	if d.offset != 30 {
		t.Errorf("expected offset 30 at bottom, got %d", d.offset)
	}

	d.ScrollToTop()
	if d.offset != 0 {
		t.Errorf("expected offset 0 at top, got %d", d.offset)
	}
}

func TestDiffViewerShortDiffDoesNotScroll(t *testing.T) {
	// A diff that fits the viewport has nothing to scroll; offset must stay put
	// so the view doesn't slide content off the top.
	d := loadedDiffViewer(diffLines(5), 80, 20)

	d.ScrollDown()
	d.PageDown()
	d.ScrollToBottom()

	if d.offset != 0 {
		t.Errorf("expected offset 0 for a diff shorter than the viewport, got %d", d.offset)
	}
}

func TestDiffViewerRenderBeforeLoad(t *testing.T) {
	d := NewDiffViewer("/tmp")
	d.SetSize(80, 20)

	if got := d.Render(); !strings.Contains(got, "Loading diff") {
		t.Errorf("expected a loading placeholder before load, got %q", got)
	}
}

func TestDiffViewerRenderError(t *testing.T) {
	d := loadedDiffViewer(nil, 80, 20)
	d.err = errors.New("not a git repository")

	got := d.Render()
	if !strings.Contains(got, "not a git repository") {
		t.Errorf("expected the git error surfaced in the view, got %q", got)
	}
}

func TestDiffViewerRenderNoChanges(t *testing.T) {
	d := loadedDiffViewer(nil, 80, 20)

	if got := d.Render(); !strings.Contains(got, "No changes detected") {
		t.Errorf("expected the empty-diff message, got %q", got)
	}
}

func TestDiffViewerRenderNoChangesForStory(t *testing.T) {
	d := loadedDiffViewer(nil, 80, 20)
	d.storyID = "US-004"

	// A story whose commit exists but changed nothing names the story, so the
	// user can tell which of the two empty states they are looking at.
	got := d.Render()
	if !strings.Contains(got, "No changes for US-004") {
		t.Errorf("expected the per-story empty message, got %q", got)
	}
}

func TestDiffViewerRenderUncommittedStory(t *testing.T) {
	d := loadedDiffViewer(nil, 80, 20)
	d.storyID = "US-004"
	d.noCommit = true

	// No commit yet is a different state from "commit with no changes" and has
	// to read differently, or the user thinks the story produced nothing.
	got := d.Render()
	if !strings.Contains(got, "Not committed yet") {
		t.Errorf("expected the uncommitted message, got %q", got)
	}
	if !strings.Contains(got, "US-004") {
		t.Errorf("expected the story ID in the uncommitted message, got %q", got)
	}
}

func TestDiffViewerRenderShowsOnlyTheViewportWindow(t *testing.T) {
	lines := []string{"line0", "line1", "line2", "line3", "line4", "line5"}
	d := loadedDiffViewer(lines, 80, 3)
	d.offset = 2

	got := d.Render()
	rendered := strings.Split(got, "\n")
	if len(rendered) != 3 {
		t.Fatalf("expected 3 rendered lines for a 3-line viewport, got %d:\n%s", len(rendered), got)
	}
	if !strings.Contains(got, "line2") || !strings.Contains(got, "line4") {
		t.Errorf("expected lines 2..4 in the window, got:\n%s", got)
	}
	if strings.Contains(got, "line1") || strings.Contains(got, "line5") {
		t.Errorf("expected lines outside the window to be omitted, got:\n%s", got)
	}
}

func TestDiffViewerRenderTruncatesLongLines(t *testing.T) {
	long := "+" + strings.Repeat("x", 200)
	d := loadedDiffViewer([]string{long}, 40, 5)

	got := d.Render()
	// Truncation keeps a long diff line from wrapping and pushing the rest of
	// the viewport off screen.
	if len([]rune(stripANSI(got))) > 40 {
		t.Errorf("expected the line truncated to 40 columns, got %d: %q", len([]rune(stripANSI(got))), got)
	}
}

func TestDiffViewerStyleLineClassifiesDiffSyntax(t *testing.T) {
	d := loadedDiffViewer(nil, 80, 20)

	// styleLine only adds colour, so the payload has to survive verbatim for
	// every branch — a mangled classification would silently drop content.
	for _, line := range []string{
		"+++ b/main.go",
		"--- a/main.go",
		"@@ -1,4 +1,6 @@",
		"+added",
		"-removed",
		"diff --git a/main.go b/main.go",
		"index abc123..def456 100644",
		"new file mode 100644",
		"deleted file mode 100644",
		" unchanged",
	} {
		if got := stripANSI(d.styleLine(line)); got != line {
			t.Errorf("styleLine(%q) changed the text to %q", line, got)
		}
	}
}

func TestDiffViewerLoadForStoryWithoutRepoShowsUncommitted(t *testing.T) {
	// A directory that is not a git repo makes the commit lookup fail, which is
	// the same user-visible state as a story that hasn't been committed.
	d := NewDiffViewer(t.TempDir())
	d.SetSize(80, 20)

	d.LoadForStory("default", "US-001", "Some story")

	if !d.noCommit {
		t.Error("expected noCommit when the commit lookup fails")
	}
	if d.err != nil {
		t.Errorf("expected no error surfaced for a missing commit, got %v", d.err)
	}
	if got := d.Render(); !strings.Contains(got, "Not committed yet") {
		t.Errorf("expected the uncommitted message, got %q", got)
	}
}

func TestDiffViewerSetBaseDirAndSetSize(t *testing.T) {
	d := NewDiffViewer("/original")

	d.SetBaseDir("/switched")
	d.SetSize(120, 40)

	if d.baseDir != "/switched" {
		t.Errorf("expected baseDir '/switched', got %q", d.baseDir)
	}
	if d.width != 120 || d.height != 40 {
		t.Errorf("expected size 120x40, got %dx%d", d.width, d.height)
	}
}
