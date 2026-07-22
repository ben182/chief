package tui

// Scrollable is the shared scrolling contract implemented by both the log and
// diff viewers. It lets the update loop drive whichever viewer is active
// without branching on the view mode for every scroll key.
type Scrollable interface {
	ScrollUp()
	ScrollDown()
	PageUp()
	PageDown()
	ScrollToTop()
	ScrollToBottom()
}

// activeScrollable returns the viewer that should receive scroll input for the
// current view, or nil when the active view isn't a scrollable one.
func (a App) activeScrollable() Scrollable {
	switch a.viewMode {
	case ViewLog:
		return a.logViewer
	case ViewDiff:
		return a.diffViewer
	default:
		return nil
	}
}
