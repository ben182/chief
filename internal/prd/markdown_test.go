package prd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMarkdownPRDFromString_Normal(t *testing.T) {
	md := `# PRD: My Test Project

A sample project for testing.

## User Stories

### US-001: Setup Project
As a developer, I need a properly structured project.

**Priority:** 1
**Status:** done

- [x] Create project structure
- [x] Add dependencies

### US-002: Add Feature
**Description:** As a user, I want a new feature.

**Status:** in-progress

- [ ] Feature works correctly
- [ ] Tests pass

### US-003: Final Polish
Some prose description here.

- [ ] Polish the UI
- [ ] Write docs
`
	p, err := ParseMarkdownPRDFromString(md)
	if err != nil {
		t.Fatalf("ParseMarkdownPRDFromString() error = %v", err)
	}

	if p.Project != "My Test Project" {
		t.Errorf("Project = %q, want %q", p.Project, "My Test Project")
	}
	if p.Description != "A sample project for testing." {
		t.Errorf("Description = %q, want %q", p.Description, "A sample project for testing.")
	}
	if len(p.UserStories) != 3 {
		t.Fatalf("len(UserStories) = %d, want 3", len(p.UserStories))
	}

	// Story 1: done
	s1 := p.UserStories[0]
	if s1.ID != "US-001" {
		t.Errorf("s1.ID = %q, want %q", s1.ID, "US-001")
	}
	if s1.Title != "Setup Project" {
		t.Errorf("s1.Title = %q, want %q", s1.Title, "Setup Project")
	}
	if !s1.Passes {
		t.Error("s1.Passes = false, want true")
	}
	if s1.InProgress {
		t.Error("s1.InProgress = true, want false")
	}
	if s1.Priority != 1 {
		t.Errorf("s1.Priority = %g, want 1", s1.Priority)
	}
	if len(s1.AcceptanceCriteria) != 2 {
		t.Errorf("len(s1.AcceptanceCriteria) = %d, want 2", len(s1.AcceptanceCriteria))
	}

	// Story 2: in-progress
	s2 := p.UserStories[1]
	if s2.ID != "US-002" {
		t.Errorf("s2.ID = %q, want %q", s2.ID, "US-002")
	}
	if !s2.InProgress {
		t.Error("s2.InProgress = false, want true")
	}
	if s2.Passes {
		t.Error("s2.Passes = true, want false")
	}
	if s2.Description != "As a user, I want a new feature." {
		t.Errorf("s2.Description = %q, want %q", s2.Description, "As a user, I want a new feature.")
	}

	// Story 3: pending (no status)
	s3 := p.UserStories[2]
	if s3.ID != "US-003" {
		t.Errorf("s3.ID = %q, want %q", s3.ID, "US-003")
	}
	if s3.Passes || s3.InProgress {
		t.Error("s3 should be pending (both false)")
	}
	if s3.Description != "Some prose description here." {
		t.Errorf("s3.Description = %q, want %q", s3.Description, "Some prose description here.")
	}
	if len(s3.AcceptanceCriteria) != 2 {
		t.Errorf("len(s3.AcceptanceCriteria) = %d, want 2", len(s3.AcceptanceCriteria))
	}
}

func TestParseMarkdownPRDFromString_ProjectWithoutPRDPrefix(t *testing.T) {
	md := `# My Project

Overview text.

### FEAT-001: First Feature
- [ ] It works
`
	p, err := ParseMarkdownPRDFromString(md)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if p.Project != "My Project" {
		t.Errorf("Project = %q, want %q", p.Project, "My Project")
	}
	if len(p.UserStories) != 1 {
		t.Fatalf("len(UserStories) = %d, want 1", len(p.UserStories))
	}
	if p.UserStories[0].ID != "FEAT-001" {
		t.Errorf("ID = %q, want %q", p.UserStories[0].ID, "FEAT-001")
	}
}

func TestParseMarkdownPRDFromString_MissingFields(t *testing.T) {
	md := `# Minimal

### US-001: Only Title
`
	p, err := ParseMarkdownPRDFromString(md)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(p.UserStories) != 1 {
		t.Fatalf("len(UserStories) = %d, want 1", len(p.UserStories))
	}
	s := p.UserStories[0]
	if s.ID != "US-001" {
		t.Errorf("ID = %q", s.ID)
	}
	if s.Priority != 1 {
		t.Errorf("Priority = %g, want 1 (auto-assigned)", s.Priority)
	}
	if s.Passes || s.InProgress {
		t.Error("should be pending")
	}
}

