package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// dividerLine renders a horizontal divider sized to fit inside a bordered,
// horizontally-padded box of the given outer width (2 border + 2 padding cells).
func dividerLine(width int) string {
	return DividerStyle.Render(strings.Repeat("─", width-4))
}

// modalBoxStyle returns the standard rounded-border modal container style with
// the given border color and standard (1, 2) padding. Callers add Width and,
// for fixed-height modals, Height.
func modalBoxStyle(borderColor lipgloss.TerminalColor) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(1, 2)
}

// centerModal positions a pre-rendered modal string in the middle of a screen
// of the given dimensions, padding with blank lines above and spaces to the
// left. It is the single shared implementation used by every modal/overlay in
// the package.
func centerModal(modal string, screenWidth, screenHeight int) string {
	lines := strings.Split(modal, "\n")
	modalHeight := len(lines)
	modalWidth := 0
	for _, line := range lines {
		if lipgloss.Width(line) > modalWidth {
			modalWidth = lipgloss.Width(line)
		}
	}

	topPadding := (screenHeight - modalHeight) / 2
	leftPadding := (screenWidth - modalWidth) / 2

	if topPadding < 0 {
		topPadding = 0
	}
	if leftPadding < 0 {
		leftPadding = 0
	}

	var result strings.Builder

	for i := 0; i < topPadding; i++ {
		result.WriteString("\n")
	}

	leftPad := strings.Repeat(" ", leftPadding)
	for _, line := range lines {
		result.WriteString(leftPad)
		result.WriteString(line)
		result.WriteString("\n")
	}

	return result.String()
}
