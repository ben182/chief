package prd

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

// WatcherEvent represents a file change event.
type WatcherEvent struct {
	PRD   *PRD
	Error error
}

// Watcher watches a prd.md file for changes and sends events. It builds on
// fileWatcher for the shared start/stop/event-loop lifecycle.
type Watcher struct {
	*fileWatcher[WatcherEvent]
	path    string
	lastPRD *PRD
}

// NewWatcher creates a new Watcher for the given PRD file path.
func NewWatcher(path string) (*Watcher, error) {
	base, err := newFileWatcher[WatcherEvent](10)
	if err != nil {
		return nil, err
	}
	return &Watcher{fileWatcher: base, path: path}, nil
}

// Start begins watching the PRD file for changes.
func (w *Watcher) Start() error {
	if !w.start() {
		return errors.New("watcher already running")
	}

	// Load the initial PRD
	prd, err := LoadPRD(w.path)
	if err != nil {
		// Don't fail startup, just send error event
		w.events <- WatcherEvent{Error: err}
	} else {
		w.lastPRD = prd
	}

	// Watch the *directory*, not the file. chief writes prd.md atomically (temp
	// file + rename) and most editors save the same way, so the watched inode is
	// swapped out on every write rather than modified in place. A file watch has
	// to be re-registered each time, and a single failed re-registration — the
	// file momentarily gone during a branch switch or a worktree teardown — ends
	// the watch silently for the rest of the session: the dashboard then sits on
	// whatever story was current when it died while the run moves on. A directory
	// watch survives inode swaps and a temporarily missing file, which is also
	// what ProgressWatcher next door already does.
	if err := w.watcher.Add(filepath.Dir(w.path)); err != nil {
		return err
	}

	// Start the event processing goroutine
	go w.process(w.onEvent, func(err error) { w.events <- WatcherEvent{Error: err} })

	return nil
}

// onEvent reloads the PRD whenever prd.md itself changes. The watch covers the
// whole PRD directory (progress.md, the run log, the temp files of an atomic
// write), so everything that isn't prd.md is filtered out here. A remove or
// rename is only reported as a removal when the file is really gone: the rename
// half of an atomic write names prd.md too, and the file is in place by then.
func (w *Watcher) onEvent(event fsnotify.Event) {
	if filepath.Base(event.Name) != filepath.Base(w.path) {
		return
	}

	if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
		return
	}

	if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		if _, err := os.Stat(w.path); err != nil {
			w.events <- WatcherEvent{Error: errors.New("prd.md was removed")}
			return
		}
	}

	w.handleFileChange()
}

// handleFileChange loads the PRD and sends an event if it changed.
func (w *Watcher) handleFileChange() {
	prd, err := LoadPRD(w.path)
	if err != nil {
		w.events <- WatcherEvent{Error: err}
		return
	}

	// Check if any story status changed
	if w.hasStatusChanged(prd) {
		w.lastPRD = prd
		w.events <- WatcherEvent{PRD: prd}
	}
}

// hasStatusChanged returns true if any story's passes, inProgress or needsReview
// field changed. needsReview counts: a story parked for human review straight out
// of "todo" moves no other field, and without it the list would keep showing the
// story as pending for the rest of the session.
func (w *Watcher) hasStatusChanged(newPRD *PRD) bool {
	if w.lastPRD == nil {
		return true
	}

	// If number of stories changed, treat as changed
	if len(w.lastPRD.UserStories) != len(newPRD.UserStories) {
		return true
	}

	// Build a map of old stories by ID for comparison
	oldStories := make(map[string]*UserStory)
	for i := range w.lastPRD.UserStories {
		oldStories[w.lastPRD.UserStories[i].ID] = &w.lastPRD.UserStories[i]
	}

	// Check each new story for status changes
	for i := range newPRD.UserStories {
		newStory := &newPRD.UserStories[i]
		oldStory, exists := oldStories[newStory.ID]

		if !exists {
			// New story added
			return true
		}

		// Check if status fields changed
		if oldStory.Passes != newStory.Passes ||
			oldStory.InProgress != newStory.InProgress ||
			oldStory.NeedsReview != newStory.NeedsReview {
			return true
		}
	}

	return false
}
