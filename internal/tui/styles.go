// Package tui provides the terminal user interface for Chief.
// It includes the main Bubble Tea application, dashboard views,
// log viewer, PRD picker, help overlay, and consistent styling.
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Color palette - consistent colors used throughout the TUI.
//
// Every color is one of the terminal's 16 ANSI palette slots (or its default
// foreground) instead of a fixed hex value. The terminal theme owns the actual
// shades, so the UI always matches it — and when the theme changes while chief
// is running (Ghostty reloading its palette, say), everything already on screen
// is recolored by the terminal itself. Detecting light/dark once at startup
// could not do that: a session started on a light theme kept its dark text
// after a switch to a dark one.
var (
	// Primary colors
	PrimaryColor = lipgloss.Color("6") // Cyan - primary brand, in-progress states
	SuccessColor = lipgloss.Color("2") // Green - passed, complete states
	WarningColor = lipgloss.Color("3") // Yellow - paused, warning states
	ErrorColor   = lipgloss.Color("1") // Red - failed, error states
	ReviewColor  = lipgloss.Color("5") // Magenta - under review by the review agent
	MutedColor   = lipgloss.Color("8") // Bright black - pending, muted text
	BorderColor  = lipgloss.Color("8") // Borders, dividers

	// Text colors. The terminal's own foreground, not palette 7/15: in light
	// themes those are often pale greys meant for dark backgrounds.
	TextColor       lipgloss.TerminalColor = lipgloss.NoColor{}  // Primary text
	TextMutedColor                         = lipgloss.Color("8") // Muted text
	TextBrightColor lipgloss.TerminalColor = lipgloss.NoColor{}  // Emphasis (callers add Bold)

	// Background colors
	BgSelectedColor = lipgloss.Color("8") // Selected item background
)

// Header styles
var (
	// Main header style with branding
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(PrimaryColor).
			Padding(0, 1)

	// Header border/divider
	HeaderBorderStyle = lipgloss.NewStyle().
				Foreground(BorderColor)

	// View indicator (e.g. "[Log View]", "[Diff]") shown in view headers
	viewIndicatorStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(PrimaryColor)
)

// Footer styles
var (
	footerStyle = lipgloss.NewStyle().
			Foreground(MutedColor).
			Padding(0, 1)

	// Shortcut key style
	ShortcutKeyStyle = lipgloss.NewStyle().
				Foreground(PrimaryColor).
				Bold(true)

	// Shortcut description style
	ShortcutDescStyle = lipgloss.NewStyle().
				Foreground(MutedColor)
)

// Panel styles
var (
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(BorderColor).
			Padding(0, 1)

	// Panel with focus/active state
	PanelActiveStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(PrimaryColor).
				Padding(0, 1)

	// Panel title style
	PanelTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(PrimaryColor)
)

// Selection styles
var (
	selectedStyle = lipgloss.NewStyle().
			Background(BgSelectedColor).
			Foreground(TextColor)

	// Unselected/normal item style
	UnselectedStyle = lipgloss.NewStyle().
			Foreground(TextColor)
)

// Status badge styles - colored badges for state indicators
var (
	// Story status styles
	statusPassedStyle     = lipgloss.NewStyle().Foreground(SuccessColor)
	statusInProgressStyle = lipgloss.NewStyle().Foreground(PrimaryColor)
	statusPendingStyle    = lipgloss.NewStyle().Foreground(MutedColor)
	statusFailedStyle     = lipgloss.NewStyle().Foreground(ErrorColor)
	statusPausedStyle     = lipgloss.NewStyle().Foreground(WarningColor)
	statusReviewingStyle  = lipgloss.NewStyle().Foreground(ReviewColor)

	// State badge styles (with bold for headers)
	StateReadyStyle    = lipgloss.NewStyle().Bold(true).Foreground(MutedColor)
	StateRunningStyle  = lipgloss.NewStyle().Bold(true).Foreground(PrimaryColor)
	StatePausedStyle   = lipgloss.NewStyle().Bold(true).Foreground(WarningColor)
	StateStoppedStyle  = lipgloss.NewStyle().Bold(true).Foreground(MutedColor)
	StateCompleteStyle = lipgloss.NewStyle().Bold(true).Foreground(SuccessColor)
	StateErrorStyle    = lipgloss.NewStyle().Bold(true).Foreground(ErrorColor)
)

// Shared single-color foreground styles.
//
// These are reused across per-frame render paths (dashboard header/indicators,
// completion screen confetti loop, picker modals) so hot render loops don't
// reallocate an identical lipgloss.Style on every frame. lipgloss.Style values
// are immutable: Render never mutates the receiver and chained setters return
// copies, so sharing a package-level base is safe.
var (
	fgText    = lipgloss.NewStyle().Foreground(TextColor)
	fgMuted   = lipgloss.NewStyle().Foreground(MutedColor)
	fgSuccess = lipgloss.NewStyle().Foreground(SuccessColor)
	fgError   = lipgloss.NewStyle().Foreground(ErrorColor)
	fgPrimary = lipgloss.NewStyle().Foreground(PrimaryColor)
	fgWarning = lipgloss.NewStyle().Foreground(WarningColor)
)

