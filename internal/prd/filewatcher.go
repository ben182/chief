package prd

import (
	"sync"

	"github.com/fsnotify/fsnotify"
)

// fileWatcher is the shared lifecycle for prd's fsnotify-based watchers. It owns
// the underlying fsnotify watcher, the running flag, and the done/events
// channels, and runs a common select loop that forwards filesystem events to a
// per-watcher handler. Concrete watchers embed it and add their own Start (which
// registers paths and launches process) plus event-typed helpers.
//
// T is the element type sent on the events channel, letting each watcher expose
// a strongly-typed Events() channel.
type fileWatcher[T any] struct {
	watcher *fsnotify.Watcher
	events  chan T
	done    chan struct{}
	mu      sync.Mutex
	running bool
}

// newFileWatcher creates the underlying fsnotify watcher and an events channel
// with the given buffer size.
func newFileWatcher[T any](buffer int) (*fileWatcher[T], error) {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &fileWatcher[T]{
		watcher: fsWatcher,
		events:  make(chan T, buffer),
		done:    make(chan struct{}),
	}, nil
}

// start marks the watcher running and returns true, or returns false if it was
// already running. The caller registers paths and launches process only on true.
func (w *fileWatcher[T]) start() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return false
	}
	w.running = true
	return true
}

// Stop stops watching. It is safe to call more than once.
func (w *fileWatcher[T]) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	w.running = false
	w.mu.Unlock()

	close(w.done)
	w.watcher.Close()
}

// Events returns the channel on which change events are delivered.
func (w *fileWatcher[T]) Events() <-chan T {
	return w.events
}

// process runs the shared event loop: it closes the events channel and returns
// when Stop is called, forwards filesystem events to onEvent, and watcher errors
// to onError. Either callback may be nil to ignore that stream.
func (w *fileWatcher[T]) process(onEvent func(fsnotify.Event), onError func(error)) {
	for {
		select {
		case <-w.done:
			close(w.events)
			return

		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if onEvent != nil {
				onEvent(event)
			}

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			if onError != nil {
				onError(err)
			}
		}
	}
}
