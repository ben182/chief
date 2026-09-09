package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/git"
)

// initRepoWithDevelop makes dir a git repo on main plus a develop branch that
// carries a file main doesn't have. A worktree containing that file can only
// have been cut from develop.
func initRepoWithDevelop(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %s", args, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	run("checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n"), 0644); err != nil {
		t.Fatalf("failed to write README: %v", err)
	}
	run("add", ".")
	run("commit", "-m", "initial commit")
	run("checkout", "-b", "develop")
	if err := os.WriteFile(filepath.Join(dir, "only-on-develop.txt"), []byte("marker\n"), 0644); err != nil {
		t.Fatalf("failed to write marker: %v", err)
	}
	run("add", ".")
	run("commit", "-m", "add marker")
	run("checkout", "main")
}

// baseBranchApp is an App far enough along to run the branch-creation step for
// PRD "auth" against repo dir.
func baseBranchApp(t *testing.T, dir, baseBranch string) *App {
	t.Helper()
	a := newTestApp(nil, 100, 30)
	a.baseDir = dir
	a.config = &config.Config{Worktree: config.WorktreeConfig{BaseBranch: baseBranch}}
	a.viewMode = ViewWorktreeSpinner
	a.pendingStartPRD = "auth"
	a.pendingWorktreePath = filepath.Join(dir, ".chief", "worktrees", "auth")
	a.worktreeSpinner = NewWorktreeSpinner()
	a.worktreeSpinner.Configure("auth", "chief/auth", "main", ".chief/worktrees/auth/", "")
	a.worktreeSpinner.SetSize(100, 30)
	return a
}

// The whole point of the setting is that the run's branch comes off the
// configured branch — and that a pull request for it targets the same branch,
// which is what RecordBaseBranch decides.
func TestWorktreeStepCutsBranchFromConfiguredBase(t *testing.T) {
	dir := t.TempDir()
	initRepoWithDevelop(t, dir)
	a := baseBranchApp(t, dir, "develop")

	cmd := a.runWorktreeStep(SpinnerStepCreateBranch, dir, a.pendingWorktreePath, "chief/auth")
	if cmd == nil {
		t.Fatal("expected a branch-creation command")
	}
	msg, ok := cmd().(worktreeStepResultMsg)
	if !ok {
		t.Fatalf("unexpected message type %T", cmd())
	}
	if msg.err != nil {
		t.Fatalf("branch creation failed: %v", msg.err)
	}

	if _, err := os.Stat(filepath.Join(a.pendingWorktreePath, "only-on-develop.txt")); err != nil {
		t.Errorf("worktree was not cut from develop: %v", err)
	}
	if got := git.RecordedBaseBranch(dir, "chief/auth"); got != "develop" {
		t.Errorf("RecordedBaseBranch() = %q, want %q", got, "develop")
	}
}

// Nothing configured has to stay what it was: the detected default branch.
func TestWorktreeStepFallsBackToDefaultBranch(t *testing.T) {
	dir := t.TempDir()
	initRepoWithDevelop(t, dir)
	a := baseBranchApp(t, dir, "")

	cmd := a.runWorktreeStep(SpinnerStepCreateBranch, dir, a.pendingWorktreePath, "chief/auth")
	msg := cmd().(worktreeStepResultMsg)
	if msg.err != nil {
		t.Fatalf("branch creation failed: %v", msg.err)
	}

	if _, err := os.Stat(filepath.Join(a.pendingWorktreePath, "only-on-develop.txt")); err == nil {
		t.Error("worktree was cut from develop, want the default branch")
	}
	if got := git.RecordedBaseBranch(dir, "chief/auth"); got != "main" {
		t.Errorf("RecordedBaseBranch() = %q, want %q", got, "main")
	}
}

// A base branch that exists nowhere stops the start with a message naming the
// config key, rather than quietly branching off somewhere else.
func TestWorktreeStepReportsMissingBaseBranch(t *testing.T) {
	dir := t.TempDir()
	initRepoWithDevelop(t, dir)
	a := baseBranchApp(t, dir, "nope")

	cmd := a.runWorktreeStep(SpinnerStepCreateBranch, dir, a.pendingWorktreePath, "chief/auth")
	msg := cmd().(worktreeStepResultMsg)
	if msg.err == nil {
		t.Fatal("expected an error for a base branch that does not exist")
	}

	model, _ := a.handleWorktreeStepResult(msg)
	view := model.(App).View()
	for _, want := range []string{"worktree.baseBranch", "nope"} {
		if !strings.Contains(view, want) {
			t.Errorf("spinner view does not mention %q:\n%s", want, view)
		}
	}
}

// The branch-creation step also runs for PRDs picked up without a config
// loaded, so the lookup has to survive a nil config.
func TestWorktreeStepWithoutConfig(t *testing.T) {
	dir := t.TempDir()
	initRepoWithDevelop(t, dir)
	a := baseBranchApp(t, dir, "")
	a.config = nil

	cmd := a.runWorktreeStep(SpinnerStepCreateBranch, dir, a.pendingWorktreePath, "chief/auth")
	msg := cmd().(worktreeStepResultMsg)
	if msg.err != nil {
		t.Fatalf("branch creation failed: %v", msg.err)
	}
	if got := git.RecordedBaseBranch(dir, "chief/auth"); got != "main" {
		t.Errorf("RecordedBaseBranch() = %q, want %q", got, "main")
	}
}

// The spinner announces "Creating branch 'x' from 'y'" — with a configured base
// branch, y is that branch and not the repository default.
func TestWorktreeSpinnerAnnouncesConfiguredBaseBranch(t *testing.T) {
	dir := t.TempDir()
	initRepoWithDevelop(t, dir)

	a := newTestApp(nil, 100, 40)
	a.baseDir = dir
	a.config = &config.Config{Worktree: config.WorktreeConfig{BaseBranch: "develop"}}
	a.worktreeSpinner = NewWorktreeSpinner()
	a.branchWarning = NewBranchWarning()
	a.branchWarning.SetSize(100, 40)
	a.branchWarning.SetContext("feature/x", "auth", ".chief/worktrees/auth/")
	a.branchWarning.SetDialogContext(DialogAnotherPRDRunning) // first option: create worktree
	a.branchWarning.Reset()
	a.pendingStartPRD = "auth"
	a.viewMode = ViewBranchWarning

	model, _ := a.handleBranchWarningKeys(key("enter"))
	view := model.(App).View()
	if !strings.Contains(view, "from 'develop'") {
		t.Errorf("spinner does not announce develop as the base branch:\n%s", view)
	}
}
