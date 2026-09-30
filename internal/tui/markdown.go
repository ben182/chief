package tui

import (
	"regexp"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
)

var (
	progressStyleOnce sync.Once
	progressStyle     ansi.StyleConfig
)

// progressStyleConfig returns the glamour style used to render progress
// markdown, building it once on first use.
func progressStyleConfig() ansi.StyleConfig {
	progressStyleOnce.Do(func() {
		progressStyle = buildProgressStyle()
	})
	return progressStyle
}

// buildProgressStyle takes glamour's dark style for its layout and swaps every
// color for a terminal palette slot, like the rest of the TUI (see styles.go).
// glamour's own styles hardcode 256-color and hex values for one background,
// so they break as soon as the terminal theme changes under a running session.
//
// It also tames two defaults that render badly inside our panels:
//
//   - the document margin is removed so markdown sits flush within the panel
//     padding
//   - inline code loses glamour's grey background block and becomes a calm
//     cyan accent (PrimaryColor)
//
// Fenced code blocks drop syntax highlighting: its theme is hex colors too.
func buildProgressStyle() ansi.StyleConfig {
	cfg := styles.DarkStyleConfig
	primary, muted, accent := string(PrimaryColor), string(MutedColor), string(ReviewColor)

	zero := uint(0)
	cfg.Document.Margin = &zero
	cfg.Document.StylePrimitive.BlockPrefix = ""
	cfg.Document.StylePrimitive.BlockSuffix = ""
	cfg.Document.Color = nil

	cfg.Heading.Color = &primary
	cfg.H1.Color = &primary
	cfg.H1.BackgroundColor = nil
	cfg.H6.Color = &muted
	cfg.HorizontalRule.Color = &muted
	cfg.Link.Color = &primary
	cfg.LinkText.Color = &primary
	cfg.Image.Color = &accent
	cfg.ImageText.Color = &muted

	cfg.Code.Color = &primary
	cfg.Code.BackgroundColor = nil
	cfg.CodeBlock.Color = nil
	cfg.CodeBlock.Chroma = nil

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
