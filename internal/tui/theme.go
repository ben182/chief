package tui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// asciiMode reports whether the TUI should render plain-ASCII glyphs instead of
// Unicode/emoji icons. It is enabled by setting CHIEF_ASCII to a truthy value
// and is meant for terminals (or muxers, or piped logs) that render emoji and
// box-drawing glyphs poorly.
var asciiMode bool

func init() {
	initTheme()
}

// initTheme reads the environment once at startup and configures glyph and
// color behaviour accordingly:
//
//   - CHIEF_ASCII=1 switches status and tool icons to ASCII fallbacks.
//   - NO_COLOR (any non-empty value, per https://no-color.org) forces lipgloss
//     to strip all styling so the UI degrades to plain text.
//
// It is split out from init() so tests can invoke it after tweaking the
// environment.
func initTheme() {
	asciiMode = envTruthy(os.Getenv("CHIEF_ASCII"))

	if os.Getenv("NO_COLOR") != "" {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
}

// envTruthy reports whether an environment variable value should be treated as
// "on". Empty, "0", "false", and "no" are off; everything else is on.
func envTruthy(v string) bool {
	switch v {
	case "", "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// glyph returns unicode normally, or ascii when the TUI is in ASCII mode.
func glyph(unicode, ascii string) string {
	if asciiMode {
		return ascii
	}
	return unicode
}
