package tui

import (
	"regexp"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
)

var (
	progressStyleOnce sync.Once
	progressStyle     ansi.StyleConfig
)

// progressStyleConfig returns the glamour style used to render progress
// markdown, building it once on first use.
//
// It is built lazily (rather than in init) so terminal-background detection
// runs after the program has taken over the terminal, matching how the rest of
// the TUI resolves its AdaptiveColors.
func progressStyleConfig() ansi.StyleConfig {
	progressStyleOnce.Do(func() {
		progressStyle = buildProgressStyle()
	})
	return progressStyle
}

// buildProgressStyle picks a glamour style matching the terminal background and
// tames two defaults that render badly inside our panels:
//
//   - the document margin is removed so markdown sits flush within the panel
//     padding
//   - inline code no longer uses glamour's default bright red (ANSI 203) on a
//     grey block. That is garish on dark terminals and, because a fixed dark
//     style was previously used regardless of background, showed up as an
//     unreadable dark-red box on light terminals. It becomes a calm cyan accent
//     (matching PrimaryColor) with no background box.
func buildProgressStyle() ansi.StyleConfig {
	cfg := styles.DarkStyleConfig
	codeColor := PrimaryColor.Dark
	if !lipgloss.HasDarkBackground() {
		cfg = styles.LightStyleConfig
		codeColor = PrimaryColor.Light
	}

	zero := uint(0)
	cfg.Document.Margin = &zero
	cfg.Document.StylePrimitive.BlockPrefix = ""
	cfg.Document.StylePrimitive.BlockSuffix = ""

	cfg.Code.Color = &codeColor
	cfg.Code.BackgroundColor = nil

	return cfg
}

// glamourCacheKey identifies a rendered-markdown result. Glamour output is
// deterministic for a given (markdown, width) once the process-wide progress
// style is built, so results can be memoized safely.
type glamourCacheKey struct {
	width int
	md    string
}

// glamourCache memoizes renderGlamour output. renderDetailsPanel calls
// renderGlamour for every progress entry on every frame; without this each
// call built a fresh glamour.TermRenderer and re-rendered unchanged markdown.
var glamourCache sync.Map // glamourCacheKey -> string

// renderGlamour renders a markdown string as styled terminal output, caching
// the result per (markdown, width).
func renderGlamour(markdown string, width int) string {
	if width <= 0 || strings.TrimSpace(markdown) == "" {
		return ""
	}

	key := glamourCacheKey{width: width, md: markdown}
	if v, ok := glamourCache.Load(key); ok {
		return v.(string)
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(progressStyleConfig()),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return markdown
	}

	rendered, err := r.Render(markdown)
	if err != nil {
		return markdown
	}

	// Trim leading/trailing blank lines that glamour adds
	result := strings.TrimSpace(rendered)
	glamourCache.Store(key, result)
	return result
}

// ansiStripRegex matches ANSI escape codes for stripping in tests.
var ansiStripRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// stripANSI removes ANSI escape codes from a string. Exported for tests.
func stripANSI(s string) string {
	return ansiStripRegex.ReplaceAllString(s, "")
}
