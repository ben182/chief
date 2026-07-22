package loop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/prd"
)

// openRunLog opens this run's per-run log file in the PRD directory and records
// its path on the Loop. The name carries a timestamp (e.g.
// claude-2006-01-02-150405.log) so each run keeps its own log next to its own
// summary-<time>.md, instead of every run appending to one file that grows
// without bound and mixes runs together. The caller owns closing l.logFile.
func (l *Loop) openRunLog() error {
	prdDir := filepath.Dir(l.prdPath)
	git.IgnoreLogsIn(prdDir)
	logPath := filepath.Join(prdDir, timestampedLogName(l.provider.LogFileName(), time.Now()))
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	l.logFile = f
	l.mu.Lock()
	l.logPath = logPath
	l.mu.Unlock()
	return nil
}

// finalizeStory resolves the outcome of the story attempted in this iteration.
//
// If the agent emitted <chief-done/> and a matching commit actually landed, the
// story is marked done in prd.md (after an optional review pass). Otherwise the
// story did not complete: the attempt is counted and, once the per-story limit is
// hit, the story is parked for human review so the loop can move on to other
// unblocked stories instead of retrying forever.
//
// It returns a non-nil error only when a source-of-truth write to prd.md fails,
// which the caller uses to stop the whole run (see setStatusOrFail).
func (l *Loop) finalizeStory(ctx context.Context, currentIter int) error {
	l.mu.Lock()
	saw := l.sawStoryDone
	storyID := l.currentStoryID
	storyTitle := l.currentStoryTitle
	l.sawStoryDone = false
	l.mu.Unlock()

	// A <chief-done/> signal is only trusted if a matching commit actually landed.
	// Otherwise the agent claimed done but produced no committed work (forgot to
	// commit, hook rejected, crash before commit): the next fresh-context iteration
	// would move on and the change could be lost. Treat that as a failed attempt so
	// the story is retried or eventually parked.
	if saw && storyID != "" && !l.storyHasCommit(storyID, storyTitle) {
		saw = false
		l.events <- Event{
			Type:      EventStoryNoCommit,
			Iteration: currentIter,
			StoryID:   storyID,
			Text:      fmt.Sprintf("Story %s signalled done but no commit was found; treating as incomplete", storyID),
		}
	}

	switch {
	case saw && storyID != "":
		// The build agent finished and committed. Before marking the story done,
		// run a separate review agent (fresh context) that reviews and fixes the
		// committed changes. It is a best-effort quality gate: a review crash is
		// logged but does not un-complete the story.
		if l.reviewEnabled() {
			l.runReview(ctx, currentIter, storyID, storyTitle)
		}
		if err := l.setStatusOrFail(currentIter, storyID, "done",
			fmt.Sprintf("failed to mark story %s done in prd.md", storyID)); err != nil {
			return err
		}
		l.commitStoryProgress(storyID, storyTitle)

	case storyID != "":
		l.mu.Lock()
		l.attempts[storyID]++
		attempts := l.attempts[storyID]
		maxAttempts := l.maxAttempts
		l.mu.Unlock()
		if maxAttempts > 0 && attempts >= maxAttempts {
			if err := l.setStatusOrFail(currentIter, storyID, "needs-review",
				fmt.Sprintf("failed to park story %s for review in prd.md", storyID)); err != nil {
				return err
			}
			l.events <- Event{
				Type:      EventStoryNeedsReview,
				Iteration: currentIter,
				StoryID:   storyID,
				Text:      fmt.Sprintf("Story %s failed %d times, parked for human review", storyID, attempts),
			}
		}
	}
	return nil
}

// setStatusOrFail writes status to the story in prd.md, which is the source of
// truth for what's done. A failed write can't be swallowed: it would leave the
// story actionable and the next fresh-context iteration would pick the same story
// again and loop forever. On failure it logs, emits EventError, and returns the
// error so the run can stop and the user can fix the cause (e.g. read-only FS)
// instead of burning iterations silently. failMsg is the already-formatted
// "failed to ..." prefix for the surfaced error.
func (l *Loop) setStatusOrFail(iteration int, storyID, status, failMsg string) error {
	if err := prd.SetStoryStatus(l.prdPath, storyID, status); err != nil {
		werr := fmt.Errorf("%s: %w", failMsg, err)
		l.logLine("[chief] " + werr.Error())
		l.events <- Event{Type: EventError, Iteration: iteration, StoryID: storyID, Err: werr}
		return werr
	}
	return nil
}
