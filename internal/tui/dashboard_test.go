package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ben182/chief/internal/prd"
)

// newTestApp creates a minimal App for testing scroll and rendering.
func newTestApp(stories []prd.UserStory, width, height int) *App {
	return &App{
		prd:      &prd.PRD{UserStories: stories},
		width:    width,
		height:   height,
		viewMode: ViewDashboard,

		storyTimings:       make(map[string][]StoryTiming),
		currentStoryID:     make(map[string]string),
		currentStoryStart:  make(map[string]time.Time),
		currentStoryCost:   make(map[string]float64),
		currentStoryTokens: make(map[string]TokenUsage),
		reviewingStoryID:   make(map[string]string),
		branchSyncChecked:  make(map[string]bool),
	}
}

func makeStories(n int) []prd.UserStory {
	stories := make([]prd.UserStory, n)
	for i := range stories {
		stories[i] = prd.UserStory{
			ID:       fmt.Sprintf("US-%03d", i+1),
			Title:    fmt.Sprintf("Story %d", i+1),
			Priority: float64(i + 1),
		}
	}
	return stories
}

func TestScrollOffset_FollowsCursorDown(t *testing.T) {
	app := newTestApp(makeStories(20), 120, 20)
	listHeight := app.storiesListHeight()
	if listHeight <= 0 {
		t.Fatalf("expected positive listHeight, got %d", listHeight)
	}

	// Navigate down past the visible range
	for i := 0; i < listHeight+3; i++ {
		if app.selectedIndex < len(app.prd.UserStories)-1 {
			app.selectedIndex++
			app.adjustStoriesScroll()
		}
	}

	// Selected index should be past the first screen
	if app.selectedIndex <= listHeight {
		t.Errorf("expected selectedIndex > %d, got %d", listHeight, app.selectedIndex)
	}

	// Scroll offset should have followed
	if app.storiesScrollOffset == 0 {
		t.Error("expected storiesScrollOffset > 0 after scrolling down past visible range")
	}

	// Selected index should be visible
	if app.selectedIndex < app.storiesScrollOffset || app.selectedIndex >= app.storiesScrollOffset+listHeight {
		t.Errorf("selectedIndex %d not visible in scroll window [%d, %d)", app.selectedIndex, app.storiesScrollOffset, app.storiesScrollOffset+listHeight)
	}
}

func TestScrollOffset_FollowsCursorUp(t *testing.T) {
	app := newTestApp(makeStories(20), 120, 20)
	listHeight := app.storiesListHeight()

	// Move down first
	for i := 0; i < listHeight+5; i++ {
		if app.selectedIndex < len(app.prd.UserStories)-1 {
			app.selectedIndex++
			app.adjustStoriesScroll()
		}
	}
	savedOffset := app.storiesScrollOffset

	// Now navigate back up past the scroll offset
	for i := 0; i < listHeight+5; i++ {
		if app.selectedIndex > 0 {
			app.selectedIndex--
			if app.selectedIndex < app.storiesScrollOffset {
				app.storiesScrollOffset = app.selectedIndex
			}
		}
	}

	// Should be back at top
	if app.selectedIndex != 0 {
		t.Errorf("expected selectedIndex 0, got %d", app.selectedIndex)
	}
	if app.storiesScrollOffset != 0 {
		t.Errorf("expected storiesScrollOffset 0, got %d", app.storiesScrollOffset)
	}
	_ = savedOffset
}

func TestScrollOffset_NoScrollWhenAllFit(t *testing.T) {
	// 3 stories in a 20-tall terminal — all should fit
	app := newTestApp(makeStories(3), 120, 20)
	listHeight := app.storiesListHeight()

	if len(app.prd.UserStories) > listHeight {
		t.Skipf("stories (%d) > listHeight (%d), skipping", len(app.prd.UserStories), listHeight)
	}

	// Navigate through all stories
	for i := 0; i < len(app.prd.UserStories); i++ {
		app.selectedIndex = i
		app.adjustStoriesScroll()
	}

	if app.storiesScrollOffset != 0 {
		t.Errorf("expected storiesScrollOffset 0 when all stories fit, got %d", app.storiesScrollOffset)
	}
}

func TestScrollOffset_ClampsToValidRange(t *testing.T) {
	app := newTestApp(makeStories(20), 120, 20)
	listHeight := app.storiesListHeight()

	// Force an invalid scroll offset
	app.storiesScrollOffset = 100
	app.adjustStoriesScroll()

	maxOffset := len(app.prd.UserStories) - listHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	if app.storiesScrollOffset > maxOffset {
		t.Errorf("expected storiesScrollOffset <= %d, got %d", maxOffset, app.storiesScrollOffset)
	}

	// Force negative
	app.storiesScrollOffset = -5
	app.adjustStoriesScroll()
	if app.storiesScrollOffset < 0 {
		t.Errorf("expected storiesScrollOffset >= 0, got %d", app.storiesScrollOffset)
	}
}

func TestScrollRange_ShownWhenScrollable(t *testing.T) {
	app := newTestApp(makeStories(20), 120, 20)

	// With 20 stories and listHeight = 15-5=10, the list is scrollable, so the
	// title names the visible slice.
	output := app.renderStoriesPanel(40, 15)
	if !strings.Contains(output, "Stories 1-10 of 20") {
		t.Errorf("expected visible range in panel title, got: %s", output)
	}

	app.storiesScrollOffset = 10
	output = app.renderStoriesPanel(40, 15)
	if !strings.Contains(output, "Stories 11-20 of 20") {
		t.Errorf("expected scrolled range in panel title, got: %s", output)
	}
}

func TestScrollRange_NotShownWhenNotScrollable(t *testing.T) {
	app := newTestApp(makeStories(3), 120, 20)

	output := app.renderStoriesPanel(40, 15)

	// 3 stories fit in listHeight=10, so the title stays bare.
	if !strings.Contains(output, "Stories") || strings.Contains(output, " of 3") {
		t.Errorf("expected no scroll range when list fits, got: %s", output)
	}
}

// The bottom of the stories panel shows run completion. The title must not also
// show a percentage, or 100%-scrolled reads as 100%-done.
func TestStoriesTitle_HasNoPercentage(t *testing.T) {
	app := newTestApp(makeStories(20), 120, 20)
	app.storiesScrollOffset = 10

	output := app.renderStoriesPanel(40, 15)
	var title string
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "Stories") {
			title = line
			break
		}
	}
	if title == "" {
		t.Fatalf("no title line found in panel: %s", output)
	}
	if strings.Contains(title, "%") {
		t.Errorf("expected no percentage in stories panel title, got: %s", title)
	}
}

func TestFooterHidden_WhenHeightLessThan12(t *testing.T) {
	app := newTestApp(makeStories(5), 120, 11)

	output := app.renderDashboard()

	// The footer contains "quit" shortcut — should not be present
	if strings.Contains(output, "q: quit") {
		t.Error("expected footer to be hidden when height < 12")
	}
}

func TestFooterShown_WhenHeightAtLeast12(t *testing.T) {
	// Need enough height to render without panic
	app := newTestApp(makeStories(5), 120, 20)

	output := app.renderDashboard()

	if !strings.Contains(output, "q: quit") {
		t.Error("expected footer to be shown when height >= 12")
	}
}

func TestAndNMore_Removed(t *testing.T) {
	// Create more stories than can fit in the panel
	app := newTestApp(makeStories(20), 120, 15)

	output := app.renderStoriesPanel(40, 12)

	if strings.Contains(output, "... and") || strings.Contains(output, "more") {
		t.Error("expected '... and N more' to be removed from stories panel")
	}
}
