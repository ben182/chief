package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/loop"
)

// loopControlApp builds an App with a manager holding the given PRDs, which is
// the minimum state the start/pause/stop helpers touch.
func loopControlApp(t *testing.T, activePRD string, register ...string) *App {
	t.Helper()
	m := loop.NewManager(10, nil)
	for _, name := range register {
		if err := m.Register(name, "/proj/.chief/prds/"+name+"/prd.md"); err != nil {
			t.Fatalf("register %q: %v", name, err)
		}
	}
	a := newTestApp(nil, 100, 30)
	a.manager = m
	a.prdName = activePRD
	a.baseDir = "/proj"
	return a
}

func TestPauseLoopReportsWhenNothingIsRunning(t *testing.T) {
	a := loopControlApp(t, "auth", "auth")

	// The loop is registered but never started, so Pause fails. Before the error
	// was surfaced, the TUI claimed "Pausing after current story..." with nothing
	// to pause.
	model, _ := a.pauseLoopForPRD("auth")

	got := model.(App)
	if !strings.HasPrefix(got.lastActivity, "Cannot pause:") {
		t.Errorf("expected a 'Cannot pause' message, got %q", got.lastActivity)
	}
	if strings.Contains(got.lastActivity, "Pausing") {
		t.Errorf("expected no pause confirmation for an idle loop, got %q", got.lastActivity)
	}
}

func TestPauseLoopReportsForUnknownPRD(t *testing.T) {
	a := loopControlApp(t, "auth", "auth")

	model, _ := a.pauseLoopForPRD("does-not-exist")

	got := model.(App)
	if !strings.HasPrefix(got.lastActivity, "Cannot pause:") {
		t.Errorf("expected a 'Cannot pause' message for an unknown PRD, got %q", got.lastActivity)
	}
}

func TestPauseLoopWithoutManagerIsInert(t *testing.T) {
	a := newTestApp(nil, 100, 30)
	a.prdName = "auth"
	// No manager: the dashboard can be up before the manager exists in tests and
	// in the error path of NewApp.
	model, cmd := a.pauseLoopForPRD("auth")

	got := model.(App)
	if cmd != nil {
		t.Error("expected no command when there is no manager")
	}
	if !strings.Contains(got.lastActivity, "Pausing") {
		t.Errorf("expected the pause message without a manager, got %q", got.lastActivity)
	}
}

func TestStopLoopAndUpdateForActivePRDSetsStoppedState(t *testing.T) {
	a := loopControlApp(t, "auth", "auth")

	model, _ := a.stopLoopAndUpdateForPRD("auth")

	got := model.(App)
	if got.state != StateStopped {
		t.Errorf("expected state Stopped for the active PRD, got %v", got.state)
	}
	if got.lastActivity != "Stopped" {
		t.Errorf("expected lastActivity 'Stopped', got %q", got.lastActivity)
	}
}

func TestStopLoopAndUpdateForBackgroundPRDKeepsActiveState(t *testing.T) {
	a := loopControlApp(t, "auth", "auth", "billing")
	a.state = StateRunning

	model, _ := a.stopLoopAndUpdateForPRD("billing")

	got := model.(App)
	// Stopping a background PRD must not mark the viewed PRD as stopped.
	if got.state != StateRunning {
		t.Errorf("expected the active PRD's state untouched, got %v", got.state)
	}
	if got.lastActivity != "Stopped billing" {
		t.Errorf("expected lastActivity 'Stopped billing', got %q", got.lastActivity)
	}
}

func TestStopLoopForPRDWithoutManagerDoesNotPanic(t *testing.T) {
	a := newTestApp(nil, 100, 30)

	a.stopLoopForPRD("auth") // must be a no-op, not a nil dereference
}

func TestStopAllLoopsWithoutManagerDoesNotPanic(t *testing.T) {
	a := newTestApp(nil, 100, 30)

	// tryQuit calls this on every exit path, including before the manager exists.
	a.stopAllLoops()
}

func TestIsAnotherPRDRunningInSameDirIgnoresIdleInstances(t *testing.T) {
	a := loopControlApp(t, "auth", "auth", "billing")

	// Registered but not running: the project root is free, so no dialog.
	if a.isAnotherPRDRunningInSameDir("auth") {
		t.Error("expected no conflict when the other PRD is idle")
	}
}

func TestAnotherPRDRunsInRootIgnoresSelf(t *testing.T) {
	instances := []*loop.LoopInstance{
		{Name: "auth", State: loop.LoopStateRunning},
	}

	// Restarting the PRD that is already running is not a same-directory clash
	// with itself.
	if anotherPRDRunsInRoot(instances, "auth") {
		t.Error("expected a PRD not to conflict with itself")
	}
}

