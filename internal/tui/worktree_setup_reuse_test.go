package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/loop"
)

// reuseApp is an App sitting on the worktree spinner for PRD "auth" against the
// real repo in dir. Unlike setupApp it goes through the branch-creation step
// itself, because whether the worktree is created or reused is exactly what
// these tests are about.
func reuseApp(t *testing.T, dir string, cfg config.WorktreeConfig) *App {
	t.Helper()
	a := newTestApp(nil, 100, 40)
	a.baseDir = dir
	a.manager = loop.NewManager(10, nil)
	a.config = &config.Config{Worktree: cfg}
	a.viewMode = ViewWorktreeSpinner
	a.pendingStartPRD = "auth"
	a.pendingWorktreePath = filepath.Join(dir, ".chief", "worktrees", "auth")
	a.worktreeSpinner = NewWorktreeSpinner()
	a.worktreeSpinner.Configure("auth", "chief/auth", "main", ".chief/worktrees/auth/", cfg.Setup)
	a.worktreeSpinner.SetSize(100, 40)
	return a
}

// createWorktree runs the branch-and-worktree step and returns its result.
func createWorktree(t *testing.T, a *App) worktreeStepResultMsg {
	t.Helper()
	cmd := a.runWorktreeStep(SpinnerStepCreateBranch, a.baseDir, a.pendingWorktreePath, "chief/auth")
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
	return msg
}

// ranSetup drives the step result the way the app does and reports which setup
// command reached git.RunSetup, if any.
func ranSetup(t *testing.T, a *App, msg worktreeStepResultMsg) string {
	t.Helper()
	var got string
	a.runSetup = func(wt git.WorktreeContext, setup string, opts git.RunOptions) (git.RunResult, error) {
		got = setup
		return git.RunResult{}, nil
	}
	model, cmd := a.handleWorktreeStepResult(msg)
	// Once the app has left the spinner the setup is settled and the command it
	// returned starts the agent loop — not something a test wants to run.
	if model.(App).viewMode == ViewWorktreeSpinner && cmd != nil {
		cmd()
	}
	return got
}

// Not configuring anything has to keep doing what every run did before the key
// existed: set the worktree up again, reused or not.
func TestSetupRunsOnReuseByDefault(t *testing.T) {
	dir := t.TempDir()
	initRepoWithDevelop(t, dir)
	cfg := config.WorktreeConfig{Setup: "make setup"}

	createWorktree(t, reuseApp(t, dir, cfg))

	a := reuseApp(t, dir, cfg)
	msg := createWorktree(t, a)
	if !msg.reused {
		t.Fatal("the second run did not reuse the worktree")
	}
	if got := ranSetup(t, a, msg); got != "make setup" {
		t.Errorf("setup command run on reuse = %q, want %q", got, "make setup")
	}
}

// setupOnReuse: false is for the setup that is expensive or not idempotent: a
// worktree that is already there gets picked up as it stands.
func TestSetupIsSkippedOnReuseWhenTurnedOff(t *testing.T) {
	dir := t.TempDir()
	initRepoWithDevelop(t, dir)
	cfg := config.WorktreeConfig{Setup: "make setup", SetupOnReuse: config.Bool(false)}

	createWorktree(t, reuseApp(t, dir, cfg))

	a := reuseApp(t, dir, cfg)
	msg := createWorktree(t, a)
	if !msg.reused {
		t.Fatal("the second run did not reuse the worktree")
	}
	if got := ranSetup(t, a, msg); got != "" {
		t.Errorf("setup command %q ran on a reused worktree, want none", got)
	}

	view := a.worktreeSpinner.Render()
	if !strings.Contains(view, "Skipped setup") {
		t.Errorf("the spinner does not say the setup was skipped:\n%s", view)
	}
}

// The setting is about reuse only. A worktree created from scratch has never
// been set up, so it is set up whatever the flag says.
func TestSetupRunsOnAFreshWorktreeWithSetupOnReuseTurnedOff(t *testing.T) {
	dir := t.TempDir()
	initRepoWithDevelop(t, dir)

	a := reuseApp(t, dir, config.WorktreeConfig{Setup: "make setup", SetupOnReuse: config.Bool(false)})
	msg := createWorktree(t, a)
	if msg.reused {
		t.Fatal("a freshly created worktree was reported as reused")
	}
	if got := ranSetup(t, a, msg); got != "make setup" {
		t.Errorf("setup command run on a fresh worktree = %q, want %q", got, "make setup")
	}
}
