package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPRDNameStepAcceptsValidName(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.prdName = "billing-v2"

	model, _ := f.handlePRDNameKeys(key("enter"))

	got := model.(FirstTimeSetup)
	if got.result.PRDName != "billing-v2" {
		t.Errorf("expected the PRD name 'billing-v2', got %q", got.result.PRDName)
	}
	if got.step != StepPostCompletion {
		t.Errorf("expected an advance to StepPostCompletion, got %v", got.step)
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

	model, _ := f.handlePRDNameKeys(key("enter"))

	got := model.(FirstTimeSetup)
	if got.step != StepPRDName {
		t.Errorf("expected to stay on the name step, got %v", got.step)
	}
	if got.prdNameError == "" {
		t.Error("expected an error message for an empty name")
	}
}

func TestPRDNameStepRejectsInvalidCharacters(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	// Set directly rather than typed: the keystroke filter would have blocked it.
	f.prdName = "my prd/../etc"

	model, _ := f.handlePRDNameKeys(key("enter"))

	got := model.(FirstTimeSetup)
	if got.step != StepPRDName {
		t.Errorf("expected to stay on the name step, got %v", got.step)
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
	// The PRD name is the first step now, so there is nothing behind it to step
	// back to: esc ends setup rather than moving.
	f := NewFirstTimeSetup(t.TempDir())
	f.step = StepPRDName

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

func TestPostCompletionStepDefaultsToBothYes(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.step = StepPostCompletion

	if f.pushSelected != 0 || f.createPRSelected != 0 {
		t.Errorf("expected both toggles on 'Yes', got push=%d pr=%d", f.pushSelected, f.createPRSelected)
	}
}

func TestPostCompletionFieldNavigationClamps(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.step = StepPostCompletion

	model, _ := f.handlePostCompletionKeys(key("up"))
	if got := model.(FirstTimeSetup).postCompField; got != 0 {
		t.Errorf("expected the field clamped at 0, got %d", got)
	}

	model, _ = model.(FirstTimeSetup).handlePostCompletionKeys(key("down"))
	if got := model.(FirstTimeSetup).postCompField; got != 1 {
		t.Errorf("expected the field at 1, got %d", got)
	}

	model, _ = model.(FirstTimeSetup).handlePostCompletionKeys(key("down"))
	if got := model.(FirstTimeSetup).postCompField; got != 1 {
		t.Errorf("expected the field clamped at 1, got %d", got)
	}
}

func TestPostCompletionSpaceTogglesTheFocusedFieldOnly(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.step = StepPostCompletion

	model, _ := f.handlePostCompletionKeys(key(" "))
	got := model.(FirstTimeSetup)
	if got.pushSelected != 1 {
		t.Errorf("expected push toggled to 'No', got %d", got.pushSelected)
	}
	if got.createPRSelected != 0 {
		t.Errorf("expected the PR toggle untouched, got %d", got.createPRSelected)
	}

	// Move to the PR field and toggle that one.
	model, _ = got.handlePostCompletionKeys(key("down"))
	model, _ = model.(FirstTimeSetup).handlePostCompletionKeys(key(" "))
	got = model.(FirstTimeSetup)
	if got.createPRSelected != 1 {
		t.Errorf("expected the PR toggle at 'No', got %d", got.createPRSelected)
	}
	if got.pushSelected != 1 {
		t.Errorf("expected the push toggle to keep its value, got %d", got.pushSelected)
	}
}

func TestPostCompletionYesNoKeysSetTheFocusedField(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.step = StepPostCompletion

	model, _ := f.handlePostCompletionKeys(key("n"))
	if got := model.(FirstTimeSetup).pushSelected; got != 1 {
		t.Errorf("expected 'n' to set push to 'No', got %d", got)
	}

	model, _ = model.(FirstTimeSetup).handlePostCompletionKeys(key("y"))
	if got := model.(FirstTimeSetup).pushSelected; got != 0 {
		t.Errorf("expected 'y' to set push back to 'Yes', got %d", got)
	}
}

func TestPostCompletionArrowKeysSetTheFocusedField(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.step = StepPostCompletion
	f.postCompField = 1 // PR field

	model, _ := f.handlePostCompletionKeys(key("right"))
	if got := model.(FirstTimeSetup).createPRSelected; got != 1 {
		t.Errorf("expected right to select 'No', got %d", got)
	}

	model, _ = model.(FirstTimeSetup).handlePostCompletionKeys(key("left"))
	if got := model.(FirstTimeSetup).createPRSelected; got != 0 {
		t.Errorf("expected left to select 'Yes', got %d", got)
	}
}

func TestPostCompletionWithoutPRFinishesImmediately(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.step = StepPostCompletion
	f.pushSelected = 0
	f.createPRSelected = 1 // no PR, so no gh check needed

	model, cmd := f.handlePostCompletionKeys(key("enter"))

	got := model.(FirstTimeSetup)
	if !got.result.PushOnComplete {
		t.Error("expected PushOnComplete recorded")
	}
	if got.result.CreatePROnComplete {
		t.Error("expected CreatePROnComplete to stay false")
	}
	if !isQuitCmd(cmd) {
		t.Error("expected setup to finish without a gh check")
	}
}

func TestPostCompletionWithPRRunsTheGHCheck(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.step = StepPostCompletion
	f.createPRSelected = 0 // PR requested

	model, cmd := f.handlePostCompletionKeys(key("enter"))

	got := model.(FirstTimeSetup)
	if !got.result.CreatePROnComplete {
		t.Error("expected CreatePROnComplete recorded")
	}
	// Setup must not finish before gh is known to work, or the first completed
	// run would fail at PR time.
	if cmd == nil {
		t.Fatal("expected a gh-check command")
	}
	if isQuitCmd(cmd) {
		t.Error("expected the gh check to run rather than exiting")
	}
}

func TestPostCompletionEscGoesBackToNameStep(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.step = StepPostCompletion

	model, cmd := f.handlePostCompletionKeys(key("esc"))

	if got := model.(FirstTimeSetup).step; got != StepPRDName {
		t.Errorf("expected a step back to StepPRDName, got %v", got)
	}
	if isQuitCmd(cmd) {
		t.Error("esc must step back, not exit")
	}
}

func TestGHCheckSuccessFinishesSetup(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.step = StepPostCompletion

	model, cmd := f.handleGHCheckResult(ghCheckResultMsg{installed: true, authenticated: true})

	if got := model.(FirstTimeSetup).step; got == StepGHError {
		t.Error("expected no error step when gh is ready")
	}
	if !isQuitCmd(cmd) {
		t.Error("expected setup to finish when gh is ready")
	}
}

func TestGHCheckNotInstalledShowsErrorStep(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())

	model, _ := f.handleGHCheckResult(ghCheckResultMsg{installed: false})

	got := model.(FirstTimeSetup)
	if got.step != StepGHError {
		t.Errorf("expected StepGHError, got %v", got.step)
	}
	if !strings.Contains(got.ghErrorMsg, "not installed") {
		t.Errorf("expected an install hint, got %q", got.ghErrorMsg)
	}
}

func TestGHCheckNotAuthenticatedShowsErrorStep(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())

	model, _ := f.handleGHCheckResult(ghCheckResultMsg{installed: true, authenticated: false})

	got := model.(FirstTimeSetup)
	if got.step != StepGHError {
		t.Errorf("expected StepGHError, got %v", got.step)
	}
	if !strings.Contains(got.ghErrorMsg, "auth login") {
		t.Errorf("expected an auth hint, got %q", got.ghErrorMsg)
	}
}

func TestGHCheckErrorShowsErrorStep(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())

	model, _ := f.handleGHCheckResult(ghCheckResultMsg{err: errors.New("exec failed")})

	got := model.(FirstTimeSetup)
	if got.step != StepGHError {
		t.Errorf("expected StepGHError, got %v", got.step)
	}
	if !strings.Contains(got.ghErrorMsg, "exec failed") {
		t.Errorf("expected the underlying error surfaced, got %q", got.ghErrorMsg)
	}
}

