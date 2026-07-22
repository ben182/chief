package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"
)

// formatDuration formats a duration in a human-readable way.
func formatDuration(d time.Duration) string {
	if d == 0 {
		return "0s"
	}

	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60

	if h > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// formatCost formats a USD cost. Small amounts get an extra decimal so
// sub-10-cent stories don't round away to "$0.00".
func formatCost(c float64) string {
	if c >= 0.10 {
		return fmt.Sprintf("$%.2f", c)
	}
	return fmt.Sprintf("$%.3f", c)
}

// formatTokenCount formats a token count compactly (e.g. 1234 -> "1.2K",
// 2_500_000 -> "2.5M").
func formatTokenCount(n int) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// formatPRDTitle converts a kebab-case PRD name to title case.
func formatPRDTitle(name string) string {
	words := strings.Split(name, "-")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// wrapText wraps text to fit within a given width.
func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}

	var result strings.Builder
	words := strings.Fields(text)
	lineLen := 0

	for i, word := range words {
		wordLen := len(word)

		if lineLen+wordLen+1 > width && lineLen > 0 {
			result.WriteString("\n")
			lineLen = 0
		}

		if lineLen > 0 {
			result.WriteString(" ")
			lineLen++
		}

		result.WriteString(word)
		lineLen += wordLen

		// Handle very long words
		if wordLen > width && i < len(words)-1 {
			result.WriteString("\n")
			lineLen = 0
		}
	}

	return result.String()
}

// truncateWithEllipsis truncates text to maxLen display columns, adding "..."
// if truncated. Width- and rune-aware (and ANSI-aware): never cuts a multi-byte
// rune in half — byte slicing did, which garbled umlauts/emoji in the display.
func truncateWithEllipsis(text string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if lipgloss.Width(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		return truncate.String(text, uint(maxLen))
	}
	return truncate.StringWithTail(text, uint(maxLen), "...")
}
