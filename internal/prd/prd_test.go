package prd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPRD(t *testing.T) {
	// Create a temp file with valid PRD markdown
	tmpDir := t.TempDir()
	prdPath := filepath.Join(tmpDir, "prd.md")

	validMd := `# Test Project

A test PRD

### US-001: First Story
**Description:** Test description

- [ ] AC1
- [ ] AC2
`

	if err := os.WriteFile(prdPath, []byte(validMd), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	p, err := LoadPRD(prdPath)
	if err != nil {
		t.Fatalf("LoadPRD failed: %v", err)
	}

	if p.Project != "Test Project" {
		t.Errorf("expected project 'Test Project', got '%s'", p.Project)
	}
	if p.Description != "A test PRD" {
		t.Errorf("expected description 'A test PRD', got '%s'", p.Description)
	}
	if len(p.UserStories) != 1 {
		t.Errorf("expected 1 user story, got %d", len(p.UserStories))
	}
	if p.UserStories[0].ID != "US-001" {
		t.Errorf("expected story ID 'US-001', got '%s'", p.UserStories[0].ID)
	}
}

func TestLoadPRD_FileNotFound(t *testing.T) {
	_, err := LoadPRD("/nonexistent/path/prd.md")
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}

func TestPRD_AllComplete_EmptyPRD(t *testing.T) {
	p := &PRD{
		Project:     "Empty",
		UserStories: []UserStory{},
	}

	if !p.AllComplete() {
		t.Error("expected AllComplete() to return true for empty PRD")
	}
}

func TestPRD_AllComplete_AllPassing(t *testing.T) {
	p := &PRD{
		Project: "Test",
		UserStories: []UserStory{
			{ID: "US-001", Passes: true},
			{ID: "US-002", Passes: true},
			{ID: "US-003", Passes: true},
		},
	}

	if !p.AllComplete() {
		t.Error("expected AllComplete() to return true when all stories pass")
	}
}

func TestPRD_AllComplete_SomePending(t *testing.T) {
	p := &PRD{
		Project: "Test",
		UserStories: []UserStory{
			{ID: "US-001", Passes: true},
			{ID: "US-002", Passes: false},
			{ID: "US-003", Passes: true},
		},
	}

	if p.AllComplete() {
		t.Error("expected AllComplete() to return false when some stories are pending")
	}
}

func TestPRD_NextStory_EmptyPRD(t *testing.T) {
	p := &PRD{
		Project:     "Empty",
		UserStories: []UserStory{},
	}

	next := p.NextStory()
	if next != nil {
		t.Errorf("expected nil for empty PRD, got %v", next)
	}
}

func TestPRD_NextStory_AllComplete(t *testing.T) {
	p := &PRD{
		Project: "Test",
		UserStories: []UserStory{
			{ID: "US-001", Passes: true},
			{ID: "US-002", Passes: true},
		},
	}

	next := p.NextStory()
	if next != nil {
		t.Errorf("expected nil when all complete, got %v", next)
	}
}

func TestPRD_NextStory_InterruptedStory(t *testing.T) {
	p := &PRD{
		Project: "Test",
		UserStories: []UserStory{
			{ID: "US-001", Priority: 1, Passes: false},
			{ID: "US-002", Priority: 2, Passes: false, InProgress: true},
			{ID: "US-003", Priority: 3, Passes: false},
		},
	}

	next := p.NextStory()
	if next == nil {
		t.Fatal("expected non-nil story")
	}
	if next.ID != "US-002" {
		t.Errorf("expected interrupted story US-002, got %s", next.ID)
	}
}

func TestPRD_NextStory_LowestPriority(t *testing.T) {
	p := &PRD{
		Project: "Test",
		UserStories: []UserStory{
			{ID: "US-001", Priority: 3, Passes: false},
			{ID: "US-002", Priority: 1, Passes: false},
			{ID: "US-003", Priority: 2, Passes: true},
		},
	}

	next := p.NextStory()
	if next == nil {
		t.Fatal("expected non-nil story")
	}
	if next.ID != "US-002" {
		t.Errorf("expected lowest priority story US-002, got %s", next.ID)
	}
}

func TestPRD_NextStory_SkipsCompleted(t *testing.T) {
	p := &PRD{
		Project: "Test",
		UserStories: []UserStory{
			{ID: "US-001", Priority: 1, Passes: true},
			{ID: "US-002", Priority: 2, Passes: false},
			{ID: "US-003", Priority: 3, Passes: false},
		},
	}

	next := p.NextStory()
	if next == nil {
		t.Fatal("expected non-nil story")
	}
	if next.ID != "US-002" {
		t.Errorf("expected US-002 (lowest priority not passing), got %s", next.ID)
	}
}

func TestPRD_NextStory_SkipsNeedsReview(t *testing.T) {
	p := &PRD{
		Project: "Test",
		UserStories: []UserStory{
			{ID: "US-001", Priority: 1, Passes: false, NeedsReview: true},
			{ID: "US-002", Priority: 2, Passes: false},
		},
	}

	next := p.NextStory()
	if next == nil {
		t.Fatal("expected non-nil story")
	}
	if next.ID != "US-002" {
		t.Errorf("expected parked US-001 to be skipped, got %s", next.ID)
	}
}

func TestPRD_AllResolved(t *testing.T) {
	resolved := &PRD{UserStories: []UserStory{
		{ID: "US-001", Passes: true},
		{ID: "US-002", NeedsReview: true},
	}}
	if !resolved.AllResolved() {
		t.Error("expected AllResolved true when every story is done or parked")
	}
	if resolved.AllComplete() {
		t.Error("expected AllComplete false while a story is only parked, not passed")
	}

	unresolved := &PRD{UserStories: []UserStory{
		{ID: "US-001", Passes: true},
		{ID: "US-002", Passes: false},
	}}
	if unresolved.AllResolved() {
		t.Error("expected AllResolved false while a story is still actionable")
	}
}

func TestPRD_NextStory_InterruptedTakesPrecedence(t *testing.T) {
	p := &PRD{
		Project: "Test",
		UserStories: []UserStory{
			{ID: "US-001", Priority: 1, Passes: false},
			{ID: "US-002", Priority: 5, Passes: false, InProgress: true},
		},
	}

	next := p.NextStory()
	if next == nil {
		t.Fatal("expected non-nil story")
	}
	if next.ID != "US-002" {
		t.Errorf("expected in-progress story US-002 to take precedence, got %s", next.ID)
	}
}

func TestPRD_NextStory_BlockedByNotYetPassed(t *testing.T) {
	p := &PRD{
		UserStories: []UserStory{
			{ID: "US-001", Priority: 1, Passes: false},
			{ID: "US-002", Priority: 2, Passes: false, BlockedBy: []string{"US-001"}},
		},
	}

	// US-002 is blocked by the still-unfinished US-001, so US-001 is picked even
	// though both are unpassed.
	next := p.NextStory()
	if next == nil {
		t.Fatal("expected non-nil story")
	}
	if next.ID != "US-001" {
		t.Errorf("expected US-001 (US-002 blocked), got %s", next.ID)
	}

	// Once the blocker passes, US-002 becomes eligible.
	p.UserStories[0].Passes = true
	next = p.NextStory()
	if next == nil {
		t.Fatal("expected non-nil story")
	}
	if next.ID != "US-002" {
		t.Errorf("expected US-002 after blocker passed, got %s", next.ID)
	}
}

func TestPRD_NextStory_BlockedStoryWithLowerPrioritySkipped(t *testing.T) {
	// US-002 has the lower priority number (would normally win) but is blocked by
	// the unpassed US-001, so the eligible US-001 is chosen instead.
	p := &PRD{
		UserStories: []UserStory{
			{ID: "US-001", Priority: 5, Passes: false},
			{ID: "US-002", Priority: 1, Passes: false, BlockedBy: []string{"US-001"}},
		},
	}
	next := p.NextStory()
	if next == nil {
		t.Fatal("expected non-nil story")
	}
	if next.ID != "US-001" {
		t.Errorf("expected US-001 (only eligible), got %s", next.ID)
	}
}

func TestPRD_NextStory_InProgressBeatsBlocking(t *testing.T) {
	// In-progress precedence still wins, even if the in-progress story has
	// unsatisfied blockers (interrupted work must resume).
	p := &PRD{
		UserStories: []UserStory{
			{ID: "US-001", Priority: 1, Passes: false},
			{ID: "US-002", Priority: 2, Passes: false, InProgress: true, BlockedBy: []string{"US-001"}},
		},
	}
	next := p.NextStory()
	if next == nil {
		t.Fatal("expected non-nil story")
	}
	if next.ID != "US-002" {
		t.Errorf("expected in-progress US-002 to take precedence, got %s", next.ID)
	}
}

func TestPRD_NextStory_UnknownBlockerIgnored(t *testing.T) {
	// A typo'd/unknown blocker ID must never deadlock: the story stays eligible.
	p := &PRD{
		UserStories: []UserStory{
			{ID: "US-001", Priority: 1, Passes: false, BlockedBy: []string{"US-999"}},
		},
	}
	next := p.NextStory()
	if next == nil {
		t.Fatal("expected non-nil story (unknown blocker should be ignored)")
	}
	if next.ID != "US-001" {
		t.Errorf("expected US-001, got %s", next.ID)
	}
}

func TestPRD_NextStory_SelfReferenceIgnored(t *testing.T) {
	// A story blocking itself is ignored rather than deadlocking.
	p := &PRD{
		UserStories: []UserStory{
			{ID: "US-001", Priority: 1, Passes: false, BlockedBy: []string{"US-001"}},
		},
	}
	if got := p.Frontier(); len(got) != 1 || got[0].ID != "US-001" {
		t.Errorf("Frontier = %v, want [US-001] (self-reference ignored)", got)
	}
	if next := p.NextStory(); next == nil || next.ID != "US-001" {
		t.Errorf("NextStory should return US-001 despite self-reference")
	}
}

func TestPRD_NextStory_CycleFallsBack(t *testing.T) {
	// A 2-cycle (A blocks B, B blocks A): the frontier is empty, but NextStory
	// must NOT return nil while both are actionable. The fallback returns the
	// lowest-priority one so the loop never hangs.
	p := &PRD{
		UserStories: []UserStory{
			{ID: "US-001", Priority: 2, Passes: false, BlockedBy: []string{"US-002"}},
			{ID: "US-002", Priority: 1, Passes: false, BlockedBy: []string{"US-001"}},
		},
	}
	if got := p.Frontier(); len(got) != 0 {
		t.Errorf("Frontier = %v, want empty for a 2-cycle", got)
	}
	next := p.NextStory()
	if next == nil {
		t.Fatal("expected fallback story for a cycle, got nil")
	}
	if next.ID != "US-002" {
		t.Errorf("expected lowest-priority fallback US-002, got %s", next.ID)
	}
}

func TestPRD_NextStory_AllBlockedByParkedFallsBack(t *testing.T) {
	// Every remaining story is blocked by a parked (NeedsReview) story. The
	// frontier is empty, but actionable work remains → fallback picks it.
	p := &PRD{
		UserStories: []UserStory{
			{ID: "US-001", Priority: 1, Passes: false, NeedsReview: true},
			{ID: "US-002", Priority: 2, Passes: false, BlockedBy: []string{"US-001"}},
		},
	}
	if got := p.Frontier(); len(got) != 0 {
		t.Errorf("Frontier = %v, want empty (only blocked story remains)", got)
	}
	next := p.NextStory()
	if next == nil {
		t.Fatal("expected fallback story, got nil")
	}
	if next.ID != "US-002" {
		t.Errorf("expected fallback US-002, got %s", next.ID)
	}
}

func TestPRD_Frontier_ReturnsEligibleOnly(t *testing.T) {
	p := &PRD{
		UserStories: []UserStory{
			{ID: "US-001", Priority: 1, Passes: true},                                 // passed → excluded
			{ID: "US-002", Priority: 2, Passes: false},                                // eligible
			{ID: "US-003", Priority: 3, Passes: false, NeedsReview: true},             // parked → excluded
			{ID: "US-004", Priority: 4, Passes: false, BlockedBy: []string{"US-002"}}, // blocked → excluded
			{ID: "US-005", Priority: 5, Passes: false, BlockedBy: []string{"US-001"}}, // blocker passed → eligible
		},
	}
	front := p.Frontier()
	gotIDs := make([]string, len(front))
	for i, s := range front {
		gotIDs[i] = s.ID
	}
	want := []string{"US-002", "US-005"}
	if len(gotIDs) != len(want) {
		t.Fatalf("Frontier IDs = %v, want %v", gotIDs, want)
	}
	for i := range want {
		if gotIDs[i] != want[i] {
			t.Errorf("Frontier IDs = %v, want %v (order = PRD order)", gotIDs, want)
		}
	}
}

func TestUserStory_Fields(t *testing.T) {
	story := UserStory{
		ID:                 "US-TEST",
		Title:              "Test Title",
		Description:        "Test Description",
		AcceptanceCriteria: []string{"AC1", "AC2", "AC3"},
		Priority:           5,
		Passes:             true,
		InProgress:         false,
	}

	if story.ID != "US-TEST" {
		t.Errorf("expected ID 'US-TEST', got '%s'", story.ID)
	}
	if len(story.AcceptanceCriteria) != 3 {
		t.Errorf("expected 3 acceptance criteria, got %d", len(story.AcceptanceCriteria))
	}
}

func TestPRD_NextStoryContext_ReturnsHighestPriority(t *testing.T) {
	p := &PRD{
		Project: "Test",
		UserStories: []UserStory{
			{ID: "US-001", Title: "Low priority", Priority: 3, Passes: false},
			{ID: "US-002", Title: "High priority", Priority: 1, Passes: false},
			{ID: "US-003", Title: "Mid priority", Priority: 2, Passes: false},
		},
	}

	ctx := p.NextStoryContext()
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}

	var story UserStory
	if err := json.Unmarshal([]byte(*ctx), &story); err != nil {
		t.Fatalf("failed to parse story context JSON: %v", err)
	}
	if story.ID != "US-002" {
		t.Errorf("expected highest-priority story US-002, got %s", story.ID)
	}
}