func TestGHErrorContinueWithoutPRDisablesPR(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.step = StepGHError
	f.result.CreatePROnComplete = true
	f.ghErrorSelected = 0 // "Continue without PR"

	model, cmd := f.handleGHErrorKeys(key("enter"))

	got := model.(FirstTimeSetup)
	// Leaving createPR on with a broken gh would fail at the end of the first run.
	if got.result.CreatePROnComplete {
		t.Error("expected PR creation disabled when continuing without gh")
	}
	if !isQuitCmd(cmd) {
		t.Error("expected setup to finish")
	}
}

func TestGHErrorTryAgainRunsTheCheckAgain(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.step = StepGHError
	f.result.CreatePROnComplete = true
	f.ghErrorSelected = 1 // "Try again"

	model, cmd := f.handleGHErrorKeys(key("enter"))

	got := model.(FirstTimeSetup)
	// The user may have installed gh in another terminal, so the choice stands.
	if !got.result.CreatePROnComplete {
		t.Error("expected PR creation to stay enabled while retrying")
	}
	if cmd == nil {
		t.Fatal("expected another gh-check command")
	}
	if isQuitCmd(cmd) {
		t.Error("expected a retry rather than an exit")
	}
}

func TestGHErrorNavigationClamps(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.step = StepGHError

	model, _ := f.handleGHErrorKeys(key("up"))
	if got := model.(FirstTimeSetup).ghErrorSelected; got != 0 {
		t.Errorf("expected the selection clamped at 0, got %d", got)
	}

	model, _ = model.(FirstTimeSetup).handleGHErrorKeys(key("down"))
	model, _ = model.(FirstTimeSetup).handleGHErrorKeys(key("down"))
	if got := model.(FirstTimeSetup).ghErrorSelected; got != 1 {
		t.Errorf("expected the selection clamped at 1, got %d", got)
	}
}

