package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// QuitConfirmOption represents the user's choice in the quit confirmation dialog.
type QuitConfirmOption int

const (
	QuitOptionQuit   QuitConfirmOption = iota // Quit and stop loop
	QuitOptionCancel                          // Cancel
)

// QuitConfirmation manages the quit confirmation dialog state.
type QuitConfirmation struct {
	width       int
	height      int
	selectedIdx int
	message     []string // lines explaining what quitting will interrupt
	quitLabel   string   // label for the affirmative (quit) option
}

// NewQuitConfirmation creates a new quit confirmation dialog.
func NewQuitConfirmation() *QuitConfirmation {
	q := &QuitConfirmation{
		selectedIdx: 1, // Default to Cancel (safe choice)
	}
	q.ForLoop()
	return q
}

// ForLoop sets the dialog copy for the case where a Ralph loop is still running.
func (q *QuitConfirmation) ForLoop() {
	q.message = []string{
		"A Ralph loop is currently running.",
		"Exiting will stop the loop.",
	}
	q.quitLabel = "Quit and stop loop"
}

// ForAutoAction sets the dialog copy for the case where a post-completion
// action (writing the run summary, pushing, or opening a PR) is still running.
// Quitting kills the underlying process, so a half-written summary never lands.
func (q *QuitConfirmation) ForAutoAction() {
	q.message = []string{
		"A post-completion action is still running.",
		"Exiting now will interrupt it.",
	}
	q.quitLabel = "Quit and interrupt"
}

// SetSize sets the dialog dimensions.
func (q *QuitConfirmation) SetSize(width, height int) {
	q.width = width
	q.height = height
}

// MoveUp moves selection up.
func (q *QuitConfirmation) MoveUp() {
	if q.selectedIdx > 0 {
		q.selectedIdx--
	}
}

// MoveDown moves selection down.
func (q *QuitConfirmation) MoveDown() {
	if q.selectedIdx < 1 {
		q.selectedIdx++
	}
}

// GetSelected returns the currently selected option.
func (q *QuitConfirmation) GetSelected() QuitConfirmOption {
	if q.selectedIdx == 0 {
		return QuitOptionQuit
	}
	return QuitOptionCancel
}

// Reset resets the dialog state to defaults.
func (q *QuitConfirmation) Reset() {
	q.selectedIdx = 1 // Default to Cancel
}

// Render renders the quit confirmation dialog.
func (q *QuitConfirmation) Render() string {
	modalWidth := min(55, q.width-10)
	if modalWidth < 40 {
		modalWidth = 40
	}

	var content strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(WarningColor)
	content.WriteString(titleStyle.Render("Quit Chief?"))
	content.WriteString("\n")
	content.WriteString(dividerLine(modalWidth))
	content.WriteString("\n\n")

	// Message
	messageStyle := lipgloss.NewStyle().Foreground(TextColor)
	for _, line := range q.message {
		content.WriteString(messageStyle.Render(line))
		content.WriteString("\n")
	}
	content.WriteString("\n")

	// Options
	optionStyle := lipgloss.NewStyle().Foreground(TextColor)
	selectedStyle := lipgloss.NewStyle().Foreground(PrimaryColor).Bold(true)

	options := []string{q.quitLabel, "Cancel"}
	for i, opt := range options {
		if i == q.selectedIdx {
			content.WriteString(selectedStyle.Render("▶ " + opt))
		} else {
			content.WriteString(optionStyle.Render("  " + opt))
		}
		content.WriteString("\n")
	}

	// Footer
	content.WriteString("\n")
	content.WriteString(dividerLine(modalWidth))
	content.WriteString("\n")
	footerStyle := lipgloss.NewStyle().Foreground(MutedColor)
	content.WriteString(footerStyle.Render("↑/↓: Navigate  Enter: Select  Esc: Cancel"))

	// Modal box
	modalStyle := modalBoxStyle(WarningColor).Width(modalWidth)

	modal := modalStyle.Render(content.String())

	// Center on screen
	return centerModal(modal, q.width, q.height)
}

// centerModal centers the modal on the screen.
