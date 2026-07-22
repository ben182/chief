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
	defer os.Remove(tmpName) // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
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

// setStoryStatusInString performs the status update on a string and returns the modified string.
func setStoryStatusInString(content, storyID, status string) (string, error) {
	lines := strings.Split(content, "\n")

	// Find the story block
	storyStart := -1
	storyEnd := len(lines) // default to end of file

	for i, line := range lines {
		if storyStart == -1 {
			// Looking for the story heading. Reuse the package-level parser regex
			// (compiled once) and compare its captured ID to the target, instead
			// of compiling a QuoteMeta'd regex on every call. Matching the exact
			// same heading pattern the parser uses keeps the two in lockstep.
			if m := storyHeadingRegex.FindStringSubmatch(strings.TrimSpace(line)); m != nil && m[1] == storyID {
				storyStart = i
			}
		} else {
			// Looking for the end of the story block (next ## or ### heading)
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "### ") || strings.HasPrefix(trimmed, "#### ") {
				storyEnd = i
				break
			}
		}
	}

	if storyStart == -1 {
		return "", fmt.Errorf("story %s not found in PRD", storyID)
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

	return strings.Join(lines, "\n"), nil
}
