package awake

import (
	"os/exec"
	"runtime"
	"testing"
)

// The literal fixtures below are real `pmset -g batt` output, not something
// rebuilt from the parser's own rules.
const (
	pmsetOnBattery = "Now drawing from 'Battery Power'\n" +
		" -InternalBattery-0 (id=70713443)\t36%; discharging; 3:00 remaining present: true\n"
	pmsetOnAC = "Now drawing from 'AC Power'\n" +
		" -InternalBattery-0 (id=70713443)\t100%; charged; 0:00 remaining present: true\n"
)

func TestParsePowerSource(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want PowerSource
	}{
		{"discharging", pmsetOnBattery, PowerBattery},
		{"plugged in", pmsetOnAC, PowerAC},
		{"unrecognised output", "something else entirely\n", PowerUnknown},
		{"no output", "", PowerUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parsePowerSource(tt.out); got != tt.want {
				t.Errorf("parsePowerSource(%q) = %v, want %v", tt.out, got, tt.want)
			}
		})
	}
}

func TestPowerSourceReadsTheCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stand-in command is POSIX-only")
	}

	got := powerSource(func() *exec.Cmd {
		return exec.Command("printf", "%s", pmsetOnBattery)
	})
	if got != PowerBattery {
		t.Errorf("powerSource() = %v, want PowerBattery", got)
	}
}

// Everything about this query is best-effort: chief only uses it to decide
// whether to warn, so anything it can't answer has to come back as "unknown"
// rather than as a guess.
func TestPowerSourceFailsOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stand-in command is POSIX-only")
	}

	t.Run("command exits non-zero", func(t *testing.T) {
		got := powerSource(func() *exec.Cmd { return exec.Command("false") })
		if got != PowerUnknown {
			t.Errorf("powerSource() = %v, want PowerUnknown", got)
		}
	})

	t.Run("command does not exist", func(t *testing.T) {
		got := powerSource(func() *exec.Cmd { return exec.Command("chief-no-such-binary") })
		if got != PowerUnknown {
			t.Errorf("powerSource() = %v, want PowerUnknown", got)
		}
	})

	t.Run("platform has no command", func(t *testing.T) {
		got := powerSource(func() *exec.Cmd { return nil })
		if got != PowerUnknown {
			t.Errorf("powerSource() = %v, want PowerUnknown", got)
		}
	})

	t.Run("no builder at all", func(t *testing.T) {
		if got := powerSource(nil); got != PowerUnknown {
			t.Errorf("powerSource(nil) = %v, want PowerUnknown", got)
		}
	})
}

// Supported gates the pre-run sleep warning: where chief can't inhibit sleep in
// the first place there is nothing to warn about.
func TestSupportedMatchesTheInhibitHelper(t *testing.T) {
	want := runtime.GOOS == "darwin"
	if got := Supported(); got != want {
		t.Errorf("Supported() = %v on %s, want %v", got, runtime.GOOS, want)
	}
}

func TestCurrentPowerSourceIsUnknownOffMacOS(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("darwin can answer the question for real")
	}
	if got := CurrentPowerSource(); got != PowerUnknown {
		t.Errorf("CurrentPowerSource() = %v on %s, want PowerUnknown", got, runtime.GOOS)
	}
}
