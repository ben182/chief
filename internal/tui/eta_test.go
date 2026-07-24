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

func TestGetETA_CountsDownWithinCurrentStory(t *testing.T) {
	stories := makeStories(6)
	for i := 0; i < 3; i++ {
		stories[i].Passes = true
	}
	app := newTestApp(stories, 120, 20)
	app.storyTimings[app.prdName] = []StoryTiming{
		{Duration: 10 * time.Minute},
		{Duration: 20 * time.Minute},
		{Duration: 30 * time.Minute},
	}
	// avg = 20m, remaining = 3 → base ETA 60m. A story has been in progress for
	// 5m, so the ETA should drop to 55m.
	app.currentStoryID[app.prdName] = stories[3].ID
	app.currentStoryStart[app.prdName] = time.Now().Add(-5 * time.Minute)

	eta, ok := app.GetETA()
	if !ok {
		t.Fatal("expected ETA to be available")
	}
	// Allow a little slack for the elapsed clock during the test.
	if eta < 54*time.Minute || eta > 55*time.Minute+time.Second {
		t.Errorf("GetETA() = %v, want ~55m", eta)
	}
}

func TestGetETA_CurrentStoryOverrunDoesNotUndershoot(t *testing.T) {
	stories := makeStories(6)
	for i := 0; i < 3; i++ {
		stories[i].Passes = true
	}
	app := newTestApp(stories, 120, 20)
	app.storyTimings[app.prdName] = []StoryTiming{
		{Duration: 20 * time.Minute},
		{Duration: 20 * time.Minute},
	}
	// avg = 20m, remaining = 3 → base 60m. The current story has already run
	// 40m (double the average); the subtraction is capped at one avg so the ETA
	// floors at 40m rather than 20m.
	app.currentStoryID[app.prdName] = stories[3].ID
	app.currentStoryStart[app.prdName] = time.Now().Add(-40 * time.Minute)

	eta, ok := app.GetETA()
	if !ok {
		t.Fatal("expected ETA to be available")
	}
	if eta < 39*time.Minute || eta > 40*time.Minute+time.Second {
		t.Errorf("GetETA() = %v, want ~40m", eta)
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
