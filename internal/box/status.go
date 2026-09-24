package box

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ben182/chief/internal/prd"
)

// statusView is everything `chief box status` shows, gathered first and drawn
// second, so the drawing can be tested without a box.
type statusView struct {
	Box State
	// Now is the time on this machine, which is what "done around" is in.
	Now     time.Time
	Running bool
	// Unit is what systemd said about the run: is-active, then Result.
	Unit string
	// PRD is the run's own copy of the PRD, or nil when it could not be read.
	PRD *prd.PRD
	Log runLog
	// Tail is the end of the log, fetched only for a run that has ended.
	Tail string
}

// storyEvent is one story starting or ending, as the run logged it.
type storyEvent struct {
	At   time.Time
	ID   string
	Kind string // "started", "done" or "parked"
}

// runLog is what the journal says about the run's stories and its spending.
type runLog struct {
	// BoxNow is the box's clock when it was asked. Durations are measured
	// against it rather than against this machine's, so a clock that is off
	// here does not make the current story look older or younger than it is.
	BoxNow time.Time
	Events []storyEvent
	// AgentUSD is the last running total the run logged, as written. Empty
	// before the first story ends, and always for a box started by a chief
	// that did not log one.
	AgentUSD string
}

// storyEventsProbe prints the box's clock, then every line this run of the unit
// logged about a story starting or ending or about what it has cost, stamped
// with when the journal got it. Only the current invocation counts: a retried
// run on the same box starts its own clock.
func storyEventsProbe(unit string) string {
	return "date +%s; journalctl _SYSTEMD_INVOCATION_ID=$(systemctl show " + unit + " -p InvocationID --value)" +
		" --no-hostname -o short-unix | grep -E ' (story +[^ ]+ (started|done|parked)|cost +\\$|run +.* stories, \\$)'"
}

// storyEventRegex reads one line of storyEventsProbe: the journal's unix time,
// then chief's own stamp and "story", then the ID and what happened to it.
var storyEventRegex = regexp.MustCompile(`^(\d+)(?:\.\d+)?\s.*\sstory\s+(\S+) (started|done|parked)\b`)

// agentSpentRegex finds the agent's running total in a journal line: the
// "cost" line written each time a story ends, or the run's closing line.
var agentSpentRegex = regexp.MustCompile(`\s(?:cost\s+\$([0-9.]+) so far|run\s+.* stories, \$([0-9.]+))`)

func parseRunLog(journal string) runLog {
	var l runLog
	lines := strings.Split(strings.TrimSpace(journal), "\n")
	if now, err := strconv.ParseInt(strings.TrimSpace(lines[0]), 10, 64); err == nil {
		l.BoxNow = time.Unix(now, 0)
	}
	for _, line := range lines[1:] {
		if m := storyEventRegex.FindStringSubmatch(line); m != nil {
			at, _ := strconv.ParseInt(m[1], 10, 64)
			l.Events = append(l.Events, storyEvent{At: time.Unix(at, 0), ID: m[2], Kind: m[3]})
			continue
		}
		if m := agentSpentRegex.FindStringSubmatch(line); m != nil {
			l.AgentUSD = m[1] + m[2]
		}
	}
	return l
}

// finishedStory is a story the run is done with, and how long it took.
type finishedStory struct {
	ID     string
	Took   time.Duration
	Ended  time.Time
	Parked bool
}

// finished lists the stories the run has ended, oldest first. A story's time
// runs from the line that said it started; one that has none — the loop only
// announces a story when it changes — is timed from whatever came before it.
func (l runLog) finished() []finishedStory {
	var out []finishedStory
	started := map[string]time.Time{}
	var prev time.Time
	for _, e := range l.Events {
		if e.Kind == "started" {
			started[e.ID] = e.At
			prev = e.At
			continue
		}
		from, ok := started[e.ID]
		if !ok {
			from = prev
		}
		out = append(out, finishedStory{ID: e.ID, Took: e.At.Sub(from), Ended: e.At, Parked: e.Kind == "parked"})
		delete(started, e.ID)
		prev = e.At
	}
	return out
}

