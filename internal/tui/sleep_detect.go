package tui

import "time"

// sleepTracker is what the App needs from the sleep detector: something to poke
// periodically, and an answer about the run just finished.
// internal/awake.SleepTracker is the real implementation.
type sleepTracker interface {
	Sample()
	SleptSince(ref time.Time) time.Duration
}

// sleptDuringRun reports how long the machine was suspended since the current
// run started. It is anchored on startTime — the same instant the "Completed in"
// figure counts from — so the two numbers describe the same window: one the work
// that got done in it, the other the part of it that was lost to sleep.
//
// Zero when no run has started, or when no tracker was wired up.
func (a *App) sleptDuringRun() time.Duration {
	if a.sleepTracker == nil || a.startTime.IsZero() {
		return 0
	}
	return a.sleepTracker.SleptSince(a.startTime)
}
