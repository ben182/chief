package loop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ben182/chief/internal/prd"
)

// git runs a git command in dir and fails the test on error.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %s", args, dir, string(out))
	}
}

// gitStatusShort returns the porcelain status of dir.
func gitStatusShort(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "status", "--short")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git status in %s: %v", dir, err)
	}
	return strings.TrimSpace(string(out))
}

// A run given a worktree keeps the PRD's working files there, which is the only
// way its per-story commit can carry them: staging a path in the project from
// inside the worktree fails with "outside repository", so before this the status
// a run recorded was left behind as an uncommitted change on whatever branch the
// project happened to be on.
func TestLoop_CommitStoryProgressFromAWorktree(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)

	homeDir := filepath.Join(repo, ".chief", "prds", "auth")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, "prd.md"), []byte("# PRD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Chief ignores the directory its worktrees live in when it creates one, so
	// the project's status here is about the run, not about the worktree itself.
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(".chief/worktrees/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-m", "add PRD")

	worktree := filepath.Join(repo, ".chief", "worktrees", "auth")
	gitIn(t, repo, "worktree", "add", "-b", "chief/auth", worktree)

	// What the start does: the worktree gets its own copy of the working files.
	runDir := filepath.Join(worktree, ".chief", "prds", "auth")
	if err := prd.Mirror(homeDir, runDir); err != nil {
		t.Fatalf("Mirror() error = %v", err)
	}

	// The run records progress in its own copy and the agent commits its story.
	runPRD := filepath.Join(runDir, "prd.md")
	if err := os.WriteFile(runPRD, []byte("# PRD\n**Status:** in-progress\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "progress.md"), []byte("# progress\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "app.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, worktree, "add", "app.go")
	gitIn(t, worktree, "commit", "-m", "feat: auth/US-001 - Story One")

	l := NewLoopWithWorkDir(runPRD, worktree, "", 1, testProvider)
	l.commitStoryProgress("US-001", "Story One")

	if !gitTracked(t, worktree, ".chief/prds/auth/prd.md") {
		t.Error("the worktree's prd.md should ride in the story commit")
	}
	if !gitTracked(t, worktree, ".chief/prds/auth/progress.md") {
		t.Error("the worktree's progress.md should ride in the story commit")
	}
	if status := gitStatusShort(t, worktree); status != "" {
		t.Errorf("worktree should be clean after the commit, got:\n%s", status)
	}
	// The point of the worktree: the project is left alone.
	if status := gitStatusShort(t, repo); status != "" {
		t.Errorf("the project should be untouched by a worktree run, got:\n%s", status)
	}
	if data, err := os.ReadFile(filepath.Join(homeDir, "prd.md")); err != nil || string(data) != "# PRD\n" {
		t.Errorf("the project's prd.md = %q (err %v), want it unchanged", string(data), err)
	}
}
