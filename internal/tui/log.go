package tui

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/minicodemonkey/chief/internal/loop"
)

// LogEntry represents a single entry in the log viewer.
type LogEntry struct {
	Type      loop.EventType
	Text      string
	Tool      string
	ToolInput map[string]interface{}
	StoryID   string
	FilePath  string   // For Read tool results, stores the file path for syntax highlighting
	CrashLog  []string // For retry entries, the last stderr lines from the crashed process

	highlightedCode string   // Pre-computed syntax highlighted code (computed once on add)
	cachedLines     []string // Pre-rendered output lines (invalidated on width change)
}

// LogViewer manages the log viewport state.
type LogViewer struct {
	entries          []LogEntry
	scrollPos        int    // Current scroll position (top line index)
	height           int    // Viewport height (lines)
	width            int    // Viewport width
	autoScroll       bool   // Auto-scroll to bottom when new content arrives
	lastReadFilePath string // Track the last Read tool's file path for syntax highlighting
	totalLineCount   int    // Running total of all rendered lines (O(1) lookup)
	reviewPending    bool   // Whether a separate review agent runs after each story — changes the "story done" marker to signal a review still follows
}

// NewLogViewer creates a new log viewer.
func NewLogViewer() *LogViewer {
	return &LogViewer{
		entries:    make([]LogEntry, 0),
		scrollPos:  0,
		autoScroll: true,
	}
}

// SetReviewPending records whether a separate review agent runs after each
// story's build agent commits. When true, the "story done" marker makes clear a
// review still follows instead of reading as the final word on the story.
func (l *LogViewer) SetReviewPending(pending bool) {
	l.reviewPending = pending
}

// AddEvent adds a loop event to the log.
func (l *LogViewer) AddEvent(event loop.Event) {
	entry := LogEntry{
		Type:      event.Type,
		Text:      event.Text,
		Tool:      event.Tool,
		ToolInput: event.ToolInput,
		StoryID:   event.StoryID,
		CrashLog:  event.CrashLog,
	}

	// Track Read tool file paths for syntax highlighting
	if event.Type == loop.EventToolStart && event.Tool == "Read" {
		if filePath, ok := event.ToolInput["file_path"].(string); ok {
			l.lastReadFilePath = filePath
		}
	}

	// For tool results, attach the file path and pre-compute syntax highlighting
	if event.Type == loop.EventToolResult && l.lastReadFilePath != "" {
		entry.FilePath = l.lastReadFilePath
		l.lastReadFilePath = "" // Clear after consuming
		if entry.Text != "" {
			entry.highlightedCode = l.highlightCode(entry.Text, entry.FilePath)
		}
	}

	// Filter out events we don't want to display
	switch event.Type {
	case loop.EventAssistantText, loop.EventToolStart, loop.EventToolResult,
		loop.EventStoryDone, loop.EventStoryNeedsReview, loop.EventStoryNoCommit, loop.EventComplete, loop.EventError, loop.EventRetrying,
		loop.EventWatchdogTimeout, loop.EventNoGitRepo, loop.EventReviewStart, loop.EventReviewDone:
		// Pre-render and cache lines
		if l.width > 0 {
			entry.cachedLines = l.renderEntry(entry)
			l.totalLineCount += len(entry.cachedLines)
		}
		l.entries = append(l.entries, entry)
	default:
		// Skip iteration start, unknown events, etc.
		return
	}

	// Auto-scroll to bottom if enabled
	if l.autoScroll && l.height > 0 {
		l.scrollToBottom()
	}
}

// SetSize sets the viewport dimensions. Rebuilds the line cache if width changed.
func (l *LogViewer) SetSize(width, height int) {
	widthChanged := l.width != width
	l.width = width
	l.height = height

	if widthChanged && width > 0 {
		l.rebuildCache()
	}
}

// rebuildCache re-renders all entries using the current width.
// This is called when the terminal is resized. Syntax highlighting is NOT
// recomputed since it's width-independent and stored in highlightedCode.
func (l *LogViewer) rebuildCache() {
	l.totalLineCount = 0
	for i := range l.entries {
		l.entries[i].cachedLines = l.renderEntry(l.entries[i])
		l.totalLineCount += len(l.entries[i].cachedLines)
	}
}