func TestAnotherPRDRunsInRootDetectsConflict(t *testing.T) {
	instances := []*loop.LoopInstance{
		{Name: "billing", State: loop.LoopStateRunning}, // project root, no worktree
	}

	// Two loops committing in the same directory would interleave their commits,
	// so this has to be caught before the second one starts.
	if !anotherPRDRunsInRoot(instances, "auth") {
		t.Error("expected a conflict with another PRD running in the project root")
	}
}

func TestAnotherPRDRunsInRootIgnoresWorktreeRuns(t *testing.T) {
	instances := []*loop.LoopInstance{
		{
			Name:        "billing",
			State:       loop.LoopStateRunning,
			WorktreeDir: "/proj/.chief/worktrees/billing",
		},
	}

	// A PRD in its own worktree commits elsewhere, so it is not a conflict.
	if anotherPRDRunsInRoot(instances, "auth") {
		t.Error("expected no conflict with a PRD running in its own worktree")
	}
}

func TestAnotherPRDRunsInRootIgnoresNonRunningStates(t *testing.T) {
	for _, state := range []loop.LoopState{
		loop.LoopStateReady,
		loop.LoopStatePaused,
		loop.LoopStateStopped,
		loop.LoopStateComplete,
		loop.LoopStateError,
	} {
		instances := []*loop.LoopInstance{{Name: "billing", State: state}}
		if anotherPRDRunsInRoot(instances, "auth") {
			t.Errorf("expected no conflict for state %v", state)
		}
	}
}

func TestAnotherPRDRunsInRootFindsConflictAmongSeveral(t *testing.T) {
	instances := []*loop.LoopInstance{
		{Name: "docs", State: loop.LoopStateComplete},
		{Name: "billing", State: loop.LoopStateRunning, WorktreeDir: "/wt/billing"},
		{Name: "infra", State: loop.LoopStateRunning}, // the actual clash
	}

	if !anotherPRDRunsInRoot(instances, "auth") {
		t.Error("expected the root-directory conflict to be found among several instances")
	}
}

func TestIsAnotherPRDRunningInSameDirWithoutManager(t *testing.T) {
	a := newTestApp(nil, 100, 30)

	if a.isAnotherPRDRunningInSameDir("auth") {
		t.Error("expected no conflict without a manager")
	}
}

// Starting from an ordinary feature branch used to skip the dialog entirely and
// branch silently, which made the worktree option unreachable unless you
// happened to be on main. Every start asks now.
func TestStartLoopAsksOnAnOrdinaryBranch(t *testing.T) {
	dir := t.TempDir()
	initRepoOnMain(t, dir)
	mustRun(t, dir, "git", "checkout", "-b", "feature/x")
	a := worktreeDirApp(t, dir, "")

	model, _ := a.startLoopForPRD("auth")
	got, ok := model.(App)
	if !ok {
		t.Fatalf("unexpected model type %T", model)
	}
	if got.viewMode != ViewBranchWarning {
		t.Fatalf("viewMode = %v, want ViewBranchWarning", got.viewMode)
	}
	if ctx := got.branchWarning.GetDialogContext(); ctx != DialogNoConflicts {
		t.Errorf("dialog context = %v, want DialogNoConflicts", ctx)
	}

	// The dialog owns the decision, so nothing may have branched yet.
	branch, err := git.GetCurrentBranch(dir)
	if err != nil {
		t.Fatalf("GetCurrentBranch: %v", err)
	}
	if branch != "feature/x" {
		t.Errorf("branch = %q, want the dialog to leave feature/x alone", branch)
	}
}

// The dialog is only useful if the worktree is one keystroke away from the
// default, so the quiet path offers it right below the recommended answer.
func TestStartLoopOffersWorktreeOnAnOrdinaryBranch(t *testing.T) {
	dir := t.TempDir()
	initRepoOnMain(t, dir)
	mustRun(t, dir, "git", "checkout", "-b", "feature/x")
	a := worktreeDirApp(t, dir, "")

	model, _ := a.startLoopForPRD("auth")
	got := model.(App)

	out := got.branchWarning.Render()
	if !strings.Contains(out, "Create worktree") {
		t.Errorf("rendered dialog offers no worktree:\n%s", out)
	}
	if !strings.Contains(out, "chief/auth") {
		t.Errorf("rendered dialog does not name the branch:\n%s", out)
	}
}

func TestResolveRunHomePrefersTheRecordedWorktree(t *testing.T) {
	home, ok := resolveRunHome(runHomeFacts{
		instWorktree:  "/proj/.chief/worktrees/auth",
		instBranch:    "chief/auth",
		currentBranch: "main",
		prdBranch:     "chief/auth",
	})
	if !ok {
		t.Fatal("expected the worktree the manager recorded to count as a home")
	}
	if home.worktree != "/proj/.chief/worktrees/auth" || home.branch != "chief/auth" {
		t.Errorf("home = %+v, want the recorded worktree and its branch", home)
	}
}

