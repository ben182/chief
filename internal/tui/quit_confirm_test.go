package tui

import (
	"strings"
	"testing"
)

func TestQuitConfirmationDefaultsToCancel(t *testing.T) {
	q := NewQuitConfirmation()

	// Cancel is the safe default: a stray Enter on the quit dialog must never
	// kill a running loop.
	if got := q.GetSelected(); got != QuitOptionCancel {
		t.Errorf("expected default selection Cancel, got %v", got)
	}
}

func TestQuitConfirmationMoveUpDownClamps(t *testing.T) {
	q := NewQuitConfirmation()

	q.MoveUp()
	if got := q.GetSelected(); got != QuitOptionQuit {
		t.Errorf("expected Quit after MoveUp, got %v", got)
	}

	// Already at the top; must not wrap around to Cancel.
	q.MoveUp()
	if got := q.GetSelected(); got != QuitOptionQuit {
		t.Errorf("expected Quit to stay selected at the top, got %v", got)
	}

	q.MoveDown()
	if got := q.GetSelected(); got != QuitOptionCancel {
		t.Errorf("expected Cancel after MoveDown, got %v", got)
	}

	// Already at the bottom; must not wrap around to Quit.
	q.MoveDown()
	if got := q.GetSelected(); got != QuitOptionCancel {
		t.Errorf("expected Cancel to stay selected at the bottom, got %v", got)
	}
}

func TestQuitConfirmationResetReturnsToCancel(t *testing.T) {
	q := NewQuitConfirmation()
	q.MoveUp()
	if q.GetSelected() != QuitOptionQuit {
		t.Fatal("precondition failed: expected Quit to be selected")
	}

	q.Reset()

	// Reset runs every time the dialog opens, so a previous session's "Quit"
	// choice must not carry over and arm the next Enter.
	if got := q.GetSelected(); got != QuitOptionCancel {
		t.Errorf("expected Cancel after Reset, got %v", got)
	}
}

func TestQuitConfirmationForLoopCopy(t *testing.T) {
	q := NewQuitConfirmation()
	q.ForLoop()
	q.SetSize(100, 30)

	out := q.Render()
	if !strings.Contains(out, "Ralph loop") {
		t.Errorf("expected loop wording in rendered dialog, got:\n%s", out)
	}
	if !strings.Contains(out, "Quit and stop loop") {
		t.Errorf("expected loop quit label in rendered dialog, got:\n%s", out)
	}
}

func TestQuitConfirmationForAutoActionCopy(t *testing.T) {
	q := NewQuitConfirmation()
	q.ForAutoAction()
	q.SetSize(100, 30)

	out := q.Render()
	if !strings.Contains(out, "post-completion action") {
		t.Errorf("expected auto-action wording in rendered dialog, got:\n%s", out)
	}
	if !strings.Contains(out, "Quit and interrupt") {
		t.Errorf("expected auto-action quit label in rendered dialog, got:\n%s", out)
	}
	// The two variants must not bleed into each other.
	if strings.Contains(out, "Ralph loop") {
		t.Errorf("auto-action dialog must not mention the loop, got:\n%s", out)
	}
}

func TestQuitConfirmationSwitchingCopyKeepsSelection(t *testing.T) {
	q := NewQuitConfirmation()
	q.MoveUp() // select Quit

	// tryQuit picks the copy after the selection was reset, so changing the copy
	// must not disturb which option is armed.
	q.ForAutoAction()

	if got := q.GetSelected(); got != QuitOptionQuit {
		t.Errorf("expected selection to survive a copy switch, got %v", got)
	}
}

func TestQuitConfirmationRenderMarksSelectedOption(t *testing.T) {
	q := NewQuitConfirmation()
	q.SetSize(100, 30)

	out := q.Render()
	// Cancel is selected by default, so the marker belongs on Cancel.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Cancel") && strings.Contains(line, "▶") {
			return
		}
	}
	t.Errorf("expected the selection marker on Cancel, got:\n%s", out)
}

func TestQuitConfirmationRenderNarrowScreenKeepsMinimumWidth(t *testing.T) {
	q := NewQuitConfirmation()
	// Narrower than the 40-column floor; modalWidth would go negative without
	// the clamp and lipgloss would panic or mangle the box.
	q.SetSize(20, 10)

	out := q.Render()
	if out == "" {
		t.Fatal("expected a rendered dialog on a narrow screen")
	}
	if !strings.Contains(out, "Quit Chief?") {
		t.Errorf("expected the title to survive a narrow render, got:\n%s", out)
	}
}
