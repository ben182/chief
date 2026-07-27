package tui

import (
	"strings"
	"testing"

	"github.com/ben182/chief/internal/awake"
	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/loop"
	tea "github.com/charmbracelet/bubbletea"
)

// sleepDialog builds the dialog the way the preflight does.
func sleepDialog(reasons ...sleepReason) *SleepWarning {
	sw := NewSleepWarning()
	sw.SetSize(100, 40)
	sw.SetReasons(reasons)
	sw.Reset()
	return sw
}

func TestSleepWarningCancelIsTheDefault(t *testing.T) {
	sw := sleepDialog(sleepReasonBattery)

	if got := sw.GetSelected(); got != SleepOptionCancel {
		t.Errorf("GetSelected() = %v, want SleepOptionCancel", got)
	}
}

func TestSleepWarningKeyboardSelection(t *testing.T) {
	sw := sleepDialog(sleepReasonBattery)

	sw.MoveUp()
	if got := sw.GetSelected(); got != SleepOptionStartAnyway {
		t.Errorf("after MoveUp: GetSelected() = %v, want SleepOptionStartAnyway", got)
	}
	// Already at the top: the selection must stay put rather than wrap around.
	sw.MoveUp()
	if got := sw.GetSelected(); got != SleepOptionStartAnyway {
		t.Errorf("after a second MoveUp: GetSelected() = %v, want SleepOptionStartAnyway", got)
	}

	sw.MoveDown()
	if got := sw.GetSelected(); got != SleepOptionCancel {
		t.Errorf("after MoveDown: GetSelected() = %v, want SleepOptionCancel", got)
	}
	sw.MoveDown()
	if got := sw.GetSelected(); got != SleepOptionCancel {
		t.Errorf("after a second MoveDown: GetSelected() = %v, want SleepOptionCancel", got)
	}
}

