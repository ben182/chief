package awake

import (
	"os/exec"
	"strings"
)

// PowerSource says how the machine is currently powered. It matters because
// keeping a Mac awake only works while it is plugged in: on battery, closing the
// lid suspends it no matter what caffeinate was asked for (clamshell sleep is not
// software-preventable on Apple Silicon), so a walk-away run stops unnoticed.
type PowerSource int

const (
	// PowerUnknown means the question could not be answered — on a platform that
	// doesn't know how to ask, or because the query failed. Callers must treat it
	// as "no reason to worry" rather than guessing.
	PowerUnknown PowerSource = iota
	PowerAC
	PowerBattery
)

func (p PowerSource) String() string {
	switch p {
	case PowerAC:
		return "AC"
	case PowerBattery:
		return "battery"
	default:
		return "unknown"
	}
}

// CurrentPowerSource reports how this machine is powered right now, or
// PowerUnknown when it can't be determined.
func CurrentPowerSource() PowerSource {
	return powerSource(powerCmd)
}

// Supported reports whether chief knows how to keep this platform awake. It is
// the same question inhibitCmd answers, so the two can never drift apart.
func Supported() bool {
	return inhibitCmd() != nil
}

// powerSource runs the platform's power query and interprets its output. newCmd
// is a parameter rather than a direct call to powerCmd so tests can stand in for
// the query on any platform, and so a platform without one (nil) is just another
// unknown answer.
func powerSource(newCmd func() *exec.Cmd) PowerSource {
	if newCmd == nil {
		return PowerUnknown
	}
	cmd := newCmd()
	if cmd == nil {
		return PowerUnknown
	}
	out, err := cmd.Output()
	if err != nil {
		return PowerUnknown
	}
	return parsePowerSource(string(out))
}

// parsePowerSource reads the source out of `pmset -g batt` output, whose first
// line is "Now drawing from 'AC Power'" or "Now drawing from 'Battery Power'".
func parsePowerSource(out string) PowerSource {
	switch {
	case strings.Contains(out, "'Battery Power'"):
		return PowerBattery
	case strings.Contains(out, "'AC Power'"):
		return PowerAC
	default:
		return PowerUnknown
	}
}

// powerCmd builds the platform's power-source query, or nil where we don't know
// how to ask.
func powerCmd() *exec.Cmd {
	if !Supported() {
		return nil
	}
	return exec.Command("pmset", "-g", "batt")
}
