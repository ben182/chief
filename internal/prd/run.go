package prd

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A run is everything chief does to finish a PRD, from the first start until
// every story is resolved. It often spans several sessions — paused in the
// evening, resumed the next morning, or picked up on the Mac after a box worked
// through the night — and each session is a fresh process that remembers
// nothing. So the run is recorded in progress.md, next to the timing records,
// as three kinds of chief-owned comment:
//
//	<!-- chief-run start_ref="<hash>" -->   a run began at this commit
//	<!-- chief-session id="…" … -->         what one session of it spent
//	<!-- chief-run-end -->                  every story is resolved
//
// The completion screen then describes the run, not the last session, and the
// summary, consolidation and code stats stay scoped to the run's commits.
var (
	runStartRegex   = regexp.MustCompile(`^\s*<!-- chief-run (.*?)\s*-->\s*$`)
	runEndRegex     = regexp.MustCompile(`^\s*<!-- chief-run-end\s*-->\s*$`)
	runSessionRegex = regexp.MustCompile(`^\s*<!-- chief-session (.+?) -->\s*$`)
)

// RunSession is what one session of a run spent. Durations are working time —
// the same monotonic clock as the story timings — so the time a machine slept
// is kept apart in Slept, and RateLimitWaited is the part of Duration spent
// sitting out a usage window.
type RunSession struct {
	ID              string
	Duration        time.Duration
	Slept           time.Duration
	RateLimitWaited time.Duration
	Cost            float64
}

// Add returns the sum of two sessions' figures. The ID is not carried over: a
// sum is no longer one session.
func (s RunSession) Add(o RunSession) RunSession {
	return RunSession{
		Duration:        s.Duration + o.Duration,
		Slept:           s.Slept + o.Slept,
		RateLimitWaited: s.RateLimitWaited + o.RateLimitWaited,
		Cost:            s.Cost + o.Cost,
	}
}

// Run is a run that has not ended yet, as progress.md records it.
type Run struct {
	// StartRef is the commit HEAD was at when the run began. Empty when the
	// branch had no commits yet.
	StartRef string
	// Sessions are the sessions recorded so far, in the order they first
	// appeared; a session recorded more than once keeps its latest figures.
	Sessions []RunSession
}

// Totals sums what every recorded session spent.
func (r Run) Totals() RunSession {
	var t RunSession
	for _, s := range r.Sessions {
		t = t.Add(s)
	}
	return t
}

// OpenRun returns the run progress.md says is still going: the last one begun
// and not ended. ok is false when there is none — no run recorded yet (a fresh
// PRD, or one last run by a chief that did not record runs), or the last one
// finished.
func OpenRun(progressPath string) (run Run, ok bool, err error) {
	f, err := os.Open(progressPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Run{}, false, nil
		}
		return Run{}, false, err
	}
	defer func() { _ = f.Close() }() // read-only: nothing to report

	var index map[string]int
	scanner := bufio.NewScanner(f)
	// Lines in progress.md can run long; a too-small buffer would end the scan
	// early and lose every record after that line.
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case runEndRegex.MatchString(line):
			run, ok = Run{}, false
		case runStartRegex.MatchString(line):
			attrs := runStartRegex.FindStringSubmatch(line)[1]
			run, ok = Run{StartRef: runFields(attrs)["start_ref"]}, true
			index = map[string]int{}
		case ok && runSessionRegex.MatchString(line):
			s, valid := parseRunSession(runSessionRegex.FindStringSubmatch(line)[1])
			if !valid {
				continue
			}
			if i, seen := index[s.ID]; seen {
				run.Sessions[i] = s
			} else {
				index[s.ID] = len(run.Sessions)
				run.Sessions = append(run.Sessions, s)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Run{}, false, err
	}
	return run, ok, nil
}

// BeginRun records that a run starts at startRef.
func BeginRun(progressPath, startRef string) error {
	return appendChiefLine(progressPath, fmt.Sprintf(`<!-- chief-run start_ref=%q -->`, startRef))
}

// EndRun records that the open run is over, so the next start begins a new one.
func EndRun(progressPath string) error {
	return appendChiefLine(progressPath, `<!-- chief-run-end -->`)
}

// RecordRunSession records what a session of the open run has spent so far. It
// is safe to call repeatedly for the same session — after every story, and once
// more when the session ends — because the latest record per ID wins; a session
// that is killed outright then loses at most its last story's worth.
func RecordRunSession(progressPath string, s RunSession) error {
	return appendChiefLine(progressPath, fmt.Sprintf(
		`<!-- chief-session id=%q duration_ms=%d slept_ms=%d waited_ms=%d cost=%.6f -->`,
		s.ID, s.Duration.Milliseconds(), s.Slept.Milliseconds(), s.RateLimitWaited.Milliseconds(), s.Cost))
}

// parseRunSession turns the attributes of a chief-session comment into a
// RunSession; ok is false when it carries no ID.
func parseRunSession(attrs string) (RunSession, bool) {
	f := runFields(attrs)
	ms := func(key string) time.Duration {
		n, _ := strconv.ParseInt(f[key], 10, 64)
		return time.Duration(n) * time.Millisecond
	}
	cost, _ := strconv.ParseFloat(f["cost"], 64)
	s := RunSession{
		ID:              f["id"],
		Duration:        ms("duration_ms"),
		Slept:           ms("slept_ms"),
		RateLimitWaited: ms("waited_ms"),
		Cost:            cost,
	}
	return s, s.ID != ""
}

// runFields reads key=value attributes, unquoting quoted values.
func runFields(attrs string) map[string]string {
	out := map[string]string{}
	for _, m := range timingFieldRegex.FindAllStringSubmatch(attrs, -1) {
		val := m[2]
		if s, err := strconv.Unquote(val); err == nil {
			val = s
		} else {
			val = strings.Trim(val, `"`)
		}
		out[m[1]] = val
	}
	return out
}
