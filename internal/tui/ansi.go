package tui

import "strings"

// ansiTruncate returns the first maxWidth visual columns of an ANSI-styled string,
// properly passing through escape sequences without counting them as visible width.
func ansiTruncate(s string, maxWidth int) string {
	var result strings.Builder
	width := 0
	inEscape := false
	for _, r := range s {
		if r == '\033' {
			inEscape = true
			result.WriteRune(r)
			continue
		}
		if inEscape {
			result.WriteRune(r)
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		if width >= maxWidth {
			break
		}
		result.WriteRune(r)
		width++
	}
	// Reset any open ANSI styling
	result.WriteString("\033[0m")
	return result.String()
}

// ansiSkip skips the first skipWidth visual columns of an ANSI-styled string
// and returns the remainder.
func ansiSkip(s string, skipWidth int) string {
	width := 0
	inEscape := false
	for i, r := range s {
		if r == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		if width >= skipWidth {
			return s[i:]
		}
		width++
	}
	return ""
}
