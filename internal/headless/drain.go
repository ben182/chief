package headless

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ben182/chief/internal/loop"
)

// stateCheckInterval is how often the run is asked whether it is over.
//
// The manager's event channel is never closed — it is shared by every PRD a
// manager runs, so no single run's end can close it — and a loop that stops
// quietly emits nothing to say so. Polling the instance state is what turns
// "no more events" into "finished", and a second of latency at the end of a run
// measured in hours costs nothing.
const stateCheckInterval = time.Second

// drain reads the run's events until the loop stops, writing each one worth
// keeping to the log, and returns the state the run ended in together with what
// it spent.
//
// It keeps reading after the state says the run is over: the manager sets the
// final state before its forwarding goroutine has finished draining the loop's
// channel, so the last few events — the story that just landed, the final
// result — are still in flight at that moment, and stopping on the state alone
// would cut the log off just short of the ending.
//
// pusher, when there is one, is told about every story that ends.
func drain(ctx context.Context, log *logger, manager *loop.Manager, name string, pusher *storyPusher) (loop.LoopState, float64) {
	ticker := time.NewTicker(stateCheckInterval)
	defer ticker.Stop()

	var cost float64
	// story is the story the run is currently on, so an iteration that stays on
	// the same one is not announced again.
	story := ""

	for {
		select {
		case <-ctx.Done():
			// The run was interrupted. Stopping the loop kills the agent; the
			// commits it already made stay on the branch.
			log.event("run", "interrupt received — stopping the agent")
			_ = manager.Stop(name)
			state, _, _ := manager.GetState(name)
			cost += flush(log, manager, &story, cost)
			return state, cost

		case ev := <-manager.Events():
			if ev.PRDName != name {
				continue
			}
			cost += ev.Event.Cost
			report(log, ev.Event, &story)
			reportSpent(log, ev.Event, cost)
			if storyEnded(ev.Event) {
				pusher.storyEnded(orCurrent(ev.Event.StoryID, story))
			}

		case <-ticker.C:
			state, _, err := manager.GetState(name)
			if err != nil {
				return loop.LoopStateError, cost
			}
			if state == loop.LoopStateRunning {
				continue
			}
			// The loop has stopped. Take whatever is still queued before
			// reporting the ending, so the log ends where the run did.
			cost += flush(log, manager, &story, cost)
			return state, cost
		}
	}
}

// flush reports the events already queued and returns what they cost, without
// waiting for any more. It is what the end of a run and an interrupt both need:
// the channel has a backlog and nobody is going to fill it further. spent is
// what the run had cost before the backlog, so the running total stays right.
func flush(log *logger, manager *loop.Manager, story *string, spent float64) float64 {
	var cost float64
	for {
		select {
		case ev := <-manager.Events():
			if ev.PRDName == "" {
				continue
			}
			cost += ev.Event.Cost
			report(log, ev.Event, story)
			reportSpent(log, ev.Event, spent+cost)
		default:
			return cost
		}
	}
}

// storyEnded says whether ev closes a story, done or parked. Either way its
// commits are final, which is what a push after it is for.
func storyEnded(ev loop.Event) bool {
	return ev.Type == loop.EventStoryDone || ev.Type == loop.EventStoryNeedsReview
}

// reportSpent writes what the run has cost so far each time a story ends. The
// cost arrives a message at a time, so a line per event would bury the log;
// once per story is often enough for `chief box status` to read a running total
// off the journal while the run is still going.
func reportSpent(log *logger, ev loop.Event, spent float64) {
	if storyEnded(ev) {
		log.event("cost", "$%.2f so far", spent)
	}
}

