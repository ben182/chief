package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ben182/chief/internal/loop"
	"github.com/ben182/chief/internal/prd"
)

// writeTransitionPRD writes a two-story prd.md with the given status lines and
// returns its path.
func writeTransitionPRD(t *testing.T, statusOne, statusTwo string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "prd.md")
	md := "# Test\n\n" +
		"### US-001: One\n**Status:** " + statusOne + "\n- [ ] a\n\n" +
		"### US-002: Two\n**Status:** " + statusTwo + "\n- [ ] b\n"
	if err := os.WriteFile(path, []byte(md), 0644); err != nil {
		t.Fatalf("write prd.md: %v", err)
	}
	return path
}

// TestIterationStart_ReloadsStoryStatusesFromDisk pins the fix for a dashboard
// that kept showing the previous story as the running one: the loop writes the
// finished story's "done" line and the next story's "in-progress" line to prd.md
// and only then reports the new iteration, so that event is the exact moment to
// re-read the file. Before, the only reload happened on EventStoryDone — which
// arrives before chief has written anything — leaving the displayed statuses to
// the file watcher alone, and stale for the whole run whenever it missed a write.
func TestIterationStart_ReloadsStoryStatusesFromDisk(t *testing.T) {
	// State on disk while US-001 runs.
	path := writeTransitionPRD(t, "in-progress", "todo")

	app := newTestApp(nil, 120, 30)
	app.prdName = "main"
	app.prdPath = path
	app.logViewer = NewLogViewer()
	loaded, err := prd.LoadPRD(path)
	if err != nil {
		t.Fatalf("load prd: %v", err)
	}
	app.prd = loaded

	cur := stepEvents(app, "main", loop.Event{Type: loop.EventIterationStart, StoryID: "US-001"})

	// The loop finishes US-001 and starts US-002: both status writes land in
	// prd.md before the new iteration is announced. No watcher is involved.
	if err := prd.SetStoryStatus(path, "US-001", "done"); err != nil {
		t.Fatalf("mark done: %v", err)
	}
	if err := prd.SetStoryStatus(path, "US-002", "in-progress"); err != nil {
		t.Fatalf("mark in-progress: %v", err)
	}

	cur = stepEvents(&cur, "main", loop.Event{Type: loop.EventIterationStart, StoryID: "US-002"})

	if !cur.prd.UserStories[0].Passes {
		t.Error("US-001 still shows as unfinished after the loop moved on")
	}
	if cur.prd.UserStories[0].InProgress {
		t.Error("US-001 still shows as the running story")
	}
	if !cur.prd.UserStories[1].InProgress {
		t.Error("US-002 does not show as the running story")
	}
	if cur.selectedIndex != 1 {
		t.Errorf("selection stayed on index %d, want the new story at 1", cur.selectedIndex)
	}
}
