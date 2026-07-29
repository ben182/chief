package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ben182/chief/internal/prd"
	"github.com/charmbracelet/lipgloss"
)

const (
	// Layout constants
	minWidth             = 80
	narrowWidthThreshold = 100 // Below this, switch to stacked layout
	storiesPanelPct      = 35  // Stories panel takes 35% of width
	detailsPanelPct      = 65  // Details panel takes 65% of width
	headerHeight         = 5   // Increased to accommodate tab bar (brand line + tab bar + border)
	footerHeight         = 3   // Increased to accommodate activity line
	activityHeight       = 1
	progressBarWidth     = 20
)

// isNarrowMode returns true if the terminal width is below the threshold for stacked layout.
func (a *App) isNarrowMode() bool {
	return a.width < narrowWidthThreshold
}

// renderDashboard renders the full dashboard view.
func (a *App) renderDashboard() string {
	if a.width == 0 || a.height == 0 {
		return "Loading..."
	}

	// Use stacked layout for narrow terminals
	if a.isNarrowMode() {
		return a.renderStackedDashboard()
	}

	header := a.renderHeader()

	// Hide footer when terminal height < 12
	fh := footerHeight
	var footer string
	if a.height < 12 {
		fh = 0
		footer = ""
	} else {
		footer = a.renderFooter()
	}

	// Calculate content area height
	contentHeight := a.height - a.effectiveHeaderHeight() - fh - 2 // -2 for panel borders

	// Render panels
	storiesWidth := (a.width * storiesPanelPct / 100) - 2
	detailsWidth := a.width - storiesWidth - 4 // -4 for borders and gap

	storiesPanel := a.renderStoriesPanel(storiesWidth, contentHeight)
	detailsPanel := a.renderDetailsPanel(detailsWidth, contentHeight)

	// Join panels horizontally
	content := lipgloss.JoinHorizontal(lipgloss.Top, storiesPanel, detailsPanel)

	// Stack header, content, and footer
	if footer == "" {
		return lipgloss.JoinVertical(lipgloss.Left, header, content)
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
}

// renderStackedDashboard renders the dashboard with stacked layout for narrow terminals.
func (a *App) renderStackedDashboard() string {
	header := a.renderNarrowHeader()

	// Hide footer when terminal height < 12
	fh := footerHeight
	var footer string
	if a.height < 12 {
		fh = 0
		footer = ""
	} else {
		footer = a.renderNarrowFooter()
	}

	// Calculate content area height
	contentHeight := a.height - a.effectiveHeaderHeight() - fh - 2 // -2 for panel borders

	// Split height between stories (40%) and details (60%)
	storiesHeight := max((contentHeight*40)/100, 5)
	detailsHeight := contentHeight - storiesHeight - 1 // -1 for gap between panels

	panelWidth := a.width - 2 // Account for borders

	storiesPanel := a.renderStoriesPanel(panelWidth, storiesHeight)
	detailsPanel := a.renderDetailsPanel(panelWidth, detailsHeight)

	// Join panels vertically
	content := lipgloss.JoinVertical(lipgloss.Left, storiesPanel, detailsPanel)

	// Stack header, content, and footer
	if footer == "" {
		return lipgloss.JoinVertical(lipgloss.Left, header, content)
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
}

// getWorktreeInfo returns the branch and directory info for the current PRD.
// Returns empty strings if no branch is set (backward compatible).
func (a *App) getWorktreeInfo() (branch, dir string) {
	if a.manager == nil {
		return "", ""
	}
	instance := a.manager.GetInstance(a.prdName)
	if instance == nil || instance.Branch == "" {
		return "", ""
	}
	branch = instance.Branch
	if instance.WorktreeDir != "" {
		// Convert absolute worktree path to relative for display
		dir = fmt.Sprintf(".chief/worktrees/%s/", a.prdName)
	} else {
		dir = "./ (current directory)"
	}
	return branch, dir
}

// hasWorktreeInfo returns true if the current PRD has branch info to display.
func (a *App) hasWorktreeInfo() bool {
	branch, _ := a.getWorktreeInfo()
	return branch != ""
}

// effectiveHeaderHeight returns the header height accounting for worktree info line.
func (a *App) effectiveHeaderHeight() int {
	if a.hasWorktreeInfo() {
		return headerHeight + 1
	}
	return headerHeight
}

// renderWorktreeInfoLine renders the branch and directory info line for the header.
func (a *App) renderWorktreeInfoLine() string {
	branch, dir := a.getWorktreeInfo()
	if branch == "" {
		return ""
	}

	branchLabel := SubtitleStyle.Render("branch:")
	branchValue := fgPrimary.Render(" " + branch)
	dirLabel := SubtitleStyle.Render("  dir:")
	dirValue := fgText.Render(" " + dir)

	return lipgloss.JoinHorizontal(lipgloss.Center, "  ", branchLabel, branchValue, dirLabel, dirValue)
}

// headerBar joins a left and right segment into a full-width bar, padding the
// gap between them so the right segment sits flush against the right edge.
// Shared by every header and footer variant.
func (a *App) headerBar(left, right string) string {
	spacing := strings.Repeat(" ", max(0, a.width-lipgloss.Width(left)-lipgloss.Width(right)-2))
	return lipgloss.JoinHorizontal(lipgloss.Center, left, spacing, right)
}

// fullWidthDivider renders a horizontal rule spanning the full terminal width,
// used as the header/footer separator.
func (a *App) fullWidthDivider() string {
	return DividerStyle.Render(strings.Repeat("─", a.width))
}

// renderHeader renders the header with branding, state, iteration, and elapsed time.
func (a *App) renderHeader() string {
	// Branding
	brand := headerStyle.Render("chief")

	// State indicator - use the centralized style system
	stateStyle := GetStateStyle(a.state)
	state := stateStyle.Render(fmt.Sprintf("[%s]", a.state.String()))

	// Iteration count (current/max)
	iteration := SubtitleStyle.Render(fmt.Sprintf("Iteration: %d/%d", a.iteration, a.maxIter))

	// Elapsed time
	elapsed := a.GetElapsedTime()
	elapsedStr := SubtitleStyle.Render(fmt.Sprintf("Time: %s", formatDuration(elapsed)))

	// ETA (only shown once velocity is meaningful)
	etaStr := ""
	if eta, ok := a.GetETA(); ok {
		etaStr = SubtitleStyle.Render(fmt.Sprintf("ETA: ~%s", formatDuration(eta)))
	}

	// Running cost (Claude only; zero for providers that don't report cost)
	costStr := ""
	if a.totalCost > 0 {
		costStr = SubtitleStyle.Render(formatCost(a.totalCost))
	}

	// Combine elements
	leftPart := lipgloss.JoinHorizontal(lipgloss.Center, brand, "  ", state)
	rightPart := lipgloss.JoinHorizontal(lipgloss.Center, iteration, "  ", elapsedStr)
	if etaStr != "" {
		rightPart = lipgloss.JoinHorizontal(lipgloss.Center, rightPart, "  ", etaStr)
	}
	if costStr != "" {
		rightPart = lipgloss.JoinHorizontal(lipgloss.Center, rightPart, "  ", costStr)
	}

	// Create the full header line with proper spacing
	headerLine := a.headerBar(leftPart, rightPart)

	// Tab bar
	tabBarLine := a.renderTabBar()

	// Worktree info line (only shown when branch is set)
	worktreeInfoLine := a.renderWorktreeInfoLine()

	// Add a border below
	border := a.fullWidthDivider()

	if worktreeInfoLine != "" {
		return lipgloss.JoinVertical(lipgloss.Left, headerLine, tabBarLine, worktreeInfoLine, border)
	}
	return lipgloss.JoinVertical(lipgloss.Left, headerLine, tabBarLine, border)
}

// renderTabBar renders the PRD tab bar.
func (a *App) renderTabBar() string {
	if a.tabBar == nil {
		return ""
	}
	a.tabBar.SetSize(a.width)
	if a.isNarrowMode() {
		return a.tabBar.RenderCompact()
	}
	return a.tabBar.Render()
}

// renderNarrowHeader renders a condensed header for narrow terminals.
func (a *App) renderNarrowHeader() string {
	// Branding
	brand := headerStyle.Render("chief")

	// State indicator - use the centralized style system
	stateStyle := GetStateStyle(a.state)
	state := stateStyle.Render(fmt.Sprintf("[%s]", a.state.String()))

	// Condensed iteration and time
	elapsed := a.GetElapsedTime()
	iterTime := SubtitleStyle.Render(fmt.Sprintf("#%d %s", a.iteration, formatDuration(elapsed)))
	if eta, ok := a.GetETA(); ok {
		iterTime = SubtitleStyle.Render(fmt.Sprintf("#%d %s ~%s", a.iteration, formatDuration(elapsed), formatDuration(eta)))
	}

	// Combine elements
	leftPart := lipgloss.JoinHorizontal(lipgloss.Center, brand, " ", state)
	rightPart := iterTime

	// Create the full header line with proper spacing
	headerLine := a.headerBar(leftPart, rightPart)

	// Tab bar (compact)
	tabBarLine := a.renderTabBar()

	// Worktree info line (only shown when branch is set)
	worktreeInfoLine := a.renderWorktreeInfoLine()

	// Add a border below
	border := a.fullWidthDivider()

	if worktreeInfoLine != "" {
		return lipgloss.JoinVertical(lipgloss.Left, headerLine, tabBarLine, worktreeInfoLine, border)
	}
	return lipgloss.JoinVertical(lipgloss.Left, headerLine, tabBarLine, border)
}

// renderFooter renders the footer with keyboard shortcuts, PRD name, and activity line.
func (a *App) renderFooter() string {
	// Keyboard shortcuts (context-sensitive based on view and state)
	var shortcuts []string

	if a.viewMode == ViewLog {
		// Log view shortcuts
		shortcuts = []string{"t: dashboard", "d: diff", "e: edit", "n: new", "l: list", "←→/1-9: switch", ",: settings", "?: help", "j/k: scroll", "q: quit"}
	} else if a.viewMode == ViewDiff {
		// Diff view shortcuts
		shortcuts = []string{"d: dashboard", "t: log", "e: edit", "n: new", "l: list", "?: help", "j/k: scroll", "q: quit"}
	} else {
		// Dashboard view shortcuts
		switch a.state {
		case StateReady, StatePaused:
			shortcuts = []string{"s: start", "d: diff", "e: edit", "t: log", "n: new", "l: list", "←→/1-9: switch", ",: settings", "?: help", "q: quit"}
		case StateRunning:
			shortcuts = []string{"p: pause", "x: stop", "d: diff", "t: log", "n: new", "l: list", "←→/1-9: switch", ",: settings", "?: help", "q: quit"}
		case StateStopped, StateError:
			shortcuts = []string{"s: retry", "d: diff", "e: edit", "t: log", "n: new", "l: list", "←→/1-9: switch", ",: settings", "?: help", "q: quit"}
		default:
			shortcuts = []string{"d: diff", "e: edit", "t: log", "n: new", "l: list", "←→/1-9: switch", ",: settings", "?: help", "q: quit"}
		}
	}
	shortcutsStr := footerStyle.Render(strings.Join(shortcuts, "  │  "))

	// PRD name
	prdInfo := footerStyle.Render(fmt.Sprintf("PRD: %s", a.prdName))

	// Create footer line with proper spacing
	footerLine := a.headerBar(shortcutsStr, prdInfo)

	// Activity line
	activityLine := a.renderActivityLine()

	// Add border above
	border := a.fullWidthDivider()

	return lipgloss.JoinVertical(lipgloss.Left, border, activityLine, footerLine)
}

// renderNarrowFooter renders a condensed footer for narrow terminals.
func (a *App) renderNarrowFooter() string {
	// Condensed keyboard shortcuts for narrow mode
	var shortcuts []string

	if a.viewMode == ViewLog {
		// Log view shortcuts - condensed
		shortcuts = []string{"t", "e", "n", "1-9", ",", "?", "q"}
	} else {
		// Dashboard view shortcuts - condensed
		switch a.state {
		case StateReady, StatePaused:
			shortcuts = []string{"s", "e", "t", "n", "1-9", ",", "?", "q"}
		case StateRunning:
			shortcuts = []string{"p", "x", "t", "n", "1-9", ",", "?", "q"}
		case StateStopped, StateError:
			shortcuts = []string{"s", "e", "t", "n", "1-9", ",", "?", "q"}
		default:
			shortcuts = []string{"e", "t", "n", "1-9", ",", "?", "q"}
		}
	}
	shortcutsStr := footerStyle.Render(strings.Join(shortcuts, " "))

	// PRD name - truncate if needed
	prdName := a.prdName
	maxPRDLen := 12
	if len(prdName) > maxPRDLen {
		prdName = prdName[:maxPRDLen-2] + ".."
	}
	prdInfo := footerStyle.Render(prdName)

	// Create footer line with proper spacing
	footerLine := a.headerBar(shortcutsStr, prdInfo)

	// Activity line - use narrower truncation
	activityLine := a.renderNarrowActivityLine()

	// Add border above
	border := a.fullWidthDivider()

	return lipgloss.JoinVertical(lipgloss.Left, border, activityLine, footerLine)
}

// renderNarrowActivityLine renders the activity line for narrow terminals.
func (a *App) renderNarrowActivityLine() string {
	activity := a.lastActivity
	if activity == "" {
		activity = "Ready"
	}

	// More aggressive truncation for narrow mode
	activity = truncateWithEllipsis(activity, a.width-2)

	// Use the centralized activity style system
	activityStyle := GetActivityStyle(a.state)

	return activityStyle.Render(activity)
}

// renderActivityLine renders the current activity status line.
func (a *App) renderActivityLine() string {
	activity := a.lastActivity
	if activity == "" {
		activity = "Ready to start"
	}

	// Truncate if too long
	activity = truncateWithEllipsis(activity, a.width-4)

	// Use the centralized activity style system
	activityStyle := GetActivityStyle(a.state)

	return activityStyle.Render(activity)
}

// renderStoriesPanel renders the stories list panel.
func (a *App) renderStoriesPanel(width, height int) string {
	var content strings.Builder

	listHeight := height - 5 // Account for title, border, and progress bar
	totalStories := len(a.prd.UserStories)

	// Clamp scroll offset before the title is built from it.
	if listHeight > 0 && a.storiesScrollOffset > totalStories-listHeight {
		a.storiesScrollOffset = totalStories - listHeight
	}
	if a.storiesScrollOffset < 0 {
		a.storiesScrollOffset = 0
	}

	// Panel title — show which slice of the list is visible when it's
	// scrollable. Deliberately not a percentage: the progress bar at the bottom
	// of this same panel shows a completion percentage, and two unrelated
	// percentages in one box read as a contradiction.
	titleText := "Stories"
	if totalStories > listHeight && listHeight > 0 {
		first := a.storiesScrollOffset + 1
		last := a.storiesScrollOffset + listHeight
		if last > totalStories {
			last = totalStories
		}
		titleText = fmt.Sprintf("Stories %d-%d of %d", first, last, totalStories)
	}
	title := PanelTitleStyle.Render(titleText)
	content.WriteString(title)
	content.WriteString("\n")
	content.WriteString(DividerStyle.Render(strings.Repeat("─", width-2)))
	content.WriteString("\n")

	// Render visible slice of stories
	endIdx := a.storiesScrollOffset + listHeight
	if endIdx > totalStories {
		endIdx = totalStories
	}
	visibleCount := 0
	for i := a.storiesScrollOffset; i < endIdx; i++ {
		story := a.prd.UserStories[i]
		icon := GetStatusIcon(story.Passes, story.InProgress, story.NeedsReview)
		if a.isReviewing(story.ID) {
			icon = ReviewingIcon()
		}

		// Truncate title to fit
		maxTitleLen := width - 12 // Account for icon, ID, and spacing
		displayTitle := truncateWithEllipsis(story.Title, maxTitleLen)

		line := fmt.Sprintf("%s %s %s", icon, story.ID, displayTitle)

		if i == a.selectedIndex {
			// Pad line to full width to ensure background fills the entire row
			lineWidth := lipgloss.Width(line)
			targetWidth := width - 2
			if lineWidth < targetWidth {
				line = line + strings.Repeat(" ", targetWidth-lineWidth)
			}
			line = selectedStyle.Render(line)
		}

		content.WriteString(line)
		content.WriteString("\n")
		visibleCount++
	}

	// Pad remaining space
	linesWritten := visibleCount + 2 // +2 for title and divider
	for i := linesWritten; i < height-3; i++ {
		content.WriteString("\n")
	}

	// Progress bar
	content.WriteString(DividerStyle.Render(strings.Repeat("─", width-2)))
	content.WriteString("\n")
	progressBar := a.renderProgressBar(width - 4)
	content.WriteString(progressBar)

	return panelStyle.Width(width).Height(height).Render(content.String())
}

// renderDetailsPanel renders the details panel for the selected story.
func (a *App) renderDetailsPanel(width, height int) string {
	// Check for empty PRD state first
	if len(a.prd.UserStories) == 0 {
		return a.renderEmptyPRDPanel(width, height)
	}

	// Check for error state - show error details instead of story details
	if a.state == StateError {
		return a.renderErrorPanel(width, height)
	}

	story := a.GetSelectedStory()
	if story == nil {
		return panelStyle.Width(width).Height(height).Render("No stories in PRD")
	}

	var content strings.Builder

	// Show interrupted story warning at the top if applicable
	if a.hasInterruptedStory() && a.state == StateReady {
		content.WriteString(a.renderInterruptedWarning(width - 4))
		content.WriteString("\n")
	}

	// Title
	content.WriteString(titleStyle.Render(story.Title))
	content.WriteString("\n\n")

	// Status and Priority with proper styling
	statusIcon := GetStatusIcon(story.Passes, story.InProgress, story.NeedsReview)
	var statusText string
	var statusStyle lipgloss.Style
	switch {
	case a.isReviewing(story.ID):
		statusIcon = ReviewingIcon()
		statusText = "Reviewing"
		statusStyle = statusReviewingStyle
	case story.Passes:
		statusText = "Passed"
		statusStyle = statusPassedStyle
	case story.NeedsReview:
		statusText = "Needs Review"
		statusStyle = statusPausedStyle
	case story.InProgress:
		statusText = "In Progress"
		statusStyle = statusInProgressStyle
	default:
		statusText = "Pending"
		statusStyle = statusPendingStyle
	}
	content.WriteString(fmt.Sprintf("%s %s  │  Priority: %g\n", statusIcon, statusStyle.Render(statusText), story.Priority))

	// Cost & tokens for this story (live while in progress, final once done).
	if cost, tokens, ok := a.storyUsage(story.ID); ok {
		content.WriteString(renderStoryUsageLine(cost, tokens))
		content.WriteString("\n")
	}

	content.WriteString(dividerLine(width))
	content.WriteString("\n\n")

	// Description
	content.WriteString(labelStyle.Render("Description"))
	content.WriteString("\n")
	content.WriteString(wrapText(story.Description, width-4))
	content.WriteString("\n\n")

	// Acceptance Criteria
	content.WriteString(labelStyle.Render("Acceptance Criteria"))
	content.WriteString("\n")
	for _, criterion := range story.AcceptanceCriteria {
		wrapped := wrapText("• "+criterion, width-6)
		content.WriteString(wrapped)
		content.WriteString("\n")
	}

	// Progress (from progress.md)
	if entries, ok := a.progress[story.ID]; ok && len(entries) > 0 {
		content.WriteString("\n")
		content.WriteString(labelStyle.Render("Progress"))
		content.WriteString("\n")
		for _, entry := range entries {
			rendered := renderGlamour(entry.Content, width-4)
			if rendered != "" {
				content.WriteString(rendered)
				content.WriteString("\n")
			}
		}
	}

	// Truncate content to fit panel height (lipgloss Height only sets minimum, not maximum)
	contentStr := content.String()
	contentLines := strings.Split(contentStr, "\n")
	if len(contentLines) > height {
		contentLines = contentLines[:height]
		contentStr = strings.Join(contentLines, "\n")
	}

	return panelStyle.Width(width).Height(height).Render(contentStr)
}

// renderErrorPanel renders the error details panel when in error state.
func (a *App) renderErrorPanel(width, height int) string {
	var content strings.Builder

	// Error header
	errorIcon := statusFailedStyle.Render(glyph(IconFailed, "x"))
	errorTitle := StateErrorStyle.Render("ERROR")
	content.WriteString(fmt.Sprintf("%s %s\n", errorIcon, errorTitle))
	content.WriteString(dividerLine(width))
	content.WriteString("\n\n")

	// Error message
	content.WriteString(labelStyle.Render("Error Details"))
	content.WriteString("\n")
	if a.err != nil {
		errorMsg := a.err.Error()
		content.WriteString(wrapText(errorMsg, width-4))
	} else {
		content.WriteString(fgMuted.Render("Unknown error occurred"))
	}
	content.WriteString("\n\n")

	// Log file hint
	content.WriteString(dividerLine(width))
	content.WriteString("\n\n")
	logName := "claude.log"
	if a.provider != nil {
		logName = a.provider.LogFileName()
	}
	// Prefer the actual per-run (timestamped) log file name when the loop has
	// opened one, so the tip points at the right file rather than the static base.
	if inst := a.manager.GetInstance(a.prdName); inst != nil && inst.Loop != nil {
		if p := inst.Loop.LogPath(); p != "" {
			logName = filepath.Base(p)
		}
	}
	content.WriteString(fgWarning.Render(fmt.Sprintf("%s Tip: Check %s in the PRD directory for full error details.", glyph("💡", ">"), logName)))
	content.WriteString("\n\n")

	// Retry instructions
	content.WriteString(labelStyle.Render("What to do"))
	content.WriteString("\n")
	content.WriteString("• Press ")
	content.WriteString(ShortcutKeyStyle.Render("s"))
	content.WriteString(" to retry\n")
	content.WriteString("• Press ")
	content.WriteString(ShortcutKeyStyle.Render("t"))
	content.WriteString(" to view the log\n")
	content.WriteString("• Press ")
	content.WriteString(ShortcutKeyStyle.Render("q"))
	content.WriteString(" to quit")

	return panelStyle.Width(width).Height(height).Render(content.String())
}

// renderEmptyPRDPanel renders a panel when there are no stories in the PRD.
func (a *App) renderEmptyPRDPanel(width, height int) string {
	var content strings.Builder

	// Centered empty state message
	emptyIcon := fgMuted.Render(glyph("📋", "="))
	emptyTitle := titleStyle.Render("No User Stories")
	content.WriteString(fmt.Sprintf("%s %s\n", emptyIcon, emptyTitle))
	content.WriteString(dividerLine(width))
	content.WriteString("\n\n")

	// Instructions
	content.WriteString(fgText.Render("This PRD has no user stories defined."))
	content.WriteString("\n\n")

	content.WriteString(labelStyle.Render("To add stories:"))
	content.WriteString("\n")
	content.WriteString("• Press ")
	content.WriteString(ShortcutKeyStyle.Render("e"))
	content.WriteString(" to edit this PRD with Claude\n")
	content.WriteString("• Press ")
	content.WriteString(ShortcutKeyStyle.Render("n"))
	content.WriteString(" to create a new PRD with Claude\n")
	content.WriteString("\n")

	content.WriteString(dividerLine(width))
	content.WriteString("\n\n")

	content.WriteString(SubtitleStyle.Render("PRD Location:"))
	content.WriteString("\n")
	content.WriteString(fgPrimary.Render(a.prdPath))

	return panelStyle.Width(width).Height(height).Render(content.String())
}

// hasInterruptedStory returns true if there's a story with inProgress: true.
func (a *App) hasInterruptedStory() bool {
	for _, story := range a.prd.UserStories {
		if story.InProgress {
			return true
		}
	}
	return false
}

// getInterruptedStory returns the interrupted story if one exists.
func (a *App) getInterruptedStory() *prd.UserStory {
	for i := range a.prd.UserStories {
		if a.prd.UserStories[i].InProgress {
			return &a.prd.UserStories[i]
		}
	}
	return nil
}

// renderInterruptedWarning renders a warning banner for interrupted stories.
func (a *App) renderInterruptedWarning(width int) string {
	story := a.getInterruptedStory()
	if story == nil {
		return ""
	}

	var content strings.Builder

	warningIcon := "⚠"
	warningText := fmt.Sprintf("%s Interrupted Story: %s (%s)", warningIcon, story.ID, truncateWithEllipsis(story.Title, width-30))
	content.WriteString(interruptedWarningStyle.Width(width).Render(warningText))
	content.WriteString("\n")
	content.WriteString(fgMuted.Render("A previous session was interrupted. Press 's' to resume."))

	return content.String()
}

// renderProgressBar renders a progress bar showing completion percentage.
func (a *App) renderProgressBar(width int) string {
	percentage := a.GetCompletionPercentage()
	completedStories, totalStories := a.runProgress()

	// Calculate bar width
	barWidth := width - 15 // Space for percentage and count
	if barWidth < 10 {
		barWidth = 10
	}

	filledWidth := int(float64(barWidth) * percentage / 100.0)
	emptyWidth := barWidth - filledWidth

	bar := progressBarFillStyle.Render(strings.Repeat("█", filledWidth)) +
		progressBarEmptyStyle.Render(strings.Repeat("░", emptyWidth))

	return fmt.Sprintf("%s %3.0f%% %d/%d", bar, percentage, completedStories, totalStories)
}

// storyUsage returns the accumulated cost and token usage for a story in the
// active PRD: live counters while it's the in-progress story, otherwise the
// finalized totals. ok is false when nothing has been recorded yet.
func (a *App) storyUsage(storyID string) (cost float64, tokens TokenUsage, ok bool) {
	if a.currentStoryID[a.prdName] == storyID {
		cost = a.currentStoryCost[a.prdName]
		tokens = a.currentStoryTokens[a.prdName]
		return cost, tokens, cost > 0 || !tokens.IsZero()
	}
	for _, st := range a.storyTimings[a.prdName] {
		if st.StoryID == storyID {
			return st.Cost, st.Tokens, st.Cost > 0 || !st.Tokens.IsZero()
		}
	}
	return 0, TokenUsage{}, false
}

// renderStoryUsageLine formats a one-line cost + token summary for a story.
func renderStoryUsageLine(cost float64, tokens TokenUsage) string {
	label := "Tokens: "
	var parts []string
	if cost > 0 {
		label = "Cost: "
		parts = append(parts, fgText.Render("~"+formatCost(cost)))
	}
	if !tokens.IsZero() {
		tok := fmt.Sprintf("%s out · %s in · %s cache",
			formatTokenCount(tokens.Output),
			formatTokenCount(tokens.Input),
			formatTokenCount(tokens.CacheCreation+tokens.CacheRead))
		parts = append(parts, fgMuted.Render(tok))
	}
	return fgMuted.Render(label) + strings.Join(parts, fgMuted.Render("  ·  "))
}

// renderScrollableView wraps pre-rendered viewer content in a bordered panel
// sized to the available area and stacks it between the given header and footer.
// Shared by the full-screen diff and log views, which differ only in the viewer.
func (a *App) scrollableContentHeight() int {
	return a.height - headerHeight - footerHeight - 2
}

func (a *App) renderScrollableView(header, footer, content string) string {
	panel := panelStyle.Width(a.width - 2).Height(a.scrollableContentHeight()).Render(content)
	return lipgloss.JoinVertical(lipgloss.Left, header, panel, footer)
}

// renderDiffView renders the full-screen diff view.
func (a *App) renderDiffView() string {
	if a.width == 0 || a.height == 0 {
		return "Loading..."
	}

	var header, footer string
	if a.isNarrowMode() {
		header = a.renderNarrowDiffHeader()
		footer = a.renderNarrowFooter()
	} else {
		header = a.renderDiffHeader()
		footer = a.renderFooter()
	}

	a.diffViewer.SetSize(a.width-4, a.scrollableContentHeight())
	return a.renderScrollableView(header, footer, a.diffViewer.Render())
}

// renderDiffHeader renders the header for the diff view.
func (a *App) renderDiffHeader() string {
	// Branding
	brand := headerStyle.Render("chief")

	// View indicator - show story ID if viewing a story-specific diff
	viewLabel := "[Diff View]"
	if a.diffViewer.storyID != "" {
		viewLabel = fmt.Sprintf("[Diff: %s]", a.diffViewer.storyID)
	}
	viewIndicator := viewIndicatorStyle.Render(viewLabel)

	// State indicator
	stateStyle := GetStateStyle(a.state)
	state := stateStyle.Render(fmt.Sprintf("[%s]", a.state.String()))

	// Scroll position
	var scrollInfo string
	if len(a.diffViewer.lines) > 0 {
		pct := 0
		if a.diffViewer.maxOffset() > 0 {
			pct = a.diffViewer.offset * 100 / a.diffViewer.maxOffset()
		}
		scrollInfo = SubtitleStyle.Render(fmt.Sprintf("%d lines  %d%%", len(a.diffViewer.lines), pct))
	}

	// Combine elements
	leftPart := lipgloss.JoinHorizontal(lipgloss.Center, brand, "  ", viewIndicator, "  ", state)
	rightPart := scrollInfo

	// Create the full header line with proper spacing
	headerLine := a.headerBar(leftPart, rightPart)

	// Stats line (show diffstat summary if available)
	var statsLine string
	if a.diffViewer.stats != "" {
		statsLines := strings.Split(a.diffViewer.stats, "\n")
		if len(statsLines) > 0 {
			summary := statsLines[len(statsLines)-1]
			statsLine = SubtitleStyle.Render(" " + strings.TrimSpace(summary))
		}
	}

	// Add a border below
	border := a.fullWidthDivider()

	if statsLine != "" {
		return lipgloss.JoinVertical(lipgloss.Left, headerLine, statsLine, border)
	}
	return lipgloss.JoinVertical(lipgloss.Left, headerLine, border)
}

// renderNarrowDiffHeader renders a condensed header for the diff view in narrow mode.
func (a *App) renderNarrowDiffHeader() string {
	brand := headerStyle.Render("chief")

	viewLabel := "[Diff]"
	if a.diffViewer.storyID != "" {
		viewLabel = fmt.Sprintf("[%s]", a.diffViewer.storyID)
	}
	viewIndicator := viewIndicatorStyle.Render(viewLabel)

	stateStyle := GetStateStyle(a.state)
	state := stateStyle.Render(fmt.Sprintf("[%s]", a.state.String()))

	leftPart := lipgloss.JoinHorizontal(lipgloss.Center, brand, " ", viewIndicator, " ", state)

	var rightPart string
	if len(a.diffViewer.lines) > 0 {
		rightPart = SubtitleStyle.Render(fmt.Sprintf("%d lines", len(a.diffViewer.lines)))
	}

	headerLine := a.headerBar(leftPart, rightPart)

	border := a.fullWidthDivider()

	return lipgloss.JoinVertical(lipgloss.Left, headerLine, border)
}

// renderLogView renders the full-screen log view.
func (a *App) renderLogView() string {
	if a.width == 0 || a.height == 0 {
		return "Loading..."
	}

	var header, footer string
	if a.isNarrowMode() {
		header = a.renderNarrowLogHeader()
		footer = a.renderNarrowFooter()
	} else {
		header = a.renderLogHeader()
		footer = a.renderFooter()
	}

	a.logViewer.SetSize(a.width-4, a.scrollableContentHeight())
	return a.renderScrollableView(header, footer, a.logViewer.Render())
}

// renderLogHeader renders the header for the log view.
func (a *App) renderLogHeader() string {
	// Branding
	brand := headerStyle.Render("chief")

	// View indicator
	viewIndicator := viewIndicatorStyle.Render("[Log View]")

	// State indicator
	stateStyle := GetStateStyle(a.state)
	state := stateStyle.Render(fmt.Sprintf("[%s]", a.state.String()))

	// Iteration count (current/max)
	iteration := SubtitleStyle.Render(fmt.Sprintf("Iteration: %d/%d", a.iteration, a.maxIter))

	// Auto-scroll indicator
	var scrollIndicator string
	if a.logViewer.IsAutoScrolling() {
		scrollIndicator = fgSuccess.Render("[Auto-scroll]")
	} else {
		scrollIndicator = fgMuted.Render("[Manual scroll]")
	}

	// Combine elements
	leftPart := lipgloss.JoinHorizontal(lipgloss.Center, brand, "  ", viewIndicator, "  ", state)
	rightPart := lipgloss.JoinHorizontal(lipgloss.Center, iteration, "  ", scrollIndicator)

	// Create the full header line with proper spacing
	headerLine := a.headerBar(leftPart, rightPart)

	// Add a border below
	border := a.fullWidthDivider()

	return lipgloss.JoinVertical(lipgloss.Left, headerLine, border)
}

// renderNarrowLogHeader renders a condensed header for the log view in narrow mode.
func (a *App) renderNarrowLogHeader() string {
	// Branding
	brand := headerStyle.Render("chief")

	// Condensed view indicator
	viewIndicator := viewIndicatorStyle.Render("[Log]")

	// State indicator
	stateStyle := GetStateStyle(a.state)
	state := stateStyle.Render(fmt.Sprintf("[%s]", a.state.String()))

	// Condensed iteration and scroll indicator
	var scrollIcon string
	if a.logViewer.IsAutoScrolling() {
		scrollIcon = fgSuccess.Render("▼")
	} else {
		scrollIcon = fgMuted.Render("▽")
	}
	rightPart := SubtitleStyle.Render(fmt.Sprintf("#%d", a.iteration)) + " " + scrollIcon

	// Combine elements
	leftPart := lipgloss.JoinHorizontal(lipgloss.Center, brand, " ", viewIndicator, " ", state)

	// Create the full header line with proper spacing
	headerLine := a.headerBar(leftPart, rightPart)

	// Add a border below
	border := a.fullWidthDivider()

	return lipgloss.JoinVertical(lipgloss.Left, headerLine, border)
}
