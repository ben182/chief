package prd

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// storyHeadingRegex matches story headings like "### US-001: Story Title" or "#### US-001: Story Title"
var storyHeadingRegex = regexp.MustCompile(`^#{3,4}\s+([A-Za-z]+-\d+):\s+(.+)$`)

// statusLineRegex matches "**Status:** value"
var statusLineRegex = regexp.MustCompile(`^\*\*Status:\*\*\s*(.+)$`)

// priorityLineRegex matches "**Priority:** value"
var priorityLineRegex = regexp.MustCompile(`^\*\*Priority:\*\*\s*(.+)$`)

// descriptionLineRegex matches "**Description:** value"
var descriptionLineRegex = regexp.MustCompile(`^\*\*Description:\*\*\s*(.+)$`)

// blockedByLineRegex matches "**Blocked by:** value"
var blockedByLineRegex = regexp.MustCompile(`^\*\*Blocked by:\*\*\s*(.+)$`)

// checkboxRegex matches "- [ ] text" or "- [x] text"
var checkboxRegex = regexp.MustCompile(`^-\s+\[([ xX])\]\s+(.+)$`)

// projectHeadingRegex matches "# PRD: Name" or "# Name"
var projectHeadingRegex = regexp.MustCompile(`^#\s+(?:PRD:\s+)?(.+)$`)

// ParseMarkdownPRD reads and parses a PRD markdown file from the given path.
func ParseMarkdownPRD(path string) (*PRD, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read PRD file: %w", err)
	}

	return ParseMarkdownPRDFromString(string(data))
}

// ParseMarkdownPRDFromString parses a PRD from a markdown string.
func ParseMarkdownPRDFromString(content string) (*PRD, error) {
	lines := strings.Split(content, "\n")
	p := &PRD{}

	type storyBuilder struct {
		story     UserStory
		descLines []string
	}

	var current *storyBuilder
	introStarted := false
	introDone := false
	autoPriority := float64(0)

	flushStory := func() {
		if current == nil {
			return
		}
		// If no explicit Description, join collected prose lines
		if current.story.Description == "" && len(current.descLines) > 0 {
			current.story.Description = strings.Join(current.descLines, " ")
		}
		// Assign auto-priority if none was set
		if current.story.Priority == 0 {
			autoPriority++
			current.story.Priority = autoPriority
		} else if current.story.Priority > autoPriority {
			autoPriority = current.story.Priority
		}
		p.UserStories = append(p.UserStories, current.story)
		current = nil
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check for project heading (# level only, not ## or ###)
		if strings.HasPrefix(line, "# ") && !strings.HasPrefix(line, "## ") {
			if m := projectHeadingRegex.FindStringSubmatch(trimmed); m != nil {
				p.Project = strings.TrimSpace(m[1])
				introStarted = true
				continue
			}
		}

		// Check for story heading (### ID: Title)
		if m := storyHeadingRegex.FindStringSubmatch(trimmed); m != nil {
			flushStory()
			introDone = true
			current = &storyBuilder{
				story: UserStory{
					ID:    m[1],
					Title: strings.TrimSpace(m[2]),
				},
			}
			continue
		}

		// Check for ## or ### heading (section boundary — ends current story block)
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ") {
			flushStory()

			heading := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			if strings.EqualFold(heading, "Introduction") || strings.EqualFold(heading, "Overview") {
				introStarted = true
				introDone = false
			} else {
				introDone = true
			}
			continue
		}

		// Inside a story block
		if current != nil {
			// **Status:** line
			if m := statusLineRegex.FindStringSubmatch(trimmed); m != nil {
				status := strings.TrimSpace(strings.ToLower(m[1]))
				switch status {
				case "done", "complete", "completed", "passed":
					current.story.Passes = true
					current.story.InProgress = false
					current.story.NeedsReview = false
				case "in-progress", "in progress", "started":
					current.story.InProgress = true
					current.story.Passes = false
					current.story.NeedsReview = false
				case "needs-review", "needs review", "blocked":
					current.story.NeedsReview = true
					current.story.Passes = false
					current.story.InProgress = false
				default:
					current.story.Passes = false
					current.story.InProgress = false
					current.story.NeedsReview = false
				}
				continue
			}

			// **Priority:** line
			if m := priorityLineRegex.FindStringSubmatch(trimmed); m != nil {
				val := strings.TrimSpace(m[1])
				var pri float64
				if _, err := fmt.Sscanf(val, "%g", &pri); err == nil && pri > 0 {
					current.story.Priority = pri
				}
				continue
			}

			// **Description:** line
			if m := descriptionLineRegex.FindStringSubmatch(trimmed); m != nil {
				current.story.Description = strings.TrimSpace(m[1])
				continue
			}

			// **Blocked by:** line — explicit dependency edges.
			if m := blockedByLineRegex.FindStringSubmatch(trimmed); m != nil {
				current.story.BlockedBy = parseBlockedBy(m[1])
				continue
			}

			// Checkbox items → acceptance criteria
			if m := checkboxRegex.FindStringSubmatch(trimmed); m != nil {
				current.story.AcceptanceCriteria = append(current.story.AcceptanceCriteria, strings.TrimSpace(m[2]))
				continue
			}

			// Collect prose lines as implicit description (only if no explicit **Description:** yet)
			if trimmed != "" && current.story.Description == "" &&
				!strings.HasPrefix(trimmed, "**") &&
				!strings.HasPrefix(trimmed, "- ") {
				current.descLines = append(current.descLines, trimmed)
			}
			continue
		}

		// Collect introduction paragraph as project description
		if introStarted && !introDone && p.Description == "" {
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				p.Description = trimmed
			}
		}
	}

	// Flush the last story
	flushStory()

	return p, nil
}

// parseBlockedBy parses the value of a "**Blocked by:**" line into a list of
// story IDs. It splits on commas and keeps tokens that look like an ID (contain
// a hyphen), mirroring the tolerance used elsewhere in the parser. An empty
// value or a "None"-style value ("None", "none", "None — no deps", ...) yields
// nil so unblocked stories carry no blockers.
func parseBlockedBy(val string) []string {
	val = strings.TrimSpace(val)
	if val == "" {
		return nil
	}
	lower := strings.ToLower(val)
	if lower == "none" ||
		strings.HasPrefix(lower, "none ") ||
		strings.HasPrefix(lower, "none-") ||
		strings.HasPrefix(lower, "none—") {
		return nil
	}

	var ids []string
	for _, tok := range strings.Split(val, ",") {
		tok = strings.TrimSpace(tok)
		if tok != "" && strings.Contains(tok, "-") {
			ids = append(ids, tok)
		}
	}
	return ids
}
