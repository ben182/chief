package tui

import (
	"time"

	"github.com/ben182/chief/internal/prd"
)

// finalizeStoryTiming records the duration of the story currently tracked for
// the given PRD. No-op if no story is being timed for that PRD.
func (a *App) finalizeStoryTiming(prdName string) {
	id := a.currentStoryID[prdName]
	if id == "" {
		return
	}
	duration := time.Since(a.currentStoryStart[prdName])
	title := id
	// The story title is only resolvable for the viewed PRD (a.prd); background
	// PRDs fall back to the ID. Titles are only shown on the completion screen,
	// which always belongs to the current PRD.
	if prdName == a.prdName {
		for _, story := range a.prd.UserStories {
			if story.ID == id {
				title = story.Title
				break
			}
		}
	}
	timing := StoryTiming{
		StoryID:  id,
		Title:    title,
		Duration: duration,
		Cost:     a.currentStoryCost[prdName],
		Tokens:   a.currentStoryTokens[prdName],
	}

	// Replace any earlier timing for this story (e.g. one restored from disk, or
	// a re-run after needs-review) so the ETA average reflects the latest run.
	replaced := false
	for i := range a.storyTimings[prdName] {
		if a.storyTimings[prdName][i].StoryID == id {
			a.storyTimings[prdName][i] = timing
			replaced = true
			break
		}
	}
	if !replaced {
		a.storyTimings[prdName] = append(a.storyTimings[prdName], timing)
	}

	// Persist so the ETA survives a restart or interruption.
	if prdPath := a.prdPathForPRD(prdName); prdPath != "" {
		_ = prd.AppendTiming(prd.ProgressPath(prdPath), prd.Timing{
			StoryID:           timing.StoryID,
			DurationMS:        timing.Duration.Milliseconds(),
			Cost:              timing.Cost,
			TokensIn:          timing.Tokens.Input,
			TokensOut:         timing.Tokens.Output,
			TokensCacheCreate: timing.Tokens.CacheCreation,
			TokensCacheRead:   timing.Tokens.CacheRead,
		})
	}

	a.currentStoryID[prdName] = ""
	a.currentStoryStart[prdName] = time.Time{}
	a.currentStoryCost[prdName] = 0
	a.currentStoryTokens[prdName] = TokenUsage{}
}

// loadPersistedTimings reconstructs a PRD's story timings from progress.md so
// the ETA is available immediately after a restart or interruption. Titles are
// only resolvable when the PRD's stories are known (the viewed PRD); others
// fall back to the story ID.
func loadPersistedTimings(prdPath string, stories []prd.UserStory) []StoryTiming {
	if prdPath == "" {
		return nil
	}
	records, err := prd.ParseTimings(prd.ProgressPath(prdPath))
	if err != nil || len(records) == 0 {
		return nil
	}
	titles := make(map[string]string, len(stories))
	for _, s := range stories {
		titles[s.ID] = s.Title
	}
	out := make([]StoryTiming, 0, len(records))
	for _, r := range records {
		title := r.StoryID
		if t, ok := titles[r.StoryID]; ok && t != "" {
			title = t
		}
		out = append(out, StoryTiming{
			StoryID:  r.StoryID,
			Title:    title,
			Duration: time.Duration(r.DurationMS) * time.Millisecond,
			Cost:     r.Cost,
			Tokens: TokenUsage{
				Input:         r.TokensIn,
				Output:        r.TokensOut,
				CacheCreation: r.TokensCacheCreate,
				CacheRead:     r.TokensCacheRead,
			},
		})
	}
	return out
}

// GetElapsedTime returns the elapsed time since the loop started.
func (a *App) GetElapsedTime() time.Duration {
	if a.startTime.IsZero() {
		return 0
	}
	return time.Since(a.startTime)
}

// runProgress returns the completed and total story counts for the current
// run, excluding stories that were already passing when the run started (e.g.
// follow-up stories appended to a finished PRD). Before any run has started
// this session runBaselineDone is nil, so the whole PRD is counted.
func (a *App) runProgress() (completed, total int) {
	for _, s := range a.prd.UserStories {
		if a.runBaselineDone[s.ID] {
			continue
		}
		total++
		if s.Passes {
			completed++
		}
	}
	return completed, total
}

// GetCompletionPercentage returns the percentage of completed stories for the
// current run.
func (a *App) GetCompletionPercentage() float64 {
	completed, total := a.runProgress()
	if total == 0 {
		return 100.0
	}
	return float64(completed) / float64(total) * 100.0
}

// minTimingsForETA is how many completed stories are needed before showing an
// ETA. The very first story is unrepresentative (codebase exploration, pattern
// establishment), so we skip it and start estimating from the second — waiting
// for three meant small PRDs finished before an ETA ever appeared.
const minTimingsForETA = 2

// GetETA estimates the time remaining to complete the PRD from observed
// per-story velocity. Returns (eta, true) once enough stories have finished for
// the estimate to be meaningful. Per-story durations already exclude idle time
// between sessions, so overnight gaps don't skew the average.
func (a *App) GetETA() (time.Duration, bool) {
	timings := a.storyTimings[a.prdName]
	if len(timings) < minTimingsForETA {
		return 0, false
	}
	remaining := 0
	for _, s := range a.prd.UserStories {
		if !s.Passes {
			remaining++
		}
	}
	if remaining == 0 {
		return 0, false
	}
	var total time.Duration
	for _, t := range timings {
		total += t.Duration
	}
	// ponytail: plain mean; switch to a recency-weighted average if early
	// exploration stories skew the estimate too high in practice.
	avg := total / time.Duration(len(timings))
	return avg * time.Duration(remaining), true
}
