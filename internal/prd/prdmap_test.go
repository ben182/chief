package prd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mappedPRD = `# PRD: Demo

## 1. Introduction

A demo.

## 3. User Stories

Conventions for all stories:
- never run the budget test

### US-001: First
**Status:** todo
- [ ] one

### US-002: Second
- [ ] two

## 7. Technical Considerations

Use the one HTTP client.
`

// The build agent sees only its own story; rules the PRD states once for all
// of them — above the first story, or in their own section — have to be found.
func TestMapPRDSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prd.md")
	if err := os.WriteFile(path, []byte(mappedPRD), 0o600); err != nil {
		t.Fatal(err)
	}
	got, ok := MapPRDSections(path)
	if !ok {
		t.Fatal("expected a map")
	}
	want := []PRDSectionRange{
		{Heading: "## 1. Introduction", LineRange: LineRange{Start: 3, End: 6}},
		{Heading: "## 3. User Stories", BeforeStories: true, LineRange: LineRange{Start: 7, End: 11}},
		{Heading: "## 7. Technical Considerations", LineRange: LineRange{Start: 19, End: 22}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d sections, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Heading != w.Heading || g.BeforeStories != w.BeforeStories || g.Start != w.Start || g.End != w.End {
			t.Errorf("section %d = %+v, want %+v", i, g, w)
		}
	}
	if _, ok := MapPRDSections(filepath.Join(t.TempDir(), "missing.md")); ok {
		t.Error("a missing PRD must not be mapped")
	}
}

// recap's stories carried thresholds as sub-bullets and "manual" blocks under
// bold sub-headings; the agent got the checkbox lines only, so RCP-028 was
// built without its thresholds. The agent now gets the story as written.
func TestStoryContextIsTheStoryAsWritten(t *testing.T) {
	p, err := ParseMarkdownPRDFromString(`# PRD: Demo

## User Stories

### US-001: Match people
**Status:** in-progress
**Priority:** 1

**Acceptance Criteria:**
- [ ] **Thresholds** for the cosine:
  - ≥ 0.80: automatic
  - 0.65–0.80: suggestion

**Manual (Ben):**
- [ ] a real call is recognised
`)
	if err != nil {
		t.Fatal(err)
	}
	ctx := *p.StoryContextByID("US-001")
	for _, want := range []string{"### US-001: Match people", "  - ≥ 0.80: automatic", "**Manual (Ben):**", "- [ ] a real call is recognised"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("story context lacks %q:\n%s", want, ctx)
		}
	}
	if strings.Contains(ctx, "**Status:**") {
		t.Errorf("the status line is chief's bookkeeping and stays out:\n%s", ctx)
	}
}
