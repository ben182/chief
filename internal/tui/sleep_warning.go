package tui

import (
	"strings"

	"github.com/ben182/chief/internal/awake"
	"github.com/ben182/chief/internal/prd"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SleepWarningOption represents the user's choice in the sleep warning dialog.
type SleepWarningOption int

const (
	SleepOptionStartAnyway SleepWarningOption = iota
	SleepOptionCancel
)

// sleepReason names one way this machine can drop out from under a run.
type sleepReason int

const (
	// sleepReasonBattery: on battery the lid decides, not chief — clamshell sleep
	// is not software-preventable, so a closed lid suspends the machine even with
	// keepAwake holding a caffeinate assertion.
	sleepReasonBattery sleepReason = iota
	// sleepReasonKeepAwakeOff: the user turned the sleep protection off, so
	// nothing stops the ordinary idle sleep of an untouched keyboard.
	sleepReasonKeepAwakeOff
)

// sleepRisks names why an unattended run on this machine may be interrupted by
// system sleep — empty when there is nothing to warn about.
//
// Pure by design: the platform, the power source and the setting all arrive as
// arguments, so the decision is testable without a Mac, a battery or a config
// file. The order of the reasons is the order they are shown in.
func sleepRisks(supported bool, power awake.PowerSource, keepAwake bool) []sleepReason {
	// Where chief can't inhibit sleep at all, neither reason means anything: the
	// setting is a no-op and the battery advice is macOS lore.
	if !supported {
		return nil
	}

	var reasons []sleepReason
	// Only a battery we actually saw counts. A failed query says nothing about the
	// machine, and a warning raised on nothing teaches the user to skip warnings.
	if power == awake.PowerBattery {
		reasons = append(reasons, sleepReasonBattery)
	}
	if !keepAwake {
		reasons = append(reasons, sleepReasonKeepAwakeOff)
	}
	return reasons
}

// sleepCheck reports how the machine is powered and whether chief can keep this
// platform awake at all — the two facts sleepRisks needs from the outside world.
type sleepCheck func() (power awake.PowerSource, keepAwakeSupported bool)

// systemSleepCheck asks the real machine.
func systemSleepCheck() (awake.PowerSource, bool) {
	return awake.CurrentPowerSource(), awake.Supported()
}

// sleepWarningPreflight raises the pre-run warning when this machine may fall
// asleep mid-run, and reports whether the start has to wait for an answer.
//
// It is the last gate before a run begins, because that is the only moment the
// answer is still cheap: a run that sleeps through the night is discovered hours
// later, and nothing about it is recoverable except the wall-clock time already
// lost. Unlike the branch checks it re-asks on every start — the machine can be
// unplugged between two runs, and a warning you can dismiss for good is a warning
// that stops telling you anything.
func (a *App) sleepWarningPreflight(prdName string) bool {
	if a.sleepCheck == nil {
		return false
	}
	power, supported := a.sleepCheck()
	// A missing config means nothing was configured away: keepAwake defaults on.
	keepAwake := a.config == nil || a.config.Loop.KeepAwake

	reasons := sleepRisks(supported, power, keepAwake)
	if len(reasons) == 0 {
		return false
	}

	if a.sleepWarning == nil {
		a.sleepWarning = NewSleepWarning()
	}
	a.sleepWarning.SetSize(a.width, a.height)
	a.sleepWarning.SetReasons(reasons)
	a.sleepWarning.Reset()
	a.pendingStartPRD = prdName
	a.previousViewMode = a.viewMode
	a.viewMode = ViewSleepWarning
	return true
}

// renderSleepWarningView renders the pre-run sleep warning dialog.
func (a *App) renderSleepWarningView() string {
	a.sleepWarning.SetSize(a.width, a.height)
	return a.sleepWarning.Render()
}

// handleSleepWarningKeys handles keyboard input for the sleep warning dialog.
func (a App) handleSleepWarningKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return a.cancelSleepWarning()

	case "up", "k":
		a.sleepWarning.MoveUp()
		return a, nil

	case "down", "j":
		a.sleepWarning.MoveDown()
		return a, nil

	case "enter":
		if a.sleepWarning.GetSelected() == SleepOptionCancel {
			return a.cancelSleepWarning()
		}
		prdName := a.pendingStartPRD
		a.pendingStartPRD = ""
		a.viewMode = a.viewBehindSleepWarning()
		// Straight to launchLoop: routing back through doStartLoop would raise this
		// same dialog again on the answer the user just gave.
		return a.launchLoop(prdName, prd.PRDDir(a.baseDir, prdName))
	}

	return a, nil
}

