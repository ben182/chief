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
}

// NewQuitConfirmation creates a new quit confirmation dialog.
func NewQuitConfirmation() *QuitConfirmation {
	return &QuitConfirmation{
		selectedIdx: 1, // Default to Cancel (safe choice)
	}
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
	content.WriteString(messageStyle.Render("A Ralph loop is currently running."))
	content.WriteString("\n")
	content.WriteString(messageStyle.Render("Exiting will stop the loop."))
	content.WriteString("\n\n")

	// Options
	optionStyle := lipgloss.NewStyle().Foreground(TextColor)
	selectedStyle := lipgloss.NewStyle().Foreground(PrimaryColor).Bold(true)

	options := []string{"Quit and stop loop", "Cancel"}
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
