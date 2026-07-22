package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// tickCompletionSpinner returns a tea.Cmd that ticks the completion screen spinner.
func tickCompletionSpinner() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return completionSpinnerTickMsg{}
	})
}

// tickConfetti returns a tea.Cmd that ticks the confetti animation.
func tickConfetti() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
		return confettiTickMsg{}
	})
}

// tickWorktreeSpinner returns a tea.Cmd that ticks the spinner animation.
func tickWorktreeSpinner() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return worktreeSpinnerTickMsg{}
	})
}

// tickElapsed returns a tea.Cmd that ticks every second for the elapsed time display.
func tickElapsed() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return elapsedTickMsg{}
	})
}
