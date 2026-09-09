package tui

import (
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
)

// WorktreeSpinnerStep represents a step in the worktree setup process.
type WorktreeSpinnerStep int

const (
	SpinnerStepCreateBranch WorktreeSpinnerStep = iota
	SpinnerStepCreateWorktree
	SpinnerStepRunSetup
	SpinnerStepDone
)

// stepInfo holds the display info for each setup step.
type stepInfo struct {
	label    string
	complete bool
	active   bool
	errMsg   string
}

// WorktreeSpinner manages the worktree setup spinner overlay state.
type WorktreeSpinner struct {
	width  int
	height int

	prdName       string
	branchName    string
	defaultBranch string
	worktreePath  string // Worktree path for display, as resolved from worktree.dir
	setupCommand  string // Empty if no setup command configured

	currentStep  WorktreeSpinnerStep
	spinnerFrame int
	steps        []stepInfo
	errMsg       string // Overall error message
	cancelled    bool

	// setupOutput is the tail of what the setup command has printed so far, so a
	// setup that takes minutes is something to watch rather than a frozen
	// spinner. The lines arrive on the goroutine running the command and are
	// read while rendering, hence the mutex.
	setupMu     sync.Mutex
	setupOutput []string
}

// setupOutputLines is how much of the running setup's output the spinner shows.
// Enough to see that something is happening and what it is; a modal has no room
// for a scrollback, and the log file has the rest.
const setupOutputLines = 8

// NewWorktreeSpinner creates a new worktree setup spinner.
func NewWorktreeSpinner() *WorktreeSpinner {
	return &WorktreeSpinner{}
}

// Configure sets up the spinner with the given parameters.
func (w *WorktreeSpinner) Configure(prdName, branchName, defaultBranch, worktreePath, setupCommand string) {
	w.prdName = prdName
	w.branchName = branchName
	w.defaultBranch = defaultBranch
	w.worktreePath = worktreePath
	w.setupCommand = setupCommand
	w.currentStep = SpinnerStepCreateBranch
	w.spinnerFrame = 0
	w.errMsg = ""
	w.cancelled = false
	w.setupMu.Lock()
	w.setupOutput = nil
	w.setupMu.Unlock()

	// Build steps list
	w.steps = []stepInfo{
		{label: fmt.Sprintf("Creating branch '%s' from '%s'", branchName, defaultBranch)},
		{label: fmt.Sprintf("Creating worktree at %s", worktreePath)},
	}
	if setupCommand != "" {
		w.steps = append(w.steps, stepInfo{label: fmt.Sprintf("Running setup: %s", setupCommand)})
	}

	// Mark first step as active
	if len(w.steps) > 0 {
		w.steps[0].active = true
	}
}

// AppendSetupOutput records one line the setup command just printed. Safe to
// call from the goroutine running the command.
func (w *WorktreeSpinner) AppendSetupOutput(line string) {
	w.setupMu.Lock()
	defer w.setupMu.Unlock()
	w.setupOutput = append(w.setupOutput, line)
	if len(w.setupOutput) > setupOutputLines {
		w.setupOutput = w.setupOutput[len(w.setupOutput)-setupOutputLines:]
	}
}

// recentSetupOutput returns a copy of the lines the spinner should show.
func (w *WorktreeSpinner) recentSetupOutput() []string {
	w.setupMu.Lock()
	defer w.setupMu.Unlock()
	return append([]string(nil), w.setupOutput...)
}

// SetSize sets the spinner dimensions.
func (w *WorktreeSpinner) SetSize(width, height int) {
	w.width = width
	w.height = height
}

// AdvanceStep marks the current step as complete and moves to the next.
func (w *WorktreeSpinner) AdvanceStep() {
	idx := int(w.currentStep)
	if idx < len(w.steps) {
		w.steps[idx].complete = true
		w.steps[idx].active = false
	}

	w.currentStep++

	// Skip setup step if no setup command
	if w.currentStep == SpinnerStepRunSetup && w.setupCommand == "" {
		w.currentStep = SpinnerStepDone
	}

	nextIdx := int(w.currentStep)
	if nextIdx < len(w.steps) {
		w.steps[nextIdx].active = true
	}
}

// SkipSetupStep finishes the spinner without running the setup command,
// relabelling the step so the modal says the setup was skipped rather than
// silently dropping a step the user configured.
func (w *WorktreeSpinner) SkipSetupStep() {
	idx := int(SpinnerStepRunSetup)
	if idx < len(w.steps) {
		w.steps[idx].label = fmt.Sprintf("Skipped setup (worktree reused): %s", w.setupCommand)
		w.steps[idx].complete = true
		w.steps[idx].active = false
	}
	w.currentStep = SpinnerStepDone
}