// Title and label styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(TextColor)

	labelStyle = lipgloss.NewStyle().
			Foreground(PrimaryColor).
			Bold(true)

	// Subtitle style
	SubtitleStyle = lipgloss.NewStyle().
			Foreground(MutedColor)

	// Description text style
	DescriptionStyle = lipgloss.NewStyle().
				Foreground(TextColor)
)

// Progress bar styles
var (
	progressBarFillStyle  = lipgloss.NewStyle().Foreground(SuccessColor)
	progressBarEmptyStyle = lipgloss.NewStyle().Foreground(MutedColor)

	// Progress percentage style
	ProgressPercentStyle = lipgloss.NewStyle().
				Foreground(MutedColor)
)

// Activity line styles
var (
	ActivityRunningStyle  = lipgloss.NewStyle().Foreground(PrimaryColor).Padding(0, 1)
	ActivityErrorStyle    = lipgloss.NewStyle().Foreground(ErrorColor).Padding(0, 1)
	ActivityCompleteStyle = lipgloss.NewStyle().Foreground(SuccessColor).Padding(0, 1)
	ActivityMutedStyle    = lipgloss.NewStyle().Foreground(MutedColor).Padding(0, 1)
)

// interruptedWarningStyle is the banner shown when a story was interrupted.
var interruptedWarningStyle = lipgloss.NewStyle().
	Foreground(WarningColor).
	Bold(true).
	Padding(0, 1)

// Divider styles
var (
	DividerStyle = lipgloss.NewStyle().
			Foreground(BorderColor)

	// Thick divider (for section separators)
	ThickDividerStyle = lipgloss.NewStyle().
				Foreground(BorderColor).
				Bold(true)
)

// Tab bar styles
var (
	// TabStyle - inactive tab with rounded border
	TabStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(BorderColor).
			Padding(0, 1)

	// TabActiveStyle - active/viewed tab with primary color border and background
	TabActiveStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(PrimaryColor).
			Background(BgSelectedColor).
			Bold(true).
			Padding(0, 1)

	// TabRunningStyle - running state with primary color border
	TabRunningStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(PrimaryColor).
			Padding(0, 1)

	// TabErrorStyle - error state with error color border
	TabErrorStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ErrorColor).
			Padding(0, 1)

	// TabNewStyle - "+ New" button with muted styling
	TabNewStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(MutedColor).
			Foreground(MutedColor).
			Padding(0, 1)
)

// Status icons
const (
	IconPassed      = "✓"
	IconInProgress  = "●"
	IconPending     = "○"
	IconFailed      = "✗"
	IconPaused      = "◐"
	IconNeedsReview = "⚑"
	IconReviewing   = "◍"
)

// GetStatusIcon returns the appropriate icon for a story's status.
func GetStatusIcon(passed, inProgress, needsReview bool) string {
	if passed {
		return statusPassedStyle.Render(glyph(IconPassed, "v"))
	}
	if needsReview {
		return statusPausedStyle.Render(glyph(IconNeedsReview, "!"))
	}
	if inProgress {
		return statusInProgressStyle.Render(glyph(IconInProgress, "*"))
	}
	return statusPendingStyle.Render(glyph(IconPending, "."))
}

// ReviewingIcon returns the icon for a story currently being inspected by the
// separate review agent. It takes precedence over the in-progress icon so the
// review pass is visually distinct from ordinary building.
func ReviewingIcon() string {
	return statusReviewingStyle.Render(glyph(IconReviewing, "?"))
}

// GetStateStyle returns the appropriate style for an app state.
func GetStateStyle(state AppState) lipgloss.Style {
	switch state {
	case StateRunning:
		return StateRunningStyle
	case StatePaused:
		return StatePausedStyle
	case StateComplete:
		return StateCompleteStyle
	case StateError:
		return StateErrorStyle
	case StateStopped:
		return StateStoppedStyle
	default:
		return StateReadyStyle
	}
}

// GetActivityStyle returns the appropriate style for activity line based on state.
func GetActivityStyle(state AppState) lipgloss.Style {
	switch state {
	case StateRunning:
		return ActivityRunningStyle
	case StateError:
		return ActivityErrorStyle
	case StateComplete:
		return ActivityCompleteStyle
	default:
		return ActivityMutedStyle
	}
}

// highlightRow renders an already styled row with the selection background
// across its full width. Wrapping it in selectedStyle alone is not enough: the
// first colored segment (a status icon, say) ends with a reset that also clears
// the background, so only that segment would appear highlighted.
func highlightRow(row string, width int) string {
	bgOn := strings.TrimSuffix(lipgloss.NewStyle().Background(BgSelectedColor).Render("x"), "x\x1b[0m")
	row = strings.ReplaceAll(row, "\x1b[0m", "\x1b[0m"+bgOn)
	return selectedStyle.Width(width).Render(row)
}