// ScrollUp scrolls up by one line.
func (l *LogViewer) ScrollUp() {
	if l.scrollPos > 0 {
		l.scrollPos--
		l.autoScroll = false
	}
}

// ScrollDown scrolls down by one line.
func (l *LogViewer) ScrollDown() {
	maxScroll := l.maxScrollPos()
	if l.scrollPos < maxScroll {
		l.scrollPos++
	}
	// Re-enable auto-scroll if at bottom
	if l.scrollPos >= maxScroll {
		l.autoScroll = true
	}
}

// PageUp scrolls up by half a page.
func (l *LogViewer) PageUp() {
	halfPage := l.height / 2
	if halfPage < 1 {
		halfPage = 1
	}
	l.scrollPos -= halfPage
	if l.scrollPos < 0 {
		l.scrollPos = 0
	}
	l.autoScroll = false
}

// PageDown scrolls down by half a page.
func (l *LogViewer) PageDown() {
	halfPage := l.height / 2
	if halfPage < 1 {
		halfPage = 1
	}
	l.scrollPos += halfPage
	maxScroll := l.maxScrollPos()
	if l.scrollPos > maxScroll {
		l.scrollPos = maxScroll
	}
	// Re-enable auto-scroll if at bottom
	if l.scrollPos >= maxScroll {
		l.autoScroll = true
	}
}

// ScrollToTop scrolls to the top.
func (l *LogViewer) ScrollToTop() {
	l.scrollPos = 0
	l.autoScroll = false
}

// ScrollToBottom (exported) scrolls to the bottom.
func (l *LogViewer) ScrollToBottom() {
	l.scrollToBottom()
}

// scrollToBottom scrolls to the bottom.
func (l *LogViewer) scrollToBottom() {
	l.scrollPos = l.maxScrollPos()
	l.autoScroll = true
}

// maxScrollPos returns the maximum scroll position.
func (l *LogViewer) maxScrollPos() int {
	maxPos := l.totalLineCount - l.height
	if maxPos < 0 {
		return 0
	}
	return maxPos
}

// totalLines returns the total number of rendered lines (O(1)).
func (l *LogViewer) totalLines() int {
	return l.totalLineCount
}

// getToolIcon returns an emoji icon for a tool name, or a short ASCII tag when
// the TUI is in ASCII mode (CHIEF_ASCII).
func getToolIcon(toolName string) string {
	switch toolName {
	case "Read":
		return glyph("📖", "[R]")
	case "Edit":
		return glyph("✏️", "[E]")
	case "Write":
		return glyph("📝", "[W]")
	case "Bash":
		return glyph("🔨", "[$]")
	case "Glob":
		return glyph("🔍", "[G]")
	case "Grep":
		return glyph("🔎", "[/]")
	case "Task":
		return glyph("🤖", "[T]")
	case "Skill":
		return glyph("🧐", "[S]")
	case "WebFetch":
		return glyph("🌐", "[@]")
	case "WebSearch":
		return glyph("🌐", "[@]")
	default:
		return glyph("⚙️", "[*]")
	}
}

// getToolArgument extracts the main argument from tool input for display.
func getToolArgument(toolName string, input map[string]interface{}) string {
	if input == nil {
		return ""
	}

	switch toolName {
	case "Read", "Edit", "Write":
		if path, ok := input["file_path"].(string); ok {
			return path
		}
	case "Bash":
		if cmd, ok := input["command"].(string); ok {
			// Truncate long commands
			return truncateWithEllipsis(cmd, 60)
		}
	case "Glob":
		if pattern, ok := input["pattern"].(string); ok {
			return pattern
		}
	case "Grep":
		if pattern, ok := input["pattern"].(string); ok {
			return pattern
		}
	case "WebFetch", "WebSearch":
		if url, ok := input["url"].(string); ok {
			return url
		}
		if query, ok := input["query"].(string); ok {
			return query
		}
	case "Task":
		if desc, ok := input["description"].(string); ok {
			return desc
		}
	case "Skill":
		if skill, ok := input["skill"].(string); ok {
			return skill
		}
	}

	return ""
}

