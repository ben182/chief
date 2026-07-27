package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/ben182/chief/internal/git"
	"github.com/charmbracelet/lipgloss"
)

// prSuccessLine describes the run's pull request: whether it was opened now or
// was already there from an earlier run on the same branch, and which branch it
// merges into (omitted when gh picked the repository default).
func prSuccessLine(pr git.PR) string {
	verb := "Created PR"
	if pr.AlreadyExisted {
		verb = "PR already open"
	}
	target := ""
	if pr.Base != "" {
		target = fmt.Sprintf(" → %s", pr.Base)
	}
	if pr.Title == "" {
		return fmt.Sprintf("✓ %s%s", verb, target)
	}
	return fmt.Sprintf("✓ %s%s: %s", verb, target, pr.Title)
}

// AutoActionState represents the progress of an auto-action (push or PR).
type AutoActionState int

const (
	AutoActionIdle       AutoActionState = iota // Not configured or not started
	AutoActionInProgress                        // Currently running
	AutoActionSuccess                           // Completed successfully
	AutoActionError                             // Failed with error
)

// StoryTiming records the duration of a completed story.
type StoryTiming struct {
	StoryID  string
	Title    string
	Duration time.Duration
	Cost     float64    // USD cost accumulated for the story (0 when unavailable)
	Tokens   TokenUsage // token usage accumulated for the story (Claude only)
}

// TokenUsage aggregates the token counts reported across a story's turns.
type TokenUsage struct {
	Input         int
	Output        int
	CacheCreation int
	CacheRead     int
}

// Total returns the sum of all token categories.
func (t TokenUsage) Total() int {
	return t.Input + t.Output + t.CacheCreation + t.CacheRead
}

// IsZero reports whether any tokens were recorded.
func (t TokenUsage) IsZero() bool {
	return t.Total() == 0
}

// CompletionScreen manages the completion screen state shown when a PRD finishes.
type CompletionScreen struct {
	width  int
	height int

	prdName        string
	completed      int
	total          int
	branch         string
	commitCount    int
	hasAutoActions bool // Whether push/PR auto-actions are configured

	// Duration data
	totalDuration time.Duration
	// slept is how long the machine was suspended during the run. Reported next
	// to the total rather than added to it: totalDuration and the per-story
	// timings are monotonic, so they are working time and this is the rest of the
	// wall clock. Zero means no sleep was detected and nothing is shown.
	slept        time.Duration
	storyTimings []StoryTiming
	totalCost    float64 // cumulative cost across the run (0 when unavailable)

	// Confetti animation
	confetti *Confetti

	// Auto-action state
	summaryState AutoActionState
	summaryError string
	summaryFile  string
	pushState    AutoActionState
	pushError    string
	prState      AutoActionState
	prError      string
	pr           git.PR
	spinnerFrame int
}

// NewCompletionScreen creates a new completion screen.
func NewCompletionScreen() *CompletionScreen {
	return &CompletionScreen{}
}

// Configure sets up the completion screen with PRD completion data.
func (c *CompletionScreen) Configure(prdName string, completed, total int, branch string, commitCount int, hasAutoActions bool, totalDuration, slept time.Duration, storyTimings []StoryTiming, totalCost float64) {
	c.prdName = prdName
	c.completed = completed
	c.total = total
	c.branch = branch
	c.commitCount = commitCount
	c.hasAutoActions = hasAutoActions
	c.totalDuration = totalDuration
	c.slept = slept
	c.storyTimings = storyTimings
	c.totalCost = totalCost
	// Reset auto-action state
	c.summaryState = AutoActionIdle
	c.summaryError = ""
	c.summaryFile = ""
	c.pushState = AutoActionIdle
	c.pushError = ""
	c.prState = AutoActionIdle
	c.prError = ""
	c.pr = git.PR{}
	c.spinnerFrame = 0
	// Initialize confetti (deferred until SetSize if dimensions aren't known yet)
	if c.width > 0 && c.height > 0 {
		c.confetti = NewConfetti(c.width, c.height)
	} else {
		c.confetti = nil
	}
}