func TestGHErrorEscGoesBack(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.step = StepGHError

	model, _ := f.handleGHErrorKeys(key("esc"))

	if got := model.(FirstTimeSetup).step; got != StepPostCompletion {
		t.Errorf("expected a step back to StepPostCompletion, got %v", got)
	}
}

func TestFirstTimeSetupUpdateRoutesByStep(t *testing.T) {
	dir := t.TempDir()

	// The same key means different things per step, so routing has to follow the
	// current step rather than a global key map.
	name := NewFirstTimeSetup(dir)
	name.prdName = ""
	model, _ := name.Update(key("x"))
	if got := model.(FirstTimeSetup).prdName; got != "x" {
		t.Errorf("expected the name step to type 'x', got %q", got)
	}

	post := NewFirstTimeSetup(dir)
	post.step = StepPostCompletion
	model, _ = post.Update(key("down"))
	if got := model.(FirstTimeSetup).postCompField; got != 1 {
		t.Errorf("expected the post-completion step to handle 'down', got %d", got)
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
	f.result = FirstTimeSetupResult{
		PRDName:            "auth",
		PushOnComplete:     true,
		CreatePROnComplete: false,
	}

	got := f.GetResult()
	if got.PRDName != "auth" || !got.PushOnComplete || got.CreatePROnComplete {
		t.Errorf("expected the result passed through unchanged, got %+v", got)
	}
}

func TestFirstTimeSetupViewRendersEveryStep(t *testing.T) {
	f := NewFirstTimeSetup(t.TempDir())
	f.width, f.height = 100, 30
	f.ghErrorMsg = "GitHub CLI (gh) is not installed."

	for _, step := range []FirstTimeSetupStep{
		StepPRDName,
		StepPostCompletion,
		StepGHError,
	} {
		f.step = step
		if out := f.View(); strings.TrimSpace(out) == "" {
			t.Errorf("expected a non-empty view for step %v", step)
		}
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
