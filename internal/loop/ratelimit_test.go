package loop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestParseLine_RateLimitWarning parses the warning line the Claude CLI emits as
// a window fills up. Its shape is copied from a real run's log.
func TestParseLine_RateLimitWarning(t *testing.T) {
	line := `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed_warning","resetsAt":1789149600,"rateLimitType":"five_hour","utilization":0.9,"unifiedWindows":{"five_hour":{"utilization":0.9,"resetsAt":1789149600},"seven_day":{"utilization":0.28,"resetsAt":1789567200}}}}`

	ev := ParseLine(line)
	if ev == nil {
		t.Fatal("expected an event for a rate_limit_event line, got nil")
	}
	if ev.Type != EventRateLimit {
		t.Errorf("event type = %v, want EventRateLimit", ev.Type)
	}
	if ev.RateLimit == nil {
		t.Fatal("expected the event to carry a rate limit report")
	}
	if !ev.RateLimit.Warning() {
		t.Errorf("status = %q, want a warning", ev.RateLimit.Status)
	}
	if ev.RateLimit.Utilization != 0.9 {
		t.Errorf("utilization = %v, want 0.9", ev.RateLimit.Utilization)
	}
	if want := time.Unix(1789149600, 0); !ev.RateLimit.ResetsAt.Equal(want) {
		t.Errorf("resetsAt = %v, want %v", ev.RateLimit.ResetsAt, want)
	}
	if ev.RateLimit.WindowType != "five_hour" {
		t.Errorf("window type = %q, want five_hour", ev.RateLimit.WindowType)
	}
}

// TestParseLine_RateLimitRejected parses the rejection line, which — unlike the
// warning — carries no top-level utilization. Reading it as 0% would report a
// window that is provably full as untouched, so the value comes from
// unifiedWindows instead.
func TestParseLine_RateLimitRejected(t *testing.T) {
	line := `{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":1789149600,"rateLimitType":"five_hour","overageStatus":"rejected","unifiedWindows":{"five_hour":{"utilization":1,"resetsAt":1789149600}}}}`

	ev := ParseLine(line)
	if ev == nil || ev.RateLimit == nil {
		t.Fatal("expected a rate limit event")
	}
	if !ev.RateLimit.Rejected() {
		t.Errorf("status = %q, want rejected", ev.RateLimit.Status)
	}
	if ev.RateLimit.Utilization != 1 {
		t.Errorf("utilization = %v, want 1 (from unifiedWindows)", ev.RateLimit.Utilization)
	}
}

// TestParseLine_RateLimitIgnoresGarbage keeps the parser's "skip what you can't
// read" contract for lines that claim the type but carry no status.
func TestParseLine_RateLimitIgnoresGarbage(t *testing.T) {
	for _, line := range []string{
		`{"type":"rate_limit_event"}`,
		`{"type":"rate_limit_event","rate_limit_info":{}}`,
	} {
		if ev := ParseLine(line); ev != nil {
			t.Errorf("ParseLine(%q) = %+v, want nil", line, ev)
		}
	}
}

// TestRateLimitInfo_WaitUntil covers when waiting is the right answer and when
// it isn't: only a rejection with a reset time still ahead is worth sleeping on.
func TestRateLimitInfo_WaitUntil(t *testing.T) {
	now := time.Unix(1789140000, 0)
	grace := 60 * time.Second

	tests := []struct {
		name string
		info RateLimitInfo
		want bool
	}{
		{
			name: "rejected with a future reset",
			info: RateLimitInfo{Status: rateLimitReject, ResetsAt: now.Add(time.Hour)},
			want: true,
		},
		{
			name: "rejected but the window already reset",
			info: RateLimitInfo{Status: rateLimitReject, ResetsAt: now.Add(-time.Hour)},
			want: false,
		},
		{
			name: "rejected without a reset time",
			info: RateLimitInfo{Status: rateLimitReject},
			want: false,
		},
		{
			name: "merely warning",
			info: RateLimitInfo{Status: rateLimitWarning, ResetsAt: now.Add(time.Hour)},
			want: false,
		},
		{
			name: "no report at all",
			info: RateLimitInfo{},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			until, ok := tc.info.WaitUntil(now, grace)
			if ok != tc.want {
				t.Fatalf("ok = %v, want %v", ok, tc.want)
			}
			if ok {
				if want := tc.info.ResetsAt.Add(grace); !until.Equal(want) {
					t.Errorf("until = %v, want %v (reset plus grace)", until, want)
				}
			}
		})
	}
}

