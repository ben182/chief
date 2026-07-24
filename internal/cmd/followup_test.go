package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initGitRepoOnBranch creates a git repo in dir and checks out branch, so tests
// can exercise the chief/<name> branch-inference in RunFollowup.
func initGitRepoOnBranch(t *testing.T, dir, branch string) {
	t.Helper()
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("# Test\n"), 0644); err != nil {
		t.Fatalf("failed to create README: %v", err)
	}
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "add", "."},
		{"git", "commit", "-m", "initial commit"},
		{"git", "checkout", "-B", branch},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup command %v failed: %s", args, string(out))
		}
	}
}

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

func TestPrdNameFromBranch(t *testing.T) {
	t.Run("infers name from chief branch when PRD exists", func(t *testing.T) {
		dir := t.TempDir()
		initGitRepoOnBranch(t, dir, "chief/linkedin-post-media")
		writePRD(t, dir, "linkedin-post-media")

		if got := PRDNameFromBranch(dir); got != "linkedin-post-media" {
			t.Errorf("expected %q, got %q", "linkedin-post-media", got)
		}
	})

	t.Run("empty when PRD dir is missing", func(t *testing.T) {
		dir := t.TempDir()
		initGitRepoOnBranch(t, dir, "chief/linkedin-post-media")
		// no PRD written — must not adopt the inferred name
		if got := PRDNameFromBranch(dir); got != "" {
			t.Errorf("expected empty (no PRD), got %q", got)
		}
	})

	t.Run("empty on a non-chief branch", func(t *testing.T) {
		dir := t.TempDir()
		initGitRepoOnBranch(t, dir, "main")
		writePRD(t, dir, "main")
		if got := PRDNameFromBranch(dir); got != "" {
			t.Errorf("expected empty (non-chief branch), got %q", got)
		}
	})

	t.Run("empty outside a git repo", func(t *testing.T) {
		dir := t.TempDir()
		if got := PRDNameFromBranch(dir); got != "" {
			t.Errorf("expected empty (no git repo), got %q", got)
		}
	})
}

// TestRunFollowupInfersNameFromBranch checks that RunFollowup, given no explicit
// name, picks up the PRD matching the current chief/<name> branch rather than
// falling back to "default".
func TestRunFollowupInfersNameFromBranch(t *testing.T) {
	dir := t.TempDir()
	initGitRepoOnBranch(t, dir, "chief/feature")
	prdDir := writePRD(t, dir, "feature")
	if err := os.WriteFile(filepath.Join(prdDir, "todos.md"), []byte("- [ ] item"), 0644); err != nil {
		t.Fatalf("write todos.md: %v", err)
	}

	// No Name given: it must resolve to "feature" (from the branch) and get past
	// the PRD-exists and inbox checks, failing only on the missing Provider.
	err := RunFollowup(FollowupOptions{BaseDir: dir})
	if err == nil {
		t.Fatal("expected provider validation error")
	}
	if !contains(err.Error(), "Provider") {
		t.Fatalf("expected to reach Provider check (name inferred from branch), got: %v", err)
	}
}
