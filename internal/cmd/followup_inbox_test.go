package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// The inbox is the one file in a PRD directory a human writes by hand, and they
// write it in the project. A run working in a worktree keeps its own copy of
// everything else, so ingesting the worktree's inbox would skip whatever the
// user just typed.
func TestFollowupIngestsTheInboxTheUserWrote(t *testing.T) {
	home := t.TempDir()
	run := t.TempDir()

	if err := os.WriteFile(filepath.Join(home, "todos.md"), []byte("- [ ] the new one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run, "todos.md"), []byte("- [ ] the stale one\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := takeInboxAlong(home, run)
	if err != nil {
		t.Fatalf("takeInboxAlong() error = %v", err)
	}
	if want := filepath.Join(run, "todos.md"); got != want {
		t.Errorf("ingesting %q, want the worktree's copy %q", got, want)
	}
	data, err := os.ReadFile(got)
	if err != nil || string(data) != "- [ ] the new one\n" {
		t.Errorf("inbox to ingest = %q (err %v), want what the user wrote", string(data), err)
	}

	// What the agent ticked off has to show up where the user reads it, or they
	// find the same items open again and ingest half of them twice.
	if err := os.WriteFile(got, []byte("- [x] (LFC-009) the new one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := returnInbox(home, got); err != nil {
		t.Fatalf("returnInbox() error = %v", err)
	}
	data, err = os.ReadFile(filepath.Join(home, "todos.md"))
	if err != nil || string(data) != "- [x] (LFC-009) the new one\n" {
		t.Errorf("the project's inbox = %q (err %v), want the ticked-off list", string(data), err)
	}
}

// Without a worktree — and for a PRD whose inbox only exists on one side — the
// lookup is the plain one it replaced.
func TestFollowupInboxWithoutAWorktree(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "followups.md"), []byte("- [ ] one\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := takeInboxAlong(dir, dir)
	if err != nil {
		t.Fatalf("takeInboxAlong() error = %v", err)
	}
	if want := filepath.Join(dir, "followups.md"); got != want {
		t.Errorf("takeInboxAlong() = %q, want %q", got, want)
	}

	t.Run("only the worktree has one", func(t *testing.T) {
		home := t.TempDir()
		run := t.TempDir()
		if err := os.WriteFile(filepath.Join(run, "todos.md"), []byte("- [ ] one\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := takeInboxAlong(home, run)
		if err != nil {
			t.Fatalf("takeInboxAlong() error = %v", err)
		}
		if want := filepath.Join(run, "todos.md"); got != want {
			t.Errorf("takeInboxAlong() = %q, want %q", got, want)
		}
	})

	t.Run("nowhere at all", func(t *testing.T) {
		got, err := takeInboxAlong(t.TempDir(), t.TempDir())
		if err != nil {
			t.Fatalf("takeInboxAlong() error = %v", err)
		}
		if got != "" {
			t.Errorf("takeInboxAlong() = %q, want no inbox", got)
		}
	})
}
