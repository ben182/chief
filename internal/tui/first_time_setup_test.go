package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPRDNameStepAcceptsValidName(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.prdName = "billing-v2"

	model, cmd := f.handlePRDNameKeys(key("enter"))

	got := model.(FirstTimeSetup)
	if got.result.PRDName != "billing-v2" {
		t.Errorf("expected the PRD name 'billing-v2', got %q", got.result.PRDName)
	}
	// The name is the only question left, so Enter ends the screen.
	if !isQuitCmd(cmd) {
		t.Error("expected Enter on a valid name to finish setup")
	}
}

func TestPRDNameStepTrimsWhitespace(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.prdName = "  auth  "

	model, _ := f.handlePRDNameKeys(key("enter"))

	// The name becomes a directory name, so stray spaces would create an
	// awkward-to-reach .chief/prds/ entry.
	if got := model.(FirstTimeSetup).result.PRDName; got != "auth" {
		t.Errorf("expected a trimmed name 'auth', got %q", got)
	}
}

func TestPRDNameStepRejectsEmptyName(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.prdName = ""

	model, cmd := f.handlePRDNameKeys(key("enter"))

	got := model.(FirstTimeSetup)
	if isQuitCmd(cmd) {
		t.Error("expected to stay on the name step")
	}
	if got.prdNameError == "" {
		t.Error("expected an error message for an empty name")
	}
}

func TestPRDNameStepRejectsInvalidCharacters(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	// Set directly rather than typed: the keystroke filter would have blocked it.
	f.prdName = "my prd/../etc"

	model, cmd := f.handlePRDNameKeys(key("enter"))

	got := model.(FirstTimeSetup)
	if isQuitCmd(cmd) {
		t.Error("expected to stay on the name step")
	}
	if got.prdNameError == "" {
		t.Error("expected an error message for an invalid name")
	}
	if got.result.PRDName != "" {
		t.Errorf("expected no name recorded, got %q", got.result.PRDName)
	}
}

func TestPRDNameStepTypingFiltersInvalidCharacters(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.prdName = ""

	model := tea.Model(*f)
	// Slashes and spaces would escape .chief/prds/, so they never enter the buffer.
	for _, ch := range "a b/c-1_x" {
		model, _ = model.(FirstTimeSetup).handlePRDNameKeys(key(string(ch)))
	}

	if got := model.(FirstTimeSetup).prdName; got != "abc-1_x" {
		t.Errorf("expected the filtered name 'abc-1_x', got %q", got)
	}
}

func TestPRDNameStepBackspaceClearsError(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.prdName = "abc"
	f.prdNameError = "Name cannot be empty"

	model, _ := f.handlePRDNameKeys(key("backspace"))

	got := model.(FirstTimeSetup)
	if got.prdName != "ab" {
		t.Errorf("expected 'ab' after backspace, got %q", got.prdName)
	}
	// The error described the old value; leaving it up would be confusing.
	if got.prdNameError != "" {
		t.Errorf("expected the error cleared on edit, got %q", got.prdNameError)
	}
}

func TestPRDNameStepBackspaceOnEmptyIsSafe(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.prdName = ""

	model, _ := f.handlePRDNameKeys(key("backspace"))

	if got := model.(FirstTimeSetup).prdName; got != "" {
		t.Errorf("expected the name to stay empty, got %q", got)
	}
}

func TestPRDNameStepEscCancels(t *testing.T) {
	// The PRD name is the only step, so there is nothing behind it to step
	// back to: esc ends setup rather than moving.
	f := NewFirstTimeSetup(t.TempDir())

	model, cmd := f.handlePRDNameKeys(key("esc"))

	got := model.(FirstTimeSetup)
	if !got.result.Cancelled {
		t.Error("expected esc on the first step to cancel setup")
	}
	if !isQuitCmd(cmd) {
		t.Error("expected setup to exit on cancel")
	}
}
func TestPRDNameStepEscCancelsWhenItIsTheFirstStep(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())

	model, cmd := f.handlePRDNameKeys(key("esc"))

	got := model.(FirstTimeSetup)
	// With no previous step, esc is the only way out.
	if !got.result.Cancelled {
		t.Error("expected esc to cancel on the first step")
	}
	if !isQuitCmd(cmd) {
		t.Error("expected setup to exit")
	}
}

func TestIsValidPRDName(t *testing.T) {
	valid := []string{"default", "auth", "billing-v2", "my_prd", "US-001", "a"}
	for _, name := range valid {
		if !isValidPRDName(name) {
			t.Errorf("expected %q to be valid", name)
		}
	}

	// Anything that could escape or complicate .chief/prds/<name>.
	invalid := []string{"", "my prd", "a/b", "../etc", "a.b", "naïve", "a:b"}
	for _, name := range invalid {
		if isValidPRDName(name) {
			t.Errorf("expected %q to be rejected", name)
		}
	}
}

func TestFirstTimeSetupUpdateTracksWindowSize(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())

	model, _ := f.Update(tea.WindowSizeMsg{Width: 110, Height: 44})

	got := model.(FirstTimeSetup)
	if got.width != 110 || got.height != 44 {
		t.Errorf("expected the size tracked as 110x44, got %dx%d", got.width, got.height)
	}
}

func TestFirstTimeSetupGetResult(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.result = FirstTimeSetupResult{PRDName: "auth"}

	got := f.GetResult()
	if got.PRDName != "auth" || got.Cancelled {
		t.Errorf("expected the result passed through unchanged, got %+v", got)
	}
}

func TestFirstTimeSetupViewRenders(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.width, f.height = 100, 30

	if out := f.View(); strings.TrimSpace(out) == "" {
		t.Error("expected a non-empty view")
	}
}

func TestFirstTimeSetupPRDNameViewShowsValidationError(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.width, f.height = 100, 30
	f.prdNameError = "Name cannot be empty"

	if out := f.View(); !strings.Contains(out, "Name cannot be empty") {
		t.Errorf("expected the validation error shown, got:\n%s", out)
	}
}