// SetSize sets the screen dimensions.
func (c *CompletionScreen) SetSize(width, height int) {
	c.width = width
	c.height = height
	if c.confetti != nil {
		c.confetti.SetSize(width, height)
	} else if c.prdName != "" && width > 0 && height > 0 {
		// Initialize confetti now that we have real dimensions
		c.confetti = NewConfetti(width, height)
	}
}

// PRDName returns the PRD name shown on the completion screen.
func (c *CompletionScreen) PRDName() string {
	return c.prdName
}

// Branch returns the branch shown on the completion screen.
func (c *CompletionScreen) Branch() string {
	return c.branch
}

// HasBranch returns true if the completion screen has a branch set.
func (c *CompletionScreen) HasBranch() bool {
	return c.branch != ""
}

// SetSummaryInProgress marks summary generation as in progress.
func (c *CompletionScreen) SetSummaryInProgress() {
	c.summaryState = AutoActionInProgress
}

// SetSummarySuccess marks summary generation as successful. fileName is the
// base name of the written summary (e.g. summary-2026-07-21-143205.md), shown
// so the user can find it; empty is tolerated.
func (c *CompletionScreen) SetSummarySuccess(fileName string) {
	c.summaryState = AutoActionSuccess
	c.summaryFile = fileName
}

// SetSummaryError marks summary generation as failed with an error message.
func (c *CompletionScreen) SetSummaryError(errMsg string) {
	c.summaryState = AutoActionError
	c.summaryError = errMsg
}

// SetPushInProgress marks the push as in progress.
func (c *CompletionScreen) SetPushInProgress() {
	c.pushState = AutoActionInProgress
}

// SetPushSuccess marks the push as successful.
func (c *CompletionScreen) SetPushSuccess() {
	c.pushState = AutoActionSuccess
}

// SetPushError marks the push as failed with an error message.
func (c *CompletionScreen) SetPushError(errMsg string) {
	c.pushState = AutoActionError
	c.pushError = errMsg
}

// SetPRInProgress marks the PR creation as in progress.
func (c *CompletionScreen) SetPRInProgress() {
	c.prState = AutoActionInProgress
}

// SetPRSuccess marks the run's pull request as settled — either created now or
// already open from an earlier run on the same branch.
func (c *CompletionScreen) SetPRSuccess(pr git.PR) {
	c.prState = AutoActionSuccess
	c.pr = pr
}

// SetPRError marks the PR creation as failed with an error message.
func (c *CompletionScreen) SetPRError(errMsg string) {
	c.prState = AutoActionError
	c.prError = errMsg
}

// Tick advances the spinner animation frame.
func (c *CompletionScreen) Tick() {
	c.spinnerFrame++
}

// TickConfetti advances the confetti animation by one frame.
func (c *CompletionScreen) TickConfetti() {
	if c.confetti != nil {
		c.confetti.Tick()
	}
}

// HasConfetti returns true if confetti is still animating.
func (c *CompletionScreen) HasConfetti() bool {
	return c.confetti != nil && c.confetti.HasParticles()
}

// IsAutoActionRunning returns true if any auto-action is currently in progress.
func (c *CompletionScreen) IsAutoActionRunning() bool {
	return c.summaryState == AutoActionInProgress || c.pushState == AutoActionInProgress || c.prState == AutoActionInProgress
}

// Static styles for the completion screen. Hoisted to package level because
// Render runs on every confetti tick (~50ms) while particles are alive.
var (
	completionHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(SuccessColor)
	completionModalStyle  = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(SuccessColor).
				Padding(1, 2)
)

