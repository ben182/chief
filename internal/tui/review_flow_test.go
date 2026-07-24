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
	app.config.Review.Enabled = true // review agent runs after each story

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
	app.config.Review.Enabled = true

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