func TestSleepWarningRenderBattery(t *testing.T) {
	out := sleepDialog(sleepReasonBattery).Render()

	// The point of this variant: on battery the lid overrides keepAwake, and the
	// two ways out are the adapter or an open lid.
	for _, want := range []string{"on battery", "lid", "keepAwake", "power adapter"} {
		if !strings.Contains(out, want) {
			t.Errorf("battery dialog missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "loop.keepAwake: false") {
		t.Error("battery dialog should not claim the setting is off")
	}
}

func TestSleepWarningRenderKeepAwakeOff(t *testing.T) {
	out := sleepDialog(sleepReasonKeepAwakeOff).Render()

	for _, want := range []string{"loop.keepAwake: false", "Sleep protection is off"} {
		if !strings.Contains(out, want) {
			t.Errorf("keepAwake dialog missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "on battery") {
		t.Error("keepAwake dialog should not claim the machine is on battery")
	}
}

// Both hazards belong in one dialog, not two in a row.
func TestSleepWarningRenderBothReasons(t *testing.T) {
	out := sleepDialog(sleepReasonBattery, sleepReasonKeepAwakeOff).Render()

	for _, want := range []string{"on battery", "loop.keepAwake: false"} {
		if !strings.Contains(out, want) {
			t.Errorf("combined dialog missing %q:\n%s", want, out)
		}
	}
}

func TestSleepWarningRenderOffersBothOptions(t *testing.T) {
	out := sleepDialog(sleepReasonBattery).Render()

	for _, want := range []string{"Start anyway", "Cancel", "Enter", "Esc"} {
		if !strings.Contains(out, want) {
			t.Errorf("dialog missing %q:\n%s", want, out)
		}
	}
}

func TestSleepRisks(t *testing.T) {
	tests := []struct {
		name      string
		supported bool
		power     awake.PowerSource
		keepAwake bool
		want      []sleepReason
	}{
		{
			name:      "on battery with sleep protection on",
			supported: true,
			power:     awake.PowerBattery,
			keepAwake: true,
			want:      []sleepReason{sleepReasonBattery},
		},
		{
			name:      "plugged in with sleep protection off",
			supported: true,
			power:     awake.PowerAC,
			keepAwake: false,
			want:      []sleepReason{sleepReasonKeepAwakeOff},
		},
		{
			name:      "both at once",
			supported: true,
			power:     awake.PowerBattery,
			keepAwake: false,
			want:      []sleepReason{sleepReasonBattery, sleepReasonKeepAwakeOff},
		},
		{
			name:      "plugged in with sleep protection on",
			supported: true,
			power:     awake.PowerAC,
			keepAwake: true,
			want:      nil,
		},
		{
			// Fail open: an unanswerable battery query must not invent a hazard.
			name:      "power source unknown",
			supported: true,
			power:     awake.PowerUnknown,
			keepAwake: true,
			want:      nil,
		},
		{
			// The fail-open above covers the probe, not the config: a keepAwake the
			// user switched off themselves is known for certain either way.
			name:      "power source unknown with sleep protection off",
			supported: true,
			power:     awake.PowerUnknown,
			keepAwake: false,
			want:      []sleepReason{sleepReasonKeepAwakeOff},
		},
		{
			name:      "platform chief cannot keep awake",
			supported: false,
			power:     awake.PowerBattery,
			keepAwake: false,
			want:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sleepRisks(tt.supported, tt.power, tt.keepAwake)
			if len(got) != len(tt.want) {
				t.Fatalf("sleepRisks() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("sleepRisks()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// sleepStartApp builds an App at the point a run is about to start, with the
// power probe stood in so the dialog can be driven on any machine. The manager
// has no provider: launchLoop registers the PRD with it and then fails to start,
// which is enough to observe whether the start path was entered at all.
func sleepStartApp(t *testing.T, power awake.PowerSource, keepAwake bool) *App {
	t.Helper()
	return &App{
		baseDir:           t.TempDir(),
		manager:           loop.NewManager(10, nil),
		config:            &config.Config{Loop: config.LoopConfig{KeepAwake: keepAwake}},
		sleepWarning:      NewSleepWarning(),
		sleepCheck:        func() (awake.PowerSource, bool) { return power, true },
		branchSyncChecked: make(map[string]bool),
		width:             100,
		height:            40,
	}
}

// startAttempted reports whether the run actually reached launchLoop, which
// registers the PRD with the manager before starting it.
func startAttempted(a App, prdName string) bool {
	return a.manager.GetInstance(prdName) != nil
}

func TestStartOnBatteryRaisesTheDialog(t *testing.T) {
	app := sleepStartApp(t, awake.PowerBattery, true)
	app.viewMode = ViewPicker

	m, cmd := app.doStartLoop("default", "/tmp/prd")
	got := m.(App)

	if got.viewMode != ViewSleepWarning {
		t.Fatalf("viewMode = %v, want ViewSleepWarning", got.viewMode)
	}
	if startAttempted(got, "default") {
		t.Error("the run must not start behind the dialog's back")
	}
	if cmd != nil {
		t.Error("expected no command while awaiting the user's decision")
	}
	if out := got.sleepWarning.Render(); !strings.Contains(out, "on battery") {
		t.Error("expected the battery variant of the dialog")
	}
	if got.pendingStartPRD != "default" {
		t.Errorf("pendingStartPRD = %q, want default", got.pendingStartPRD)
	}
}

func TestStartWithKeepAwakeOffRaisesTheDialog(t *testing.T) {
	app := sleepStartApp(t, awake.PowerAC, false)

	got := mustApp(app.doStartLoop("default", "/tmp/prd"))

	if got.viewMode != ViewSleepWarning {
		t.Fatalf("viewMode = %v, want ViewSleepWarning", got.viewMode)
	}
	if out := got.sleepWarning.Render(); !strings.Contains(out, "loop.keepAwake: false") {
		t.Error("expected the disabled-protection variant of the dialog")
	}
}

func TestStartOnACWithKeepAwakeStartsWithoutADialog(t *testing.T) {
	app := sleepStartApp(t, awake.PowerAC, true)

	got := mustApp(app.doStartLoop("default", "/tmp/prd"))

	if got.viewMode == ViewSleepWarning {
		t.Error("a plugged-in machine with sleep protection on needs no warning")
	}
	if !startAttempted(got, "default") {
		t.Error("expected the run to start without an extra step")
	}
}

// Fail open: if the probe can't answer, the run must not be gated behind a
// dialog raised on a guess.
func TestStartWithUnknownPowerSourceStartsWithoutADialog(t *testing.T) {
	app := sleepStartApp(t, awake.PowerUnknown, true)

	got := mustApp(app.doStartLoop("default", "/tmp/prd"))

	if got.viewMode == ViewSleepWarning {
		t.Error("an unanswerable power query must not raise a dialog")
	}
	if !startAttempted(got, "default") {
		t.Error("expected the run to start when the hazard is unknown")
	}
}

// An App that was never handed a probe (nor a platform that has one) behaves as
// it did before this dialog existed.
func TestStartWithoutAProbeStartsWithoutADialog(t *testing.T) {
	app := sleepStartApp(t, awake.PowerBattery, false)
	app.sleepCheck = nil

	got := mustApp(app.doStartLoop("default", "/tmp/prd"))

	if got.viewMode == ViewSleepWarning {
		t.Error("no probe means no dialog")
	}
	if !startAttempted(got, "default") {
		t.Error("expected the run to start as before")
	}
}

func TestSleepWarningCancelReturnsToThePreviousView(t *testing.T) {
	app := sleepStartApp(t, awake.PowerBattery, true)
	app.viewMode = ViewPicker
	raised := mustApp(app.doStartLoop("default", "/tmp/prd"))

	// Cancel is preselected, so Enter on the untouched dialog cancels.
	got := mustApp(raised.Update(tea.KeyMsg{Type: tea.KeyEnter}))

	if got.viewMode != ViewPicker {
		t.Errorf("viewMode = %v, want the view the start came from (ViewPicker)", got.viewMode)
	}
	if startAttempted(got, "default") {
		t.Error("cancelling must not start the run")
	}
	if got.pendingStartPRD != "" {
		t.Errorf("pendingStartPRD = %q, want it cleared", got.pendingStartPRD)
	}
}

func TestSleepWarningEscapeCancelsTheStart(t *testing.T) {
	app := sleepStartApp(t, awake.PowerBattery, true)
	app.viewMode = ViewDashboard
	raised := mustApp(app.doStartLoop("default", "/tmp/prd"))

	got := mustApp(raised.Update(tea.KeyMsg{Type: tea.KeyEsc}))

	if got.viewMode != ViewDashboard {
		t.Errorf("viewMode = %v, want ViewDashboard", got.viewMode)
	}
	if startAttempted(got, "default") {
		t.Error("escaping must not start the run")
	}
}

func TestSleepWarningStartAnywayStartsTheRun(t *testing.T) {
	app := sleepStartApp(t, awake.PowerBattery, true)
	app.viewMode = ViewDashboard
	raised := mustApp(app.doStartLoop("default", "/tmp/prd"))
	if raised.viewMode != ViewSleepWarning {
		t.Fatalf("viewMode = %v, want the dialog raised first", raised.viewMode)
	}

	// Move the selection off the safe default, then confirm.
	moved := mustApp(raised.Update(tea.KeyMsg{Type: tea.KeyUp}))
	got := mustApp(moved.Update(tea.KeyMsg{Type: tea.KeyEnter}))

	if got.viewMode == ViewSleepWarning {
		t.Error("confirming must dismiss the dialog rather than re-raise it")
	}
	if !startAttempted(got, "default") {
		t.Error("expected the run to start after confirming")
	}
}

// No "don't show this again": the machine can be unplugged between two runs, so
// the question is asked afresh every time.
func TestSleepWarningReappearsOnEveryStart(t *testing.T) {
	app := sleepStartApp(t, awake.PowerBattery, true)
	app.viewMode = ViewDashboard

	raised := mustApp(app.doStartLoop("default", "/tmp/prd"))
	cancelled := mustApp(raised.Update(tea.KeyMsg{Type: tea.KeyEsc}))
	again := mustApp(cancelled.doStartLoop("default", "/tmp/prd"))

	if again.viewMode != ViewSleepWarning {
		t.Errorf("viewMode = %v, want the dialog raised again on the next start", again.viewMode)
	}
}

// The help overlay overwrites previousViewMode while the dialog is up, so
// cancelling must not drop the user back into the modal they just dismissed.
func TestSleepWarningCancelNeverReturnsToItself(t *testing.T) {
	app := sleepStartApp(t, awake.PowerBattery, true)
	app.viewMode = ViewDashboard
	app.helpOverlay = NewHelpOverlay()
	raised := mustApp(app.doStartLoop("default", "/tmp/prd"))

	help := mustApp(raised.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}}))
	back := mustApp(help.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}}))
	got := mustApp(back.Update(tea.KeyMsg{Type: tea.KeyEsc}))

	if got.viewMode == ViewSleepWarning {
		t.Error("cancelling returned to the dialog itself")
	}
}

// mustApp unwraps the model an App update returns.
func mustApp(m tea.Model, _ tea.Cmd) App {
	return m.(App)
}