// Render renders the completion screen with confetti background.
func (c *CompletionScreen) Render() string {
	modalWidth := min(70, c.width-6)
	if modalWidth < 30 {
		modalWidth = 30
	}

	// Inner content width (inside padding and border)
	innerWidth := modalWidth - 6 // 2 padding each side + 2 border

	var content strings.Builder

	// Header
	content.WriteString(completionHeaderStyle.Render(glyph("🎉", "*") + " PRD Complete!"))
	content.WriteString("\n")

	// Subtitle
	prdTitle := formatPRDTitle(c.prdName)
	content.WriteString(fgText.Render(fmt.Sprintf("%s — %d/%d stories", prdTitle, c.completed, c.total)))
	content.WriteString("\n")
	content.WriteString(DividerStyle.Render(strings.Repeat("─", innerWidth)))
	content.WriteString("\n")

	// Total duration
	if c.totalDuration > 0 {
		content.WriteString("\n")
		line := fmt.Sprintf("Completed in %s", formatDuration(c.totalDuration))
		if c.totalCost > 0 {
			line += fmt.Sprintf("  •  %s", formatCost(c.totalCost))
		}
		content.WriteString(fgSuccess.Render(line))
		content.WriteString("\n")
	}

	// Time the machine spent suspended, which is missing from every duration
	// above — those are monotonic and stop with the machine.
	if c.slept > 0 {
		if c.totalDuration == 0 {
			content.WriteString("\n")
		}
		content.WriteString(fgMuted.Render(fmt.Sprintf("Mac slept %s during the run", formatDuration(c.slept))))
		content.WriteString("\n")
	}

	// Per-story timings
	if len(c.storyTimings) > 0 {
		content.WriteString("\n")
		content.WriteString(c.renderStoryTimings(innerWidth))
	}

	// Branch and commit info (combined to single line)
	content.WriteString("\n")
	if c.branch != "" {
		commitLabel := "commit"
		if c.commitCount != 1 {
			commitLabel = "commits"
		}
		content.WriteString(fgText.Render(fmt.Sprintf("Branch: %s  •  %d %s", c.branch, c.commitCount, commitLabel)))
		content.WriteString("\n")
	}

	// Auto-actions progress or hint
	if c.summaryState != AutoActionIdle || c.pushState != AutoActionIdle || c.prState != AutoActionIdle {
		content.WriteString(c.renderAutoActions(innerWidth))
	} else if !c.hasAutoActions {
		content.WriteString(fgMuted.Render("Configure auto-push and PR in settings (,)"))
		content.WriteString("\n")
	}

	// Footer
	content.WriteString(DividerStyle.Render(strings.Repeat("─", innerWidth)))
	content.WriteString("\n")

	var shortcuts []string
	if c.branch != "" {
		shortcuts = append(shortcuts, "m: merge")
		shortcuts = append(shortcuts, "c: clean")
	}
	shortcuts = append(shortcuts, "l: switch PRD")
	shortcuts = append(shortcuts, "q: quit")
	content.WriteString(fgMuted.Render(strings.Join(shortcuts, "  │  ")))

	// Calculate dynamic height
	modalHeight := c.calculateModalHeight()

	// Modal box style (width/height are the only per-render varying parts)
	modal := completionModalStyle.Width(modalWidth).Height(modalHeight).Render(content.String())

	// Render confetti background and overlay modal
	if c.confetti != nil && c.confetti.HasParticles() {
		background := c.confetti.Render(c.width, c.height)
		return overlayModal(background, modal, c.width, c.height)
	}

	return centerModal(modal, c.width, c.height)
}

// calculateModalHeight determines the dynamic modal height based on content.
func (c *CompletionScreen) calculateModalHeight() int {
	// Base: header(1) + subtitle(1) + divider(1) + blank(1) + duration(1) + blank(1)
	//       + branch(1) + blank(1) + divider(1) + footer(1) + padding(2) = ~12
	base := 12

	// Story timings
	storyLines := len(c.storyTimings)
	maxStoryLines := c.height - base - 6
	if maxStoryLines < 3 {
		maxStoryLines = 3
	}
	if storyLines > maxStoryLines {
		storyLines = maxStoryLines + 1 // +1 for "... and N more"
	}
	if storyLines > 0 {
		storyLines++ // blank line before stories
	}

	// Auto-action lines
	autoLines := 0
	if c.summaryState != AutoActionIdle {
		autoLines++
	}
	if c.pushState != AutoActionIdle {
		autoLines++
	}
	if c.prState != AutoActionIdle {
		autoLines++
		if c.prState == AutoActionSuccess {
			autoLines++ // URL line
		}
	}
	if !c.hasAutoActions && c.pushState == AutoActionIdle && c.prState == AutoActionIdle {
		autoLines++ // hint line
	}

	// No duration line if zero
	durationLine := 0
	if c.totalDuration > 0 {
		durationLine = 2 // blank + duration text
	}

	// Slept-time line, when there was any sleep to report. Without a duration line
	// above it, it brings its own blank separator (see Render).
	sleepLine := 0
	if c.slept > 0 {
		sleepLine = 1
		if durationLine == 0 {
			sleepLine = 2
		}
	}

	calculated := base + storyLines + autoLines + durationLine + sleepLine
	maxHeight := c.height - 4
	if maxHeight < 10 {
		maxHeight = 10
	}
	if calculated > maxHeight {
		calculated = maxHeight
	}
	if calculated < 10 {
		calculated = 10
	}
	return calculated
}

