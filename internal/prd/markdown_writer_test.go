package prd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetStoryStatusInString_ExistingStatusLine(t *testing.T) {
	md := `# P

### US-001: First
**Status:** todo
- [ ] A
- [ ] B

### US-002: Second
- [ ] C
`
	result, err := setStoryStatusInString(md, "US-001", "done")
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	if !strings.Contains(result, "**Status:** done") {
		t.Error("expected **Status:** done in result")
	}
	// Should not contain the old status
	if strings.Contains(result, "**Status:** todo") {
		t.Error("old status should be replaced")
	}
	// Checkboxes should be flipped to checked
	if strings.Contains(result, "- [ ] A") {
		t.Error("expected checkbox A to be checked")
	}
	if !strings.Contains(result, "- [x] A") {
		t.Error("expected checkbox A to be [x]")
	}
	// US-002 should be untouched
	if !strings.Contains(result, "- [ ] C") {
		t.Error("US-002 checkboxes should be untouched")
	}
}

func TestSetStoryStatusInString_MissingStatusLine(t *testing.T) {
	md := `# P

### US-001: First
- [ ] A
`
	result, err := setStoryStatusInString(md, "US-001", "in-progress")
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	if !strings.Contains(result, "**Status:** in-progress") {
		t.Error("expected **Status:** in-progress to be inserted")
	}

	// Status line should appear after the heading
	lines := strings.Split(result, "\n")
	for i, line := range lines {
		if strings.Contains(line, "### US-001") {
			if i+1 >= len(lines) || !strings.Contains(lines[i+1], "**Status:** in-progress") {
				t.Error("status line should be directly after heading")
			}
			break
		}
	}
}

func TestSetStoryStatusInString_CheckboxFlipping(t *testing.T) {
	md := `# P

### US-001: First
**Status:** in-progress
- [ ] Unchecked A
- [x] Already checked B
- [ ] Unchecked C
`
	result, err := setStoryStatusInString(md, "US-001", "done")
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	if strings.Contains(result, "- [ ] Unchecked A") {
		t.Error("checkbox A should be checked")
	}
	if !strings.Contains(result, "- [x] Unchecked A") {
		t.Error("expected [x] Unchecked A")
	}
	if !strings.Contains(result, "- [x] Already checked B") {
		t.Error("already checked B should remain checked")
	}
	if !strings.Contains(result, "- [x] Unchecked C") {
		t.Error("checkbox C should be checked")
	}
}

func TestSetStoryStatusInString_MultiStory(t *testing.T) {
	md := `# P

### US-001: First
**Status:** todo
- [ ] A

### US-002: Second
**Status:** todo
- [ ] B

### US-003: Third
- [ ] C
`
	// Mark US-002 as done
	result, err := setStoryStatusInString(md, "US-002", "done")
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	// US-001 should be unchanged
	if !strings.Contains(result, "- [ ] A") {
		t.Error("US-001 checkboxes should be untouched")
	}
	// US-003 should be unchanged
	if !strings.Contains(result, "- [ ] C") {
		t.Error("US-003 checkboxes should be untouched")
	}
	// US-002 should be done with checked boxes
	if !strings.Contains(result, "- [x] B") {
		t.Error("US-002 checkbox should be checked")
	}
}

func TestSetStoryStatusInString_StoryNotFound(t *testing.T) {
	md := `# P

### US-001: First
- [ ] A
`
	_, err := setStoryStatusInString(md, "US-999", "done")
	if err == nil {
		t.Error("expected error for missing story")
	}
}

func TestSetStoryStatus_File(t *testing.T) {
	tmpDir := t.TempDir()
	prdPath := filepath.Join(tmpDir, "prd.md")

	md := `# P

### US-001: First
- [ ] A
`
	if err := os.WriteFile(prdPath, []byte(md), 0644); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	if err := SetStoryStatus(prdPath, "US-001", "done"); err != nil {
		t.Fatalf("SetStoryStatus() error = %v", err)
	}

	data, err := os.ReadFile(prdPath)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	result := string(data)
	if !strings.Contains(result, "**Status:** done") {
		t.Error("expected **Status:** done in file")
	}
	if !strings.Contains(result, "- [x] A") {
		t.Error("expected checkbox to be checked")
	}
}

