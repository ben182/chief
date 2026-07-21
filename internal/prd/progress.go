package prd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// ProgressEntry represents progress notes for a single story from a single session.
type ProgressEntry struct {
	StoryID string
	Date    string
	Content string // raw markdown body (bullet lines)
}

// Timing is a machine-readable record of how long a completed story took,
// persisted into progress.md so the ETA can survive a restart or interruption.
type Timing struct {
	StoryID           string
	DurationMS        int64
	Cost              float64
	TokensIn          int
	TokensOut         int
	TokensCacheCreate int
	TokensCacheRead   int
}

// ProgressPath returns the progress.md path for a given prd.json path.
func ProgressPath(prdPath string) string {
	return filepath.Join(filepath.Dir(prdPath), "progress.md")
}

var storyHeaderRegex = regexp.MustCompile(`^## (\d{4}-\d{2}-\d{2}) - (.+)$`)

// timingCommentRegex matches a chief-owned timing record. It is written as an
// HTML comment so it stays invisible in rendered markdown, and is stripped from
// the human-readable progress content while parsing.
var timingCommentRegex = regexp.MustCompile(`^\s*<!-- chief-timing (.+?) -->\s*$`)

// timingFieldRegex extracts key=value attributes; values are either a Go-quoted
// string (the story ID) or a bare number.
var timingFieldRegex = regexp.MustCompile(`(\w+)=("(?:[^"\\]|\\.)*"|\S+)`)

// ParseProgress reads and parses a progress.md file.
// Returns a map of story ID -> list of progress entries (one per session/date).
func ParseProgress(path string) (map[string][]ProgressEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	result := make(map[string][]ProgressEntry)
	var current *ProgressEntry
	var lines []string

	flush := func() {
		if current != nil && len(lines) > 0 {
			current.Content = strings.Join(lines, "\n")
			result[current.StoryID] = append(result[current.StoryID], *current)
		}
		current = nil
		lines = nil
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		// chief-owned timing metadata is machine-readable only and must never
		// surface in the human-readable progress content.
		if timingCommentRegex.MatchString(line) {
			continue
		}

		// Check for section separator
		if strings.TrimSpace(line) == "---" {
			flush()
			continue
		}

		// Check for story header
		if matches := storyHeaderRegex.FindStringSubmatch(line); matches != nil {
			flush()
			current = &ProgressEntry{
				Date:    matches[1],
				StoryID: matches[2],
			}
			continue
		}

		// Collect lines within a story section
		if current != nil {
			lines = append(lines, line)
		}
	}

	// Flush the last entry
	flush()

	if err := scanner.Err(); err != nil {
		return result, err
	}
	return result, nil
}

// ParseTimings reads the chief-owned timing records from progress.md. When a
// story has been timed more than once (e.g. re-run after needs-review, or
// re-recorded across runs) the most recent record wins, keeping its position.
func ParseTimings(path string) ([]Timing, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var timings []Timing
	index := make(map[string]int) // story ID -> position in timings

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		matches := timingCommentRegex.FindStringSubmatch(scanner.Text())
		if matches == nil {
			continue
		}
		t, ok := parseTimingFields(matches[1])
		if !ok {
			continue
		}
		if i, seen := index[t.StoryID]; seen {
			timings[i] = t
		} else {
			index[t.StoryID] = len(timings)
			timings = append(timings, t)
		}
	}
	if err := scanner.Err(); err != nil {
		return timings, err
	}
	return timings, nil
}

// parseTimingFields turns the attribute string of a timing comment into a
// Timing. It returns ok=false when no story ID could be recovered.
func parseTimingFields(attrs string) (Timing, bool) {
	var t Timing
	for _, m := range timingFieldRegex.FindAllStringSubmatch(attrs, -1) {
		key, val := m[1], m[2]
		switch key {
		case "story":
			s, err := strconv.Unquote(val)
			if err != nil {
				s = strings.Trim(val, `"`)
			}
			t.StoryID = s
		case "duration_ms":
			t.DurationMS, _ = strconv.ParseInt(val, 10, 64)
		case "cost":
			t.Cost, _ = strconv.ParseFloat(val, 64)
		case "in":
			t.TokensIn, _ = strconv.Atoi(val)
		case "out":
			t.TokensOut, _ = strconv.Atoi(val)
		case "cache_create":
			t.TokensCacheCreate, _ = strconv.Atoi(val)
		case "cache_read":
			t.TokensCacheRead, _ = strconv.Atoi(val)
		}
	}
	if t.StoryID == "" {
		return t, false
	}
	return t, true
}

// AppendTiming appends a timing record for a completed story to progress.md. It
// is append-only (never a read-modify-write of the whole file) so it cannot
// clobber a concurrent append from the coding agent; duplicates are resolved by
// ParseTimings on read.
func AppendTiming(path string, t Timing) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	// Make sure our comment starts on its own line even if the agent left the
	// file without a trailing newline.
	prefix := ""
	if info, err := f.Stat(); err == nil && info.Size() > 0 {
		buf := make([]byte, 1)
		if _, err := f.ReadAt(buf, info.Size()-1); err == nil && buf[0] != '\n' {
			prefix = "\n"
		}
	}

	line := fmt.Sprintf(
		`<!-- chief-timing story=%q duration_ms=%d cost=%.6f in=%d out=%d cache_create=%d cache_read=%d -->`,
		t.StoryID, t.DurationMS, t.Cost, t.TokensIn, t.TokensOut, t.TokensCacheCreate, t.TokensCacheRead,
	)
	_, err = f.WriteString(prefix + line + "\n")
	return err
}

// ProgressWatcher watches progress.md for changes and sends parsed entries.
type ProgressWatcher struct {
	dir     string
	watcher *fsnotify.Watcher
	events  chan map[string][]ProgressEntry
	done    chan struct{}
	mu      sync.Mutex
	running bool
}

// NewProgressWatcher creates a new watcher for progress.md in the same
// directory as the given prd.json path.
func NewProgressWatcher(prdPath string) (*ProgressWatcher, error) {
	dir := filepath.Dir(prdPath)
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &ProgressWatcher{
		dir:     dir,
		watcher: fsWatcher,
		events:  make(chan map[string][]ProgressEntry, 10),
		done:    make(chan struct{}),
	}, nil
}

// Start begins watching for progress.md changes.
func (w *ProgressWatcher) Start() error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.mu.Unlock()

	// Watch the directory so we catch creates and writes
	if err := w.watcher.Add(w.dir); err != nil {
		return err
	}

	go w.processEvents()
	return nil
}

// Stop stops watching.
func (w *ProgressWatcher) Stop() {
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

// Events returns the channel for receiving parsed progress data.
func (w *ProgressWatcher) Events() <-chan map[string][]ProgressEntry {
	return w.events
}

// processEvents listens for filesystem events and re-parses progress.md on change.
func (w *ProgressWatcher) processEvents() {
	progressPath := filepath.Join(w.dir, "progress.md")
	for {
		select {
		case <-w.done:
			close(w.events)
			return

		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				if filepath.Base(event.Name) == "progress.md" {
					entries, err := ParseProgress(progressPath)
					if err == nil && entries != nil {
						w.events <- entries
					}
				}
			}

		case _, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
		}
	}
}
