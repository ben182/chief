package awake

import (
	"sync"
	"time"
)

// SleepThreshold is the smallest divergence between the two clocks that counts as
// the system having slept. Below a minute the gap is dominated by mundane causes
// — a busy machine delivering a timer late, a small clock correction — and
// reporting those as "the Mac slept" would be noise.
const SleepThreshold = time.Minute

// clockReader reports the current wall-clock time together with the monotonic
// time elapsed since the tracker started.
type clockReader func() (wall time.Time, mono time.Duration)

// sleepPhase is one stretch the process spent frozen: gap long, ending at the
// wall-clock time of the first sample taken after waking.
type sleepPhase struct {
	end time.Time
	gap time.Duration
}

// SleepTracker measures how long the system was suspended.
//
// Everything chief times — story durations, total run time, ETA — is measured
// against the monotonic clock, which stops while the machine is asleep. Those
// numbers are therefore pure working time, and a run can look far shorter than
// the clock on the wall said it took. The gap between the two clocks is exactly
// the missing time, and this is what accounts for it.
//
// The arithmetic is deliberately platform-blind: a suspend freezes the process
// wherever it runs, so unlike the caffeinate helper and the pre-run warning,
// nothing here needs to know which OS it is on.
//
// Nothing is persisted. A restarted chief starts counting from zero, because it
// has no way to attribute a suspend to a run it wasn't watching.
type SleepTracker struct {
	mu     sync.Mutex
	now    clockReader
	wall   time.Time     // wall clock at the previous sample
	mono   time.Duration // monotonic time elapsed at the previous sample
	phases []sleepPhase
}

// NewSleepTracker returns a tracker reading the real clocks, with its first
// sample taken now.
func NewSleepTracker() *SleepTracker {
	start := time.Now()
	return newSleepTracker(func() (time.Time, time.Duration) {
		now := time.Now()
		// Round(0) strips the monotonic reading, so Sub compares wall clocks and
		// picks up the jump that a suspend leaves behind.
		return now.Round(0), now.Sub(start)
	})
}

// newSleepTracker builds a tracker over an arbitrary pair of clocks. The clocks
// are the seam: a test can't stage a real suspend, and a suspend is the only
// thing that makes these two readings disagree.
func newSleepTracker(now clockReader) *SleepTracker {
	t := &SleepTracker{now: now}
	t.wall, t.mono = now()
	return t
}

// Sample records the time since the previous sample. Callers drive this
// periodically; the interval sets how soon a suspend is noticed, not how
// accurately it is measured — the length of the gap comes from the clocks, not
// from how often we look. Gaps at or below SleepThreshold are dropped as jitter.
//
// Safe on a nil tracker, which never reports any sleep.
func (t *SleepTracker) Sample() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	wall, mono := t.now()
	gap := wall.Sub(t.wall) - (mono - t.mono)
	t.wall, t.mono = wall, mono
	if gap > SleepThreshold {
		t.phases = append(t.phases, sleepPhase{end: wall, gap: gap})
	}
}

// SleptSince returns the total time the system spent asleep after ref, summed
// across however many suspends happened. Anchoring on a timestamp rather than
// returning a running total keeps a run's figure to that run: a second run
// started in the same session doesn't inherit the first one's naps, and a suspend
// straddling ref contributes only the part that fell inside the run.
//
// ref is compared on the wall clock, since that is the only clock still running
// while the machine is asleep.
func (t *SleepTracker) SleptSince(ref time.Time) time.Duration {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	ref = ref.Round(0)
	var total time.Duration
	for _, p := range t.phases {
		if !p.end.After(ref) {
			continue
		}
		begin := p.end.Add(-p.gap)
		if begin.Before(ref) {
			begin = ref
		}
		total += p.end.Sub(begin)
	}
	return total
}