// IsAutoScrolling returns whether auto-scroll is enabled.
func (l *LogViewer) IsAutoScrolling() bool {
	return l.autoScroll
}

// Clear clears all log entries.
func (l *LogViewer) Clear() {
	l.entries = make([]LogEntry, 0)
	l.scrollPos = 0
	l.autoScroll = true
	l.totalLineCount = 0
}

// Render renders only the visible portion of the log viewer.
func (l *LogViewer) Render() string {
	if len(l.entries) == 0 {
		emptyStyle := lipgloss.NewStyle().
			Foreground(MutedColor).
			Padding(1, 2)
		return emptyStyle.Render("No log entries yet. Start the loop to see Claude's activity.")
	}

	// Calculate visible range
	startLine := l.scrollPos
	if startLine < 0 {
		startLine = 0
	}
	if startLine >= l.totalLineCount {
		startLine = l.totalLineCount - 1
		if startLine < 0 {
			startLine = 0
		}
	}

	endLine := startLine + l.height
	if endLine > l.totalLineCount {
		endLine = l.totalLineCount
	}

	// Collect only visible lines by scanning cached entries
	currentLine := 0
	var visibleLines []string

	for i := range l.entries {
		lines := l.entries[i].cachedLines
		entryEnd := currentLine + len(lines)

		// Skip entries entirely before the viewport
		if entryEnd <= startLine {
			currentLine = entryEnd
			continue
		}
		// Stop once we're past the viewport
		if currentLine >= endLine {
			break
		}

		// This entry has some visible lines
		for j, line := range lines {
			lineNum := currentLine + j
			if lineNum >= startLine && lineNum < endLine {
				visibleLines = append(visibleLines, line)
			}
		}
		currentLine = entryEnd
	}

	// Add cursor indicator at bottom if streaming
	content := strings.Join(visibleLines, "\n")
	if l.autoScroll && len(l.entries) > 0 {
		lastEntry := l.entries[len(l.entries)-1]
		if lastEntry.Type == loop.EventAssistantText || lastEntry.Type == loop.EventToolStart {
			cursorStyle := lipgloss.NewStyle().Foreground(PrimaryColor).Blink(true)
			content += "\n" + cursorStyle.Render("▌")
		}
	}

	return content
}

// renderEntry renders a single log entry as lines.
func (l *LogViewer) renderEntry(entry LogEntry) []string {
	switch entry.Type {
	case loop.EventToolStart:
		return l.renderToolCard(entry)
	case loop.EventToolResult:
		return l.renderToolResult(entry)
	case loop.EventStoryDone:
		return l.renderStoryDone(entry)
	case loop.EventReviewStart, loop.EventReviewDone:
		return l.renderReview(entry)
	case loop.EventComplete:
		return l.renderComplete(entry)
	case loop.EventError:
		return l.renderError(entry)
	case loop.EventRetrying:
		return l.renderRetrying(entry)
	case loop.EventWatchdogTimeout:
		return l.renderWatchdogTimeout(entry)
	default:
		return l.renderText(entry)
	}
}

// renderText renders an assistant text entry.
func (l *LogViewer) renderText(entry LogEntry) []string {
	if entry.Text == "" {
		return []string{}
	}

	textStyle := lipgloss.NewStyle().Foreground(TextColor)
	wrapped := wrapText(entry.Text, l.width-4)
	lines := strings.Split(wrapped, "\n")

	var result []string
	for _, line := range lines {
		result = append(result, textStyle.Render(line))
	}
	return result
}

