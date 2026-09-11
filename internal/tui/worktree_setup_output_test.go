package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/git"
)

// setupApp stages an App sitting on the setup step of the worktree spinner, the
// way it looks after the branch and the worktree are in place.
func setupApp(t *testing.T, cfg config.WorktreeConfig) *App {
	t.Helper()
	base := t.TempDir()
	initRepoWithRecordedBase(t, base, "chief/auth", "main")

	a := newTestApp(nil, 100, 40)
	a.baseDir = base
	a.config = &config.Config{Worktree: cfg}
	a.viewMode = ViewWorktreeSpinner
	a.pendingStartPRD = "auth"
	a.pendingWorktreePath = filepath.Join(base, ".chief", "worktrees", "auth")
	a.worktreeSpinner = NewWorktreeSpinner()
	a.worktreeSpinner.Configure("auth", "chief/auth", "main", ".chief/worktrees/auth/", cfg.Setup)
	a.worktreeSpinner.SetSize(100, 40)
	return a
}

// A setup that takes minutes has to be watchable, and afterwards readable: the
// spinner gets the lines as they arrive, the PRD directory keeps all of them.
// That directory is the worktree's own — the setup prepares the worktree, and a
// run given one is meant to leave the project untouched.
func TestWorktreeSetupStreamsIntoTheSpinnerAndLogsIntoThePRDDirectory(t *testing.T) {
	a := setupApp(t, config.WorktreeConfig{Setup: "make setup", SetupTimeoutSeconds: 90})

	var gotOpts git.RunOptions
	a.runSetup = func(wt git.WorktreeContext, setup string, opts git.RunOptions) (git.RunResult, error) {
		gotOpts = opts
		opts.OnLine("Installing dependencies")
		return git.RunResult{}, nil
	}

	_, cmd := a.handleWorktreeStepResult(worktreeStepResultMsg{step: SpinnerStepCreateBranch})
	if cmd == nil {
		t.Fatal("expected a setup command")
	}
	cmd()

	if want := filepath.Join(a.pendingWorktreePath, ".chief", "prds", "auth"); gotOpts.LogDir != want {
		t.Errorf("setup log dir = %q, want %q", gotOpts.LogDir, want)
	}
	if gotOpts.Timeout != 90*time.Second {
		t.Errorf("setup timeout = %s, want 1m30s", gotOpts.Timeout)
	}
	if !strings.Contains(a.worktreeSpinner.Render(), "Installing dependencies") {
		t.Error("the spinner does not show the setup output it was handed")
	}
}

// A setup with no timeout configured must keep waiting: slow is not stuck.
func TestWorktreeSetupWithoutAConfiguredTimeoutWaitsForever(t *testing.T) {
	a := setupApp(t, config.WorktreeConfig{Setup: "make setup"})

	var gotOpts git.RunOptions
	a.runSetup = func(wt git.WorktreeContext, setup string, opts git.RunOptions) (git.RunResult, error) {
		gotOpts = opts
		return git.RunResult{}, nil
	}

	_, cmd := a.handleWorktreeStepResult(worktreeStepResultMsg{step: SpinnerStepCreateBranch})
	cmd()

	if gotOpts.Timeout != 0 {
		t.Errorf("setup timeout = %s, want none", gotOpts.Timeout)
	}
}

// A failed `composer install` prints hundreds of lines. The modal says where to
// read them; it does not try to be the pager.
func TestWorktreeSetupFailurePointsAtTheLogInsteadOfDumpingTheOutput(t *testing.T) {
	a := setupApp(t, config.WorktreeConfig{Setup: "make setup"})

	logPath := filepath.Join(a.baseDir, ".chief", "prds", "auth", "setup-2026-09-09-120000.log")
	var noise []string
	for i := 0; i < 40; i++ {
		noise = append(noise, fmt.Sprintf("noise-%02d", i))
	}
	a.runSetup = func(wt git.WorktreeContext, setup string, opts git.RunOptions) (git.RunResult, error) {
		return git.RunResult{Output: strings.Join(noise, "\n"), LogPath: logPath}, errors.New("exit status 1")
	}

	_, cmd := a.handleWorktreeStepResult(worktreeStepResultMsg{step: SpinnerStepCreateBranch})
	if cmd == nil {
		t.Fatal("expected a setup command")
	}
	model, _ := a.handleWorktreeStepResult(cmd().(worktreeStepResultMsg))

	view := model.(App).worktreeSpinner.Render()
	if !strings.Contains(view, "setup-2026-09-09-120000.log") {
		t.Errorf("the failure does not name the log file:\n%s", view)
	}
	if !strings.Contains(view, "exit status 1") {
		t.Errorf("the failure does not say the setup failed:\n%s", view)
	}
	if strings.Contains(view, "noise-00") {
		t.Errorf("the failure dumped the command output into the modal:\n%s", view)
	}
}

