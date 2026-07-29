package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ben182/chief/internal/loop"
	tea "github.com/charmbracelet/bubbletea"
)

// writePRDFixture writes a PRD with the given story statuses to
// <base>/.chief/prds/<name>/prd.md and returns its path.
func writePRDFixture(t *testing.T, base, name string, statuses ...string) string {
	t.Helper()
	dir := filepath.Join(base, ".chief", "prds", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}

	var b strings.Builder
	b.WriteString("# " + name + "\n\n")
	for i, status := range statuses {
		b.WriteString("### US-00")
		b.WriteByte(byte('1' + i))
		b.WriteString(": Story ")
		b.WriteByte(byte('1' + i))
		b.WriteString("\n\n**Status:** " + status + "\n\n")
	}

	path := filepath.Join(dir, "prd.md")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// pickerApp builds an App with a live picker over base, which is what the picker
// key handler and switchToPRD operate on.
func pickerApp(t *testing.T, base, currentPRD string) *App {
	t.Helper()
	m := loop.NewManager(10, nil)
	a := newTestApp(nil, 100, 30)
	a.baseDir = base
	a.prdName = currentPRD
	a.manager = m
	a.picker = NewPRDPicker(base, currentPRD, m)
	a.tabBar = NewTabBar(base, currentPRD, m)
	a.logViewer = NewLogViewer()
	a.quitConfirm = NewQuitConfirmation()
	a.completionScreen = NewCompletionScreen()
	a.viewMode = ViewPicker
	return a
}

func key(s string) tea.KeyMsg {
	switch s {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestPickerEscReturnsToDashboard(t *testing.T) {
	base := t.TempDir()
	writePRDFixture(t, base, "auth", "todo")
	a := pickerApp(t, base, "auth")

	model, _ := a.handlePickerKeys(key("esc"))

	if got := model.(App); got.viewMode != ViewDashboard {
		t.Errorf("expected ViewDashboard, got %v", got.viewMode)
	}
}

func TestPickerLKeyReturnsToDashboard(t *testing.T) {
	base := t.TempDir()
	writePRDFixture(t, base, "auth", "todo")
	a := pickerApp(t, base, "auth")

	model, _ := a.handlePickerKeys(key("l"))

	if got := model.(App); got.viewMode != ViewDashboard {
		t.Errorf("expected ViewDashboard, got %v", got.viewMode)
	}
}

func TestPickerNavigationMovesSelection(t *testing.T) {
	base := t.TempDir()
	writePRDFixture(t, base, "alpha", "todo")
	writePRDFixture(t, base, "beta", "todo")
	a := pickerApp(t, base, "alpha")

	if len(a.picker.entries) < 2 {
		t.Fatalf("fixture: expected 2 entries, got %d", len(a.picker.entries))
	}

	model, _ := a.handlePickerKeys(key("down"))
	got := model.(App)
	if got.picker.selectedIndex != 1 {
		t.Errorf("expected selectedIndex 1 after down, got %d", got.picker.selectedIndex)
	}

	model, _ = got.handlePickerKeys(key("up"))
	got = model.(App)
	if got.picker.selectedIndex != 0 {
		t.Errorf("expected selectedIndex 0 after up, got %d", got.picker.selectedIndex)
	}
}

func TestPickerVimNavigationMovesSelection(t *testing.T) {
	base := t.TempDir()
	writePRDFixture(t, base, "alpha", "todo")
	writePRDFixture(t, base, "beta", "todo")
	a := pickerApp(t, base, "alpha")

	model, _ := a.handlePickerKeys(key("j"))
	got := model.(App)
	if got.picker.selectedIndex != 1 {
		t.Errorf("expected selectedIndex 1 after 'j', got %d", got.picker.selectedIndex)
	}

	model, _ = got.handlePickerKeys(key("k"))
	got = model.(App)
	if got.picker.selectedIndex != 0 {
		t.Errorf("expected selectedIndex 0 after 'k', got %d", got.picker.selectedIndex)
	}
}

func TestPickerNStartsInputMode(t *testing.T) {
	base := t.TempDir()
	writePRDFixture(t, base, "auth", "todo")
	a := pickerApp(t, base, "auth")

	model, _ := a.handlePickerKeys(key("n"))

	if got := model.(App); !got.picker.IsInputMode() {
		t.Error("expected 'n' to start input mode")
	}
}

func TestPickerInputModeCollectsCharacters(t *testing.T) {
	base := t.TempDir()
	a := pickerApp(t, base, "")
	a.picker.StartInputMode()

	// The picker is shared through the App copies the handler returns, so the
	// characters accumulate on the same overlay.
	for _, ch := range "billing" {
		a.handlePickerKeys(key(string(ch)))
	}

	if got := a.picker.GetInputValue(); got != "billing" {
		t.Errorf("expected input value 'billing', got %q", got)
	}
}

func TestPickerInputModeBackspaceDeletes(t *testing.T) {
	base := t.TempDir()
	a := pickerApp(t, base, "")
	a.picker.StartInputMode()
	for _, ch := range "abc" {
		a.picker.AddInputChar(ch)
	}

	model, _ := a.handlePickerKeys(key("backspace"))

	if got := model.(App).picker.GetInputValue(); got != "ab" {
		t.Errorf("expected 'ab' after backspace, got %q", got)
	}
}

func TestPickerInputModeEscCancels(t *testing.T) {
	base := t.TempDir()
	a := pickerApp(t, base, "")
	a.picker.StartInputMode()
	a.picker.AddInputChar('x')

	model, _ := a.handlePickerKeys(key("esc"))

	got := model.(App)
	if got.picker.IsInputMode() {
		t.Error("expected esc to leave input mode")
	}
	// Escaping the name prompt must not also leave the picker.
	if got.viewMode != ViewPicker {
		t.Errorf("expected to stay on the picker, got view %v", got.viewMode)
	}
}

func TestPickerInputModeEnterLaunchesInit(t *testing.T) {
	base := t.TempDir()
	a := pickerApp(t, base, "")
	a.picker.StartInputMode()
	for _, ch := range "billing" {
		a.picker.AddInputChar(ch)
	}

	model, cmd := a.handlePickerKeys(key("enter"))

	if got := model.(App); got.picker.IsInputMode() {
		t.Error("expected input mode to end on enter")
	}
	if cmd == nil {
		t.Fatal("expected a command to launch the init session")
	}
	msg, ok := cmd().(LaunchInitMsg)
	if !ok {
		t.Fatalf("expected a LaunchInitMsg, got %T", cmd())
	}
	if msg.Name != "billing" {
		t.Errorf("expected the typed name 'billing', got %q", msg.Name)
	}
}

func TestPickerInputModeEnterOnEmptyNameJustCancels(t *testing.T) {
	base := t.TempDir()
	a := pickerApp(t, base, "")
	a.picker.StartInputMode()

	model, cmd := a.handlePickerKeys(key("enter"))

	if got := model.(App); got.picker.IsInputMode() {
		t.Error("expected input mode to end")
	}
	// An empty name must not start an agent session for a nameless PRD.
	if cmd != nil {
		t.Error("expected no launch command for an empty name")
	}
}

func TestPickerEnterSwitchesToSelectedPRD(t *testing.T) {
	base := t.TempDir()
	writePRDFixture(t, base, "alpha", "done")
	writePRDFixture(t, base, "beta", "todo", "todo")
	a := pickerApp(t, base, "alpha")

	// Select the second entry, whichever order Refresh produced.
	target := ""
	for i, e := range a.picker.entries {
		if e.Name == "beta" {
			a.picker.selectedIndex = i
			target = e.Name
			break
		}
	}
	if target == "" {
		t.Fatal("fixture: 'beta' not found in the picker entries")
	}

	model, _ := a.handlePickerKeys(key("enter"))

	got := model.(App)
	if got.prdName != "beta" {
		t.Errorf("expected a switch to 'beta', got %q", got.prdName)
	}
	if got.viewMode != ViewDashboard {
		t.Errorf("expected the dashboard after switching, got view %v", got.viewMode)
	}
}

func TestPickerEnterOnLoadErrorEntryDoesNothing(t *testing.T) {
	base := t.TempDir()
	writePRDFixture(t, base, "auth", "todo")
	a := pickerApp(t, base, "auth")
	// A PRD that cannot be parsed must not become the active one.
	a.picker.entries = []PRDEntry{{Name: "broken", Path: "/nope/prd.md", LoadError: os.ErrNotExist}}
	a.picker.selectedIndex = 0

	model, cmd := a.handlePickerKeys(key("enter"))

	got := model.(App)
	if got.prdName != "auth" {
		t.Errorf("expected the active PRD unchanged, got %q", got.prdName)
	}
	if cmd != nil {
		t.Error("expected no command for an unloadable entry")
	}
}

func TestPickerEditLaunchesEditForSelectedPRD(t *testing.T) {
	base := t.TempDir()
	writePRDFixture(t, base, "auth", "todo")
	a := pickerApp(t, base, "auth")

	model, cmd := a.handlePickerKeys(key("e"))

	_ = model
	if cmd == nil {
		t.Fatal("expected a command to launch the edit session")
	}
	msg, ok := cmd().(LaunchEditMsg)
	if !ok {
		t.Fatalf("expected a LaunchEditMsg, got %T", cmd())
	}
	if msg.Name != "auth" {
		t.Errorf("expected the selected PRD 'auth', got %q", msg.Name)
	}
}

func TestPickerEditOnArchivedEntryDoesNothing(t *testing.T) {
	base := t.TempDir()
	writePRDFixture(t, base, "auth", "todo")
	a := pickerApp(t, base, "auth")
	a.picker.entries = []PRDEntry{{Name: "old", Path: "/x/prd.md", Archived: true}}
	a.picker.selectedIndex = 0

	_, cmd := a.handlePickerKeys(key("e"))

	// Editing an archived PRD would write into .chief/archive/.
	if cmd != nil {
		t.Error("expected no edit command for an archived entry")
	}
}

func TestPickerStartOnRunningPRDIsIgnored(t *testing.T) {
	base := t.TempDir()
	writePRDFixture(t, base, "auth", "todo")
	a := pickerApp(t, base, "auth")
	a.picker.entries = []PRDEntry{{Name: "auth", Path: "/x/prd.md", LoopState: loop.LoopStateRunning}}
	a.picker.selectedIndex = 0

	model, cmd := a.handlePickerKeys(key("s"))

	got := model.(App)
	// Starting a second loop for a PRD already running would double its commits.
	if got.viewMode != ViewPicker {
		t.Errorf("expected to stay on the picker, got view %v", got.viewMode)
	}
	if cmd != nil {
		t.Error("expected no start command for an already-running PRD")
	}
}

func TestPickerPauseOnlyActsOnRunningPRD(t *testing.T) {
	base := t.TempDir()
	writePRDFixture(t, base, "auth", "todo")
	a := pickerApp(t, base, "auth")
	a.picker.entries = []PRDEntry{{Name: "auth", Path: "/x/prd.md", LoopState: loop.LoopStateReady}}
	a.picker.selectedIndex = 0

	model, _ := a.handlePickerKeys(key("p"))

	got := model.(App)
	if got.lastActivity != "" {
		t.Errorf("expected no pause attempt for an idle PRD, got %q", got.lastActivity)
	}
}

func TestPickerStopOnlyActsOnRunningOrPausedPRD(t *testing.T) {
	base := t.TempDir()
	writePRDFixture(t, base, "auth", "todo")
	a := pickerApp(t, base, "auth")
	a.picker.entries = []PRDEntry{{Name: "auth", Path: "/x/prd.md", LoopState: loop.LoopStateComplete}}
	a.picker.selectedIndex = 0

	model, _ := a.handlePickerKeys(key("x"))

	got := model.(App)
	if got.lastActivity != "" {
		t.Errorf("expected no stop attempt for a completed PRD, got %q", got.lastActivity)
	}
}

func TestPickerCleanResultDismissedOnAnyKey(t *testing.T) {
	base := t.TempDir()
	writePRDFixture(t, base, "auth", "todo")
	a := pickerApp(t, base, "auth")
	a.picker.SetCleanResult(&CleanResult{Success: true, Message: "cleaned"})

	model, _ := a.handlePickerKeys(key("z"))

	got := model.(App)
	if got.picker.HasCleanResult() {
		t.Error("expected the clean result dismissed on any key")
	}
	// The dismissing key must not also act as a picker command.
	if got.viewMode != ViewPicker {
		t.Errorf("expected to stay on the picker, got view %v", got.viewMode)
	}
}

func TestPickerMergeResultDismissedOnAnyKey(t *testing.T) {
	base := t.TempDir()
	writePRDFixture(t, base, "auth", "todo")
	a := pickerApp(t, base, "auth")
	a.picker.SetMergeResult(&MergeResult{Success: true, Message: "merged"})

	model, _ := a.handlePickerKeys(key("z"))

	got := model.(App)
	if got.picker.HasMergeResult() {
		t.Error("expected the merge result dismissed on any key")
	}
	if got.viewMode != ViewPicker {
		t.Errorf("expected to stay on the picker, got view %v", got.viewMode)
	}
}

func TestPickerUnknownKeyIsInert(t *testing.T) {
	base := t.TempDir()
	writePRDFixture(t, base, "auth", "todo")
	a := pickerApp(t, base, "auth")

	model, cmd := a.handlePickerKeys(key("z"))

	got := model.(App)
	if got.viewMode != ViewPicker {
		t.Errorf("expected to stay on the picker, got view %v", got.viewMode)
	}
	if cmd != nil {
		t.Error("expected no command for an unknown key")
	}
}

func TestSwitchToPRDLoadsStateAndActivity(t *testing.T) {
	base := t.TempDir()
	writePRDFixture(t, base, "alpha", "done")
	betaPath := writePRDFixture(t, base, "beta", "done", "todo", "todo")
	a := pickerApp(t, base, "alpha")

	model, _ := a.switchToPRD("beta", betaPath)

	got := model.(App)
	if got.prdName != "beta" {
		t.Errorf("expected prdName 'beta', got %q", got.prdName)
	}
	if got.prdPath != betaPath {
		t.Errorf("expected prdPath %q, got %q", betaPath, got.prdPath)
	}
	if got.prd == nil || len(got.prd.UserStories) != 3 {
		t.Fatalf("expected the 3-story PRD loaded, got %v", got.prd)
	}
	if !strings.Contains(got.lastActivity, "beta") {
		t.Errorf("expected the switch reported in lastActivity, got %q", got.lastActivity)
	}
	if got.viewMode != ViewDashboard {
		t.Errorf("expected the dashboard after switching, got view %v", got.viewMode)
	}
	// A fresh PRD starts at the top of its story list.
	if got.selectedIndex != 0 || got.storiesScrollOffset != 0 {
		t.Errorf("expected the story list reset, got index %d offset %d", got.selectedIndex, got.storiesScrollOffset)
	}
}

func TestSwitchToPRDRegistersWithManager(t *testing.T) {
	base := t.TempDir()
	writePRDFixture(t, base, "alpha", "done")
	betaPath := writePRDFixture(t, base, "beta", "todo")
	a := pickerApp(t, base, "alpha")

	if a.manager.GetInstance("beta") != nil {
		t.Fatal("precondition: 'beta' should not be registered yet")
	}

	model, _ := a.switchToPRD("beta", betaPath)

	got := model.(App)
	if got.manager.GetInstance("beta") == nil {
		t.Error("expected the switched-to PRD registered with the manager")
	}
}

func TestSwitchToPRDBudgetsIterationsFromRemainingStories(t *testing.T) {
	base := t.TempDir()
	writePRDFixture(t, base, "alpha", "done")
	// Two stories left to do, one already done and one parked for review.
	betaPath := writePRDFixture(t, base, "beta", "done", "todo", "todo", "needs-review")
	a := pickerApp(t, base, "alpha")

	model, _ := a.switchToPRD("beta", betaPath)

	got := model.(App)
	want := 2*loop.DefaultMaxAttemptsPerStory + 5
	if got.maxIter != want {
		t.Errorf("expected maxIter %d for 2 remaining stories, got %d", want, got.maxIter)
	}
}

func TestSwitchToPRDKeepsAFloorOnIterations(t *testing.T) {
	base := t.TempDir()
	writePRDFixture(t, base, "alpha", "todo")
	// Nothing left to do: the budget still needs to allow a verification pass.
	donePath := writePRDFixture(t, base, "done-prd", "done", "done")
	a := pickerApp(t, base, "alpha")

	model, _ := a.switchToPRD("done-prd", donePath)

	if got := model.(App); got.maxIter < 5 {
		t.Errorf("expected a floor of 5 iterations, got %d", got.maxIter)
	}
}

func TestSwitchToPRDLoadErrorStaysOnDashboard(t *testing.T) {
	base := t.TempDir()
	writePRDFixture(t, base, "alpha", "todo")
	a := pickerApp(t, base, "alpha")

	model, cmd := a.switchToPRD("ghost", filepath.Join(base, "does-not-exist", "prd.md"))

	got := model.(App)
	if !strings.Contains(got.lastActivity, "Error loading PRD") {
		t.Errorf("expected a load error reported, got %q", got.lastActivity)
	}
	// The previously viewed PRD has to stay put rather than being half-replaced.
	if got.prdName != "alpha" {
		t.Errorf("expected the active PRD unchanged, got %q", got.prdName)
	}
	if cmd != nil {
		t.Error("expected no watcher commands after a failed load")
	}
}

func TestSwitchToPRDClearsTheLogViewer(t *testing.T) {
	base := t.TempDir()
	writePRDFixture(t, base, "alpha", "todo")
	betaPath := writePRDFixture(t, base, "beta", "todo")
	a := pickerApp(t, base, "alpha")
	a.logViewer.AddEvent(loop.Event{Type: loop.EventAssistantText, Text: "alpha's output"})

	model, _ := a.switchToPRD("beta", betaPath)

	got := model.(App)
	// The log holds only the viewed PRD's output; carrying it over would attribute
	// alpha's work to beta.
	if rendered := got.logViewer.Render(); strings.Contains(rendered, "alpha's output") {
		t.Errorf("expected the log cleared on switch, got:\n%s", rendered)
	}
}

func TestParseMergeSuccessMessageWithoutRepoFallsBackToGenericTarget(t *testing.T) {
	// Outside a repo the default branch is unknown; the message still has to name
	// the branch that was merged.
	got := parseMergeSuccessMessage(t.TempDir(), "chief/auth")

	if !strings.Contains(got, "chief/auth") {
		t.Errorf("expected the merged branch named, got %q", got)
	}
	if !strings.Contains(got, "current branch") {
		t.Errorf("expected the generic target fallback, got %q", got)
	}
}
