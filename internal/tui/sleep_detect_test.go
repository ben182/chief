package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/loop"
	"github.com/ben182/chief/internal/prd"
)

// stubSleepTracker stands in for the real detector. A test can't suspend the
// machine it runs on, so the answer is canned; the ref it was asked about is
// recorded, because scoping the figure to the run is the interesting part.
type stubSleepTracker struct {
	slept   time.Duration
	samples *int
	askedAt *time.Time
}

func (s stubSleepTracker) Sample() {
	if s.samples != nil {
		*s.samples++
	}
}

func (s stubSleepTracker) SleptSince(ref time.Time) time.Duration {
	if s.askedAt != nil {
		*s.askedAt = ref
	}
	return s.slept
}

// completionApp builds the smallest App that can raise a completion screen.
func completionApp(t *testing.T, tracker sleepTracker, startedAgo time.Duration) *App {
	t.Helper()
	return &App{
		prd: &prd.PRD{
			Project:     "Auth",
			UserStories: []prd.UserStory{{ID: "AUTH-1", Title: "Login", Passes: true}},
		},
		prdName:          "auth",
		baseDir:          t.TempDir(),
		manager:          loop.NewManager(10, nil),
		config:           &config.Config{},
		completionScreen: NewCompletionScreen(),
		storyTimings:     map[string][]StoryTiming{},
		startTime:        time.Now().Add(-startedAgo),
		sleepTracker:     tracker,
		width:            100,
		height:           40,
	}
}

func TestCompletionScreenReportsSleepDuringTheRun(t *testing.T) {
	app := completionApp(t, stubSleepTracker{slept: 56 * time.Minute}, 2*time.Hour)

	app.showCompletionScreen("auth")

	rendered := app.completionScreen.Render()
	if !strings.Contains(rendered, "Mac slept 56m00s during the run") {
		t.Errorf("expected the slept-time line on the completion screen, got:\n%s", rendered)
	}
}

// The figure has to be scoped to this run, not to everything chief has seen
// since launch — otherwise a followup run inherits the previous one's naps.
func TestCompletionScreenScopesSleepToTheRunStart(t *testing.T) {
	var askedAt time.Time
	app := completionApp(t, stubSleepTracker{slept: time.Hour, askedAt: &askedAt}, 2*time.Hour)

	app.showCompletionScreen("auth")

	if !askedAt.Equal(app.startTime) {
		t.Errorf("sleep was measured from %v, want the run start %v", askedAt, app.startTime)
	}
}

func TestCompletionScreenWithoutSleepLooksUnchanged(t *testing.T) {
	app := completionApp(t, stubSleepTracker{slept: 0}, 2*time.Hour)

	app.showCompletionScreen("auth")

	rendered := app.completionScreen.Render()
	if strings.Contains(rendered, "slept") {
		t.Errorf("expected no slept-time line when nothing was detected, got:\n%s", rendered)
	}
}

// Hand-built App literals elsewhere in these tests carry no tracker at all; that
// has to mean "no sleep reported", not a crash.
func TestCompletionScreenWithoutTrackerReportsNoSleep(t *testing.T) {
	app := completionApp(t, nil, 2*time.Hour)

	app.showCompletionScreen("auth")

	rendered := app.completionScreen.Render()
	if strings.Contains(rendered, "slept") {
		t.Errorf("expected no slept-time line without a tracker, got:\n%s", rendered)
	}
}

// A run that never started has no window to measure sleep over.
func TestNoSleepReportedBeforeAnyRun(t *testing.T) {
	app := completionApp(t, stubSleepTracker{slept: time.Hour}, 0)
	app.startTime = time.Time{}

	if got := app.sleptDuringRun(); got != 0 {
		t.Errorf("sleptDuringRun() = %v, want 0 before a run has started", got)
	}
}

// The per-second elapsed tick is where the two clocks get compared.
func TestElapsedTickSamplesTheSleepTracker(t *testing.T) {
	samples := 0
	app := App{
		state:        StateRunning,
		sleepTracker: stubSleepTracker{samples: &samples},
	}

	m, cmd := app.Update(elapsedTickMsg{})

	if samples != 1 {
		t.Errorf("samples = %d, want 1 per elapsed tick", samples)
	}
	if cmd == nil {
		t.Error("expected the elapsed tick to keep ticking while the run is in flight")
	}
	if _, ok := m.(App); !ok {
		t.Fatalf("Update returned %T, want App", m)
	}
}

func TestElapsedTickDoesNotSampleWhenIdle(t *testing.T) {
	samples := 0
	app := App{
		state:        StateReady,
		sleepTracker: stubSleepTracker{samples: &samples},
	}

	app.Update(elapsedTickMsg{})

	if samples != 0 {
		t.Errorf("samples = %d, want 0 while no run is in flight", samples)
	}
}