func TestParseMarkdownPRDFromString_PhaseHeadingsIgnored(t *testing.T) {
	md := `# My Project

## Phase 1: Setup

### US-001: Do Setup
- [ ] Setup done

## Phase 2: Build

### US-002: Do Build
- [ ] Build done
`
	p, err := ParseMarkdownPRDFromString(md)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(p.UserStories) != 2 {
		t.Fatalf("len(UserStories) = %d, want 2", len(p.UserStories))
	}
	if p.UserStories[0].ID != "US-001" {
		t.Errorf("first story ID = %q", p.UserStories[0].ID)
	}
	if p.UserStories[1].ID != "US-002" {
		t.Errorf("second story ID = %q", p.UserStories[1].ID)
	}
}

func TestParseMarkdownPRDFromString_IntroductionSection(t *testing.T) {
	md := `# PRD: Test

## Introduction

This is the introduction paragraph.

## Stories

### US-001: First
- [ ] Done
`
	p, err := ParseMarkdownPRDFromString(md)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if p.Description != "This is the introduction paragraph." {
		t.Errorf("Description = %q, want %q", p.Description, "This is the introduction paragraph.")
	}
}

func TestParseMarkdownPRDFromString_FreeSections(t *testing.T) {
	md := `# Test Project

Overview.

## Background

Some background text.

## User Stories

### US-001: First
- [ ] Criterion A

## Appendix

Extra info here.
`
	p, err := ParseMarkdownPRDFromString(md)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(p.UserStories) != 1 {
		t.Fatalf("len(UserStories) = %d, want 1", len(p.UserStories))
	}
	if p.UserStories[0].ID != "US-001" {
		t.Errorf("ID = %q", p.UserStories[0].ID)
	}
}

func TestParseMarkdownPRDFromString_StatusMapping(t *testing.T) {
	tests := []struct {
		status     string
		wantPasses bool
		wantIP     bool
	}{
		{"done", true, false},
		{"complete", true, false},
		{"completed", true, false},
		{"passed", true, false},
		{"in-progress", false, true},
		{"in progress", false, true},
		{"started", false, true},
		{"todo", false, false},
		{"pending", false, false},
		{"", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			md := "# P\n\n### US-001: S\n**Status:** " + tt.status + "\n"
			p, err := ParseMarkdownPRDFromString(md)
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if len(p.UserStories) != 1 {
				t.Fatalf("len(UserStories) = %d", len(p.UserStories))
			}
			if p.UserStories[0].Passes != tt.wantPasses {
				t.Errorf("Passes = %v, want %v", p.UserStories[0].Passes, tt.wantPasses)
			}
			if p.UserStories[0].InProgress != tt.wantIP {
				t.Errorf("InProgress = %v, want %v", p.UserStories[0].InProgress, tt.wantIP)
			}
		})
	}
}

func TestParseMarkdownPRD_File(t *testing.T) {
	tmpDir := t.TempDir()
	prdPath := filepath.Join(tmpDir, "prd.md")

	md := `# Test

### US-001: First Story
- [ ] Works
`
	if err := os.WriteFile(prdPath, []byte(md), 0644); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	p, err := ParseMarkdownPRD(prdPath)
	if err != nil {
		t.Fatalf("ParseMarkdownPRD() error = %v", err)
	}
	if p.Project != "Test" {
		t.Errorf("Project = %q", p.Project)
	}
	if len(p.UserStories) != 1 {
		t.Fatalf("len(UserStories) = %d", len(p.UserStories))
	}
}

