package tui

import (
	"testing"
	"time"
)

func TestGetETA(t *testing.T) {
	stories := makeStories(6)
	// Mark first 3 as passed.
	for i := 0; i < 3; i++ {
		stories[i].Passes = true
	}
	app := newTestApp(stories, 120, 20)
	app.storyTimings[app.prdName] = []StoryTiming{
		{Duration: 10 * time.Minute},
		{Duration: 20 * time.Minute},
		{Duration: 30 * time.Minute},
	}

	eta, ok := app.GetETA()
	if !ok {
		t.Fatal("expected ETA to be available with 3 timings")
	}
	// avg = 20m, remaining = 3 → 60m
	if eta != 60*time.Minute {
		t.Errorf("GetETA() = %v, want 60m", eta)
	}
}

func TestGetETA_NotEnoughTimings(t *testing.T) {
	app := newTestApp(makeStories(6), 120, 20)
	app.storyTimings[app.prdName] = []StoryTiming{{Duration: time.Minute}}
	if _, ok := app.GetETA(); ok {
		t.Error("expected no ETA with fewer than 2 timings")
	}
}

func TestGetETA_AllComplete(t *testing.T) {
	stories := makeStories(3)
	for i := range stories {
		stories[i].Passes = true
	}
	app := newTestApp(stories, 120, 20)
	app.storyTimings[app.prdName] = []StoryTiming{
		{Duration: time.Minute}, {Duration: time.Minute}, {Duration: time.Minute},
	}
	if _, ok := app.GetETA(); ok {
		t.Error("expected no ETA when nothing remains")
	}
}