func TestPRD_StoryContextByID(t *testing.T) {
	p := &PRD{
		Project: "Test",
		UserStories: []UserStory{
			{ID: "US-001", Title: "First", Priority: 1, Passes: true},
			{ID: "US-002", Title: "Second", Priority: 2, Passes: false},
		},
	}

	// Looks up a specific story regardless of status (the review agent targets an
	// already-completed story, which NextStory would skip).
	ctx := p.StoryContextByID("US-001")
	if ctx == nil {
		t.Fatal("expected non-nil context for existing story")
	}
	var story UserStory
	if err := json.Unmarshal([]byte(*ctx), &story); err != nil {
		t.Fatalf("failed to parse story context JSON: %v", err)
	}
	if story.ID != "US-001" || story.Title != "First" {
		t.Errorf("expected US-001/First, got %s/%s", story.ID, story.Title)
	}

	// Unknown ID returns nil.
	if got := p.StoryContextByID("US-999"); got != nil {
		t.Errorf("expected nil for unknown ID, got %q", *got)
	}
}

func TestPRD_NextStoryContext_ReturnsNilWhenAllComplete(t *testing.T) {
	p := &PRD{
		Project: "Test",
		UserStories: []UserStory{
			{ID: "US-001", Passes: true},
			{ID: "US-002", Passes: true},
		},
	}

	ctx := p.NextStoryContext()
	if ctx != nil {
		t.Errorf("expected nil when all stories complete, got %q", *ctx)
	}
}