// SetError sets an error on the current step.
func (w *WorktreeSpinner) SetError(err string) {
	w.errMsg = err
	idx := int(w.currentStep)
	if idx < len(w.steps) {
		w.steps[idx].errMsg = err
		w.steps[idx].active = false
	}
}

// HasError returns true if there is an error.
func (w *WorktreeSpinner) HasError() bool {
	return w.errMsg != ""
}

// IsDone returns true if all steps are complete.
func (w *WorktreeSpinner) IsDone() bool {
	return w.currentStep >= SpinnerStepDone
}

// GetCurrentStep returns the current step.
func (w *WorktreeSpinner) GetCurrentStep() WorktreeSpinnerStep {
	return w.currentStep
}

// HasSetupCommand returns true if a setup command is configured.
func (w *WorktreeSpinner) HasSetupCommand() bool {
	return w.setupCommand != ""
}

// IsCancelled returns true if the user cancelled.
func (w *WorktreeSpinner) IsCancelled() bool {
	return w.cancelled
}

// Cancel marks the spinner as cancelled.
func (w *WorktreeSpinner) Cancel() {
	w.cancelled = true
}

// Tick advances the spinner animation frame.
func (w *WorktreeSpinner) Tick() {
	w.spinnerFrame++
}

// Render renders the spinner overlay.
func (w *WorktreeSpinner) Render() string {
	modalWidth := min(65, w.width-10)
	if modalWidth < 40 {
		modalWidth = 40
	}

	var content strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(PrimaryColor)
	content.WriteString(titleStyle.Render("Setting up worktree"))
	content.WriteString("\n")
	content.WriteString(dividerLine(modalWidth))
	content.WriteString("\n\n")

	spinnerFrames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	spinnerStyle := lipgloss.NewStyle().Foreground(PrimaryColor)
	checkStyle := lipgloss.NewStyle().Foreground(SuccessColor)
	errorStyle := lipgloss.NewStyle().Foreground(ErrorColor)
	textStyle := lipgloss.NewStyle().Foreground(TextColor)
	mutedStyle := lipgloss.NewStyle().Foreground(MutedColor)

	// Render steps
	for _, step := range w.steps {
		if step.complete {
			content.WriteString(checkStyle.Render("✓"))
			content.WriteString(" ")
			// Show completed label
			completedLabel := strings.Replace(step.label, "Creating branch", "Created branch", 1)
			completedLabel = strings.Replace(completedLabel, "Creating worktree", "Created worktree", 1)
			completedLabel = strings.Replace(completedLabel, "Running setup", "Ran setup", 1)
			content.WriteString(textStyle.Render(completedLabel))
		} else if step.errMsg != "" {
			content.WriteString(errorStyle.Render("✗"))
			content.WriteString(" ")
			content.WriteString(errorStyle.Render(step.label))
			// The message names a log file rather than carrying the output, so
			// it is short enough to wrap into the modal instead of bursting it.
			for _, line := range strings.Split(wrapText(step.errMsg, modalWidth-6), "\n") {
				content.WriteString("\n  ")
				content.WriteString(errorStyle.Render(line))
			}
		} else if step.active {
			frame := spinnerFrames[w.spinnerFrame%len(spinnerFrames)]
			content.WriteString(spinnerStyle.Render(frame))
			content.WriteString(" ")
			content.WriteString(textStyle.Render(step.label))
			for _, line := range w.recentSetupOutput() {
				content.WriteString("\n    ")
				content.WriteString(mutedStyle.Render(truncateWithEllipsis(line, modalWidth-8)))
			}
		} else {
			content.WriteString(mutedStyle.Render("○"))
			content.WriteString(" ")
			content.WriteString(mutedStyle.Render(step.label))
		}
		content.WriteString("\n")
	}

	// Done state - show "Starting loop..."
	if w.IsDone() {
		content.WriteString("\n")
		content.WriteString(checkStyle.Render("Starting loop..."))
	}

	// Footer
	content.WriteString("\n")
	content.WriteString(dividerLine(modalWidth))
	content.WriteString("\n")

	footerStyle := lipgloss.NewStyle().Foreground(MutedColor)
	if w.HasError() {
		content.WriteString(footerStyle.Render("Esc: Cancel and clean up"))
	} else if w.IsDone() {
		// No footer needed when transitioning
	} else {
		content.WriteString(footerStyle.Render("Esc: Cancel"))
	}

	// Modal box
	modalStyle := modalBoxStyle(PrimaryColor).Width(modalWidth)

	modal := modalStyle.Render(content.String())

	return centerModal(modal, w.width, w.height)
}

// centerModal centers the modal on the screen.