// renderToolCard renders a tool call as a single styled line with icon and argument.
func (l *LogViewer) renderToolCard(entry LogEntry) []string {
	toolName := entry.Tool
	if toolName == "" {
		toolName = "unknown"
	}

	// Get icon and argument
	icon := getToolIcon(toolName)
	arg := getToolArgument(toolName, entry.ToolInput)

	// Style the output
	toolNameStyle := lipgloss.NewStyle().Foreground(PrimaryColor).Bold(true)
	argStyle := lipgloss.NewStyle().Foreground(TextColor)

	// Build the line: icon + tool name + argument
	var line string
	if arg != "" {
		// Truncate argument if too long
		maxArgLen := l.width - len(toolName) - 8
		if maxArgLen > 0 {
			arg = truncateWithEllipsis(arg, maxArgLen)
		}
		line = fmt.Sprintf("%s %s %s", icon, toolNameStyle.Render(toolName), argStyle.Render(arg))
	} else {
		line = fmt.Sprintf("%s %s", icon, toolNameStyle.Render(toolName))
	}

	return []string{line}
}

// renderToolResult renders a tool result.
func (l *LogViewer) renderToolResult(entry LogEntry) []string {
	resultStyle := lipgloss.NewStyle().Foreground(MutedColor)
	checkStyle := lipgloss.NewStyle().Foreground(SuccessColor)

	text := entry.Text
	if text == "" {
		return []string{resultStyle.Render(checkStyle.Render("  ↳ ") + "(no output)")}
	}

	// Use pre-computed syntax highlighting if available
	if entry.highlightedCode != "" {
		lines := strings.Split(entry.highlightedCode, "\n")
		var result []string
		result = append(result, checkStyle.Render("  ↳ ")) // Result indicator
		// Limit to 20 lines to keep the log view manageable
		maxLines := 20
		for i, line := range lines {
			if i >= maxLines {
				result = append(result, resultStyle.Render(fmt.Sprintf("    ... (%d more lines)", len(lines)-maxLines)))
				break
			}
			result = append(result, "    "+line)
		}
		return result
	}

	// Fallback: show a compact single-line result
	maxLen := l.width - 8
	if maxLen < 20 {
		maxLen = 20
	}
	text = truncateWithEllipsis(text, maxLen)
	return []string{resultStyle.Render(checkStyle.Render("  ↳ ") + text)}
}

// highlightCode applies syntax highlighting to code based on file extension.
func (l *LogViewer) highlightCode(code, filePath string) string {
	// Strip line number prefixes from Read tool output (format: "   1→" or "   1\t")
	code = stripLineNumbers(code)

	// Get lexer based on file extension
	ext := filepath.Ext(filePath)
	lexer := lexers.Match(filePath)
	if lexer == nil {
		lexer = lexers.Get(ext)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	// Use Tokyo Night theme for syntax highlighting
	style := styles.Get("tokyonight-night")
	if style == nil {
		style = styles.Fallback
	}

	// Use terminal256 formatter for ANSI color output
	formatter := formatters.Get("terminal256")
	if formatter == nil {
		formatter = formatters.Fallback
	}

	// Tokenize and format
	iterator, err := lexer.Tokenise(nil, code)
	if err != nil {
		return ""
	}

	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, iterator); err != nil {
		return ""
	}

	return buf.String()
}

// stripLineNumbers removes line number prefixes from Read tool output.
// The format is: optional spaces + line number + → or tab + content
func stripLineNumbers(code string) string {
	lines := strings.Split(code, "\n")
	var result []string

	for _, line := range lines {
		// Look for patterns like "   1→", "  10→", "   1\t", etc.
		stripped := line

		// Find the arrow or tab after the line number
		arrowIdx := strings.Index(line, "→")
		tabIdx := strings.Index(line, "\t")

		idx := -1
		if arrowIdx != -1 && tabIdx != -1 {
			if arrowIdx < tabIdx {
				idx = arrowIdx
			} else {
				idx = tabIdx
			}
		} else if arrowIdx != -1 {
			idx = arrowIdx
		} else if tabIdx != -1 {
			idx = tabIdx
		}

		if idx > 0 && idx < 10 { // Line number prefix is typically short
			// Check if everything before is spaces and digits
			prefix := line[:idx]
			isLineNum := true
			hasDigit := false
			for _, ch := range prefix {
				if ch >= '0' && ch <= '9' {
					hasDigit = true
				} else if ch != ' ' {
					isLineNum = false
					break
				}
			}
			if isLineNum && hasDigit {
				// Skip the arrow/tab character (→ is multi-byte)
				if line[idx] == '\t' {
					stripped = line[idx+1:]
				} else {
					// → is 3 bytes in UTF-8
					stripped = line[idx+3:]
				}
			}
		}

		result = append(result, stripped)
	}

	return strings.Join(result, "\n")
}