func TestParseMarkdownPRD_FileNotFound(t *testing.T) {
	_, err := ParseMarkdownPRD("/nonexistent/prd.md")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestParseMarkdownPRDFromString_H4StoryHeadings(t *testing.T) {
	md := `# PRD: Phased Project

## Phase 1: Foundation

### Design System

#### US-001: Setup Theme
**Priority:** 1
- [ ] Theme configured
- [ ] Tokens defined

#### US-002: Build Components
- [ ] Components built

## Phase 2: Features

### Core Features

#### US-003: Add Feature
**Status:** done
- [x] Feature works
`
	p, err := ParseMarkdownPRDFromString(md)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if p.Project != "Phased Project" {
		t.Errorf("Project = %q, want %q", p.Project, "Phased Project")
	}
	if len(p.UserStories) != 3 {
		t.Fatalf("len(UserStories) = %d, want 3", len(p.UserStories))
	}
	if p.UserStories[0].ID != "US-001" {
		t.Errorf("s1.ID = %q", p.UserStories[0].ID)
	}
	if len(p.UserStories[0].AcceptanceCriteria) != 2 {
		t.Errorf("s1 AC count = %d, want 2", len(p.UserStories[0].AcceptanceCriteria))
	}
	if p.UserStories[2].ID != "US-003" {
		t.Errorf("s3.ID = %q", p.UserStories[2].ID)
	}
	if !p.UserStories[2].Passes {
		t.Error("s3 should be done")
	}
}

func TestParseMarkdownPRDFromString_AutoPriority(t *testing.T) {
	md := `# P

### US-001: First
- [ ] A

### US-002: Second
**Priority:** 5
- [ ] B

### US-003: Third
- [ ] C
`
	p, err := ParseMarkdownPRDFromString(md)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(p.UserStories) != 3 {
		t.Fatalf("len = %d", len(p.UserStories))
	}
	// First: auto-priority 1
	if p.UserStories[0].Priority != 1 {
		t.Errorf("s1.Priority = %g, want 1", p.UserStories[0].Priority)
	}
	// Second: explicit priority 5
	if p.UserStories[1].Priority != 5 {
		t.Errorf("s2.Priority = %g, want 5", p.UserStories[1].Priority)
	}
	// Third: auto-priority 6 (after 5)
	if p.UserStories[2].Priority != 6 {
		t.Errorf("s3.Priority = %g, want 6", p.UserStories[2].Priority)
	}
}

func TestParseMarkdownPRDFromString_BlockedBy(t *testing.T) {
	md := `# P

### US-001: First
**Priority:** 1
- [ ] A

### US-002: Second
**Priority:** 2
**Blocked by:** US-001
- [ ] B

### US-003: Third
**Priority:** 3
**Blocked by:** US-001, US-002
This is prose that should become the description.
- [ ] C
`
	p, err := ParseMarkdownPRDFromString(md)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(p.UserStories) != 3 {
		t.Fatalf("len = %d, want 3", len(p.UserStories))
	}

	// US-001 has no blockers.
	if len(p.UserStories[0].BlockedBy) != 0 {
		t.Errorf("US-001 BlockedBy = %v, want empty", p.UserStories[0].BlockedBy)
	}

	// US-002 blocked by a single story.
	if got := p.UserStories[1].BlockedBy; len(got) != 1 || got[0] != "US-001" {
		t.Errorf("US-002 BlockedBy = %v, want [US-001]", got)
	}

	// US-003 blocked by two stories, comma-separated.
	if got := p.UserStories[2].BlockedBy; len(got) != 2 || got[0] != "US-001" || got[1] != "US-002" {
		t.Errorf("US-003 BlockedBy = %v, want [US-001 US-002]", got)
	}

	// The Blocked-by line must not leak into the description.
	if desc := p.UserStories[2].Description; desc != "This is prose that should become the description." {
		t.Errorf("US-003 Description = %q, blocked-by line should not be captured as prose", desc)
	}
}

func TestParseMarkdownPRDFromString_BlockedByNone(t *testing.T) {
	cases := map[string]string{
		"absent":      "### US-001: S\n**Priority:** 1\n- [ ] A\n",
		"none":        "### US-001: S\n**Blocked by:** None\n- [ ] A\n",
		"lower none":  "### US-001: S\n**Blocked by:** none\n- [ ] A\n",
		"none dash":   "### US-001: S\n**Blocked by:** None - nothing blocks this\n- [ ] A\n",
		"none emdash": "### US-001: S\n**Blocked by:** None — nothing blocks this\n- [ ] A\n",
		"empty":       "### US-001: S\n**Blocked by:**\n- [ ] A\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			p, err := ParseMarkdownPRDFromString("# P\n\n" + body)
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if len(p.UserStories) != 1 {
				t.Fatalf("len = %d, want 1", len(p.UserStories))
			}
			if got := p.UserStories[0].BlockedBy; len(got) != 0 {
				t.Errorf("BlockedBy = %v, want empty", got)
			}
		})
	}
}

func TestParseMarkdownPRDFromString_FloatPriority(t *testing.T) {
	md := `# P

### US-001: First
**Priority:** 0.1
- [ ] A

### US-002: Second
**Priority:** 1.5
- [ ] B

### US-003: Third
**Priority:** 2
- [ ] C
`
	p, err := ParseMarkdownPRDFromString(md)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(p.UserStories) != 3 {
		t.Fatalf("len = %d", len(p.UserStories))
	}
	if p.UserStories[0].Priority != 0.1 {
		t.Errorf("s1.Priority = %g, want 0.1", p.UserStories[0].Priority)
	}
	if p.UserStories[1].Priority != 1.5 {
		t.Errorf("s2.Priority = %g, want 1.5", p.UserStories[1].Priority)
	}
	if p.UserStories[2].Priority != 2 {
		t.Errorf("s3.Priority = %g, want 2", p.UserStories[2].Priority)
	}
}