// A chief started from inside a PRD's own worktree stands on that PRD's branch,
// and the run belongs right where it is.
func TestResolveRunHomeAcceptsTheCurrentBranch(t *testing.T) {
	home, ok := resolveRunHome(runHomeFacts{
		currentBranch: "chief/auth",
		prdBranch:     "chief/auth",
	})
	if !ok {
		t.Fatal("expected the PRD's own branch to count as a home")
	}
	if home.worktree != "" {
		t.Errorf("worktree = %q, want the current directory", home.worktree)
	}
	if home.branch != "chief/auth" {
		t.Errorf("branch = %q, want chief/auth", home.branch)
	}
}

// A run started with an edited branch name is still that run: what it is called
// is the manager's record, not the convention.
func TestResolveRunHomeAcceptsTheRecordedBranch(t *testing.T) {
	home, ok := resolveRunHome(runHomeFacts{
		instBranch:    "feature/login",
		currentBranch: "feature/login",
		prdBranch:     "chief/auth",
	})
	if !ok {
		t.Fatal("expected the branch the run recorded to count as a home")
	}
	if home.branch != "feature/login" {
		t.Errorf("branch = %q, want the recorded branch", home.branch)
	}
}

// The worktree of a run from an earlier chief session: nothing in memory knows
// about it, but git does.
func TestResolveRunHomeFallsBackToTheBranchWorktree(t *testing.T) {
	home, ok := resolveRunHome(runHomeFacts{
		currentBranch:  "main",
		prdBranch:      "chief/auth",
		branchWorktree: "/proj/.chief/worktrees/auth",
	})
	if !ok {
		t.Fatal("expected the worktree holding the branch to count as a home")
	}
	if home.worktree != "/proj/.chief/worktrees/auth" || home.branch != "chief/auth" {
		t.Errorf("home = %+v, want the worktree holding the branch", home)
	}
}

func TestResolveRunHomeFindsNothingForAFreshPRD(t *testing.T) {
	if _, ok := resolveRunHome(runHomeFacts{currentBranch: "main", prdBranch: "chief/auth"}); ok {
		t.Error("expected a PRD that never ran to have no home, so the dialog can ask")
	}
}

// A stopped run left its branch checked out. Asking again where the work should
// happen only invites an answer that walks away from the commits already on it.
func TestStartLoopResumesOnTheRunsOwnBranch(t *testing.T) {
	dir := t.TempDir()
	initRepoOnMain(t, dir)
	mustRun(t, dir, "git", "checkout", "-b", "chief/auth")
	a := worktreeDirApp(t, dir, "")
	a.manager = loop.NewManager(10, nil)

	model, cmd := a.startLoopForPRD("auth")
	got := model.(App)
	if got.viewMode == ViewBranchWarning {
		t.Fatalf("the dialog was raised again for a run already on its branch:\n%s", got.branchWarning.Render())
	}
	if cmd == nil {
		t.Error("expected the start to carry on rather than stop at the dialog")
	}
}

// Same for the worktree an earlier session left behind: the branch lives there,
// so that is the only place this run can go.
func TestStartLoopResumesInTheWorktreeHoldingTheBranch(t *testing.T) {
	dir := t.TempDir()
	initRepoOnMain(t, dir)
	worktreePath := filepath.Join(dir, ".chief", "worktrees", "auth")
	mustRun(t, dir, "git", "worktree", "add", "-b", "chief/auth", worktreePath)
	a := worktreeDirApp(t, dir, "")
	a.manager = loop.NewManager(10, nil)
	a.worktreeSpinner = NewWorktreeSpinner()

	model, _ := a.startLoopForPRD("auth")
	got := model.(App)
	if got.viewMode != ViewWorktreeSpinner {
		t.Fatalf("viewMode = %v, want the run to go straight back to its worktree", got.viewMode)
	}
	if got.pendingWorktreePath != worktreePath {
		t.Errorf("pendingWorktreePath = %q, want %q", got.pendingWorktreePath, worktreePath)
	}
}

// A worktree that was picked up as it stands holds an earlier run's work.
// Cancelling the setup must leave it — and the story progress recorded in it —
// alone.
func TestCancellingSetupKeepsAReusedWorktree(t *testing.T) {
	dir := t.TempDir()
	initRepoOnMain(t, dir)
	worktreePath := filepath.Join(dir, ".chief", "worktrees", "auth")
	mustRun(t, dir, "git", "worktree", "add", "-b", "chief/auth", worktreePath)
	a := worktreeDirApp(t, dir, "")
	a.pendingWorktreePath = worktreePath
	a.pendingWorktreeReused = true

	a.cleanupWorktreeSetup()

	if !git.IsWorktree(worktreePath) {
		t.Error("the reused worktree was removed when the setup was cancelled")
	}
}