// TestLoop_RecordRateLimit checks which reports reach the UI. Claude sends one
// on every turn, so a healthy window must stay silent — but the return to health
// after a warning is worth a line.
func TestLoop_RecordRateLimit(t *testing.T) {
	l := NewLoop("/test/prd.md", "test", 5, testProvider)

	if l.recordRateLimit(&RateLimitInfo{Status: rateLimitAllowed}) {
		t.Error("a healthy window should not be surfaced")
	}
	if !l.recordRateLimit(&RateLimitInfo{Status: rateLimitWarning, Utilization: 0.9}) {
		t.Error("a warning should be surfaced")
	}
	if !l.recordRateLimit(&RateLimitInfo{Status: rateLimitReject}) {
		t.Error("a rejection should be surfaced")
	}
	if !l.recordRateLimit(&RateLimitInfo{Status: rateLimitAllowed}) {
		t.Error("recovery after a rejection should be surfaced")
	}
	if l.recordRateLimit(&RateLimitInfo{Status: rateLimitAllowed}) {
		t.Error("a healthy window after a healthy window should stay silent")
	}
	if l.recordRateLimit(nil) {
		t.Error("a nil report should be ignored")
	}
	if l.rateLimit.Status != rateLimitAllowed {
		t.Errorf("stored status = %q, want the last real report", l.rateLimit.Status)
	}
}

// rateLimitScript writes a fake agent CLI that fails its first run the way a
// rate-limited Claude does — a rejection line on stdout, no stderr, non-zero
// exit — and succeeds on every run after that. It returns the script path and a
// func reporting how often it was invoked.
func rateLimitScript(t *testing.T, dir string, resetsAt time.Time) (string, func() int) {
	t.Helper()

	countPath := filepath.Join(dir, "runs")
	scriptPath := filepath.Join(dir, "mock-agent")
	rejection := fmt.Sprintf(`{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":%d,"rateLimitType":"five_hour","unifiedWindows":{"five_hour":{"utilization":1,"resetsAt":%d}}}}`,
		resetsAt.Unix(), resetsAt.Unix())

	script := fmt.Sprintf(`#!/bin/bash
count_file=%q
runs=$(cat "$count_file" 2>/dev/null || echo 0)
echo $((runs + 1)) > "$count_file"
if [ "$runs" -eq 0 ]; then
  echo '%s'
  echo '{"type":"assistant","message":{"content":[{"type":"text","text":"You'"'"'ve hit your session limit"}]}}'
  exit 1
fi
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"back at work"}]}}'
exit 0
`, countPath, rejection)

	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write mock agent: %v", err)
	}

	return scriptPath, func() int {
		raw, err := os.ReadFile(countPath)
		if err != nil {
			return 0
		}
		var n int
		fmt.Sscanf(strings.TrimSpace(string(raw)), "%d", &n)
		return n
	}
}

// drainEvents collects everything the loop emitted without blocking.
func drainEvents(l *Loop) []Event {
	var events []Event
	for {
		select {
		case ev := <-l.events:
			events = append(events, ev)
		default:
			return events
		}
	}
}

func countEvents(events []Event, t EventType) int {
	n := 0
	for _, ev := range events {
		if ev.Type == t {
			n++
		}
	}
	return n
}

