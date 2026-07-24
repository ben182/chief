package tui

import "testing"

// TestRunProgress_NoBaselineCountsWholePRD verifies that without a run baseline
// (e.g. just viewing a PRD), progress spans the whole story list.
func TestRunProgress_NoBaselineCountsWholePRD(t *testing.T) {
	stories := makeStories(10)
	for i := 0; i < 4; i++ {
		stories[i].Passes = true
	}
	app := newTestApp(stories, 120, 20)

	completed, total := app.runProgress()
	if completed != 4 || total != 10 {
		t.Fatalf("runProgress() = %d/%d, want 4/10", completed, total)
	}
	if pct := app.GetCompletionPercentage(); pct != 40 {
		t.Errorf("GetCompletionPercentage() = %v, want 40", pct)
	}
}

// TestRunProgress_ExcludesBaselineFollowups mirrors adding follow-up stories to
// a finished PRD: the ten original stories are the baseline, so the two new
// stories are the only ones this run tracks.
func TestRunProgress_ExcludesBaselineFollowups(t *testing.T) {
	stories := makeStories(10)
	for i := range stories {
		stories[i].Passes = true
	}
	app := newTestApp(stories, 120, 20)

	// Snapshot the finished PRD as the baseline (what doStartLoop records).
	app.runBaselineDone = make(map[string]bool)
	for _, s := range app.prd.UserStories {
		app.runBaselineDone[s.ID] = true
	}

	// Two follow-up stories appended, not yet passing.
	followups := makeStories(12)[10:]
	app.prd.UserStories = append(app.prd.UserStories, followups...)

	completed, total := app.runProgress()
	if completed != 0 || total != 2 {
		t.Fatalf("runProgress() = %d/%d, want 0/2", completed, total)
	}
	if pct := app.GetCompletionPercentage(); pct != 0 {
		t.Errorf("GetCompletionPercentage() = %v, want 0", pct)
	}

	// Finishing one follow-up moves the run to 1/2 = 50%.
	app.prd.UserStories[10].Passes = true
	completed, total = app.runProgress()
	if completed != 1 || total != 2 {
		t.Fatalf("runProgress() = %d/%d, want 1/2", completed, total)
	}
	if pct := app.GetCompletionPercentage(); pct != 50 {
		t.Errorf("GetCompletionPercentage() = %v, want 50", pct)
	}
}
