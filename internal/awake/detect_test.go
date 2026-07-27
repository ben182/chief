package awake

import (
	"testing"
	"time"
)

// fakeClock stands in for the pair of clocks a SleepTracker watches. A test
// cannot suspend the machine it runs on, and a suspend is the only thing that
// makes the wall clock and the monotonic clock disagree — so the disagreement is
// staged here instead.
type fakeClock struct {
	wall time.Time
	mono time.Duration
}

func newFakeClock() *fakeClock {
	return &fakeClock{wall: time.Date(2026, 7, 27, 21, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) read() (time.Time, time.Duration) {
	return c.wall, c.mono
}

// run advances both clocks: ordinary time with the process awake.
func (c *fakeClock) run(d time.Duration) {
	c.wall = c.wall.Add(d)
	c.mono += d
}

// suspend advances only the wall clock: the machine was asleep and the process
// got no monotonic time at all.
func (c *fakeClock) suspend(d time.Duration) {
	c.wall = c.wall.Add(d)
}

// nap models what the tick loop actually sees: a sample, then the machine drops
// out for d, then the pending tick fires a second of working time after waking.
func nap(clock *fakeClock, tracker *SleepTracker, d time.Duration) {
	clock.run(time.Second)
	tracker.Sample()
	clock.suspend(d)
	clock.run(time.Second)
	tracker.Sample()
}

func TestSleepTrackerNoSleep(t *testing.T) {
	clock := newFakeClock()
	start := clock.wall
	tracker := newSleepTracker(clock.read)

	for range 10 {
		clock.run(time.Second)
		tracker.Sample()
	}

	if got := tracker.SleptSince(start); got != 0 {
		t.Errorf("SleptSince(start) = %v, want 0 for a run that never slept", got)
	}
}

func TestSleepTrackerDetectsSuspend(t *testing.T) {
	clock := newFakeClock()
	start := clock.wall
	tracker := newSleepTracker(clock.read)

	nap(clock, tracker, 56*time.Minute)

	if got := tracker.SleptSince(start); got != 56*time.Minute {
		t.Errorf("SleptSince(start) = %v, want 56m", got)
	}
}

func TestSleepTrackerSumsMultipleSuspends(t *testing.T) {
	clock := newFakeClock()
	start := clock.wall
	tracker := newSleepTracker(clock.read)

	for _, d := range []time.Duration{20 * time.Minute, 5 * time.Minute, 31 * time.Minute} {
		nap(clock, tracker, d)
	}

	// 20m + 5m + 31m, worked out independently of the accumulator's own arithmetic.
	if got := tracker.SleptSince(start); got != 56*time.Minute {
		t.Errorf("SleptSince(start) = %v, want 56m across three naps", got)
	}
}

func TestSleepTrackerIgnoresJitter(t *testing.T) {
	clock := newFakeClock()
	start := clock.wall
	tracker := newSleepTracker(clock.read)

	// A busy machine delivering a late tick, or a small clock correction: the two
	// clocks diverge, but not by enough to call it sleep.
	for _, d := range []time.Duration{time.Second, 20 * time.Second, time.Minute} {
		nap(clock, tracker, d)
	}

	if got := tracker.SleptSince(start); got != 0 {
		t.Errorf("SleptSince(start) = %v, want 0 for gaps at or below the one-minute threshold", got)
	}
}

func TestSleepTrackerJustOverThreshold(t *testing.T) {
	clock := newFakeClock()
	start := clock.wall
	tracker := newSleepTracker(clock.read)

	nap(clock, tracker, time.Minute+time.Second)

	if got := tracker.SleptSince(start); got != time.Minute+time.Second {
		t.Errorf("SleptSince(start) = %v, want 1m1s — more than a minute counts", got)
	}
}

// A second run in the same session must not inherit the first run's naps.
func TestSleepTrackerExcludesSleepBeforeRef(t *testing.T) {
	clock := newFakeClock()
	tracker := newSleepTracker(clock.read)

	nap(clock, tracker, 20*time.Minute)
	secondRunStart := clock.wall
	nap(clock, tracker, 36*time.Minute)

	if got := tracker.SleptSince(secondRunStart); got != 36*time.Minute {
		t.Errorf("SleptSince(secondRunStart) = %v, want 36m — the earlier 20m nap predates the run", got)
	}
}

// Sleep from before the run must not land on the run's bill. This is the case
// that makes it tempting to: chief sits idle at the dashboard taking no samples
// at all, the machine dozes for half an hour, and the very first sample after the
// run starts is the one that discovers the gap. Only the sliver of that gap
// falling inside the run may count, and one sampling interval is the most the
// clocks can resolve.
func TestSleepTrackerExcludesSleepDiscoveredAfterRunStart(t *testing.T) {
	clock := newFakeClock()
	tracker := newSleepTracker(clock.read)

	clock.run(time.Second)
	tracker.Sample()
	clock.suspend(30 * time.Minute)
	runStart := clock.wall // the run is kicked off right after waking
	clock.run(time.Second)
	tracker.Sample()

	if got := tracker.SleptSince(runStart); got > time.Second {
		t.Errorf("SleptSince(runStart) = %v, want at most the 1s sampling interval — the 30m nap predates the run", got)
	}
}

// A ref carrying a monotonic reading (anything from time.Now()) has to be
// compared on the wall clock, or the suspend it should include gets dropped.
func TestSleepTrackerRefWithMonotonicReading(t *testing.T) {
	tracker := NewSleepTracker()
	runStart := time.Now()
	tracker.Sample()

	if got := tracker.SleptSince(runStart); got != 0 {
		t.Errorf("SleptSince(now) = %v, want 0 immediately after starting", got)
	}
}

// A nil tracker is the fail-open case for callers built without one.
func TestSleepTrackerNilIsInert(t *testing.T) {
	var tracker *SleepTracker
	tracker.Sample()
	if got := tracker.SleptSince(time.Now()); got != 0 {
		t.Errorf("SleptSince() = %v, want 0 from a nil tracker", got)
	}
}
