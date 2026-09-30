package prd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SetStoryStatus performs a surgical update of a story's status in a prd.md file.
// It finds the story block by its heading, updates or inserts the **Status:** line,
// and when status is "done", flips all unchecked checkboxes to checked.
func SetStoryStatus(path, storyID, status string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read PRD file: %w", err)
	}

	result, err := setStoryStatusInString(string(data), storyID, status)
	if err != nil {
		return err
	}

	return writeFileAtomic(path, []byte(result))
}

// writeFileAtomic writes data to path by writing a temp file in the same
// directory and renaming it into place. prd.md is the source of truth for all
// story state; a crash mid-write must never truncate it.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close() // already failing; the deferred Remove is the real cleanup
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close() // already failing; the deferred Remove is the real cleanup
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// CreateTemp makes the file 0600; match the previous 0644.
	if err := os.Chmod(tmpName, 0644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// SetStoryBlocked marks a story blocked in a prd.md file and writes why into it,
// as a "**Blockiert (Ben):** reason" line under its status. A block left from an
// earlier time the story was blocked is replaced, not added to.
func SetStoryBlocked(path, storyID, reason string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read PRD file: %w", err)
	}

	result, err := setStoryBlockedInString(string(data), storyID, reason)
	if err != nil {
		return err
	}

	return writeFileAtomic(path, []byte(result))
}

// blockedReasonLine is the line SetStoryBlocked writes. The reason is folded
// onto one line: the parser reads the block as a single line, and an agent's
// explanation arrives with whatever line breaks it happened to write.
func blockedReasonLine(reason string) string {
	reason = strings.Join(strings.Fields(reason), " ")
	if reason == "" {
		reason = "no reason given"
	}
	return "**Blockiert (Ben):** " + reason
}

// storyBounds finds a story's block: the index of its heading line and the index
// of the first line after it (the next ##/###/#### heading, or len(lines)).
func storyBounds(lines []string, storyID string) (start, end int, err error) {
	start, end = -1, len(lines)
	for i, line := range lines {
		if start == -1 {
			// Looking for the story heading. Reuse the package-level parser regex
			// (compiled once) and compare its captured ID to the target, instead
			// of compiling a QuoteMeta'd regex on every call. Matching the exact
			// same heading pattern the parser uses keeps the two in lockstep.
			if m := storyHeadingRegex.FindStringSubmatch(strings.TrimSpace(line)); m != nil && m[1] == storyID {
				start = i
			}
		} else {
			// Looking for the end of the story block (next ## or ### heading)
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "### ") || strings.HasPrefix(trimmed, "#### ") {
				end = i
				break
			}
		}
	}
	if start == -1 {
		return 0, 0, fmt.Errorf("story %s not found in PRD", storyID)
	}
	return start, end, nil
}

// setStoryStatusInString performs the status update on a string and returns the modified string.
// Any status but "blocked" also removes the story's "**Blockiert (Ben):**" block:
// whatever it said no longer applies.
func setStoryStatusInString(content, storyID, status string) (string, error) {
	lines := strings.Split(content, "\n")

	storyStart, storyEnd, err := storyBounds(lines, storyID)
	if err != nil {
		return "", err
	}

	// Process the story block
	statusLineIdx := -1
	statusLine := fmt.Sprintf("**Status:** %s", status)

	for i := storyStart + 1; i < storyEnd; i++ {
		if statusLineRegex.MatchString(strings.TrimSpace(lines[i])) {
			statusLineIdx = i
			break
		}
	}

	if statusLineIdx >= 0 {
		// Replace existing status line
		lines[statusLineIdx] = statusLine
	} else {
		// Insert status line as first line after heading
		newLines := make([]string, 0, len(lines)+1)
		newLines = append(newLines, lines[:storyStart+1]...)
		newLines = append(newLines, statusLine)
		newLines = append(newLines, lines[storyStart+1:]...)
		lines = newLines
		storyEnd++ // adjust for the inserted line
	}

	// When status is "done", flip all unchecked checkboxes to checked
	if status == "done" {
		for i := storyStart + 1; i < storyEnd; i++ {
			lines[i] = strings.Replace(lines[i], "- [ ]", "- [x]", 1)
		}
	}

	if status != "blocked" {
		lines = removeBlockedReason(lines, storyStart, storyEnd)
	}

	return strings.Join(lines, "\n"), nil
}

// setStoryBlockedInString sets the story's status to blocked and puts the reason
// directly under the status line, replacing any reason already there.
func setStoryBlockedInString(content, storyID, reason string) (string, error) {
	content, err := setStoryStatusInString(content, storyID, "blocked")
	if err != nil {
		return "", err
	}
	lines := strings.Split(content, "\n")
	storyStart, storyEnd, err := storyBounds(lines, storyID)
	if err != nil {
		return "", err
	}
	lines = removeBlockedReason(lines, storyStart, storyEnd)

	// The status line exists: setStoryStatusInString just wrote it.
	statusLineIdx := storyStart + 1
	for i := storyStart + 1; i < storyEnd; i++ {
		if statusLineRegex.MatchString(strings.TrimSpace(lines[i])) {
			statusLineIdx = i
			break
		}
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:statusLineIdx+1]...)
	out = append(out, blockedReasonLine(reason))
	out = append(out, lines[statusLineIdx+1:]...)
	return strings.Join(out, "\n"), nil
}

// removeBlockedReason drops every "**Blockiert (Ben):**" line between start and
// end (a story's bounds) and returns the lines that are left.
func removeBlockedReason(lines []string, start, end int) []string {
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		if i > start && i < end && blockedReasonLineRegex.MatchString(strings.TrimSpace(line)) {
			continue
		}
		out = append(out, line)
	}
	return out
}