// renderStoryTimings renders the per-story timing list with mini bar charts.
func (c *CompletionScreen) renderStoryTimings(innerWidth int) string {
	var b strings.Builder

	// Find max duration for proportional bars; detect whether cost data exists.
	var maxDur time.Duration
	hasCost := false
	for _, st := range c.storyTimings {
		if st.Duration > maxDur {
			maxDur = st.Duration
		}
		if st.Cost > 0 {
			hasCost = true
		}
	}

	// Cost column is only reserved when at least one story has cost (Claude only).
	costW := 0
	if hasCost {
		costW = 9 // " $12.3456" style, generous
	}

	maxBarWidth := 10
	// Layout: "✓ " + title + " " + dots + " " + duration + [cost] + "  " + bar
	// Reserve: 2 (check+space) + 1 (space before dots) + 1 (space after dots) + 8 (duration) + costW + 2 (gap) + bar
	fixedWidth := 2 + 1 + 1 + 8 + costW + 2 + maxBarWidth
	maxTitleWidth := innerWidth - fixedWidth
	if maxTitleWidth < 10 {
		maxTitleWidth = 10
	}

	// Limit visible stories
	maxVisible := c.height - 16
	if maxVisible < 3 {
		maxVisible = 3
	}
	visible := c.storyTimings
	truncated := 0
	if len(visible) > maxVisible {
		truncated = len(visible) - maxVisible
		visible = visible[:maxVisible]
	}

	for _, st := range visible {
		// Truncate title if needed
		title := st.Title
		titleLen := lipgloss.Width(title)
		if titleLen > maxTitleWidth {
			title = title[:maxTitleWidth-1] + "…"
			titleLen = maxTitleWidth
		}

		// Duration string (right-aligned in 8 chars)
		durStr := formatDuration(st.Duration)
		if len(durStr) > 8 {
			durStr = durStr[:8]
		}

		// Dot leaders
		dotCount := innerWidth - 2 - titleLen - 1 - len(durStr) - costW - 2 - maxBarWidth - 1
		if dotCount < 2 {
			dotCount = 2
		}
		dots := strings.Repeat(".", dotCount)

		// Mini bar
		barWidth := 0
		if maxDur > 0 {
			barWidth = int(float64(maxBarWidth) * float64(st.Duration) / float64(maxDur))
			if barWidth < 1 && st.Duration > 0 {
				barWidth = 1
			}
		}
		bar := strings.Repeat("█", barWidth)

		b.WriteString(fgSuccess.Render("✓"))
		b.WriteString(" ")
		b.WriteString(fgText.Render(title))
		b.WriteString(" ")
		b.WriteString(fgMuted.Render(dots))
		b.WriteString(" ")
		b.WriteString(fgText.Render(durStr))
		if costW > 0 {
			costStr := ""
			if st.Cost > 0 {
				costStr = formatCost(st.Cost)
			}
			// Right-align cost within the reserved column.
			b.WriteString(fgMuted.Render(fmt.Sprintf("%*s", costW, costStr)))
		}
		b.WriteString("  ")
		b.WriteString(fgSuccess.Render(bar))
		b.WriteString("\n")
	}

	if truncated > 0 {
		b.WriteString(fgMuted.Render(fmt.Sprintf("  ... and %d more", truncated)))
		b.WriteString("\n")
	}

	return b.String()
}

