package loop

import (
	"context"
	"fmt"
	"time"
)

// Rate limit statuses as the Claude CLI reports them on its `rate_limit_event`
// lines. "allowed" is the steady state, "allowed_warning" means the window is
// nearly used up, and "rejected" means requests are already being refused.
const (
	rateLimitAllowed = "allowed"
	rateLimitWarning = "allowed_warning"
	rateLimitReject  = "rejected"
)

// RateLimitInfo is the account-level rate limit state the provider reports
// alongside its normal output: how much of the current window is spent, when the
// window resets, and whether requests are already being rejected.
//
// It exists because a rate limit is not a crash. An agent that stops because the
// five-hour window is full looks exactly like an agent that segfaulted — same
// non-zero exit, same empty stderr — and retrying it three times in twenty
// seconds only proves the limit is still there. With the reset time in hand the
// loop can wait the window out, and the UI can warn before it is hit.
type RateLimitInfo struct {
	Status      string    // "allowed", "allowed_warning" or "rejected"
	Utilization float64   // fraction of the window used, 0..1
	ResetsAt    time.Time // when the window resets; zero when not reported
	WindowType  string    // which window this is about, e.g. "five_hour"
}

// Rejected reports whether the provider is currently refusing requests.
func (r RateLimitInfo) Rejected() bool { return r.Status == rateLimitReject }

// Warning reports whether the window is close enough to full that the user
// should know before the run walks into the wall.
func (r RateLimitInfo) Warning() bool { return r.Status == rateLimitWarning }

// Known reports whether this holds a real report rather than a zero value.
func (r RateLimitInfo) Known() bool { return r.Status != "" }

// WaitUntil returns the moment the loop should try again after being rejected,
// and whether waiting makes sense at all. It adds a small grace period because
// the reset time comes from the server's clock, not this machine's, and coming
// back a few seconds early just spends another rejection.
//
// ok is false when the limit isn't actually rejecting, when no reset time was
// reported (nothing to wait for — better to fail loudly than sleep blind), or
// when the reset is already in the past.
func (r RateLimitInfo) WaitUntil(now time.Time, grace time.Duration) (time.Time, bool) {
	if !r.Rejected() || r.ResetsAt.IsZero() {
		return time.Time{}, false
	}
	target := r.ResetsAt.Add(grace)
	if !target.After(now) {
		return time.Time{}, false
	}
	return target, true
}

// rateLimitGrace is added to the reported reset time before the loop resumes,
// covering clock skew between this machine and the provider's.
const rateLimitGrace = 60 * time.Second

// windowLabel turns a window type into something readable in a UI line
// ("five_hour" → "5h window"). Unknown types are passed through unchanged so a
// new window type the CLI invents still shows up instead of vanishing.
func windowLabel(windowType string) string {
	switch windowType {
	case "five_hour":
		return "5h window"
	case "seven_day":
		return "7d window"
	case "":
		return ""
	default:
		return windowType
	}
}

// FormatWait describes the wait in the words the activity line uses: the local
// clock time the run resumes at, plus how long that is from now.
func (r RateLimitInfo) FormatWait(until time.Time, now time.Time) string {
	remaining := until.Sub(now).Round(time.Minute)
	if remaining < time.Minute {
		remaining = time.Minute
	}
	label := windowLabel(r.WindowType)
	if label != "" {
		label = " (" + label + ")"
	}
	return fmt.Sprintf("Rate limit reached%s — waiting until %s (%s)",
		label, until.Format("15:04"), formatWaitDuration(remaining))
}

