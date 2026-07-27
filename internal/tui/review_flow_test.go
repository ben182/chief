package tui

import (
	"testing"

	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/loop"
)

// stepEvents replays a slice of events against a copy of app and returns the
// resulting App value.
func stepEvents(app *App, prdName string, events ...loop.Event) App {
	cur := *app
	for _, ev := range events {
		m, _ := cur.handleLoopEvent(prdName, ev)
		cur = m.(App)
	}
	return cur
}

// TestReview_TimingDeferredUntilReviewDone verifies that when a review agent is
// active, a story's timing is only recorded once the review finishes — so the
// review pass counts toward the ETA instead of being dropped at build-done.
func TestReview_TimingDeferredUntilReviewDone(t *testing.T) {
	app := newTestApp(makeStories(6), 120, 20)
	app.prdName = "main"
	app.logViewer = NewLogViewer()
	app.config = config.Default()
	app.config.Review.Enabled = config.Bool(true) // review agent runs after each story

	id := app.prd.UserStories[0].ID

	// Build agent finishes and signals done — but with review active this must NOT
	// finalize the story's timing yet.
	cur := stepEvents(app, "main",
		loop.Event{Type: loop.EventIterationStart, StoryID: id},
		loop.Event{Type: loop.EventStoryDone, StoryID: id},
	)
	if got := len(cur.storyTimings["main"]); got != 0 {
		t.Fatalf("expected timing deferred while review pending, got %d timings", got)
	}

	// The review pass runs and completes: now the timing is recorded.
	cur = stepEvents(&cur, "main",
		loop.Event{Type: loop.EventReviewStart, StoryID: id},
		loop.Event{Type: loop.EventReviewDone, StoryID: id},
	)
	if got := len(cur.storyTimings["main"]); got != 1 {
		t.Fatalf("expected 1 timing after review done, got %d", got)
	}
}

// TestReview_StaysOnStoryWithTag verifies that while a story is under review the
// selection stays on it (rather than jumping ahead) and it is reported as being
// reviewed.
func TestReview_StaysOnStoryWithTag(t *testing.T) {
	app := newTestApp(makeStories(6), 120, 20)
	app.prdName = "main"
	app.logViewer = NewLogViewer()
	app.config = config.Default()
	app.config.Review.Enabled = config.Bool(true)

	id := app.prd.UserStories[2].ID

	cur := stepEvents(app, "main",
		loop.Event{Type: loop.EventIterationStart, StoryID: id},
		loop.Event{Type: loop.EventStoryDone, StoryID: id},
		loop.Event{Type: loop.EventReviewStart, StoryID: id},
	)

	if !cur.isReviewing(id) {
		t.Fatalf("expected story %s to be marked reviewing", id)
	}
	if cur.selectedIndex != 2 {
		t.Fatalf("expected selection pinned to reviewed story (index 2), got %d", cur.selectedIndex)
	}

	// A PRD reload mid-review must not drift the selection off the reviewed story.
	cur.selectInProgressStory()
	if cur.selectedIndex != 2 {
		t.Fatalf("expected selection to stay on reviewed story after reload, got %d", cur.selectedIndex)
	}

	cur = stepEvents(&cur, "main", loop.Event{Type: loop.EventReviewDone, StoryID: id})
	if cur.isReviewing(id) {
		t.Fatalf("expected reviewing tag cleared after EventReviewDone")
	}
}

// TestIterationStart_MovesSelectionAndClearsStaleReviewTag verifies that the
// selection follows the loop's own story events. It used to be driven solely by
// the prd.md file watcher, so a review that ended without EventReviewDone (the
// loop paused, stopped or cancelled mid-review) left the reviewing tag set and
// pinned the UI to that story while every following story built, timed and
// logged underneath it.
func TestIterationStart_MovesSelectionAndClearsStaleReviewTag(t *testing.T) {
	app := newTestApp(makeStories(9), 120, 20)
	app.prdName = "main"
	app.logViewer = NewLogViewer()
	app.config = config.Default()
	app.config.Review.Enabled = config.Bool(true)

	seventh := app.prd.UserStories[6].ID
	eighth := app.prd.UserStories[7].ID

	// Story 7 builds and goes into review — but the review never reports done.
	cur := stepEvents(app, "main",
		loop.Event{Type: loop.EventIterationStart, StoryID: seventh},
		loop.Event{Type: loop.EventStoryDone, StoryID: seventh},
		loop.Event{Type: loop.EventReviewStart, StoryID: seventh},
	)
	if cur.selectedIndex != 6 {
		t.Fatalf("expected selection on the reviewed story (index 6), got %d", cur.selectedIndex)
	}

	// Story 8 starts building: the run has moved on, so the UI must too.
	cur = stepEvents(&cur, "main", loop.Event{Type: loop.EventIterationStart, StoryID: eighth})

	if cur.isReviewing(seventh) {
		t.Errorf("stale reviewing tag on %s survived the next story's start", seventh)
	}
	if cur.selectedIndex != 7 {
		t.Errorf("expected selection to follow to story 8 (index 7), got %d", cur.selectedIndex)
	}
	// A PRD reload must not drag it back to the pinned story either.
	cur.selectInProgressStory()
	if cur.selectedIndex != 7 {
		t.Errorf("expected selection to stay on story 8 after reload, got %d", cur.selectedIndex)
	}
}
