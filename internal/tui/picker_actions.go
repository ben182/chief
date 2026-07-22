package tui

import (
	"fmt"
	"time"

	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/loop"
	"github.com/ben182/chief/internal/prd"
	tea "github.com/charmbracelet/bubbletea"
)

// handlePickerKeys handles keyboard input when the picker is active.
func (a App) handlePickerKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle input mode (creating new PRD)
	if a.picker.IsInputMode() {
		switch msg.String() {
		case "esc":
			a.picker.CancelInputMode()
			return a, nil
		case "enter":
			name := a.picker.GetInputValue()
			if name != "" {
				// Launch interactive Claude session to create the PRD
				a.picker.CancelInputMode()
				a.stopAllLoops()
				a.stopWatcher()
				return a, func() tea.Msg {
					return LaunchInitMsg{Name: name}
				}
			}
			a.picker.CancelInputMode()
			return a, nil
		case "backspace":
			a.picker.DeleteInputChar()
			return a, nil
		default:
			// Handle character input
			if len(msg.String()) == 1 {
				a.picker.AddInputChar(rune(msg.String()[0]))
			}
			return a, nil
		}
	}

	// Dismiss clean result on any key
	if a.picker.HasCleanResult() {
		a.picker.ClearCleanResult()
		a.picker.Refresh()
		return a, nil
	}

	// Handle clean confirmation dialog
	if a.picker.HasCleanConfirmation() {
		return a.handleCleanConfirmationKeys(msg)
	}

	// Dismiss merge result on any key
	if a.picker.HasMergeResult() {
		a.picker.ClearMergeResult()
		a.picker.Refresh()
		return a, nil
	}

	// Normal picker mode
	switch msg.String() {
	case "esc", "l":
		a.viewMode = ViewDashboard
		return a, nil
	case "q", "ctrl+c":
		return a.tryQuit()
	case "up", "k":
		a.picker.MoveUp()
		a.picker.Refresh() // Refresh to get latest state
		return a, nil
	case "down", "j":
		a.picker.MoveDown()
		a.picker.Refresh() // Refresh to get latest state
		return a, nil
	case "enter":
		entry := a.picker.GetSelectedEntry()
		if entry != nil && !entry.Archived && entry.LoadError == nil {
			return a.switchToPRD(entry.Name, entry.Path)
		}
		return a, nil
	case "n":
		a.picker.StartInputMode()
		return a, nil
	case "e":
		// Edit the selected PRD - launch interactive Claude session
		entry := a.picker.GetSelectedEntry()
		if entry != nil && !entry.Archived && entry.LoadError == nil {
			a.stopAllLoops()
			a.stopWatcher()
			return a, func() tea.Msg {
				return LaunchEditMsg{Name: entry.Name}
			}
		}
		return a, nil

	// Loop controls for the SELECTED PRD (not current)
	case "s":
		entry := a.picker.GetSelectedEntry()
		if entry != nil && !entry.Archived && entry.LoadError == nil {
			state := entry.LoopState
			if state == loop.LoopStateReady || state == loop.LoopStatePaused ||
				state == loop.LoopStateStopped || state == loop.LoopStateError {
				model, cmd := a.startLoopForPRD(entry.Name)
				a.picker.Refresh()
				return model, cmd
			}
		}
		return a, nil
	case "p":
		entry := a.picker.GetSelectedEntry()
		if entry != nil && entry.LoopState == loop.LoopStateRunning {
			model, cmd := a.pauseLoopForPRD(entry.Name)
			a.picker.Refresh()
			return model, cmd
		}
		return a, nil
	case "x":
		entry := a.picker.GetSelectedEntry()
		if entry != nil {
			state := entry.LoopState
			if state == loop.LoopStateRunning || state == loop.LoopStatePaused {
				model, cmd := a.stopLoopAndUpdateForPRD(entry.Name)
				a.picker.Refresh()
				return model, cmd
			}
		}
		return a, nil

	case "m":
		// Merge completed PRD's branch
		if a.picker.CanMerge() {
			entry := a.picker.GetSelectedEntry()
			branch := entry.Branch
			baseDir := a.baseDir
			return a, func() tea.Msg {
				conflicts, err := git.MergeBranch(baseDir, branch)
				if err != nil {
					return mergeResultMsg{branch: branch, conflicts: conflicts, err: err}
				}
				// Build success message with merge details
				output := parseMergeSuccessMessage(baseDir, branch)
				return mergeResultMsg{branch: branch, output: output}
			}
		}
		return a, nil

	case "c":
		// Clean worktree for non-running PRD
		if a.picker.CanClean() {
			a.picker.StartCleanConfirmation()
		}
		return a, nil

	case "a":
		// Archive the selected (active, non-running) PRD.
		if a.picker.CanArchive() {
			entry := a.picker.GetSelectedEntry()
			name := entry.Name
			if err := prd.ArchivePRD(a.baseDir, name); err != nil {
				a.lastActivity = "Archive failed: " + err.Error()
				return a, nil
			}
			// Drop it from the loop manager so it no longer tracks state.
			_ = a.manager.Unregister(name)
			a.lastActivity = "Archived PRD: " + name
			a.picker.Refresh()
			a.tabBar.Refresh()
			// If we archived the PRD currently being viewed, switch away from it.
			if name == a.prdName {
				if next := a.picker.FirstActiveEntry(); next != nil {
					return a.switchToPRD(next.Name, next.Path)
				}
			}
		}
		return a, nil

	case "u":
		// Restore the selected archived PRD back into .chief/prds/.
		if a.picker.CanRestore() {
			entry := a.picker.GetSelectedEntry()
			name := entry.Name
			if err := prd.RestorePRD(a.baseDir, name); err != nil {
				a.lastActivity = "Restore failed: " + err.Error()
				return a, nil
			}
			a.lastActivity = "Restored PRD: " + name
			a.picker.Refresh()
			a.tabBar.Refresh()
		}
		return a, nil
	}

	return a, nil
}

