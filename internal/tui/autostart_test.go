package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// collectBatchMsgs runs a batch cmd and returns the messages produced by its
// (non-blocking) sub-commands. Watchers/manager are nil in these tests, so the
// channel-reading listeners are excluded and nothing blocks.
func collectBatchMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var msgs []tea.Msg
	for _, c := range batch {
		if c != nil {
			msgs = append(msgs, c())
		}
	}
	return msgs
}

func TestInit_EmitsAutoStartMsgWhenEnabled(t *testing.T) {
	app := &App{state: StateReady, autoStart: true}
	found := false
	for _, m := range collectBatchMsgs(app.Init()) {
		if _, ok := m.(autoStartMsg); ok {
			found = true
		}
	}
	if !found {
		t.Error("expected Init to emit autoStartMsg when autoStart is set")
	}
}

func TestInit_NoAutoStartMsgWhenDisabled(t *testing.T) {
	app := &App{state: StateReady, autoStart: false}
	for _, m := range collectBatchMsgs(app.Init()) {
		if _, ok := m.(autoStartMsg); ok {
			t.Error("did not expect autoStartMsg when autoStart is unset")
		}
	}
}

func TestAutoStartMsg_IgnoredWhenNotReady(t *testing.T) {
	// Guard: autoStartMsg must not start (touch nil manager) unless Ready.
	app := &App{state: StateRunning}
	model, cmd := app.Update(autoStartMsg{})
	if got := model.(App).state; got != StateRunning {
		t.Errorf("state changed on non-ready autoStartMsg: got %v", got)
	}
	if cmd != nil {
		t.Error("expected nil cmd when autoStartMsg ignored")
	}
}
