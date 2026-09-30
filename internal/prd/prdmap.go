package prd

import (
	"os"
	"strings"
)

// PRDSectionRange is one stretch of a PRD that is not a story, under the "## "
// heading it sits in: conventions written above the first story, technical
// considerations, testing decisions, design.
type PRDSectionRange struct {
	// Heading is the section's "## " heading, or "" for what comes before the
	// first one.
	Heading string
	// BeforeStories is true for the part of a section that precedes stories in
	// the same section — typically the conventions that apply to all of them.
	BeforeStories bool
	LineRange
}

// MapPRDSections lists where a PRD says things that are not a story. A story
// agent is handed only its own story, so without this it never sees rules the
// PRD states once for every story. Blank stretches are left out. ok is false
// when there is no file to map.
func MapPRDSections(path string) (sections []PRDSectionRange, ok bool) {
	data, err := os.ReadFile(path) //nolint:gosec // the PRD chief is running
	if err != nil {
		return nil, false
	}
	lines := strings.Split(string(data), "\n")

	heading := ""
	inStory := false
	sawStory := false
	var run *PRDSectionRange
	flush := func() {
		if run != nil && run.Bytes > 0 {
			sections = append(sections, *run)
		}
		run = nil
	}

	for i, line := range lines {
		n := i + 1
		trimmed := strings.TrimSpace(line)
		switch {
		case storyHeadingRegex.MatchString(trimmed):
			flush()
			inStory, sawStory = true, true
			continue
		case strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "# "):
			flush()
			if strings.HasPrefix(line, "## ") {
				heading = trimmed
			}
			inStory, sawStory = false, false
		case strings.HasPrefix(line, "### "):
			// A non-story sub-heading ends the story above it.
			inStory = false
		}
		if inStory {
			continue
		}
		if run == nil {
			run = &PRDSectionRange{Heading: heading, LineRange: LineRange{Start: n}}
		}
		run.End = n
		// Count only lines with content, so a stretch of blank lines between
		// stories is not reported as a section.
		if trimmed != "" && !strings.HasPrefix(line, "#") {
			run.Bytes += len(line) + 1
		}
		if !sawStory {
			// Stories later in this section make this the part before them.
			run.BeforeStories = true
		}
	}
	flush()

	// A section with no stories at all is not "before" anything.
	for i := range sections {
		if sections[i].BeforeStories && !sectionHasStory(lines, sections[i]) {
			sections[i].BeforeStories = false
		}
	}
	return sections, true
}

// sectionHasStory reports whether a story heading follows r inside r's section.
func sectionHasStory(lines []string, r PRDSectionRange) bool {
	for _, line := range lines[r.End:] {
		trimmed := strings.TrimSpace(line)
		if storyHeadingRegex.MatchString(trimmed) {
			return true
		}
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "# ") {
			return false
		}
	}
	return false
}