// TestLoop_WaitsOutRateLimitInsteadOfRetrying is the regression test for the run
// that died at 18:27: a rate-limited agent looks exactly like a crashed one, so
// the loop spent its three retries in twenty seconds against a window with an
// hour and a half left, then gave up with "max retries exceeded". Now it waits
// for the reset and carries on, and the wait doesn't cost a retry.
func TestLoop_WaitsOutRateLimitInsteadOfRetrying(t *testing.T) {
	dir := t.TempDir()
	resetsAt := time.Now().Add(90 * time.Minute)
	scriptPath, runs := rateLimitScript(t, dir, resetsAt)

	l := NewLoopWithWorkDir(filepath.Join(dir, "prd.md"), dir, "test", 5, &mockProvider{cliPath: scriptPath})
	l.SetWatchdogTimeout(0)

	var slept []time.Duration
	l.rateLimitSleep = func(_ context.Context, d time.Duration) bool {
		slept = append(slept, d)
		return true
	}

	if err := l.runIterationWithRetry(context.Background(), modeBuild); err != nil {
		t.Fatalf("runIterationWithRetry returned %v, want nil — the second attempt succeeded", err)
	}

	if got := runs(); got != 2 {
		t.Errorf("agent was invoked %d times, want 2 (one rejected, one after the wait)", got)
	}
	if len(slept) != 1 {
		t.Fatalf("waited %d times, want once", len(slept))
	}
	// The wait runs to the reset plus the grace period, not to some fixed backoff.
	if want := time.Until(resetsAt) + rateLimitGrace; slept[0] < want-time.Minute || slept[0] > want+time.Minute {
		t.Errorf("waited %v, want roughly %v", slept[0], want)
	}

	events := drainEvents(l)
	if n := countEvents(events, EventRetrying); n != 0 {
		t.Errorf("emitted %d retry events, want 0 — a rate limit is not a crash", n)
	}
	if n := countEvents(events, EventRateLimitWait); n != 1 {
		t.Errorf("emitted %d wait events, want 1", n)
	}
	for _, ev := range events {
		if ev.Type == EventRateLimitWait {
			if !strings.Contains(ev.Text, "Rate limit reached") {
				t.Errorf("wait event text = %q, want it to name the rate limit", ev.Text)
			}
			if ev.RateLimit == nil || !ev.RateLimit.Rejected() {
				t.Error("wait event should carry the rejection it is waiting out")
			}
		}
	}
}

// TestLoop_RateLimitWaitStopped makes sure stopping a run mid-wait ends it
// rather than starting another agent once the window resets.
func TestLoop_RateLimitWaitStopped(t *testing.T) {
	dir := t.TempDir()
	scriptPath, runs := rateLimitScript(t, dir, time.Now().Add(time.Hour))

	l := NewLoopWithWorkDir(filepath.Join(dir, "prd.md"), dir, "test", 5, &mockProvider{cliPath: scriptPath})
	l.SetWatchdogTimeout(0)
	l.rateLimitSleep = func(context.Context, time.Duration) bool { return false } // interrupted

	if err := l.runIterationWithRetry(context.Background(), modeBuild); err != nil {
		t.Fatalf("returned %v, want nil — an interrupted wait is not a failure", err)
	}
	if got := runs(); got != 1 {
		t.Errorf("agent was invoked %d times, want 1 — nothing should start after the wait was cut short", got)
	}
}

// TestLoop_RateLimitWaitCancelled reports a cancelled context as such, so a run
// torn down mid-wait doesn't look like it finished cleanly.
func TestLoop_RateLimitWaitCancelled(t *testing.T) {
	dir := t.TempDir()
	scriptPath, _ := rateLimitScript(t, dir, time.Now().Add(time.Hour))

	l := NewLoopWithWorkDir(filepath.Join(dir, "prd.md"), dir, "test", 5, &mockProvider{cliPath: scriptPath})
	l.SetWatchdogTimeout(0)

	ctx, cancel := context.WithCancel(context.Background())
	l.rateLimitSleep = func(context.Context, time.Duration) bool {
		cancel()
		return false
	}

	if err := l.runIterationWithRetry(ctx, modeBuild); err != context.Canceled {
		t.Errorf("returned %v, want context.Canceled", err)
	}
}

