package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ben182/chief/internal/loop"
	"github.com/ben182/chief/internal/prd"
)

// newRateLimitApp builds an App wired up enough to feed loop events through.
func newRateLimitApp(t *testing.T) *App {
	t.Helper()

	a := newTestApp([]prd.UserStory{{ID: "US-001", Title: "Story"}}, 120, 30)
	a.prdName = "demo"
	a.state = StateRunning
	a.logViewer = NewLogViewer()
	a.logViewer.SetSize(120, 20)
	return a
}

// TestRateLimitChip covers what the header shows: nothing while the window is
// healthy, a percentage once it is filling up, and the reset time once it is
// full — the number the user needs to decide whether to wait or stop.
func TestRateLimitChip(t *testing.T) {
	resets := time.Now().Add(90 * time.Minute)

	tests := []struct {
		name string
		info loop.RateLimitInfo
		want string // "" means: show nothing
	}{
		{
			name: "healthy window stays quiet",
			info: loop.RateLimitInfo{Status: "allowed", Utilization: 0.2, ResetsAt: resets},
			want: "",
		},
		{
			name: "no report at all stays quiet",
			info: loop.RateLimitInfo{},
			want: "",
		},
		{
			name: "warning shows how full it is",
			info: loop.RateLimitInfo{Status: "allowed_warning", Utilization: 0.9, ResetsAt: resets},
			want: "Limit 90% · " + resets.Local().Format("15:04"),
		},
		{
			name: "rejection shows when it comes back",
			info: loop.RateLimitInfo{Status: "rejected", Utilization: 1, ResetsAt: resets},
			want: "Limit reached · " + resets.Local().Format("15:04"),
		},
		{
			name: "a window that has since reset is dropped",
			info: loop.RateLimitInfo{Status: "rejected", ResetsAt: time.Now().Add(-time.Minute)},
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newRateLimitApp(t)
			a.rateLimit = tc.info
			if got := a.rateLimitChip(); got != tc.want {
				t.Errorf("chip = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRateLimitChipInHeader checks the chip actually reaches the rendered header
// rather than only the helper that builds it.
func TestRateLimitChipInHeader(t *testing.T) {
	a := newRateLimitApp(t)
	a.rateLimit = loop.RateLimitInfo{Status: "allowed_warning", Utilization: 0.9, ResetsAt: time.Now().Add(time.Hour)}

	if got := a.renderHeader(); !strings.Contains(got, "Limit 90%") {
		t.Errorf("header does not show the rate limit chip:\n%s", got)
	}
}

// TestRateLimitWaitShowsInActivityLine is what the user sees instead of the old
// bare "Error": the run is alive, waiting for a window it named, and the state
// stays Running because it will carry on by itself.
func TestRateLimitWaitShowsInActivityLine(t *testing.T) {
	a := newRateLimitApp(t)
	info := loop.RateLimitInfo{Status: "rejected", WindowType: "five_hour", ResetsAt: time.Now().Add(time.Hour)}

	model, _ := a.handleLoopEvent("demo", loop.Event{
		Type:      loop.EventRateLimitWait,
		Text:      "Rate limit reached (5h window) — waiting until 20:00 (1h 0m)",
		RateLimit: &info,
	})

	got, ok := model.(App)
	if !ok {
		t.Fatalf("handleLoopEvent returned %T, want App", model)
	}
	if !strings.Contains(got.lastActivity, "waiting until 20:00") {
		t.Errorf("activity line = %q, want the wait and its end time", got.lastActivity)
	}
	if got.state != StateRunning {
		t.Errorf("state = %v, want StateRunning — a waiting run has not failed", got.state)
	}
	if !got.rateLimit.Rejected() {
		t.Error("the wait should leave the rejection in the header chip")
	}
}

// TestRateLimitReportUpdatesHeader feeds the warning event the loop forwards and
// expects it to land in the header state.
func TestRateLimitReportUpdatesHeader(t *testing.T) {
	a := newRateLimitApp(t)
	info := loop.RateLimitInfo{Status: "allowed_warning", Utilization: 0.9, ResetsAt: time.Now().Add(time.Hour)}

	model, _ := a.handleLoopEvent("demo", loop.Event{Type: loop.EventRateLimit, RateLimit: &info})

	got := model.(App)
	if !got.rateLimit.Warning() {
		t.Errorf("stored status = %q, want the warning", got.rateLimit.Status)
	}
	if chip := got.rateLimitChip(); !strings.Contains(chip, "90%") {
		t.Errorf("chip = %q, want the utilization", chip)
	}
}

// TestRateLimitEventsReachTheLog makes sure both the warning and the wait show
// up in the log view, where a user goes to find out what a run did while they
// were away.
func TestRateLimitEventsReachTheLog(t *testing.T) {
	lv := NewLogViewer()
	lv.SetSize(120, 20)

	warning := loop.RateLimitInfo{Status: "allowed_warning", Utilization: 0.9, WindowType: "five_hour", ResetsAt: time.Now().Add(time.Hour)}
	lv.AddEvent(loop.Event{Type: loop.EventRateLimit, RateLimit: &warning})
	lv.AddEvent(loop.Event{Type: loop.EventRateLimitWait, Text: "Rate limit reached — waiting until 20:00 (1h 33m)"})

	if len(lv.entries) != 2 {
		t.Fatalf("log holds %d entries, want 2", len(lv.entries))
	}

	rendered := strings.Join(lv.entries[0].cachedLines, "\n") + "\n" + strings.Join(lv.entries[1].cachedLines, "\n")
	for _, want := range []string{"90%", "5h window", "waiting until 20:00"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("log does not mention %q:\n%s", want, rendered)
		}
	}
}

// TestHealthyRateLimitNotLogged guards the log against the report Claude sends
// on every single turn. The loop filters those out; this pins the UI side so a
// future change there can't flood the log either.
func TestHealthyRateLimitNotLogged(t *testing.T) {
	lv := NewLogViewer()
	lv.SetSize(120, 20)

	healthy := loop.RateLimitInfo{Status: "allowed", Utilization: 0.2}
	lv.AddEvent(loop.Event{Type: loop.EventRateLimit, RateLimit: &healthy})

	rendered := strings.Join(lv.entries[0].cachedLines, "\n")
	if !strings.Contains(rendered, "cleared") {
		t.Errorf("a healthy report should read as recovery, got:\n%s", rendered)
	}
}

// TestErrorLogShowsTheError is the other half of the run that died on a rate
// limit: the log said "An error occurred" because loop errors carry their text
// on Err, not Text.
func TestErrorLogShowsTheError(t *testing.T) {
	lv := NewLogViewer()
	lv.SetSize(120, 20)

	lv.AddEvent(loop.Event{Type: loop.EventError, Err: errors.New("max retries (3) exceeded: Claude exited with error: exit status 1")})

	rendered := strings.Join(lv.entries[0].cachedLines, "\n")
	if strings.Contains(rendered, "An error occurred") {
		t.Errorf("log swallowed the error message:\n%s", rendered)
	}
	if !strings.Contains(rendered, "max retries (3) exceeded") {
		t.Errorf("log does not show the error:\n%s", rendered)
	}
}