// While the setup runs, the spinner is the only place progress shows. It keeps
// the tail rather than the whole log: a modal has no room for a scrollback.
func TestWorktreeSpinnerShowsOnlyTheLastLinesOfSetupOutput(t *testing.T) {
	w := NewWorktreeSpinner()
	w.Configure("auth", "chief/auth", "main", ".chief/worktrees/auth/", "make setup")
	w.SetSize(100, 40)
	w.AdvanceStep()
	w.AdvanceStep()

	for i := 1; i <= 12; i++ {
		w.AppendSetupOutput(fmt.Sprintf("step-%02d", i))
	}

	view := w.Render()
	for i := 5; i <= 12; i++ {
		if want := fmt.Sprintf("step-%02d", i); !strings.Contains(view, want) {
			t.Errorf("spinner is missing %q, it should show the last %d lines:\n%s", want, setupOutputLines, view)
		}
	}
	for i := 1; i <= 4; i++ {
		if gone := fmt.Sprintf("step-%02d", i); strings.Contains(view, gone) {
			t.Errorf("spinner still shows %q, it should keep only the last %d lines", gone, setupOutputLines)
		}
	}
}

// A setup line can be any width at all — a compiler command line, a stack
// trace. It is shortened to one row rather than wrapped: a wrapping line eats
// the rows the modal already sized itself for and pushes its footer off screen.
func TestWorktreeSpinnerGivesEverySetupLineExactlyOneRow(t *testing.T) {
	rows := func(line string) int {
		w := NewWorktreeSpinner()
		w.Configure("auth", "chief/auth", "main", ".chief/worktrees/auth/", "make setup")
		w.SetSize(100, 120)
		w.AdvanceStep()
		w.AdvanceStep()
		w.AppendSetupOutput(line)
		// Count the modal's own rows, not the render's: the render is padded to
		// the screen height either way.
		rows := 0
		for _, l := range strings.Split(w.Render(), "\n") {
			if strings.Contains(l, "│") {
				rows++
			}
		}
		return rows
	}

	short := rows("ok")
	long := rows(strings.Repeat("very-long-token ", 30))
	if long != short {
		t.Errorf("a long setup line renders %d rows where a short one renders %d — it wrapped instead of being shortened", long, short)
	}
}

// A failed teardown gets the same treatment as a failed setup: the dialog keeps
// the tail that usually holds the error, and names the file holding the rest.
func TestTeardownFailureDialogNamesTheLogFile(t *testing.T) {
	a := cleanApp(t, "make drop-db", "chief/auth", 1) // "Remove worktree only"
	logPath := filepath.Join(a.baseDir, ".chief", "prds", "auth", "teardown-2026-09-09-151204.log")

	a.runTeardown = func(wt git.WorktreeContext, teardown string, opts git.RunOptions) (git.RunResult, error) {
		return git.RunResult{Output: "database is still in use", LogPath: logPath}, errors.New("exit status 1")
	}
	a.removeWorktree = func(repoDir, worktreePath string) error { return nil }

	_, cmd := a.handlePickerKeys(key("enter"))
	if cmd == nil {
		t.Fatal("expected a clean command")
	}
	model, _ := a.handleTeardownFailed(cmd().(teardownFailedMsg))

	tf := model.(App).picker.GetTeardownFailure()
	if tf == nil {
		t.Fatal("expected the teardown failure dialog")
	}
	if want := filepath.Join(".chief", "prds", "auth", "teardown-2026-09-09-151204.log"); tf.LogPath != want {
		t.Errorf("dialog log path = %q, want %q relative to the repository", tf.LogPath, want)
	}
}
