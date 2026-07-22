package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writePRD creates .chief/prds/<name>/prd.md under baseDir and returns the PRD dir.
func writePRD(t *testing.T, baseDir, name string) string {
	t.Helper()
	prdDir := filepath.Join(baseDir, ".chief", "prds", name)
	if err := os.MkdirAll(prdDir, 0755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(prdDir, "prd.md"), []byte("# PRD"), 0644); err != nil {
		t.Fatalf("Failed to create prd.md: %v", err)
	}
	return prdDir
}

func TestRunFollowupRequiresPRDExists(t *testing.T) {
	tmpDir := t.TempDir()

	err := RunFollowup(FollowupOptions{Name: "nonexistent", BaseDir: tmpDir})
	if err == nil {
		t.Fatal("Expected error for non-existent PRD")
	}
	if !contains(err.Error(), "chief new") {
		t.Errorf("Error should suggest chief new, got: %s", err.Error())
	}
}

func TestRunFollowupRejectsInvalidName(t *testing.T) {
	tmpDir := t.TempDir()

	err := RunFollowup(FollowupOptions{Name: "invalid name with space", BaseDir: tmpDir})
	if err == nil {
		t.Fatal("Expected error for invalid name")
	}
}

func TestRunFollowupRequiresInbox(t *testing.T) {
	tmpDir := t.TempDir()
	writePRD(t, tmpDir, "feature")

	// PRD exists but no inbox file — should fail before touching the provider.
	err := RunFollowup(FollowupOptions{Name: "feature", BaseDir: tmpDir})
	if err == nil {
		t.Fatal("Expected error when no follow-up inbox exists")
	}
	if !contains(err.Error(), "todos.md") {
		t.Errorf("Error should name the expected inbox file, got: %s", err.Error())
	}
}

func TestRunFollowupRequiresProvider(t *testing.T) {
	tmpDir := t.TempDir()
	prdDir := writePRD(t, tmpDir, "feature")
	if err := os.WriteFile(filepath.Join(prdDir, "todos.md"), []byte("- [ ] something"), 0644); err != nil {
		t.Fatalf("Failed to create todos.md: %v", err)
	}

	// Inbox present but no provider — the provider check must fire.
	err := RunFollowup(FollowupOptions{Name: "feature", BaseDir: tmpDir})
	if err == nil {
		t.Fatal("expected provider validation error")
	}
	if !contains(err.Error(), "Provider") {
		t.Fatalf("expected error to mention Provider, got: %v", err)
	}
}

func TestScaffoldFollowupInbox(t *testing.T) {
	tmpDir := t.TempDir()

	// Fresh dir: creates todos.md.
	if err := scaffoldFollowupInbox(tmpDir); err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	todos := filepath.Join(tmpDir, "todos.md")
	content, err := os.ReadFile(todos)
	if err != nil {
		t.Fatalf("expected todos.md to be created: %v", err)
	}
	// The scaffold is comment-only: everything sits inside an HTML comment, so
	// `chief followup` sees no open items until the user adds real ones.
	s := string(content)
	if !contains(s, "<!--") || !contains(s, "-->") {
		t.Error("scaffold should be wrapped in an HTML comment")
	}
	if before, _, _ := strings.Cut(s, "<!--"); contains(before, "- [ ]") {
		t.Error("scaffold has an open checkbox item outside the comment")
	}
	if _, after, found := strings.Cut(s, "-->"); found && contains(after, "- [ ]") {
		t.Error("scaffold has an open checkbox item after the comment")
	}

	// Idempotent: does not overwrite an existing inbox.
	if err := os.WriteFile(todos, []byte("- [ ] real item"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := scaffoldFollowupInbox(tmpDir); err != nil {
		t.Fatalf("scaffold (existing): %v", err)
	}
	again, _ := os.ReadFile(todos)
	if string(again) != "- [ ] real item" {
		t.Errorf("scaffold overwrote an existing inbox: %q", string(again))
	}

	// Also skips when only a fallback-named inbox exists.
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, "followups.md"), []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := scaffoldFollowupInbox(dir2); err != nil {
		t.Fatalf("scaffold (fallback present): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir2, "todos.md")); !os.IsNotExist(err) {
		t.Error("scaffold created todos.md even though followups.md already exists")
	}
}

func TestFindFollowupInbox(t *testing.T) {
	tmpDir := t.TempDir()

	if got := findFollowupInbox(tmpDir); got != "" {
		t.Errorf("expected no inbox, got %q", got)
	}

	// followups.md is accepted as a fallback name.
	fallback := filepath.Join(tmpDir, "followups.md")
	if err := os.WriteFile(fallback, []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := findFollowupInbox(tmpDir); got != fallback {
		t.Errorf("expected fallback inbox %q, got %q", fallback, got)
	}

	// todos.md takes precedence when both exist.
	todos := filepath.Join(tmpDir, "todos.md")
	if err := os.WriteFile(todos, []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := findFollowupInbox(tmpDir); got != todos {
		t.Errorf("expected todos.md to win, got %q", got)
	}
}
