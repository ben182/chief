package tui

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/loop"
	"github.com/ben182/chief/internal/notify"
	"github.com/ben182/chief/internal/prd"
	tea "github.com/charmbracelet/bubbletea"
)

// startLoop starts the agent loop for the current PRD.
func (a App) startLoop() (tea.Model, tea.Cmd) {
	return a.startLoopForPRD(a.prdName)
}

// startLoopForPRD starts the agent loop for a specific PRD.
func (a App) startLoopForPRD(prdName string) (tea.Model, tea.Cmd) {
	// Get the PRD directory
	prdDir := prd.PRDDir(a.baseDir, prdName)

	if !git.IsGitRepo(a.baseDir) {
		return a.doStartLoop(prdName, prdDir)
	}

	branch, err := git.GetCurrentBranch(a.baseDir)
	if err != nil {
		return a.doStartLoop(prdName, prdDir)
	}

	worktreePath := git.WorktreePathForPRD(a.baseDir, prdName)
	relWorktreePath := fmt.Sprintf(".chief/worktrees/%s/", prdName)

	// Determine dialog context
	isProtected := git.IsProtectedBranch(branch)
	anotherRunningInSameDir := a.isAnotherPRDRunningInSameDir(prdName)

	if !isProtected && !anotherRunningInSameDir {
		// No conflicts, no dialog. Still give the PRD its own branch unless
		// we're already on it (resume): otherwise a leftover feature branch
		// from a previous PRD silently soaks up this PRD's commits.
		expectedBranch := fmt.Sprintf("chief/%s", prdName)
		if branch != expectedBranch {
			if err := git.CreateBranch(a.baseDir, expectedBranch); err != nil {
				a.lastActivity = "Error creating branch: " + err.Error()
				return a, nil
			}
			a.lastActivity = "Created branch: " + expectedBranch
		}
		return a.doStartLoop(prdName, prdDir)
	}

	var dialogCtx DialogContext
	if isProtected {
		dialogCtx = DialogProtectedBranch
	} else {
		dialogCtx = DialogAnotherPRDRunning
	}

	// Show the dialog only for protected branch or another PRD running
	a.branchWarning.SetSize(a.width, a.height)
	a.branchWarning.SetContext(branch, prdName, relWorktreePath)
	a.branchWarning.SetDialogContext(dialogCtx)
	a.branchWarning.Reset()
	a.pendingStartPRD = prdName
	a.pendingWorktreePath = worktreePath
	a.viewMode = ViewBranchWarning
	return a, nil
}

// isAnotherPRDRunningInSameDir checks if another PRD is running in the project root (no worktree).
func (a *App) isAnotherPRDRunningInSameDir(prdName string) bool {
	if a.manager == nil {
		return false
	}
	return anotherPRDRunsInRoot(a.manager.GetAllInstances(), prdName)
}

// anotherPRDRunsInRoot reports whether any instance other than prdName is
// running in the project root. Two loops committing in the same directory would
// interleave their commits, so a start has to be held back when one is found.
// Split out from the method so it can be exercised without a live manager, whose
// instance getters hand back snapshots.
func anotherPRDRunsInRoot(instances []*loop.LoopInstance, prdName string) bool {
	for _, inst := range instances {
		if inst.Name != prdName && inst.State == loop.LoopStateRunning && inst.WorktreeDir == "" {
			return true
		}
	}
	return false
}

// doStartLoop runs the pre-run checks and starts the loop once they pass (or
// hands off to a dialog when one of them needs the user's decision).
func (a App) doStartLoop(prdName, prdDir string) (tea.Model, tea.Cmd) {
	// Every start path funnels through here, so this is where the branch gets its
	// last look before hours of work land on it.
	if cmd := a.branchSyncPreflight(prdName, prdDir); cmd != nil {
		return a, cmd
	}

	// ...and the same for the machine itself: an unattended run is only unattended
	// as long as the Mac stays awake.
	if a.sleepWarningPreflight(prdName) {
		return a, nil
	}

	return a.launchLoop(prdName, prdDir)
}