// parseMergeSuccessMessage constructs a success message after a merge.
func parseMergeSuccessMessage(repoDir, branch string) string {
	// Try to get the default branch for display
	defaultBranch := "current branch"
	if db, err := git.GetDefaultBranch(repoDir); err == nil {
		defaultBranch = db
	}
	return fmt.Sprintf("Merged %s into %s", branch, defaultBranch)
}

// switchToPRD switches to a different PRD (view only - does not stop other loops).
func (a App) switchToPRD(name, prdPath string) (tea.Model, tea.Cmd) {
	// Stop current watcher (but NOT the loop - it can keep running)
	a.stopWatcher()

	// Load the new PRD
	newPRD, err := prd.LoadPRD(prdPath)
	if err != nil {
		a.lastActivity = "Error loading PRD: " + err.Error()
		a.viewMode = ViewDashboard
		return a, nil
	}

	// Register with manager if not already registered
	if instance := a.manager.GetInstance(name); instance == nil {
		a.manager.Register(name, prdPath)
	}

	// Create new watcher for the new PRD
	newWatcher, err := prd.NewWatcher(prdPath)
	if err != nil {
		a.lastActivity = "Warning: file watcher failed"
	} else {
		a.watcher = newWatcher
		if err := a.watcher.Start(); err != nil {
			a.lastActivity = "Warning: file watcher failed to start"
		}
	}

	// Create new progress watcher and load initial progress
	newProgressWatcher, err := prd.NewProgressWatcher(prdPath)
	if err == nil {
		a.progressWatcher = newProgressWatcher
		_ = a.progressWatcher.Start()
	}
	a.progress, _ = prd.ParseProgress(prd.ProgressPath(prdPath))

	// Restore persisted timings the first time we see this PRD (timings are kept
	// in memory across switches, so don't reload — and thus double — them).
	if len(a.storyTimings[name]) == 0 {
		a.storyTimings[name] = loadPersistedTimings(prdPath, newPRD.UserStories)
	}

	// Get the state from the manager for this PRD
	loopState, iteration, loopErr := a.manager.GetState(name)
	appState := StateReady
	switch loopState {
	case loop.LoopStateRunning:
		appState = StateRunning
	case loop.LoopStatePaused:
		appState = StatePaused
	case loop.LoopStateStopped:
		appState = StateStopped
	case loop.LoopStateComplete:
		appState = StateComplete
	case loop.LoopStateError:
		appState = StateError
	}

	// Only recalculate max iterations if no loop is currently running for this PRD
	if instance := a.manager.GetInstance(name); instance == nil || instance.State != loop.LoopStateRunning {
		remaining := 0
		for _, story := range newPRD.UserStories {
			if !story.Passes && !story.NeedsReview {
				remaining++
			}
		}
		a.maxIter = remaining*loop.DefaultMaxAttemptsPerStory + 5
		if a.maxIter < 5 {
			a.maxIter = 5
		}
		// Propagate to the manager so a loop started for THIS PRD uses this PRD's
		// budget, not the budget computed for whichever PRD was loaded first.
		a.manager.SetMaxIterations(a.maxIter)
	}

	// Update app state
	a.prd = newPRD
	a.prdPath = prdPath
	a.prdName = name
	a.selectedIndex = 0
	a.storiesScrollOffset = 0
	a.state = appState
	a.iteration = iteration
	a.err = loopErr
	if appState == StateRunning {
		// Keep the existing start time if running
		if instance := a.manager.GetInstance(name); instance != nil {
			a.startTime = instance.StartTime
		}
	} else {
		a.startTime = time.Time{}
	}
	a.lastActivity = "Switched to PRD: " + name
	a.viewMode = ViewDashboard
	a.picker.SetCurrentPRD(name)
	a.tabBar.SetActiveByName(name)
	a.tabBar.Refresh()

	// Clear the log viewer (it only holds the viewed PRD's log). Story timings
	// are kept per PRD and intentionally NOT cleared here, so switching tabs
	// doesn't wipe the data the ETA depends on.
	a.logViewer.Clear()

	// Return with new watcher listeners (and elapsed tick if running)
	cmds := []tea.Cmd{a.listenForPRDChanges(), a.listenForProgressChanges()}
	if appState == StateRunning {
		cmds = append(cmds, tickElapsed())
	}
	return a, tea.Batch(cmds...)
}