// spinnerChars are the animation frames for the completion screen spinner.
var spinnerChars = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// renderAutoActions renders the auto-action progress section.
func (c *CompletionScreen) renderAutoActions(innerWidth int) string {
	var lines strings.Builder

	// Summary status
	if c.summaryState != AutoActionIdle {
		switch c.summaryState {
		case AutoActionInProgress:
			frame := spinnerChars[c.spinnerFrame%len(spinnerChars)]
			lines.WriteString(fgPrimary.Render(fmt.Sprintf("%s Writing run summary...", frame)))
		case AutoActionSuccess:
			label := "✓ Wrote run summary"
			if c.summaryFile != "" {
				label += fmt.Sprintf(" (%s)", c.summaryFile)
			}
			lines.WriteString(fgSuccess.Render(label))
		case AutoActionError:
			lines.WriteString(fgError.Render(fmt.Sprintf("✗ Summary failed: %s", c.summaryError)))
		}
		lines.WriteString("\n")
	}

	// Push status
	if c.pushState != AutoActionIdle {
		switch c.pushState {
		case AutoActionInProgress:
			frame := spinnerChars[c.spinnerFrame%len(spinnerChars)]
			lines.WriteString(fgPrimary.Render(fmt.Sprintf("%s Pushing branch to remote...", frame)))
		case AutoActionSuccess:
			lines.WriteString(fgSuccess.Render("✓ Pushed branch to remote"))
		case AutoActionError:
			lines.WriteString(fgError.Render(fmt.Sprintf("✗ Push failed: %s", c.pushError)))
		}
		lines.WriteString("\n")
	}

	// PR status
	if c.prState != AutoActionIdle {
		switch c.prState {
		case AutoActionInProgress:
			frame := spinnerChars[c.spinnerFrame%len(spinnerChars)]
			lines.WriteString(fgPrimary.Render(fmt.Sprintf("%s Creating pull request...", frame)))
		case AutoActionSuccess:
			lines.WriteString(fgSuccess.Render(prSuccessLine(c.pr)))
			lines.WriteString("\n")
			lines.WriteString(fgText.Render(fmt.Sprintf("  %s", c.pr.URL)))
		case AutoActionError:
			lines.WriteString(fgError.Render(fmt.Sprintf("✗ PR creation failed: %s", c.prError)))
		}
		lines.WriteString("\n")
	}

	_ = innerWidth
	return lines.String()
}

// overlayModal composites a modal on top of a background, centering the modal.
func overlayModal(background, modal string, screenWidth, screenHeight int) string {
	bgLines := strings.Split(background, "\n")
	modalLines := strings.Split(modal, "\n")

	// Measure modal dimensions
	modalHeight := len(modalLines)
	modalWidth := 0
	for _, line := range modalLines {
		w := lipgloss.Width(line)
		if w > modalWidth {
			modalWidth = w
		}
	}

	// Calculate centering offsets
	offsetY := (screenHeight - modalHeight) / 2
	offsetX := (screenWidth - modalWidth) / 2
	if offsetY < 0 {
		offsetY = 0
	}
	if offsetX < 0 {
		offsetX = 0
	}

	// Pad background to full screen height
	for len(bgLines) < screenHeight {
		bgLines = append(bgLines, strings.Repeat(" ", screenWidth))
	}

	// Overlay modal lines onto background
	for i, mLine := range modalLines {
		bgIdx := offsetY + i
		if bgIdx >= len(bgLines) {
			break
		}

		mWidth := lipgloss.Width(mLine)
		if mWidth == 0 {
			continue
		}

		bgLine := bgLines[bgIdx]

		// Build: bg prefix (ANSI-aware) + modal line + bg suffix (ANSI-aware)
		prefix := ansiTruncate(bgLine, offsetX)
		suffix := ansiSkip(bgLine, offsetX+mWidth)

		bgLines[bgIdx] = prefix + mLine + suffix
	}

	return strings.Join(bgLines[:screenHeight], "\n")
}