// launchLoop starts the loop, past every pre-run check. Callers that resume an
// interrupted start come in here rather than through doStartLoop, so a question
// the user has already answered isn't asked again.
func (a App) launchLoop(prdName, prdDir string) (tea.Model, tea.Cmd) {
	// Check if this PRD is registered, if not register it
	if instance := a.manager.GetInstance(prdName); instance == nil {
		// Find the PRD path, preferring the already-known one (handles the legacy
		// .chief/prd.md and direct-path layouts) before falling back to convention.
		prdPath := a.prdPathForPRD(prdName)
		if prdPath == "" {
			prdPath = filepath.Join(prdDir, "prd.md")
		}
		// Guarded by the GetInstance check above, so "already registered" cannot fire.
		_ = a.manager.Register(prdName, prdPath)
	}

	// Start the loop via manager
	if err := a.manager.Start(prdName); err != nil {
		a.lastActivity = "Error starting loop: " + err.Error()
		return a, nil
	}

	// Record the branch the loop runs on so on-complete push/PR know what to
	// push. Only when nothing is tracked yet (worktree starts set both first)
	// and never for main/master - we never auto-push a protected branch.
	if inst := a.manager.GetInstance(prdName); inst != nil && inst.Branch == "" && inst.WorktreeDir == "" && git.IsGitRepo(a.baseDir) {
		if b, err := git.GetCurrentBranch(a.baseDir); err == nil && !git.IsProtectedBranch(b) {
			_ = a.manager.UpdateWorktreeInfo(prdName, "", b) // instance existence just checked
		}
	}

	// For the viewed PRD, reload from disk so any follow-up stories added since
	// the last render are reflected before we snapshot the run baseline and
	// restore timings.
	if prdName == a.prdName {
		if p, err := prd.LoadPRD(a.prdPath); err == nil {
			a.prd = p
		}
		// Snapshot the stories that are already passing: this run only owns the
		// rest, so the progress bar tracks this run rather than the whole PRD.
		a.runBaselineDone = make(map[string]bool)
		for _, s := range a.prd.UserStories {
			if s.Passes {
				a.runBaselineDone[s.ID] = true
			}
		}
	}

	// Restore this PRD's timings from progress.md (rather than wiping them) so a
	// stopped/interrupted run keeps its ETA instead of needing two fresh stories
	// again. Only the in-flight tracking is reset.
	var stories []prd.UserStory
	if prdName == a.prdName {
		stories = a.prd.UserStories
	}
	a.storyTimings[prdName] = loadPersistedTimings(a.prdPathForPRD(prdName), stories)
	a.currentStoryID[prdName] = ""
	a.currentStoryStart[prdName] = time.Time{}
	a.currentStoryCost[prdName] = 0
	a.currentStoryTokens[prdName] = TokenUsage{}

	// Update state if this is the current PRD
	if prdName == a.prdName {
		a.state = StateRunning
		a.startTime = time.Now()
		a.lastActivity = "Starting loop..."
		return a, tickElapsed()
	}

	a.lastActivity = "Started loop for: " + prdName
	return a, nil
}

// pauseLoop sets the pause flag so the loop stops after the current story
// fully completes (including its review pass, when one is configured).
func (a App) pauseLoop() (tea.Model, tea.Cmd) {
	return a.pauseLoopForPRD(a.prdName)
}

// pauseLoopForPRD pauses the loop for a specific PRD.
func (a App) pauseLoopForPRD(prdName string) (tea.Model, tea.Cmd) {
	if a.manager != nil {
		// Pause fails when the loop is not running. Swallowing that made the TUI
		// claim "Pausing after current story..." with nothing to pause.
		if err := a.manager.Pause(prdName); err != nil {
			a.lastActivity = "Cannot pause: " + err.Error()
			return a, nil
		}
	}
	if prdName == a.prdName {
		a.lastActivity = "Pausing after current story..."
	} else {
		a.lastActivity = "Pausing " + prdName + " after current story..."
	}
	return a, nil
}

// stopLoopForPRD stops the loop for a specific PRD immediately.
func (a *App) stopLoopForPRD(prdName string) {
	if a.manager != nil {
		_ = a.manager.Stop(prdName) // best-effort: a loop that is already gone is the goal
	}
}

// stopLoopAndUpdate stops the loop and updates the state.
func (a App) stopLoopAndUpdate() (tea.Model, tea.Cmd) {
	return a.stopLoopAndUpdateForPRD(a.prdName)
}

// stopLoopAndUpdateForPRD stops the loop for a specific PRD and updates state.
func (a App) stopLoopAndUpdateForPRD(prdName string) (tea.Model, tea.Cmd) {
	a.stopLoopForPRD(prdName)
	if prdName == a.prdName {
		a.state = StateStopped
		a.lastActivity = "Stopped"
	} else {
		a.lastActivity = "Stopped " + prdName
	}
	return a, nil
}

// stopAllLoops stops all running loops.
func (a *App) stopAllLoops() {
	if a.manager != nil {
		a.manager.StopAll()
	}
}

