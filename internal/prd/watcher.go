package prd

import (
	"errors"

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

	// Add the file to the watcher
	if err := w.watcher.Add(w.path); err != nil {
		return err
	}

	// Start the event processing goroutine
	go w.process(w.onEvent, func(err error) { w.events <- WatcherEvent{Error: err} })

	return nil
}

// onEvent reloads the PRD on write/create and re-arms the watch on
// remove/rename. Chief writes prd.md atomically (temp file + rename) and many
// editors save the same way, so the watched inode is swapped out rather than
// modified in place. Re-add the watch and reload; only report a genuine removal
// if the file is actually gone.
func (w *Watcher) onEvent(event fsnotify.Event) {
	// Only react to write and create events
	if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
		w.handleFileChange()
	}

	if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		if err := w.watcher.Add(w.path); err != nil {
			w.events <- WatcherEvent{Error: errors.New("prd.md was removed")}
		} else {
			w.handleFileChange()
		}
	}
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

// hasStatusChanged returns true if any story's inProgress or passes field changed.
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
		if oldStory.Passes != newStory.Passes || oldStory.InProgress != newStory.InProgress {
			return true
		}
	}

	return false
}
