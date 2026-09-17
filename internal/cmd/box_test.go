package cmd

import (
	"strings"
	"testing"
)

func TestParseBoxArgsReadsTheCommandAndPRD(t *testing.T) {
	o, err := ParseBoxArgs([]string{"up", "refactor-billing"})
	if err != nil {
		t.Fatalf("ParseBoxArgs: %v", err)
	}
	if o.Command != "up" || o.PRD != "refactor-billing" {
		t.Errorf("got command=%q prd=%q", o.Command, o.PRD)
	}
}

func TestParseBoxArgsTakesTheRunsFlags(t *testing.T) {
	o, err := ParseBoxArgs([]string{
		"run", "auth", "--worktree", "--verbose", "-n", "20",
		"--type", "cpx41", "--location", "nbg1", "--image", "ubuntu-26.04",
		"--file", ".env", "--file", "config/local.php",
		"--package", "imagemagick",
	})
	if err != nil {
		t.Fatalf("ParseBoxArgs: %v", err)
	}
	if !o.Worktree || !o.Verbose {
		t.Errorf("flags lost: %+v", o)
	}
	if o.MaxIterations != 20 {
		t.Errorf("MaxIterations = %d, want 20", o.MaxIterations)
	}
	if o.Type != "cpx41" || o.Location != "nbg1" || o.Image != "ubuntu-26.04" {
		t.Errorf("instance options lost: %+v", o)
	}
	// Repeatable, because a project needing two untracked files is not unusual.
	if len(o.Files) != 2 || o.Files[1] != "config/local.php" {
		t.Errorf("Files = %v, want both", o.Files)
	}
	if len(o.Packages) != 1 || o.Packages[0] != "imagemagick" {
		t.Errorf("Packages = %v", o.Packages)
	}
	// The PRD must survive being surrounded by flags.
	if o.PRD != "auth" {
		t.Errorf("PRD = %q, want auth", o.PRD)
	}
}

func TestParseBoxArgsAcceptsEqualsForm(t *testing.T) {
	o, err := ParseBoxArgs([]string{"up", "auth", "--type=cpx41", "--max-iterations=5", "--file=.env.testing"})
	if err != nil {
		t.Fatalf("ParseBoxArgs: %v", err)
	}
	if o.Type != "cpx41" || o.MaxIterations != 5 || len(o.Files) != 1 || o.Files[0] != ".env.testing" {
		t.Errorf("equals form not parsed: %+v", o)
	}
}

func TestParseBoxArgsRejectsWhatItCannotHonour(t *testing.T) {
	cases := [][]string{
		{"up", "--type"},             // flag with no value
		{"up", "--nonsense"},         // unknown flag
		{"up", "--nonsense=x"},       // unknown flag, equals form
		{"up", "-x"},                 // unknown short flag
		{"up", "-n", "0"},            // an iteration cap of zero would end the run at once
		{"up", "-n", "-3"},           // and a negative one is nonsense
		{"up", "--max-iterations=x"}, // not a number
	}
	for _, args := range cases {
		if _, err := ParseBoxArgs(args); err == nil {
			t.Errorf("ParseBoxArgs(%v) succeeded, want an error", args)
		}
	}

	if _, err := ParseBoxArgs(nil); err == nil {
		t.Error("ParseBoxArgs(nil) succeeded, want an error")
	}
}

func TestParseBoxArgsAllowsCommandsWithoutAPRD(t *testing.T) {
	// logs, status, ssh and down all work out which box they mean from the
	// project's own state.
	for _, command := range []string{"logs", "status", "ssh", "down"} {
		o, err := ParseBoxArgs([]string{command})
		if err != nil {
			t.Fatalf("ParseBoxArgs(%q): %v", command, err)
		}
		if o.Command != command || o.PRD != "" {
			t.Errorf("got %+v", o)
		}
	}

	o, err := ParseBoxArgs([]string{"down", "--force"})
	if err != nil {
		t.Fatalf("ParseBoxArgs: %v", err)
	}
	if !o.Force {
		t.Error("--force was not parsed")
	}
}

func TestBoxUsageNamesEveryCommandAndTheCostWarning(t *testing.T) {
	for _, want := range []string{"up", "run", "logs", "status", "ssh", "down"} {
		if !strings.Contains(BoxUsage, "  "+want) {
			t.Errorf("the usage does not document %q", want)
		}
	}
	// Somebody reading this for the first time has to learn that a box left
	// running costs money, and that `down` is what stops it.
	if !strings.Contains(BoxUsage, "billing") {
		t.Error("the usage never mentions that a box bills until it is destroyed")
	}
	// The defaults shown have to be the real ones, or they are worse than absent.
	if !strings.Contains(BoxUsage, "ubuntu-26.04") {
		t.Error("the usage does not show the real default image")
	}
}

func TestParseBoxArgsTakesTheSSHCommandVerbatim(t *testing.T) {
	// Everything after `ssh` belongs to the box, flags included. Parsing them
	// here would swallow them as chief's own and leave the remote command
	// mangled — `chief box ssh ls -la` must not lose the -la.
	o, err := ParseBoxArgs([]string{"ssh", "git", "log", "--oneline", "-n", "5"})
	if err != nil {
		t.Fatalf("ParseBoxArgs: %v", err)
	}
	if o.Command != "ssh" {
		t.Errorf("Command = %q", o.Command)
	}
	if o.Command2 != "git log --oneline -n 5" {
		t.Errorf("Command2 = %q, want the command unchanged", o.Command2)
	}
	// The flags must not have leaked into chief's own options.
	if o.MaxIterations != 0 {
		t.Errorf("-n was parsed as chief's own flag: MaxIterations = %d", o.MaxIterations)
	}

	// Bare ssh opens a shell.
	o, err = ParseBoxArgs([]string{"ssh"})
	if err != nil {
		t.Fatalf("ParseBoxArgs: %v", err)
	}
	if o.Command2 != "" {
		t.Errorf("Command2 = %q, want empty for an interactive shell", o.Command2)
	}
}