// current is the story the run is on and when it started on it, or false when
// the last thing the run did was finish one.
func (l runLog) current() (storyEvent, bool) {
	if n := len(l.Events); n > 0 && l.Events[n-1].Kind == "started" {
		return l.Events[n-1], true
	}
	return storyEvent{}, false
}

// estimate is the time the run still needs for left stories, extrapolated from
// how long the finished ones took.
//
// The clock starts at the first story, not at the unit: provisioning and
// worktree setup happen once and say nothing about the next story. Parked
// stories count as finished — the time went into them all the same. What the
// current story has already taken is subtracted, so the estimate goes down
// while a story is being worked on rather than only in steps.
//
// There is no estimate before the first story has ended; one story's worth of
// history is a guess, and none is not even that.
func (l runLog) estimate(left int) (remaining, per time.Duration, ok bool) {
	done := l.finished()
	if left == 0 || len(done) == 0 || l.BoxNow.IsZero() {
		return 0, 0, false
	}
	first, last := l.Events[0].At, done[len(done)-1].Ended
	per = last.Sub(first) / time.Duration(len(done))
	remaining = per*time.Duration(left) - l.BoxNow.Sub(last)
	return max(remaining, 0), per, true
}

// storiesLeft is how many stories the run still has to work through. A parked
// story is not one of them: the loop skips it until a person has looked.
func storiesLeft(p *prd.PRD) int {
	n := 0
	for _, st := range p.UserStories {
		if !st.Passes && !st.NeedsReview {
			n++
		}
	}
	return n
}

// progressWidth is how many cells the bar has.
const progressWidth = 30

// recentStories is how many finished stories the status lists.
const recentStories = 5

// renderStatus draws the status as a block of labelled rows, one thing per
// row, so the eye finds "how far" and "how long" without reading sentences.
func renderStatus(out io.Writer, v statusView) {
	s := v.Box
	w := func(format string, args ...any) { _, _ = fmt.Fprintf(out, format+"\n", args...) }
	row := func(label, format string, args ...any) {
		w("  %-10s %s", label, fmt.Sprintf(format, args...))
	}
	more := func(format string, args ...any) { row("", format, args...) }

	w("%s", s.Name)
	facts := []string{runState(v), FormatAge(v.Now.Sub(s.Created))}
	if s.Type != "" {
		facts = append(facts, s.Type+" in "+s.Location)
	}
	facts = append(facts, s.IP, "PRD "+s.PRD)
	w("  %s", strings.Join(facts, " · "))
	w("")

	if p := v.PRD; p != nil {
		total, done := len(p.UserStories), p.CompletedCount()
		filled := done * progressWidth / total
		bar := strings.Repeat("█", filled) + strings.Repeat("░", progressWidth-filled)
		line := fmt.Sprintf("%s  %d/%d stories · %d%%", bar, done, total, done*100/total)
		if parked := countParked(p); parked > 0 {
			line += fmt.Sprintf(" · %d parked for review", parked)
		}
		row("Progress", "%s", line)
	}

	if v.waiting() {
		row("Now", "waiting — the box starts the run at %s, in %s",
			clockAt(v.Now, s.StartAt), roundMinutes(s.StartAt.Sub(v.Now)))
	}

	if v.Running {
		if cur, ok := v.Log.current(); ok {
			row("Now", "%s  %s", cur.ID, clip(titleOf(v.PRD, cur.ID), 64))
			if !v.Log.BoxNow.IsZero() {
				more("for %s, since %s", roundMinutes(v.Log.BoxNow.Sub(cur.At)), cur.At.In(v.Now.Location()).Format("15:04"))
			}
		} else if len(v.Log.Events) == 0 {
			row("Now", "setting up — no story started yet")
		}
		if v.PRD != nil {
			left := storiesLeft(v.PRD)
			if remaining, per, ok := v.Log.estimate(left); ok {
				if remaining < time.Minute {
					row("Left", "any minute now")
				} else {
					row("Left", "about %s — done around %s", roundMinutes(remaining), clockAt(v.Now, v.Now.Add(remaining)))
				}
				more("≈%s per story, %s to go", roundMinutes(per), plural(left, "story", "stories"))
			} else if left > 0 {
				row("Left", "no estimate until the first story is done")
			}
		}
	}

	machine := "machine cost unknown"
	if s.HourlyEUR > 0 {
		machine = "machine " + FormatEUR(s.HourlyEUR*v.Now.Sub(s.Created).Hours())
	}
	if v.Log.AgentUSD != "" {
		// Kept apart from the machine: euros to Hetzner, and dollars of tokens
		// that a subscription may already be paying for.
		row("Cost", "%s · agent $%s", machine, v.Log.AgentUSD)
	} else {
		row("Cost", "%s", machine)
	}

	if done := v.Log.finished(); len(done) > 0 {
		w("")
		label := "Recent"
		for i := len(done) - 1; i >= 0 && i >= len(done)-recentStories; i-- {
			f := done[i]
			how := "done"
			if f.Parked {
				how = "parked"
			}
			row(label, "%-10s %4s   %s %s", f.ID, roundMinutes(f.Took), how, f.Ended.In(v.Now.Location()).Format("15:04"))
			label = ""
		}
	}

	if !v.Running && !v.waiting() && strings.TrimSpace(v.Tail) != "" {
		w("")
		w("  Log")
		for _, line := range strings.Split(strings.TrimRight(v.Tail, "\n"), "\n") {
			w("    %s", line)
		}
	}
}