func TestPRD_NextStoryContext_SkipsPassingStories(t *testing.T) {
	p := &PRD{
		Project: "Test",
		UserStories: []UserStory{
			{ID: "US-001", Title: "Done", Priority: 1, Passes: true},
			{ID: "US-002", Title: "Pending", Priority: 2, Passes: false},
		},
	}

	ctx := p.NextStoryContext()
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}

	var story UserStory
	if err := json.Unmarshal([]byte(*ctx), &story); err != nil {
		t.Fatalf("failed to parse story context JSON: %v", err)
	}
	if story.ID != "US-002" {
		t.Errorf("expected US-002 (only pending story), got %s", story.ID)
	}
}

func TestPRD_NextStoryContext_EmptyPRD(t *testing.T) {
	p := &PRD{
		Project:     "Empty",
		UserStories: []UserStory{},
	}

	ctx := p.NextStoryContext()
	if ctx != nil {
		t.Errorf("expected nil for empty PRD, got %q", *ctx)
	}
}

func TestPRD_NextStoryContext_ValidJSON(t *testing.T) {
	p := &PRD{
		Project: "Test",
		UserStories: []UserStory{
			{
				ID:                 "US-001",
				Title:              "Test Story",
				Description:        "A test description",
				AcceptanceCriteria: []string{"AC1", "AC2"},
				Priority:           1,
				Passes:             false,
			},
		},
	}

	ctx := p.NextStoryContext()
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}

	var story UserStory
	if err := json.Unmarshal([]byte(*ctx), &story); err != nil {
		t.Fatalf("NextStoryContext did not return valid JSON: %v", err)
	}
	if story.ID != "US-001" {
		t.Errorf("expected ID US-001, got %s", story.ID)
	}
	if story.Title != "Test Story" {
		t.Errorf("expected title 'Test Story', got '%s'", story.Title)
	}
	if len(story.AcceptanceCriteria) != 2 {
		t.Errorf("expected 2 acceptance criteria, got %d", len(story.AcceptanceCriteria))
	}
}