// cancelSleepWarning drops the pending start and returns to where it came from.
func (a App) cancelSleepWarning() (tea.Model, tea.Cmd) {
	a.pendingStartPRD = ""
	a.viewMode = a.viewBehindSleepWarning()
	a.lastActivity = "Cancelled"
	return a, nil
}

// viewBehindSleepWarning is the view to return to when the dialog closes.
// previousViewMode can point at the dialog itself — the help overlay overwrites
// it while the dialog is up — so guard against dropping the user back into the
// modal they just dismissed.
func (a App) viewBehindSleepWarning() ViewMode {
	if a.previousViewMode == ViewSleepWarning {
		return ViewDashboard
	}
	return a.previousViewMode
}

// sleepReasonLines is the dialog copy for each reason: what will happen, and
// what to do about it.
func sleepReasonLines(r sleepReason) []string {
	switch r {
	case sleepReasonBattery:
		return []string{
			"This Mac is running on battery. With the lid",
			"closed it sleeps even while keepAwake is on —",
			"clamshell sleep can't be prevented in software.",
			"→ Plug in the power adapter, or leave the lid open.",
		}
	case sleepReasonKeepAwakeOff:
		return []string{
			"Sleep protection is off (loop.keepAwake: false),",
			"so an untouched keyboard idle-sleeps the machine.",
			"→ Switch it back on in the settings.",
		}
	}
	return nil
}

// SleepWarning manages the pre-run sleep warning dialog state. It follows the
// BranchWarning pattern: a blocking modal with keyboard-selected options, where
// cancelling is the safe default.
type SleepWarning struct {
	width       int
	height      int
	selectedIdx int
	reasons     []sleepReason
}

// NewSleepWarning creates a sleep warning dialog with no reasons yet.
func NewSleepWarning() *SleepWarning {
	s := &SleepWarning{}
	s.Reset()
	return s
}

// SetSize sets the dialog dimensions.
func (s *SleepWarning) SetSize(width, height int) {
	s.width = width
	s.height = height
}

// SetReasons sets which hazards the dialog explains. All of them are shown in
// one dialog rather than one dialog per reason.
func (s *SleepWarning) SetReasons(reasons []sleepReason) {
	s.reasons = reasons
}

// Reset returns the selection to the safe default (Cancel).
func (s *SleepWarning) Reset() {
	s.selectedIdx = 1
}

// MoveUp moves selection up.
func (s *SleepWarning) MoveUp() {
	if s.selectedIdx > 0 {
		s.selectedIdx--
	}
}

// MoveDown moves selection down.
func (s *SleepWarning) MoveDown() {
	if s.selectedIdx < 1 {
		s.selectedIdx++
	}
}

// GetSelected returns the currently selected option.
func (s *SleepWarning) GetSelected() SleepWarningOption {
	if s.selectedIdx == 0 {
		return SleepOptionStartAnyway
	}
	return SleepOptionCancel
}

// Render renders the sleep warning dialog.
func (s *SleepWarning) Render() string {
	modalWidth := min(58, s.width-10)
	if modalWidth < 40 {
		modalWidth = 40
	}

	var content strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(WarningColor)
	content.WriteString(titleStyle.Render("⚠️  Mac May Fall Asleep Mid-Run"))
	content.WriteString("\n")
	content.WriteString(dividerLine(modalWidth))
	content.WriteString("\n\n")

	messageStyle := lipgloss.NewStyle().Foreground(TextColor)
	hintStyle := lipgloss.NewStyle().Foreground(MutedColor)
	for _, reason := range s.reasons {
		for _, line := range sleepReasonLines(reason) {
			if strings.HasPrefix(line, "→") {
				content.WriteString(hintStyle.Render(line))
			} else {
				content.WriteString(messageStyle.Render(line))
			}
			content.WriteString("\n")
		}
		content.WriteString("\n")
	}

	optionStyle := lipgloss.NewStyle().Foreground(TextColor)
	selectedStyle := lipgloss.NewStyle().Foreground(PrimaryColor).Bold(true)
	for i, opt := range []string{"Start anyway", "Cancel"} {
		if i == s.selectedIdx {
			content.WriteString(selectedStyle.Render("▶ " + opt))
		} else {
			content.WriteString(optionStyle.Render("  " + opt))
		}
		content.WriteString("\n")
	}

	content.WriteString("\n")
	content.WriteString(dividerLine(modalWidth))
	content.WriteString("\n")
	footerStyle := lipgloss.NewStyle().Foreground(MutedColor)
	content.WriteString(footerStyle.Render("↑/↓: Navigate  Enter: Select  Esc: Cancel"))

	modal := modalBoxStyle(WarningColor).Width(modalWidth).Render(content.String())

	return centerModal(modal, s.width, s.height)
}