// report writes one event to the log. Which events earn a line and which only
// show up under --verbose is the whole design of this file: a five-hour run
// emits tens of thousands of events, almost all of them the agent reading a
// file, and a log that keeps all of them is one nobody reads. What is left is
// the run's skeleton — which story, how it ended, what it cost, what went wrong
// — which is what someone reconnecting over SSH actually needs.
func report(log *logger, ev loop.Event, story *string) {
	switch ev.Type {
	case loop.EventIterationStart:
		// Iterations are the loop's own rhythm, not the user's: several of them
		// go into one story. The story changing is the event worth a line.
		if ev.StoryID != "" && ev.StoryID != *story {
			*story = ev.StoryID
			log.event("story", "%s started (iteration %d)", ev.StoryID, ev.Iteration)
		} else {
			log.detail("iteration", "%d", ev.Iteration)
		}

	case loop.EventStoryDone:
		log.event("story", "%s done", orCurrent(ev.StoryID, *story))

	case loop.EventStoryNeedsReview:
		log.event("story", "%s parked for human review after too many failed attempts",
			orCurrent(ev.StoryID, *story))

	case loop.EventStoryNoCommit:
		log.event("story", "%s claimed done but committed nothing — retrying",
			orCurrent(ev.StoryID, *story))

	case loop.EventReviewStart:
		log.event("review", "reviewing %s", orCurrent(ev.StoryID, *story))

	case loop.EventReviewDone:
		log.event("review", "%s reviewed", orCurrent(ev.StoryID, *story))

	case loop.EventConsolidateStart:
		log.event("consolidate", "refactoring across this run's commits")

	case loop.EventConsolidateDone:
		log.event("consolidate", "done")

	case loop.EventRetrying:
		log.event("retry", "agent crashed, attempt %d of %d", ev.RetryCount, ev.RetryMax)
		for _, line := range ev.CrashLog {
			log.event("retry", "  %s", line)
		}

	case loop.EventWatchdogTimeout:
		log.event("watchdog", "agent went silent and was killed")

	case loop.EventRateLimitWait:
		log.event("ratelimit", "%s", rateLimitWait(ev))

	case loop.EventNoGitRepo:
		log.event("git", "not a git repository — progress is not persisted between iterations")

	case loop.EventError:
		if ev.Err != nil {
			kind := "error"
			if ev.Fatal {
				kind = "fatal"
			}
			log.event(kind, "%v", ev.Err)
		}

	case loop.EventComplete:
		log.event("run", "all stories resolved")

	case loop.EventMaxIterationsReached:
		log.event("run", "max iterations reached with stories left")

	case loop.EventResult:
		if ev.Cost > 0 {
			log.detail("result", "iteration %d · $%.4f", ev.Iteration, ev.Cost)
		}

	case loop.EventAssistantText:
		log.detail("agent", "%s", collapse(ev.Text))

	case loop.EventToolStart:
		log.detail("tool", "%s", toolLine(ev))
	}
}

// orCurrent falls back to the story the run is on when an event does not name
// one, so a line never reads "story  done".
func orCurrent(id, current string) string {
	if id != "" {
		return id
	}
	if current != "" {
		return current
	}
	return "story"
}

// rateLimitWait describes the pause the loop is taking, including when it
// expects to carry on — the one thing someone looking at a stalled log wants to
// know.
func rateLimitWait(ev loop.Event) string {
	if ev.RateLimit == nil || ev.RateLimit.ResetsAt.IsZero() {
		return "usage limit reached — waiting for the window to reset"
	}
	resets := ev.RateLimit.ResetsAt
	return fmt.Sprintf("usage limit reached — waiting until %s (%s)",
		resets.Format("15:04"), round(time.Until(resets)))
}

// toolLine renders a tool call as one line: the tool, and the argument that
// says what it was pointed at.
func toolLine(ev loop.Event) string {
	for _, key := range []string{"file_path", "path", "command", "pattern", "url", "prompt"} {
		if v, ok := ev.ToolInput[key].(string); ok && strings.TrimSpace(v) != "" {
			return ev.Tool + " " + collapse(v)
		}
	}
	return ev.Tool
}

// maxDetailLine caps how much of an agent's paragraph or a tool's argument
// reaches the log. The full text is in the run's own log file; this is the
// glance-able version.
const maxDetailLine = 200

// collapse folds a multi-line value onto one line and truncates it, so a log
// meant to be skimmed is not taken over by a heredoc someone piped into bash.
func collapse(s string) string {
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	if len(s) <= maxDetailLine {
		return s
	}
	return s[:maxDetailLine] + "…"
}