func TestPRD_NextStoryContext_PromptSizeUnder10KB(t *testing.T) {
	stories := make([]UserStory, 300)
	for i := range stories {
		stories[i] = UserStory{
			ID:                 fmt.Sprintf("US-%03d", i+1),
			Title:              fmt.Sprintf("Story %d with a reasonably long title for realism", i+1),
			Description:        "This is a description that is moderately long to simulate realistic PRD content for testing purposes.",
			AcceptanceCriteria: []string{"Criterion A", "Criterion B", "Criterion C"},
			Priority:           float64(i + 1),
			Passes:             i > 0,
		}
	}
	p := &PRD{
		Project:     "Large Project",
		Description: "A large PRD with 300 stories",
		UserStories: stories,
	}

	ctx := p.NextStoryContext()
	if ctx == nil {
		t.Fatal("expected non-nil context for 300-story PRD")
	}
	if len(*ctx) > 10*1024 {
		t.Errorf("story context is %d bytes, expected under 10KB", len(*ctx))
	}
}

func TestPRD_ExtractIDPrefix_US(t *testing.T) {
	p := &PRD{
		Project: "Test",
		UserStories: []UserStory{
			{ID: "US-001"},
			{ID: "US-002"},
		},
	}
	if got := p.ExtractIDPrefix(); got != "US" {
		t.Errorf("ExtractIDPrefix() = %q, want %q", got, "US")
	}
}

func TestPRD_ExtractIDPrefix_MFR(t *testing.T) {
	p := &PRD{
		Project: "Test",
		UserStories: []UserStory{
			{ID: "MFR-001"},
			{ID: "MFR-002"},
		},
	}
	if got := p.ExtractIDPrefix(); got != "MFR" {
		t.Errorf("ExtractIDPrefix() = %q, want %q", got, "MFR")
	}
}

func TestPRD_ExtractIDPrefix_Default(t *testing.T) {
	p := &PRD{
		Project:     "Empty",
		UserStories: []UserStory{},
	}
	if got := p.ExtractIDPrefix(); got != "US" {
		t.Errorf("ExtractIDPrefix() = %q, want %q for empty PRD", got, "US")
	}
}

func TestPRD_ExtractIDPrefix_SingleChar(t *testing.T) {
	p := &PRD{
		Project: "Test",
		UserStories: []UserStory{
			{ID: "T-001"},
		},
	}
	if got := p.ExtractIDPrefix(); got != "T" {
		t.Errorf("ExtractIDPrefix() = %q, want %q", got, "T")
	}
}
