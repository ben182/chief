package loop

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ben182/chief/internal/prd"
)

// TestLoop_OpenRunLog verifies that openRunLog creates a per-run, timestamped log
// file next to the PRD, records its path on the Loop (surfaced via LogPath), and
// ensures *.log is gitignored in the PRD directory.
func TestLoop_OpenRunLog(t *testing.T) {
	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.md")
	if err := os.WriteFile(prdPath, []byte("# PRD\n"), 0644); err != nil {
		t.Fatalf("write prd: %v", err)
	}

	l := NewLoop(prdPath, "test", 1, testProvider)
	if l.LogPath() != "" {
		t.Errorf("expected empty LogPath before openRunLog, got %q", l.LogPath())
	}

	if err := l.openRunLog(); err != nil {
		t.Fatalf("openRunLog: %v", err)
	}
	defer l.logFile.Close()

	logPath := l.LogPath()
	if logPath == "" {
		t.Fatal("expected LogPath to be set after openRunLog")
	}
	if filepath.Dir(logPath) != dir {
		t.Errorf("log should live next to the PRD (%s), got %s", dir, filepath.Dir(logPath))
	}
	// Name carries the provider's log base name plus a sortable timestamp.
	if ok, _ := regexp.MatchString(`^claude-\d{4}-\d{2}-\d{2}-\d{6}\.log$`, filepath.Base(logPath)); !ok {
		t.Errorf("log name %q does not match timestamped pattern", filepath.Base(logPath))
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Errorf("expected log file to exist: %v", err)
	}
	// *.log must be gitignored so run logs never get committed.
	gi, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(gi), "*.log") {
		t.Errorf("expected .gitignore to contain *.log, got:\n%s", gi)
	}
}

// TestLoop_OpenRunLog_Error verifies openRunLog surfaces an error when the log
// file cannot be created (its target directory does not exist).
func TestLoop_OpenRunLog_Error(t *testing.T) {
	dir := t.TempDir()
	// The PRD's parent directory does not exist, so OpenFile must fail.
	prdPath := filepath.Join(dir, "missing", "prd.md")

	l := NewLoop(prdPath, "test", 1, testProvider)
	if err := l.openRunLog(); err == nil {
		t.Error("expected an error when the log directory does not exist")
	}
	if l.LogPath() != "" {
		t.Errorf("LogPath must stay empty on failure, got %q", l.LogPath())
	}
}

// TestLoop_SetStatusOrFail verifies both branches of setStatusOrFail: a
// successful write returns nil and persists the status, while a failed write
// returns a wrapped error and emits an EventError so the run can stop.
func TestLoop_SetStatusOrFail(t *testing.T) {
	t.Run("success persists status and returns nil", func(t *testing.T) {
		dir := t.TempDir()
		prdPath := createTestPRD(t, dir, false) // US-001, not complete

		l := NewLoop(prdPath, "test", 1, testProvider)
		// Drain events: the success path emits none, but guard against blocking.
		go func() {
			for range l.Events() {
			}
		}()

		if err := l.setStatusOrFail(1, "US-001", "done", "failed to mark US-001 done"); err != nil {
			t.Fatalf("expected nil error on success, got %v", err)
		}

		p, err := prd.LoadPRD(prdPath)
		if err != nil {
			t.Fatalf("reload prd: %v", err)
		}
		if !p.UserStories[0].Passes {
			t.Error("expected US-001 to be marked done on disk")
		}
	})

	t.Run("failure returns wrapped error and emits EventError", func(t *testing.T) {
		dir := t.TempDir()
		// Point at a PRD that does not exist so SetStoryStatus fails on read.
		prdPath := filepath.Join(dir, "does-not-exist.md")

		l := NewLoop(prdPath, "test", 1, testProvider)

		type collected struct {
			ev Event
			ok bool
		}
		gotEv := make(chan collected, 1)
		go func() {
			for e := range l.Events() {
				if e.Type == EventError {
					gotEv <- collected{e, true}
					return
				}
			}
			gotEv <- collected{ok: false}
		}()

		failMsg := "failed to mark US-001 done in prd.md"
		err := l.setStatusOrFail(7, "US-001", "done", failMsg)
		if err == nil {
			t.Fatal("expected an error when the PRD write fails")
		}
		if !strings.Contains(err.Error(), failMsg) {
			t.Errorf("error %q should carry the failMsg prefix %q", err.Error(), failMsg)
		}
		// The underlying cause must remain unwrappable for callers.
		if errors.Unwrap(err) == nil {
			t.Error("expected the returned error to wrap the underlying write error")
		}

		close(l.events)
		c := <-gotEv
		if !c.ok {
			t.Fatal("expected an EventError to be emitted on failure")
		}
		if c.ev.Iteration != 7 || c.ev.StoryID != "US-001" {
			t.Errorf("EventError carried iteration=%d story=%q, want 7/US-001", c.ev.Iteration, c.ev.StoryID)
		}
		if c.ev.Err == nil {
			t.Error("expected EventError to carry the error")
		}
	})
}
