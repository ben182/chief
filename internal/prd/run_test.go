package prd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenRunMissingFile(t *testing.T) {
	_, ok, err := OpenRun(filepath.Join(t.TempDir(), "progress.md"))
	if err != nil || ok {
		t.Fatalf("OpenRun on a missing file = ok %v, err %v; want no run, no error", ok, err)
	}
}

// A run paused in the evening and resumed the next morning is one run: the
// second session finds the first one's start and figures.
func TestOpenRunCollectsSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.md")
	mustOK(t, os.WriteFile(path, []byte("## 2026-09-30 - US-001\n- did things"), 0644))
	mustOK(t, BeginRun(path, "abc123"))
	mustOK(t, RecordRunSession(path, RunSession{ID: "evening", Duration: 10 * time.Minute, Cost: 1.5}))
	// The same session recorded again later: the latest record wins.
	mustOK(t, RecordRunSession(path, RunSession{ID: "evening", Duration: 3 * time.Hour, Slept: time.Minute, RateLimitWaited: 20 * time.Minute, Cost: 30}))
	mustOK(t, RecordRunSession(path, RunSession{ID: "morning", Duration: time.Hour, Cost: 17}))

	run, ok, err := OpenRun(path)
	if err != nil || !ok {
		t.Fatalf("OpenRun = ok %v, err %v; want an open run", ok, err)
	}
	if run.StartRef != "abc123" {
		t.Errorf("StartRef = %q, want abc123", run.StartRef)
	}
	if len(run.Sessions) != 2 {
		t.Fatalf("got %d sessions, want 2: %+v", len(run.Sessions), run.Sessions)
	}
	got := run.Totals()
	want := RunSession{Duration: 4 * time.Hour, Slept: time.Minute, RateLimitWaited: 20 * time.Minute, Cost: 47}
	if got != want {
		t.Errorf("Totals = %+v, want %+v", got, want)
	}
}

// Once a run has ended, the next start is a new run, and nothing of the old
// one leaks into it.
func TestOpenRunAfterEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.md")
	mustOK(t, BeginRun(path, "old"))
	mustOK(t, RecordRunSession(path, RunSession{ID: "a", Duration: time.Hour, Cost: 5}))
	mustOK(t, EndRun(path))

	if _, ok, _ := OpenRun(path); ok {
		t.Fatal("an ended run is still reported open")
	}

	// A stray record after the end belongs to no run.
	mustOK(t, RecordRunSession(path, RunSession{ID: "a", Duration: 2 * time.Hour}))
	mustOK(t, BeginRun(path, "new"))
	run, ok, _ := OpenRun(path)
	if !ok || run.StartRef != "new" || len(run.Sessions) != 0 {
		t.Fatalf("OpenRun = %+v, ok %v; want the fresh run with no sessions", run, ok)
	}
}

// The run records are chief's bookkeeping and stay out of the progress notes
// the dashboard shows.
func TestParseProgressSkipsRunRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.md")
	mustOK(t, os.WriteFile(path, []byte("## 2026-09-30 - US-001\n- built the thing\n"), 0644))
	mustOK(t, BeginRun(path, "abc"))
	mustOK(t, RecordRunSession(path, RunSession{ID: "s", Duration: time.Minute}))
	mustOK(t, EndRun(path))

	entries, err := ParseProgress(path)
	if err != nil {
		t.Fatal(err)
	}
	body := entries["US-001"][0].Content
	if strings.Contains(body, "chief-") {
		t.Errorf("progress content carries run records:\n%s", body)
	}
}

func mustOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
