package tui

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/git"
)

// initRepoWithRecordedBase makes dir a git repo and records base as the branch
// branch was cut from — the state chief leaves behind after creating a worktree
// branch. The record lives in the repo's git config, so `git init` is enough.
func initRepoWithRecordedBase(t *testing.T, dir, branch, base string) {
	t.Helper()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %s", out)
	}
	git.RecordBaseBranch(dir, branch, base)
}

// A setup script derives a database name, a hostname or a diff target from the
// PRD and the branch, so it has to be told both rather than parse the directory
// it happens to sit in.
func TestWorktreeSetupGetsTheWorktreeContext(t *testing.T) {
	base := t.TempDir()
	initRepoWithRecordedBase(t, base, "chief/auth", "develop")
	worktreePath := filepath.Join(base, ".chief", "worktrees", "auth")

	a := newTestApp(nil, 100, 30)
	a.baseDir = base
	a.config = &config.Config{Worktree: config.WorktreeConfig{Setup: "make setup"}}
	a.viewMode = ViewWorktreeSpinner
	a.pendingStartPRD = "auth"
	a.pendingWorktreePath = worktreePath
	a.worktreeSpinner = NewWorktreeSpinner()
	a.worktreeSpinner.Configure("auth", "chief/auth", "develop", ".chief/worktrees/auth/", "make setup")

	var got git.WorktreeContext
	var gotCommand string
	a.runSetup = func(wt git.WorktreeContext, setup string) (string, error) {
		got, gotCommand = wt, setup
		return "", nil
	}

	// The branch and worktree are in place; the setup step is what comes next.
	_, cmd := a.handleWorktreeStepResult(worktreeStepResultMsg{step: SpinnerStepCreateBranch})
	if cmd == nil {
		t.Fatal("expected a setup command")
	}
	cmd()

	if gotCommand != "make setup" {
		t.Errorf("setup command = %q, want %q", gotCommand, "make setup")
	}
	want := git.WorktreeContext{
		PRDName:      "auth",
		Branch:       "chief/auth",
		BaseBranch:   "develop",
		WorktreePath: worktreePath,
		RepoDir:      base,
	}
	if got != want {
		t.Errorf("setup context = %+v, want %+v", got, want)
	}
}

// The teardown drops the same resources the setup created, so it needs the same
// context — including the base branch recorded when the branch was cut.
func TestCleanGivesTheTeardownTheWorktreeContext(t *testing.T) {
	a := cleanApp(t, "make drop-db", "chief/auth", 1) // "Remove worktree only"
	initRepoWithRecordedBase(t, a.baseDir, "chief/auth", "develop")

	var got git.WorktreeContext
	a.runTeardown = func(wt git.WorktreeContext, teardown string) (string, error) {
		got = wt
		return "", nil
	}
	a.removeWorktree = func(repoDir, worktreePath string) error { return nil }

	_, cmd := a.handlePickerKeys(key("enter"))
	if cmd == nil {
		t.Fatal("expected a clean command")
	}
	cmd()

	want := git.WorktreeContext{
		PRDName:      "auth",
		Branch:       "chief/auth",
		BaseBranch:   "develop",
		WorktreePath: filepath.Join(a.baseDir, ".chief", "worktrees", "auth"),
		RepoDir:      a.baseDir,
	}
	if got != want {
		t.Errorf("teardown context = %+v, want %+v", got, want)
	}
}
