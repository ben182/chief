package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ben182/chief/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

// isQuitCmd reports whether cmd is tea.Quit, by running it and inspecting the
// message it produces.
func isQuitCmd(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// quitTestApp builds an App far enough along to exercise the quit paths.
func quitTestApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(nil, 100, 30)
	a.quitConfirm = NewQuitConfirmation()
	a.completionScreen = NewCompletionScreen()
	return a
}

func TestTryQuitWithNothingRunningQuitsImmediately(t *testing.T) {
	a := quitTestApp(t)

	model, cmd := a.tryQuit()

	got := model.(App)
	if got.viewMode == ViewQuitConfirm {
		t.Error("expected no confirmation dialog when nothing is running")
	}
	if !isQuitCmd(cmd) {
		t.Error("expected tea.Quit when nothing is running")
	}
}

func TestTryQuitDuringAutoActionAsksForConfirmation(t *testing.T) {
	a := quitTestApp(t)
	a.viewMode = ViewCompletion
	a.completionScreen.SetSummaryInProgress()

	model, cmd := a.tryQuit()

	got := model.(App)
	// Quitting would kill the summary process and leave a half-written file, so
	// the user has to confirm.
	if got.viewMode != ViewQuitConfirm {
		t.Errorf("expected the quit confirmation, got view %v", got.viewMode)
	}
	if isQuitCmd(cmd) {
		t.Error("expected no immediate quit while an auto-action runs")
	}
	if got.previousViewMode != ViewCompletion {
		t.Errorf("expected previousViewMode ViewCompletion for the cancel path, got %v", got.previousViewMode)
	}
	if rendered := got.quitConfirm.Render(); !strings.Contains(rendered, "post-completion action") {
		t.Errorf("expected the auto-action wording in the dialog, got:\n%s", rendered)
	}
}

func TestTryQuitOnCompletionScreenWithoutRunningActionQuits(t *testing.T) {
	a := quitTestApp(t)
	a.viewMode = ViewCompletion
	// A finished run with all auto-actions done has nothing left to interrupt.

	model, cmd := a.tryQuit()

	got := model.(App)
	if got.viewMode == ViewQuitConfirm {
		t.Error("expected no confirmation once the auto-actions are done")
	}
	if !isQuitCmd(cmd) {
		t.Error("expected tea.Quit on a settled completion screen")
	}
}

func TestTryQuitIgnoresAutoActionOutsideCompletionView(t *testing.T) {
	a := quitTestApp(t)
	a.viewMode = ViewDashboard
	a.completionScreen.SetSummaryInProgress()

	// The guard is scoped to the completion view; a stale in-progress state on a
	// screen the user has left must not block quitting forever.
	_, cmd := a.tryQuit()

	if !isQuitCmd(cmd) {
		t.Error("expected tea.Quit when not on the completion screen")
	}
}

func TestTryQuitResetsDialogSelectionToCancel(t *testing.T) {
	a := quitTestApp(t)
	a.viewMode = ViewCompletion
	a.completionScreen.SetPushInProgress()
	a.quitConfirm.MoveUp() // arm "Quit" from a previous visit

	model, _ := a.tryQuit()

	got := model.(App)
	// A stray Enter right after the dialog opens must not kill a running action.
	if got.quitConfirm.GetSelected() != QuitOptionCancel {
		t.Error("expected the dialog to open on Cancel")
	}
}

func TestQuitConfirmEscReturnsToPreviousView(t *testing.T) {
	a := quitTestApp(t)
	a.viewMode = ViewQuitConfirm
	a.previousViewMode = ViewLog

	model, cmd := a.handleQuitConfirmKeys(tea.KeyMsg{Type: tea.KeyEsc})

	got := model.(App)
	if got.viewMode != ViewLog {
		t.Errorf("expected a return to ViewLog, got %v", got.viewMode)
	}
	if isQuitCmd(cmd) {
		t.Error("esc must not quit")
	}
}

