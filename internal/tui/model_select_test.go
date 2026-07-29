package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// optionIndex returns the index of the option carrying value, for assertions
// that must not hardcode the order of modelOptions.
func optionIndex(t *testing.T, value string) int {
	t.Helper()
	for i, opt := range modelOptions {
		if opt.value == value {
			return i
		}
	}
	t.Fatalf("no model option with value %q", value)
	return -1
}

func TestNewModelSelectPreSelectsAlias(t *testing.T) {
	m := NewModelSelect("Create PRD", "haiku")

	if got, want := m.selected, optionIndex(t, "haiku"); got != want {
		t.Errorf("expected the Haiku row pre-selected (index %d), got %d", want, got)
	}
	if m.customInput != "" {
		t.Errorf("expected no custom pre-fill for an alias, got %q", m.customInput)
	}
}

func TestNewModelSelectDefaultsToFirstOption(t *testing.T) {
	m := NewModelSelect("Create PRD", "")

	if got, want := m.selected, optionIndex(t, ""); got != want {
		t.Errorf("expected the Default row pre-selected (index %d), got %d", want, got)
	}
}

func TestNewModelSelectPreFillsCustomForSpecificModelID(t *testing.T) {
	m := NewModelSelect("Create PRD", "claude-opus-4-8")

	// A specific ID is not one of the aliases, so it has to land on Custom… with
	// the value kept — otherwise reopening the picker would silently drop it.
	if got, want := m.selected, optionIndex(t, customValue); got != want {
		t.Errorf("expected the Custom row pre-selected (index %d), got %d", want, got)
	}
	if m.customInput != "claude-opus-4-8" {
		t.Errorf("expected the model ID pre-filled, got %q", m.customInput)
	}
}

func TestModelSelectNavigationClamps(t *testing.T) {
	m := NewModelSelect("Create PRD", "")

	model := tea.Model(*m)
	// Walk past the top: index 0 must hold.
	for i := 0; i < 3; i++ {
		model, _ = model.(ModelSelect).handleListKeys(key("up"))
	}
	if got := model.(ModelSelect).selected; got != 0 {
		t.Errorf("expected the selection clamped at 0, got %d", got)
	}

	// Walk past the bottom: the last row must hold.
	for i := 0; i < len(modelOptions)+3; i++ {
		model, _ = model.(ModelSelect).handleListKeys(key("down"))
	}
	if got, want := model.(ModelSelect).selected, len(modelOptions)-1; got != want {
		t.Errorf("expected the selection clamped at %d, got %d", want, got)
	}
}

func TestModelSelectVimNavigation(t *testing.T) {
	m := NewModelSelect("Create PRD", "")

	model, _ := m.handleListKeys(key("j"))
	if got := model.(ModelSelect).selected; got != 1 {
		t.Errorf("expected 'j' to move down to 1, got %d", got)
	}

	model, _ = model.(ModelSelect).handleListKeys(key("k"))
	if got := model.(ModelSelect).selected; got != 0 {
		t.Errorf("expected 'k' to move back to 0, got %d", got)
	}
}

func TestModelSelectEnterPicksAlias(t *testing.T) {
	m := NewModelSelect("Create PRD", "")
	m.selected = optionIndex(t, "sonnet")

	model, cmd := m.handleListKeys(key("enter"))

	got := model.(ModelSelect)
	if got.result != "sonnet" {
		t.Errorf("expected the result 'sonnet', got %q", got.result)
	}
	if got.cancelled {
		t.Error("expected a confirmed pick, not a cancel")
	}
	if !isQuitCmd(cmd) {
		t.Error("expected the picker to close after a pick")
	}
}

func TestModelSelectEnterOnDefaultYieldsEmptyModel(t *testing.T) {
	m := NewModelSelect("Create PRD", "")
	m.selected = optionIndex(t, "")

	model, cmd := m.handleListKeys(key("enter"))

	got := model.(ModelSelect)
	// An empty result means "use the model from the Claude config", which is a
	// valid choice rather than a missing one.
	if got.result != "" {
		t.Errorf("expected an empty result for Default, got %q", got.result)
	}
	if got.cancelled {
		t.Error("expected Default to be a confirmed pick")
	}
	if !isQuitCmd(cmd) {
		t.Error("expected the picker to close")
	}
}

func TestModelSelectEnterOnCustomOpensInput(t *testing.T) {
	m := NewModelSelect("Create PRD", "")
	m.selected = optionIndex(t, customValue)

	model, cmd := m.handleListKeys(key("enter"))

	got := model.(ModelSelect)
	if !got.customMode {
		t.Error("expected Custom… to open the text input")
	}
	// The sentinel must never leak out as a model name.
	if got.result == customValue {
		t.Error("the custom sentinel leaked into the result")
	}
	if isQuitCmd(cmd) {
		t.Error("expected the picker to stay open for the custom input")
	}
}

func TestModelSelectEscCancels(t *testing.T) {
	m := NewModelSelect("Create PRD", "")

	model, cmd := m.handleListKeys(key("esc"))

	got := model.(ModelSelect)
	if !got.cancelled {
		t.Error("expected esc to cancel")
	}
	if !isQuitCmd(cmd) {
		t.Error("expected the picker to close on cancel")
	}
}

