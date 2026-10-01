package loop

import (
	"time"

	"github.com/ben182/chief/internal/prd"
)

// SetSleptFn hands the manager a way to ask how long the machine slept since a
// moment, so a session's record says so. Without one, sleep is recorded as zero.
func (m *Manager) SetSleptFn(fn func(since time.Time) time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sleptFn = fn
}

// RunTotals reports what a PRD's run has spent so far: the earlier sessions of
// a resumed run plus this one. It is what the end of a run should report — the
// whole run, not just the session that happened to finish it. Zero for a PRD
// that has not been started in this process.
func (m *Manager) RunTotals(name string) prd.RunSession {
	// The live instance, not GetInstance's copy: the session's cost and end and
	// the loop's rate-limit wait are not part of the copy.
	instance, err := m.lookup(name)
	if err != nil {
		return prd.RunSession{}
	}
	session := m.currentSession(instance)
	instance.mu.Lock()
	prior := instance.RunPrior
	instance.mu.Unlock()
	return prior.Add(session)
}

// currentSession is what the instance's latest session has spent: up to now
// while it runs, up to its end once it has stopped. Zero before any start.
func (m *Manager) currentSession(instance *LoopInstance) prd.RunSession {
	m.mu.RLock()
	sleptFn := m.sleptFn
	m.mu.RUnlock()

	instance.mu.Lock()
	start, end, cost, l := instance.StartTime, instance.sessionEnd, instance.sessionCost, instance.Loop
	instance.mu.Unlock()
	if start.IsZero() {
		return prd.RunSession{}
	}
	if end.IsZero() {
		end = time.Now()
	}
	s := prd.RunSession{
		// The start instant names the session: each start is a new one, and
		// re-recording the same session replaces its earlier record.
		ID:       start.UTC().Format("2006-01-02T15:04:05.000Z"),
		Duration: end.Sub(start),
		Cost:     cost,
	}
	if l != nil {
		s.RateLimitWaited = l.RateLimitWaited()
	}
	if sleptFn != nil {
		s.Slept = sleptFn(start)
	}
	return s
}

// recordSession writes the instance's current session into progress.md, where
// the next session of an unfinished run picks it up. Best-effort: a record that
// does not land costs the run's totals, not the run.
func (m *Manager) recordSession(instance *LoopInstance) {
	instance.mu.Lock()
	path := instance.PRDPath
	instance.mu.Unlock()
	_ = prd.RecordRunSession(prd.ProgressPath(path), m.currentSession(instance))
}