// handleLoopEvent handles events from the manager.
func (a App) handleLoopEvent(prdName string, event loop.Event) (tea.Model, tea.Cmd) {
	// Only update iteration and log if this is the currently viewed PRD
	isCurrentPRD := prdName == a.prdName

	if isCurrentPRD {
		a.iteration = event.Iteration
		// Add event to log viewer
		a.logViewer.AddEvent(event)
	}

	// Accumulate token usage and derived cost onto the story currently being
	// timed for this PRD. Claude reports these on every assistant message (its
	// final result event never arrives because the loop kills the process on
	// <chief-done/>); other providers report cost on the result event. Tracked
	// for every PRD so background runs stay accurate.
	if event.Cost > 0 || event.InputTokens > 0 || event.OutputTokens > 0 ||
		event.CacheCreationTokens > 0 || event.CacheReadTokens > 0 {
		a.currentStoryCost[prdName] += event.Cost
		tok := a.currentStoryTokens[prdName]
		tok.Input += event.InputTokens
		tok.Output += event.OutputTokens
		tok.CacheCreation += event.CacheCreationTokens
		tok.CacheRead += event.CacheReadTokens
		a.currentStoryTokens[prdName] = tok
		if isCurrentPRD {
			a.totalCost += event.Cost
		}
	}

	var autoActionCmd tea.Cmd

	switch event.Type {
	case loop.EventIterationStart:
		if isCurrentPRD {
			a.lastActivity = "Starting iteration..."
		}
		// Track story timing for every PRD, not just the viewed one, so the ETA
		// survives tab switches and background runs. Start a new story's clock
		// when the loop reports a story ID different from the one we're timing.
		if event.StoryID != "" && event.StoryID != a.currentStoryID[prdName] {
			a.finalizeStoryTiming(prdName)
			a.currentStoryID[prdName] = event.StoryID
			a.currentStoryStart[prdName] = time.Now()
			// Start this story's cost/token counters fresh.
			a.currentStoryCost[prdName] = 0
			a.currentStoryTokens[prdName] = TokenUsage{}
			// Whatever the previous story's review was doing, it is over once the
			// next story starts building. runReview returns without emitting
			// EventReviewDone when the loop is stopped or cancelled
			// mid-review, and the tag outranks everything in selectInProgressStory
			// — so without this the selection stays pinned to that story for the
			// rest of the run.
			delete(a.reviewingStoryID, prdName)
			// Move the selection with the loop. The prd.md watcher does this too,
			// but it is a single point of failure: one missed or dropped file
			// event and the UI sits on the previous story while timings, costs and
			// the log all move on. The loop knows which story it just started.
			if isCurrentPRD {
				a.selectStoryByID(event.StoryID)
			}
		}
	case loop.EventAssistantText:
		if isCurrentPRD {
			// Truncate long text for activity display
			a.lastActivity = truncateWithEllipsis(event.Text, 100)
		}
	case loop.EventToolStart:
		if isCurrentPRD {
			a.lastActivity = "Running tool: " + event.Tool
		}
	case loop.EventToolResult:
		if isCurrentPRD {
			a.lastActivity = "Tool completed"
		}
	case loop.EventStoryDone:
		if isCurrentPRD {
			a.lastActivity = "Story done"
		}
		// Finalize story timing (for every PRD, not just the viewed one). When a
		// review agent runs after the build commit, the story isn't really finished
		// yet: defer finalizing until EventReviewDone so the review pass counts
		// toward the story's duration (and thus the ETA) instead of falling into an
		// unmeasured gap.
		if !a.reviewActive() {
			a.finalizeStoryTiming(prdName)
		}
	case loop.EventStoryNeedsReview:
		if isCurrentPRD {
			a.lastActivity = event.Text
		}
		a.finalizeStoryTiming(prdName)
	case loop.EventReviewStart:
		if isCurrentPRD {
			a.lastActivity = event.Text
		}
		// Keep the story that's being reviewed selected and tagged instead of
		// letting the UI drift to the next story while the review agent runs.
		if event.StoryID != "" {
			a.reviewingStoryID[prdName] = event.StoryID
			if isCurrentPRD {
				a.selectStoryByID(event.StoryID)
			}
		}
	case loop.EventReviewDone:
		if isCurrentPRD {
			a.lastActivity = event.Text
		}
		// The build+review pass for this story is complete: record its full
		// duration now (see EventStoryDone) and drop the reviewing tag.
		a.finalizeStoryTiming(prdName)
		delete(a.reviewingStoryID, prdName)
	case loop.EventConsolidateStart, loop.EventConsolidateDone:
		// The end-of-run consolidation pass belongs to no single story, so there is
		// no story timing or selection to adjust — it just needs to be visible as the
		// current activity while it runs, since it sits between the last story and
		// the completion screen.
		if isCurrentPRD {
			a.lastActivity = event.Text
		}
	case loop.EventComplete:
		// Finalize the last story's timing for any PRD that completes.
		a.finalizeStoryTiming(prdName)
		if isCurrentPRD {
			a.state = StateComplete
			a.lastActivity = "All stories complete!"
			autoActionCmd = a.showCompletionScreen(prdName)
		} else {
			// For background PRDs, trigger auto-push/PR without showing completion screen
			autoActionCmd = a.runBackgroundAutoActions(prdName)
		}
		// Trigger completion callback for any PRD
		if a.onCompletion != nil {
			a.onCompletion(prdName)
		}
		// Ping the user's desktop — they may have walked away from a long run.
		if a.config == nil || a.config.OnComplete.Notify {
			body := fmt.Sprintf("%s — all stories complete", formatPRDTitle(prdName))
			if a.totalCost > 0 {
				body += fmt.Sprintf(" (%s)", formatCost(a.totalCost))
			}
			notify.Send("Chief", body)
		}
	case loop.EventMaxIterationsReached:
		if isCurrentPRD {
			a.state = StatePaused
			a.lastActivity = "Max iterations reached"
			// A capped run is still a partial result worth summarizing: the
			// stories that did complete have commits. Generate (and commit) a
			// summary of what got done so far. No push chain — the run didn't
			// finish, so we only leave the summary on the branch.
			if a.config != nil && a.config.OnComplete.Summary {
				if branch := a.branchFor(prdName); branch != "" &&
					git.CommitCount(a.completionGitDir(prdName), branch) > 0 {
					a.lastActivity = "Max iterations reached — writing summary..."
					autoActionCmd = a.runAutoSummary(prdName, false, false)
				}
			}
		}
	case loop.EventError:
		if isCurrentPRD {
			a.state = StateError
			a.err = event.Err
			if event.Err != nil {
				a.lastActivity = "Error: " + event.Err.Error()
			}
		}
	case loop.EventRetrying:
		if isCurrentPRD {
			a.lastActivity = event.Text
		}
	case loop.EventWatchdogTimeout:
		if isCurrentPRD {
			a.lastActivity = event.Text
		}
	}

	// Reload PRD from disk only on meaningful state changes (not every event)
	if isCurrentPRD {
		switch event.Type {
		case loop.EventStoryDone, loop.EventStoryNeedsReview, loop.EventComplete, loop.EventError, loop.EventMaxIterationsReached:
			if p, err := prd.LoadPRD(a.prdPath); err == nil {
				a.prd = p
			}
		}

		// Clear in-progress when the PRD completes or the loop stops
		if event.Type == loop.EventComplete || event.Type == loop.EventError || event.Type == loop.EventMaxIterationsReached {
			a.clearInProgress()
		}
	}

	// Refresh the tab bar only on events that change a tab's displayed state
	// (progress counts, done/needs-review/error badges). Streaming events like
	// assistant text, tool calls and usage fire many times per second, and each
	// Refresh re-reads and re-parses every prd.md from disk — so refreshing on
	// those chunks turned every token into a full directory scan.
	if a.tabBar != nil {
		switch event.Type {
		case loop.EventIterationStart, loop.EventStoryDone, loop.EventStoryNeedsReview,
			loop.EventComplete, loop.EventError, loop.EventMaxIterationsReached:
			a.tabBar.Refresh()
		}
	}

	// Continue listening for manager events, plus any auto-action commands
	if autoActionCmd != nil {
		return a, tea.Batch(a.listenForManagerEvents(), autoActionCmd)
	}
	return a, a.listenForManagerEvents()
}

// handleLoopFinished handles when a loop finishes.
func (a App) handleLoopFinished(prdName string, err error) (tea.Model, tea.Cmd) {
	// Only update state if this is the current PRD
	if prdName == a.prdName {
		// Get the actual state from the manager
		if state, _, _ := a.manager.GetState(prdName); state != 0 {
			switch state {
			case loop.LoopStateError:
				a.state = StateError
				a.err = err
				if err != nil {
					a.lastActivity = "Error: " + err.Error()
				}
			case loop.LoopStatePaused:
				a.state = StatePaused
				a.lastActivity = "Paused"
			case loop.LoopStateStopped:
				a.state = StateStopped
				a.lastActivity = "Stopped"
			case loop.LoopStateComplete:
				a.state = StateComplete
				a.lastActivity = "All stories complete!"
			}
		}

		// Reload PRD to reflect any changes
		if p, err := prd.LoadPRD(a.prdPath); err == nil {
			a.prd = p
		}
	}

	return a, nil
}
