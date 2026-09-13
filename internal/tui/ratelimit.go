package tui

import (
	"fmt"
	"time"

	"github.com/ben182/chief/internal/loop"
	"github.com/charmbracelet/lipgloss"
)

// formatRateLimitChip renders the header's usage chip for a limit report, or ""
// when there is nothing to say. A healthy window says nothing: the point of the
// chip is the warning before the wall, not a permanent gauge.
func formatRateLimitChip(info loop.RateLimitInfo) string {
	switch {
	case info.Rejected():
		if info.ResetsAt.IsZero() {
			return "Limit reached"
		}
		return fmt.Sprintf("Limit reached · %s", info.ResetsAt.Local().Format("15:04"))
	case info.Warning():
		pct := int(info.Utilization*100 + 0.5)
		if info.ResetsAt.IsZero() {
			return fmt.Sprintf("Limit %d%%", pct)
		}
		return fmt.Sprintf("Limit %d%% · %s", pct, info.ResetsAt.Local().Format("15:04"))
	default:
		return ""
	}
}

// formatRateLimitChipCompact is the narrow-terminal form of the same chip. The
// warning is the part worth the few columns it costs; the reset time isn't.
func formatRateLimitChipCompact(info loop.RateLimitInfo) string {
	switch {
	case info.Rejected():
		return glyph("⚠", "!") + "limit"
	case info.Warning():
		return fmt.Sprintf("%s%d%%", glyph("⚠", "!"), int(info.Utilization*100+0.5))
	default:
		return ""
	}
}

// rateLimitChipStyle colours the chip by how bad the news is: a filling window
// is a warning, a full one is an error in everything but name — the run can't
// make progress until it resets.
func rateLimitChipStyle(info loop.RateLimitInfo) lipgloss.Style {
	if info.Rejected() {
		return lipgloss.NewStyle().Foreground(ErrorColor).Bold(true)
	}
	return lipgloss.NewStyle().Foreground(WarningColor)
}

// formatRateLimitLog renders a limit report as a log line. Unlike the header
// chip it also reports recovery, because in the log that is news: it is the line
// that says the run picked up where it left off.
func formatRateLimitLog(info loop.RateLimitInfo) string {
	window := rateLimitWindowLabel(info.WindowType)
	resets := ""
	if !info.ResetsAt.IsZero() {
		resets = fmt.Sprintf(", resets %s", info.ResetsAt.Local().Format("15:04"))
	}

	switch {
	case info.Rejected():
		return fmt.Sprintf("Rate limit reached — %s exhausted%s", window, resets)
	case info.Warning():
		return fmt.Sprintf("Rate limit warning — %d%% of the %s used%s",
			int(info.Utilization*100+0.5), window, resets)
	default:
		return "Rate limit cleared — the window has room again"
	}
}

// rateLimitWindowLabel names a usage window for a log line, falling back to
// "usage window" when the provider reports a type this build doesn't know.
func rateLimitWindowLabel(windowType string) string {
	switch windowType {
	case "five_hour":
		return "5h window"
	case "seven_day":
		return "7d window"
	default:
		return "usage window"
	}
}

// rateLimitStale reports whether a limit report has been overtaken by its own
// reset time, so the header can drop a chip for a window that has since rolled
// over even if no fresh report arrived (a stopped run sends none).
func rateLimitStale(info loop.RateLimitInfo, now time.Time) bool {
	return !info.ResetsAt.IsZero() && !info.ResetsAt.After(now)
}
