package tui

import (
	"strings"
	"testing"

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
