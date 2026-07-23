package prd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFollowupInboxPath(t *testing.T) {
	tmpDir := t.TempDir()

	if got := FollowupInboxPath(tmpDir); got != "" {
		t.Errorf("expected no inbox, got %q", got)
	}

	// followups.md is accepted as a fallback name.
	fallback := filepath.Join(tmpDir, "followups.md")
	if err := os.WriteFile(fallback, []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := FollowupInboxPath(tmpDir); got != fallback {
		t.Errorf("expected fallback inbox %q, got %q", fallback, got)
	}

	// todos.md takes precedence when both exist.
	todos := filepath.Join(tmpDir, "todos.md")
	if err := os.WriteFile(todos, []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := FollowupInboxPath(tmpDir); got != todos {
		t.Errorf("expected todos.md to win, got %q", got)
	}
}