func TestQuitConfirmEnterOnCancelReturnsToPreviousView(t *testing.T) {
	a := quitTestApp(t)
	a.viewMode = ViewQuitConfirm
	a.previousViewMode = ViewCompletion
	// Cancel is the default selection.

	model, cmd := a.handleQuitConfirmKeys(tea.KeyMsg{Type: tea.KeyEnter})

	got := model.(App)
	if got.viewMode != ViewCompletion {
		t.Errorf("expected a return to ViewCompletion, got %v", got.viewMode)
	}
	if isQuitCmd(cmd) {
		t.Error("Enter on Cancel must not quit")
	}
}

func TestQuitConfirmEnterOnQuitQuits(t *testing.T) {
	a := quitTestApp(t)
	a.viewMode = ViewQuitConfirm
	a.previousViewMode = ViewCompletion
	a.quitConfirm.MoveUp() // select Quit

	_, cmd := a.handleQuitConfirmKeys(tea.KeyMsg{Type: tea.KeyEnter})

	if !isQuitCmd(cmd) {
		t.Error("expected tea.Quit after confirming")
	}
}

func TestQuitConfirmArrowKeysMoveSelection(t *testing.T) {
	a := quitTestApp(t)
	a.viewMode = ViewQuitConfirm

	model, _ := a.handleQuitConfirmKeys(tea.KeyMsg{Type: tea.KeyUp})
	got := model.(App)
	if got.quitConfirm.GetSelected() != QuitOptionQuit {
		t.Error("expected Up to select Quit")
	}

	model, _ = got.handleQuitConfirmKeys(tea.KeyMsg{Type: tea.KeyDown})
	got = model.(App)
	if got.quitConfirm.GetSelected() != QuitOptionCancel {
		t.Error("expected Down to select Cancel")
	}
}

func TestQuitConfirmVimKeysMoveSelection(t *testing.T) {
	a := quitTestApp(t)
	a.viewMode = ViewQuitConfirm

	model, _ := a.handleQuitConfirmKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	got := model.(App)
	if got.quitConfirm.GetSelected() != QuitOptionQuit {
		t.Error("expected 'k' to select Quit")
	}

	model, _ = got.handleQuitConfirmKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	got = model.(App)
	if got.quitConfirm.GetSelected() != QuitOptionCancel {
		t.Error("expected 'j' to select Cancel")
	}
}

func TestQuitConfirmUnhandledKeyIsInert(t *testing.T) {
	a := quitTestApp(t)
	a.viewMode = ViewQuitConfirm

	model, cmd := a.handleQuitConfirmKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})

	got := model.(App)
	if got.viewMode != ViewQuitConfirm {
		t.Errorf("expected to stay on the dialog, got view %v", got.viewMode)
	}
	if cmd != nil {
		t.Error("expected no command for an unhandled key")
	}
}

// A config that cannot be written used to fail silently, so the user saw the
// setting apply and then found it gone after a restart.
func TestPublishSettingsReportsAFailedSave(t *testing.T) {
	dir := t.TempDir()
	notADir := filepath.Join(dir, "blocker")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	app := &App{
		// baseDir sits under a regular file, so creating .chief fails.
		baseDir:         filepath.Join(notADir, "project"),
		config:          config.Default(),
		settingsOverlay: NewSettingsOverlay(),
	}
	app.settingsOverlay.LoadFromConfig(app.config)

	app.publishSettings()

	if !strings.Contains(app.lastActivity, "could not be saved") {
		t.Errorf("expected the failed save reported in lastActivity, got %q", app.lastActivity)
	}
}

// The in-memory change still has to take effect when only persistence failed —
// reverting it would be a worse surprise than a warning.
func TestPublishSettingsAppliesInMemoryDespiteFailedSave(t *testing.T) {
	dir := t.TempDir()
	notADir := filepath.Join(dir, "blocker")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	app := &App{
		baseDir:         filepath.Join(notADir, "project"),
		config:          config.Default(),
		logViewer:       NewLogViewer(),
		settingsOverlay: NewSettingsOverlay(),
	}
	app.settingsOverlay.LoadFromConfig(app.config)

	selectKey(t, app.settingsOverlay, "review.enabled")
	app.settingsOverlay.CycleTriBool() // unset -> true
	app.publishSettings()

	if !app.config.Review.Active() {
		t.Error("expected the review toggle to apply in memory even when the save failed")
	}
	if !app.logViewer.reviewPending {
		t.Error("expected the story-done marker to follow the in-memory toggle")
	}
}
