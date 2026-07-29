package tui

import "testing"

func TestActiveScrollableForLogView(t *testing.T) {
	a := newTestApp(nil, 100, 30)
	a.logViewer = NewLogViewer()
	a.diffViewer = NewDiffViewer(t.TempDir())
	a.viewMode = ViewLog

	got := a.activeScrollable()
	if got != Scrollable(a.logViewer) {
		t.Errorf("expected the log viewer for ViewLog, got %T", got)
	}
}

func TestActiveScrollableForDiffView(t *testing.T) {
	a := newTestApp(nil, 100, 30)
	a.logViewer = NewLogViewer()
	a.diffViewer = NewDiffViewer(t.TempDir())
	a.viewMode = ViewDiff

	got := a.activeScrollable()
	if got != Scrollable(a.diffViewer) {
		t.Errorf("expected the diff viewer for ViewDiff, got %T", got)
	}
}

func TestActiveScrollableNilForNonScrollableViews(t *testing.T) {
	// Scroll keys in these views drive other things (list selection, modal
	// navigation); handing them a viewer would move content behind the modal.
	for _, mode := range []ViewMode{
		ViewDashboard,
		ViewPicker,
		ViewSettings,
		ViewCompletion,
		ViewHelp,
		ViewQuitConfirm,
	} {
		a := newTestApp(nil, 100, 30)
		a.logViewer = NewLogViewer()
		a.diffViewer = NewDiffViewer(t.TempDir())
		a.viewMode = mode

		if got := a.activeScrollable(); got != nil {
			t.Errorf("expected nil scrollable for view %v, got %T", mode, got)
		}
	}
}

func TestActiveScrollableRoutesScrollToTheActiveViewerOnly(t *testing.T) {
	a := newTestApp(nil, 100, 30)
	a.logViewer = NewLogViewer()
	a.diffViewer = loadedDiffViewer(diffLines(50), 80, 10)
	a.viewMode = ViewDiff

	s := a.activeScrollable()
	if s == nil {
		t.Fatal("expected a scrollable for ViewDiff")
	}
	s.ScrollDown()
	s.ScrollDown()

	if a.diffViewer.offset != 2 {
		t.Errorf("expected the diff viewer to receive the scrolls, got offset %d", a.diffViewer.offset)
	}
}

func TestActiveScrollableSurvivesNilViewer(t *testing.T) {
	// The log viewer is nil before the first loop starts. activeScrollable
	// returns the typed nil, so the caller's nil check has to catch it rather
	// than the update loop panicking on a scroll key.
	a := newTestApp(nil, 100, 30)
	a.viewMode = ViewLog

	s := a.activeScrollable()
	if s == nil {
		return // an explicit nil is fine too
	}
	if lv, ok := s.(*LogViewer); !ok || lv != nil {
		t.Errorf("expected a nil *LogViewer, got %#v", s)
	}
}