// renderStoryDone renders a story done marker. When a review agent still has to
// run (reviewPending), it is styled as an intermediate "build done, review next"
// marker in the in-progress color rather than the final success green, so the
// reader knows the story is not yet signed off.
func (l *LogViewer) renderStoryDone(entry LogEntry) []string {
	label := glyph("✓ Story done", "[x] Story done")
	color := SuccessColor
	if l.reviewPending {
		label = glyph("✓ Build done — review pending", "[x] Build done - review pending")
		color = WarningColor
	}

	storyStyle := lipgloss.NewStyle().
		Foreground(color).
		Bold(true).
		Padding(0, 1)

	dividerStyle := lipgloss.NewStyle().Foreground(color)
	divider := dividerStyle.Render(strings.Repeat("─", l.width-4))

	return []string{
		"",
		divider,
		storyStyle.Render(label),
		divider,
		"",
	}
}

// renderReview renders the separate review agent's start/finish markers so the
// review phase is visibly distinct from the build agent's work in the log.
func (l *LogViewer) renderReview(entry LogEntry) []string {
	// The start marker uses the primary accent (a phase is underway); the done
	// marker turns success-green — this is the point the story is actually signed
	// off, after the yellow "review pending" build marker.
	color := PrimaryColor
	icon := glyph("🔍", "[review]")
	text := entry.Text
	if entry.Type == loop.EventReviewDone {
		color = SuccessColor
		icon = glyph("✓", "[x]")
		if text == "" {
			text = "Review complete"
		}
	} else if text == "" {
		text = "Reviewing changes"
	}

	reviewStyle := lipgloss.NewStyle().
		Foreground(color).
		Bold(true).
		Padding(0, 1)
	return []string{
		"",
		reviewStyle.Render(icon + " " + text),
	}
}

// renderComplete renders a completion message.
func (l *LogViewer) renderComplete(entry LogEntry) []string {
	completeStyle := lipgloss.NewStyle().
		Foreground(SuccessColor).
		Bold(true).
		Padding(0, 1)

	dividerStyle := lipgloss.NewStyle().Foreground(SuccessColor)
	divider := dividerStyle.Render(strings.Repeat("═", l.width-4))

	return []string{
		"",
		divider,
		completeStyle.Render("✓ All stories complete!"),
		divider,
	}
}

// renderError renders an error message.
func (l *LogViewer) renderError(entry LogEntry) []string {
	errorStyle := lipgloss.NewStyle().
		Foreground(ErrorColor).
		Bold(true)

	text := entry.Text
	if text == "" {
		text = "An error occurred"
	}

	return []string{errorStyle.Render("✗ Error: " + text)}
}

// renderRetrying renders a retry message.
func (l *LogViewer) renderRetrying(entry LogEntry) []string {
	retryStyle := lipgloss.NewStyle().
		Foreground(WarningColor).
		Bold(true)

	text := entry.Text
	if text == "" {
		text = "Retrying..."
	}

	lines := []string{retryStyle.Render(glyph("🔄", "~") + " " + text)}

	// Surface the crashed process's stderr so users don't have to dig in log files.
	if len(entry.CrashLog) > 0 {
		crashStyle := lipgloss.NewStyle().Foreground(MutedColor)
		for _, cl := range entry.CrashLog {
			for _, w := range strings.Split(wrapText(cl, l.width-6), "\n") {
				lines = append(lines, crashStyle.Render("   │ "+w))
			}
		}
	}

	return lines
}

// renderWatchdogTimeout renders a watchdog timeout message.
func (l *LogViewer) renderWatchdogTimeout(entry LogEntry) []string {
	style := lipgloss.NewStyle().
		Foreground(WarningColor).
		Bold(true)

	text := entry.Text
	if text == "" {
		text = "Watchdog timeout: process killed"
	}

	return []string{style.Render("⏱ " + text)}
}