// waiting reports a run that was scheduled with --at and has not started yet.
// systemd calls that "inactive", which is also what it calls a run that is
// over, so the box's own record is what tells the two apart.
func (v statusView) waiting() bool {
	return !v.Running && v.Now.Before(v.Box.StartAt)
}

// runState is the run's state in a word or two. systemd's own words are kept
// for a run that has ended, because they are what explains how.
func runState(v statusView) string {
	if v.Running {
		return "running"
	}
	if v.waiting() {
		return "starts at " + clockAt(v.Now, v.Box.StartAt)
	}
	lines := strings.Fields(v.Unit)
	switch len(lines) {
	case 0:
		return "state unknown"
	case 1:
		return lines[0]
	default:
		return lines[0] + " (" + lines[1] + ")"
	}
}

func countParked(p *prd.PRD) int {
	n := 0
	for _, st := range p.UserStories {
		if st.NeedsReview {
			n++
		}
	}
	return n
}

// titleOf is a story's title, or nothing when the PRD cannot say.
func titleOf(p *prd.PRD, id string) string {
	if p == nil {
		return ""
	}
	for _, st := range p.UserStories {
		if st.ID == id {
			return st.Title
		}
	}
	return ""
}

// clip shortens s to n characters, so a long story title stays on its row.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// clockAt is a moment the way you would say it today: "23:40", "05:10
// tomorrow", and a date only once it is further out than that.
func clockAt(now, t time.Time) string {
	t = t.In(now.Location())
	day := func(x time.Time) time.Time { y, m, d := x.Date(); return time.Date(y, m, d, 0, 0, 0, 0, x.Location()) }
	switch day(t).Sub(day(now)).Hours() / 24 {
	case 0:
		return t.Format("15:04")
	case 1:
		return t.Format("15:04") + " tomorrow"
	default:
		return t.Format("Mon 2 Jan 15:04")
	}
}

// roundMinutes prints a duration the way a person estimates one: "2h10m",
// "35m", never seconds.
func roundMinutes(d time.Duration) string {
	d = d.Round(time.Minute)
	if d < time.Minute {
		return "<1m"
	}
	h, m := int(d.Hours()), int(d.Minutes())%60
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", m)
	case m == 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dh%dm", h, m)
	}
}