// formatWaitDuration renders a wait as "1h 33m" / "12m", which reads better in a
// status line than time.Duration's "1h33m0s".
func formatWaitDuration(d time.Duration) string {
	if d < time.Minute {
		d = time.Minute
	}
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// rateLimitEvent is the `rate_limit_event` line the Claude CLI writes whenever
// the account's usage window changes. The window the status refers to is named
// by rateLimitType and repeated under unifiedWindows; the top-level utilization
// is missing on some lines (a rejection carries none), so unifiedWindows is read
// as the fallback rather than reporting a full window as 0% used.
type rateLimitEvent struct {
	Type string `json:"type"`
	Info struct {
		Status      string  `json:"status"`
		ResetsAt    int64   `json:"resetsAt"`
		Utilization float64 `json:"utilization"`
		Type        string  `json:"rateLimitType"`
		Windows     map[string]struct {
			Utilization float64 `json:"utilization"`
			ResetsAt    int64   `json:"resetsAt"`
		} `json:"unifiedWindows"`
	} `json:"rate_limit_info"`
}

// parseRateLimitLine parses a `rate_limit_event` line into an event. It returns
// nil for a line that carries no status, which is the one field everything else
// hangs off.
func parseRateLimitLine(line string) *Event {
	msg, ok := decodeLine[rateLimitEvent](line)
	if !ok || msg.Info.Status == "" {
		return nil
	}
	info := RateLimitInfo{
		Status:      msg.Info.Status,
		Utilization: msg.Info.Utilization,
		WindowType:  msg.Info.Type,
	}
	resetsAt := msg.Info.ResetsAt
	if w, found := msg.Info.Windows[msg.Info.Type]; found {
		if info.Utilization == 0 {
			info.Utilization = w.Utilization
		}
		if resetsAt == 0 {
			resetsAt = w.ResetsAt
		}
	}
	if resetsAt > 0 {
		info.ResetsAt = time.Unix(resetsAt, 0)
	}
	return &Event{Type: EventRateLimit, RateLimit: &info}
}

// maxRateLimitWaits caps how many windows a single run will sit out before it
// gives up and reports the limit as an error. Six five-hour windows is more than
// a day of waiting — past that, a run that was started to be watched has long
// stopped being watched, and failing is more honest than sleeping on.
const maxRateLimitWaits = 6

// rateLimitClock reads the current time through the loop's clock, which tests
// replace to avoid waiting out real windows.
func (l *Loop) rateLimitClock() time.Time {
	if l.rateLimitNow != nil {
		return l.rateLimitNow()
	}
	return time.Now()
}

// grace is the padding added to a reported reset time before resuming.
func (l *Loop) grace() time.Duration {
	if l.rateLimitGrace > 0 {
		return l.rateLimitGrace
	}
	return rateLimitGrace
}

// recordRateLimit stores the provider's latest limit report and reports whether
// it is worth surfacing. Claude sends one on every turn, and a healthy window
// says nothing a user needs — so only warnings, rejections, and the return to
// health after one are passed on. l.mu must be held.
func (l *Loop) recordRateLimit(info *RateLimitInfo) bool {
	if info == nil {
		return false
	}
	previous := l.rateLimit
	l.rateLimit = *info
	if info.Status != rateLimitAllowed {
		return true
	}
	return previous.Known() && previous.Status != rateLimitAllowed
}

// rateLimitPause decides whether the iteration that just failed ran into a rate
// limit rather than crashing, and until when the loop should wait. It consumes
// the stored report either way: whether the limit is still there is a question
// only the next iteration's own events can answer.
func (l *Loop) rateLimitPause() (RateLimitInfo, time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	info := l.rateLimit
	until, ok := info.WaitUntil(l.rateLimitClock(), l.grace())
	if !ok {
		return RateLimitInfo{}, time.Time{}, false
	}
	if l.rateLimitWaits >= maxRateLimitWaits {
		return RateLimitInfo{}, time.Time{}, false
	}
	l.rateLimitWaits++
	l.rateLimit = RateLimitInfo{}
	return info, until, true
}

// waitOutRateLimit announces the wait and sleeps until the window resets. It
// returns false when the wait was cut short — a cancelled context or a stopped
// run — in which case the caller must not start another iteration.
func (l *Loop) waitOutRateLimit(ctx context.Context, info RateLimitInfo, until time.Time) bool {
	now := l.rateLimitClock()

	l.mu.Lock()
	iter := l.iteration
	l.mu.Unlock()

	reported := info
	l.events <- Event{
		Type:      EventRateLimitWait,
		Iteration: iter,
		Text:      info.FormatWait(until, now),
		RateLimit: &reported,
	}

	planned := until.Sub(now)
	if !l.sleepUntilReset(ctx, planned) {
		// Cut short by a stop or a cancelled context. The partial wait is not
		// counted: the run is ending, and the figure exists to explain a finished
		// run's wall clock.
		return false
	}

	l.mu.Lock()
	l.rateLimitWaited += planned
	l.mu.Unlock()
	return true
}

// RateLimitWaited reports how long this run spent waiting out full usage
// windows. It is part of the run's total duration, not additional to it.
func (l *Loop) RateLimitWaited() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rateLimitWaited
}

// sleepUntilReset waits for d, polling once a second so a user who stops the run
// mid-wait doesn't have to sit out the rest of the window. It returns false when
// the wait was interrupted rather than completed.
func (l *Loop) sleepUntilReset(ctx context.Context, d time.Duration) bool {
	if l.rateLimitSleep != nil {
		return l.rateLimitSleep(ctx, d)
	}
	if d <= 0 {
		return true
	}

	deadline := time.Now().Add(d)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			l.mu.Lock()
			stopped := l.stopped
			l.mu.Unlock()
			if stopped {
				return false
			}
			if !time.Now().Before(deadline) {
				return true
			}
		}
	}
}
