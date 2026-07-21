package tui

import (
	"testing"

	"github.com/minicodemonkey/chief/internal/loop"
)

// TestETA_SimContinuousRun simulates the real event stream for a continuous run
// reaching story 4 in-progress with stories 1-3 done, and checks that timings
// get recorded and an ETA becomes available.
func TestETA_SimContinuousRun(t *testing.T) {
	app := newTestApp(makeStories(6), 120, 20)
	app.prdName = "main"
	app.logViewer = NewLogViewer()

	cur := *app
	step := func(ev loop.Event) {
		m, _ := cur.handleLoopEvent("main", ev)
		cur = m.(App)
	}

	for i := 0; i < 3; i++ {
		id := cur.prd.UserStories[i].ID
		step(loop.Event{Type: loop.EventIterationStart, StoryID: id})
		step(loop.Event{Type: loop.EventIterationStart}) // bare parser init
		cur.prd.UserStories[i].Passes = true
		step(loop.Event{Type: loop.EventStoryDone})
	}
	step(loop.Event{Type: loop.EventIterationStart, StoryID: cur.prd.UserStories[3].ID})

	if got := len(cur.storyTimings["main"]); got != 3 {
		t.Fatalf("expected 3 timings, got %d", got)
	}
	if _, ok := cur.GetETA(); !ok {
		t.Fatalf("expected ETA with 3 timings")
	}
}

// TestETA_SimBackgroundPRD reproduces the real-world bug: a PRD's stories
// complete while a DIFFERENT PRD is on screen. Timings for the background PRD
// must still be recorded (previously they were dropped because tracking was
// gated on the viewed PRD).
func TestETA_SimBackgroundPRD(t *testing.T) {
	app := newTestApp(makeStories(6), 120, 20)
	app.prdName = "other" // viewing a different PRD than the one making progress
	app.logViewer = NewLogViewer()

	cur := *app
	// Events arrive for the background PRD "bg" while "other" is on screen.
	stepBG := func(ev loop.Event) {
		m, _ := cur.handleLoopEvent("bg", ev)
		cur = m.(App)
	}
	for i := 0; i < 3; i++ {
		id := cur.prd.UserStories[i].ID
		stepBG(loop.Event{Type: loop.EventIterationStart, StoryID: id})
		stepBG(loop.Event{Type: loop.EventStoryDone})
	}

	if got := len(cur.storyTimings["bg"]); got != 3 {
		t.Fatalf("background PRD: expected 3 timings, got %d", got)
	}
	// The viewed PRD ("other") has no timings, so no ETA leaks across PRDs.
	if _, ok := cur.GetETA(); ok {
		t.Fatal("viewed PRD has no timings; expected no ETA")
	}
}