func TestModelSelectQuitKeysCancel(t *testing.T) {
	for _, k := range []string{"q", "esc"} {
		m := NewModelSelect("Create PRD", "")

		model, _ := m.handleListKeys(key(k))

		if !model.(ModelSelect).cancelled {
			t.Errorf("expected %q to cancel", k)
		}
	}
}

func TestModelSelectCustomInputCollectsCharacters(t *testing.T) {
	m := NewModelSelect("Create PRD", "")
	m.customMode = true

	model := tea.Model(*m)
	for _, ch := range "claude-opus-5" {
		model, _ = model.(ModelSelect).handleCustomKeys(key(string(ch)))
	}

	if got := model.(ModelSelect).customInput; got != "claude-opus-5" {
		t.Errorf("expected the typed model ID, got %q", got)
	}
}

func TestModelSelectCustomBackspaceDeletes(t *testing.T) {
	m := NewModelSelect("Create PRD", "")
	m.customMode = true
	m.customInput = "abc"

	model, _ := m.handleCustomKeys(key("backspace"))

	if got := model.(ModelSelect).customInput; got != "ab" {
		t.Errorf("expected 'ab' after backspace, got %q", got)
	}
}

func TestModelSelectCustomBackspaceOnEmptyInputIsSafe(t *testing.T) {
	m := NewModelSelect("Create PRD", "")
	m.customMode = true

	model, _ := m.handleCustomKeys(key("backspace"))

	if got := model.(ModelSelect).customInput; got != "" {
		t.Errorf("expected the input to stay empty, got %q", got)
	}
}

func TestModelSelectCustomEscReturnsToListKeepingInput(t *testing.T) {
	m := NewModelSelect("Create PRD", "")
	m.customMode = true
	m.customInput = "claude-opus-5"

	model, cmd := m.handleCustomKeys(key("esc"))

	got := model.(ModelSelect)
	if got.customMode {
		t.Error("expected esc to return to the list")
	}
	// Going back to the list must not discard what was typed.
	if got.customInput != "claude-opus-5" {
		t.Errorf("expected the typed value kept, got %q", got.customInput)
	}
	if isQuitCmd(cmd) {
		t.Error("esc from the custom input must not close the picker")
	}
}

func TestModelSelectCustomEnterUsesTrimmedValue(t *testing.T) {
	m := NewModelSelect("Create PRD", "")
	m.customMode = true
	m.customInput = "  claude-opus-5  "

	model, cmd := m.handleCustomKeys(key("enter"))

	got := model.(ModelSelect)
	// Stray whitespace would be passed to the CLI as part of the model name.
	if got.result != "claude-opus-5" {
		t.Errorf("expected a trimmed result, got %q", got.result)
	}
	if !isQuitCmd(cmd) {
		t.Error("expected the picker to close after confirming")
	}
}

func TestModelSelectCustomEnterOnBlankFallsBackToDefault(t *testing.T) {
	m := NewModelSelect("Create PRD", "")
	m.customMode = true
	m.customInput = "   "

	model, _ := m.handleCustomKeys(key("enter"))

	got := model.(ModelSelect)
	if got.result != "" {
		t.Errorf("expected a blank custom entry to behave like Default, got %q", got.result)
	}
	if got.cancelled {
		t.Error("expected a blank entry to confirm as Default, not cancel")
	}
}

func TestModelSelectUpdateRoutesByMode(t *testing.T) {
	m := NewModelSelect("Create PRD", "")

	// In list mode 'j' navigates.
	model, _ := m.Update(key("j"))
	if got := model.(ModelSelect).selected; got != 1 {
		t.Errorf("expected 'j' to navigate in list mode, got selected %d", got)
	}

	// In custom mode the same key is text.
	custom := NewModelSelect("Create PRD", "")
	custom.customMode = true
	model, _ = custom.Update(key("j"))
	if got := model.(ModelSelect).customInput; got != "j" {
		t.Errorf("expected 'j' to be typed in custom mode, got %q", got)
	}
}

func TestModelSelectUpdateTracksWindowSize(t *testing.T) {
	m := NewModelSelect("Create PRD", "")

	model, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	got := model.(ModelSelect)
	if got.width != 120 || got.height != 40 {
		t.Errorf("expected the size tracked as 120x40, got %dx%d", got.width, got.height)
	}
}

func TestModelSelectViewShowsTitleAndOptions(t *testing.T) {
	m := NewModelSelect("Create PRD", "")
	m.width, m.height = 100, 30

	out := m.View()
	if !strings.Contains(out, "Create PRD") {
		t.Errorf("expected the title in the view, got:\n%s", out)
	}
	for _, opt := range modelOptions {
		if !strings.Contains(out, opt.label) {
			t.Errorf("expected the option %q listed, got:\n%s", opt.label, out)
		}
	}
}

func TestModelSelectViewShowsCustomInput(t *testing.T) {
	m := NewModelSelect("Create PRD", "")
	m.width, m.height = 100, 30
	m.customMode = true
	m.customInput = "claude-opus-5"

	out := m.View()
	if !strings.Contains(out, "claude-opus-5") {
		t.Errorf("expected the typed model ID shown, got:\n%s", out)
	}
}