func TestSetStoryStatusInString_H4Headings(t *testing.T) {
	md := `# P

## Phase 1

#### US-001: First
- [ ] A

#### US-002: Second
- [ ] B
`
	result, err := setStoryStatusInString(md, "US-001", "done")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(result, "**Status:** done") {
		t.Error("expected **Status:** done")
	}
	if !strings.Contains(result, "- [x] A") {
		t.Error("expected checkbox A to be checked")
	}
	// US-002 should be untouched
	if !strings.Contains(result, "- [ ] B") {
		t.Error("US-002 should be untouched")
	}
}

func TestSetStoryStatusInString_NoCheckboxFlipForNonDone(t *testing.T) {
	md := `# P

### US-001: First
- [ ] A
`
	result, err := setStoryStatusInString(md, "US-001", "in-progress")
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	// Checkboxes should NOT be flipped for in-progress
	if !strings.Contains(result, "- [ ] A") {
		t.Error("checkboxes should not be flipped for non-done status")
	}
}

func TestSetStoryStatusInString_RoundTrip(t *testing.T) {
	md := `# My Project

A description.

### US-001: First
**Status:** todo
- [ ] A
- [ ] B

### US-002: Second
- [ ] C
`
	// Set US-001 to in-progress
	result, err := setStoryStatusInString(md, "US-001", "in-progress")
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	// Parse and verify
	p, err := ParseMarkdownPRDFromString(result)
	if err != nil {
		t.Fatalf("parse error = %v", err)
	}
	if !p.UserStories[0].InProgress {
		t.Error("US-001 should be in-progress")
	}
	if p.UserStories[0].Passes {
		t.Error("US-001 should not be passes")
	}

	// Now set US-001 to done
	result, err = setStoryStatusInString(result, "US-001", "done")
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	// Parse and verify
	p, err = ParseMarkdownPRDFromString(result)
	if err != nil {
		t.Fatalf("parse error = %v", err)
	}
	if !p.UserStories[0].Passes {
		t.Error("US-001 should be passes")
	}
	if p.UserStories[0].InProgress {
		t.Error("US-001 should not be in-progress")
	}
}

// TestSetStoryStatusInString_PreservesBlockedBy verifies that a surgical status
// update (the only markdown writer in the package) leaves a story's
// **Blocked by:** line untouched, so BlockedBy survives a parse→write→parse
// round-trip. A story without a Blocked-by line must never gain one.
func TestSetStoryStatusInString_PreservesBlockedBy(t *testing.T) {
	md := `# P

### US-001: First
**Priority:** 1
- [ ] A

### US-002: Second
**Priority:** 2
**Blocked by:** US-001
- [ ] B
`
	// Parse → confirm the blocker is captured.
	p, err := ParseMarkdownPRDFromString(md)
	if err != nil {
		t.Fatalf("parse error = %v", err)
	}
	if got := p.UserStories[1].BlockedBy; len(got) != 1 || got[0] != "US-001" {
		t.Fatalf("US-002 BlockedBy = %v, want [US-001]", got)
	}

	// Write (surgical status update) → the Blocked-by line must remain verbatim.
	result, err := setStoryStatusInString(md, "US-001", "done")
	if err != nil {
		t.Fatalf("write error = %v", err)
	}
	if !strings.Contains(result, "**Blocked by:** US-001") {
		t.Error("**Blocked by:** line should be preserved by the surgical writer")
	}

	// Parse again → BlockedBy round-trips unchanged.
	p2, err := ParseMarkdownPRDFromString(result)
	if err != nil {
		t.Fatalf("re-parse error = %v", err)
	}
	if got := p2.UserStories[1].BlockedBy; len(got) != 1 || got[0] != "US-001" {
		t.Errorf("after round-trip US-002 BlockedBy = %v, want [US-001]", got)
	}
	// US-001 had no blockers and must not have gained a Blocked-by line.
	if len(p2.UserStories[0].BlockedBy) != 0 {
		t.Errorf("US-001 BlockedBy = %v, want empty", p2.UserStories[0].BlockedBy)
	}
	if strings.Contains(result, "### US-001: First\n**Status:** done\n**Blocked by:") {
		t.Error("US-001 should not have gained a Blocked-by line")
	}
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prd.md")

	// Overwrite an existing file.
	if err := os.WriteFile(path, []byte("old content"), 0644); err != nil {
		t.Fatal(err)
	}
	want := "new content\nsecond line\n"
	if err := writeFileAtomic(path, []byte(want)); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("content = %q, want %q", got, want)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0644 {
		t.Errorf("perm = %v, want 0644", info.Mode().Perm())
	}

	// No temp files must be left behind in the directory.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}
