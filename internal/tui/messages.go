package tui

import (
	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/loop"
	"github.com/ben182/chief/internal/prd"
)

// PRDUpdateMsg is sent when the PRD file changes.
type PRDUpdateMsg struct {
	PRD   *prd.PRD
	Error error
}

// ProgressUpdateMsg is sent when progress.md changes.
type ProgressUpdateMsg struct {
	Entries map[string][]prd.ProgressEntry
}

// AppState represents the current state of the application.
type AppState int

const (
	StateReady AppState = iota
	StateRunning
	StatePaused
	StateStopped
	StateComplete
	StateError
)

func (s AppState) String() string {
	switch s {
	case StateReady:
		return "Ready"
	case StateRunning:
		return "Running"
	case StatePaused:
		return "Paused"
	case StateStopped:
		return "Stopped"
	case StateComplete:
		return "Complete"
	case StateError:
		return "Error"
	default:
		return "Unknown"
	}
}

// LoopEventMsg wraps a loop event for the Bubble Tea model.
type LoopEventMsg struct {
	PRDName string
	Event   loop.Event
}

// LoopFinishedMsg is sent when the loop finishes (complete, paused, stopped, or error).
type LoopFinishedMsg struct {
	PRDName string
	Err     error
}

// PRDCompletedMsg is sent when any PRD completes all stories.
type PRDCompletedMsg struct {
	PRDName string
}

// mergeResultMsg is sent when a merge operation completes.
type mergeResultMsg struct {
	branch    string
	conflicts []string
	output    string
	err       error
}

// cleanResultMsg is sent when a clean operation completes.
type cleanResultMsg struct {
	prdName     string
	success     bool
	message     string
	clearBranch bool
}

// autoActionResultMsg is sent when a post-completion auto-action (push/PR) completes.
type autoActionResultMsg struct {
	action string // "push" or "pr"
	err    error
	pr     git.PR // Only set when action is "pr" and it succeeded
}

// summaryResultMsg is sent when post-completion run-summary generation finishes.
type summaryResultMsg struct {
	prdName      string
	fileName     string // base name of the written summary (empty on failure/skip)
	err          error
	showOnScreen bool // true: reflect state on the completion screen; false: only lastActivity
	pushAfter    bool // whether to start auto-push once the summary lands
}

// completionSpinnerTickMsg is sent to animate the completion screen spinner.
type completionSpinnerTickMsg struct{}

// confettiTickMsg is sent to animate confetti particles on the completion screen.
type confettiTickMsg struct{}

// worktreeStepResultMsg is sent when a worktree setup step completes.
type worktreeStepResultMsg struct {
	step WorktreeSpinnerStep
	err  error
}

// worktreeSpinnerTickMsg is sent to animate the worktree setup spinner.
type worktreeSpinnerTickMsg struct{}

// branchSyncCheckMsg carries the result of the pre-run check comparing the branch
// a loop is about to commit to against origin. prdDir is carried through so the
// handler can resume the start it interrupted.
type branchSyncCheckMsg struct {
	prdName string
	prdDir  string
	branch  string
	sync    git.BranchSync
}

// branchSyncResultMsg is sent when reconciling a diverged branch with origin
// finishes, so the interrupted start can resume (or report why it can't).
type branchSyncResultMsg struct {
	prdName string
	prdDir  string
	branch  string
	err     error
}

// elapsedTickMsg is sent every second to update the elapsed time display.
type elapsedTickMsg struct{}

// settingsGHCheckResultMsg is sent when GH CLI validation completes in settings.
type settingsGHCheckResultMsg struct {
	installed     bool
	authenticated bool
	err           error
}

// LaunchInitMsg signals the TUI should exit to launch the init flow.
type LaunchInitMsg struct {
	Name string
}

// LaunchEditMsg signals the TUI should exit to launch the edit flow.
type LaunchEditMsg struct {
	Name string
}

// ViewMode represents which view is currently active.
type ViewMode int

const (
	ViewDashboard ViewMode = iota
	ViewLog
	ViewDiff
	ViewPicker
	ViewHelp
	ViewBranchWarning
	ViewSleepWarning
	ViewWorktreeSpinner
	ViewCompletion
	ViewSettings
	ViewQuitConfirm
)

// PostExitAction signals a follow-up flow the caller should run after the TUI exits.
type PostExitAction int

const (
	PostExitNone PostExitAction = iota
	PostExitInit
	PostExitEdit
)

// autoStartMsg triggers the loop automatically after launch (chief start).
type autoStartMsg struct{}

// backgroundAutoActionResultMsg reports completion of a background PRD's
// post-completion auto-action (push/PR).
type backgroundAutoActionResultMsg struct {
	prdName string
	action  string // "push" or "pr"
	err     error
}