// TestLoop_RateLimitWaitsAreCapped stops a run that keeps meeting a wall: after
// maxRateLimitWaits windows the limit is reported as the error it has become,
// instead of the loop sleeping through another day of them.
func TestLoop_RateLimitWaitsAreCapped(t *testing.T) {
	l := NewLoop("/test/prd.md", "test", 5, testProvider)
	l.rateLimit = RateLimitInfo{Status: rateLimitReject, ResetsAt: time.Now().Add(time.Hour)}

	for i := 0; i < maxRateLimitWaits; i++ {
		if _, _, ok := l.rateLimitPause(); !ok {
			t.Fatalf("wait %d was refused, want it allowed", i+1)
		}
		// Each iteration's own rejection replaces the one the pause consumed.
		l.rateLimit = RateLimitInfo{Status: rateLimitReject, ResetsAt: time.Now().Add(time.Hour)}
	}

	if _, _, ok := l.rateLimitPause(); ok {
		t.Errorf("wait %d was allowed, want the cap to stop it", maxRateLimitWaits+1)
	}
}

// TestLoop_RateLimitPauseConsumesReport makes sure a stale rejection can't send
// the loop to sleep again: whether the limit is still there is answered by the
// next iteration's own events.
func TestLoop_RateLimitPauseConsumesReport(t *testing.T) {
	l := NewLoop("/test/prd.md", "test", 5, testProvider)
	l.rateLimit = RateLimitInfo{Status: rateLimitReject, ResetsAt: time.Now().Add(time.Hour)}

	if _, _, ok := l.rateLimitPause(); !ok {
		t.Fatal("first pause was refused")
	}
	if _, _, ok := l.rateLimitPause(); ok {
		t.Error("second pause used the same report again")
	}
}

// TestLoop_RateLimitNotWaitedWhenRetriesDisabled honours --no-retry: it means
// "don't carry on by yourself", which covers waiting out a window too.
func TestLoop_RateLimitNotWaitedWhenRetriesDisabled(t *testing.T) {
	dir := t.TempDir()
	scriptPath, runs := rateLimitScript(t, dir, time.Now().Add(time.Hour))

	l := NewLoopWithWorkDir(filepath.Join(dir, "prd.md"), dir, "test", 5, &mockProvider{cliPath: scriptPath})
	l.SetWatchdogTimeout(0)
	l.DisableRetry()
	l.rateLimitSleep = func(context.Context, time.Duration) bool {
		t.Error("waited for the window with retries disabled")
		return true
	}

	if err := l.runIterationWithRetry(context.Background(), modeBuild); err == nil {
		t.Fatal("expected the rate-limited run to fail")
	}
	if got := runs(); got != 1 {
		t.Errorf("agent was invoked %d times, want 1", got)
	}
}

// TestFormatWait describes the wait the way the activity line shows it: when the
// run resumes, and how long that is away.
func TestFormatWait(t *testing.T) {
	now := time.Date(2026, 9, 11, 18, 27, 0, 0, time.Local)
	until := time.Date(2026, 9, 11, 20, 0, 0, 0, time.Local)
	info := RateLimitInfo{Status: rateLimitReject, WindowType: "five_hour", ResetsAt: until}

	got := info.FormatWait(until, now)
	for _, want := range []string{"Rate limit reached", "5h window", "20:00", "1h 33m"} {
		if !strings.Contains(got, want) {
			t.Errorf("FormatWait() = %q, want it to contain %q", got, want)
		}
	}
}
