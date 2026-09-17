package box

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Login gets every credential a box needs into place, asking only for what it
// cannot work out and only for what is missing.
//
// It exists as its own command because one of the three needs a browser. A box
// is normally created from a script, a background shell, or a session nobody is
// watching, and a login flow started there waits forever on a prompt that will
// never be answered. Doing it once, deliberately, at a terminal, means every
// `chief box up` afterwards is non-interactive.
//
// The other reason is narrower and was learned the hard way: the documented
// alternative for the Hetzner token used to be a multi-line shell block with a
// `read` in it. Paste that at a prompt and `read` consumes the next line of the
// paste as its input, so the file ends up holding `chmod 600 ...` and the
// failure surfaces much later as a rejected token. A program that owns the
// prompt cannot make that mistake.
func Login(ctx context.Context, in *os.File, out io.Writer) error {
	return loginWith(ctx, in, out, newHetzner)
}

// loginWith is Login with the API client injected, so a test can check the
// token-is-verified-before-it-is-saved rule without a Hetzner account.
func loginWith(ctx context.Context, in *os.File, out io.Writer, dial func(token string) *hetzner) error {
	say := func(format string, args ...any) {
		_, _ = fmt.Fprintf(out, format+"\n", args...)
	}

	say("Setting up the credentials a box needs. Nothing here is asked twice.")
	say("")

	var problems []error

	// 1. Hetzner: the one nothing can mint, so it is the one that gets a prompt.
	say("Hetzner  — creates and destroys the machine")
	if err := setUpHetzner(ctx, in, out, dial); err != nil {
		problems = append(problems, err)
	}
	say("")

	// 2. GitHub: whoever can open a pull request from this machine already has a
	// token that works, so this is usually just a confirmation.
	say("GitHub   — clones the project, pushes what the run builds")
	if _, err := resolveGitHubToken(ctx); err != nil {
		say("  ✗ %s", indent(err.Error()))
		problems = append(problems, errors.New("GitHub"))
	} else {
		say("  ✓ from gh")
	}
	say("")

	// 3. Claude: the one that opens a browser, which is the reason this command
	// exists at all.
	say("Claude   — logs the agent in on the box")
	if _, err := resolveClaudeToken(ctx, true, os.Stdout, out); err != nil {
		say("  ✗ %s", indent(err.Error()))
		problems = append(problems, errors.New("Claude"))
	} else {
		say("  ✓ ready")
	}
	say("")

	if len(problems) > 0 {
		return fmt.Errorf("%d credential(s) still missing — see above", len(problems))
	}
	say("All set. Create a box with: chief box up <prd>")
	return nil
}

// setUpHetzner confirms the stored token still works, or asks for one.
func setUpHetzner(ctx context.Context, in *os.File, out io.Writer, dial func(token string) *hetzner) error {
	say := func(format string, args ...any) {
		_, _ = fmt.Fprintf(out, format+"\n", args...)
	}

	// An existing token is checked rather than trusted: one that was revoked in
	// the console looks identical to a good one until a box fails to appear.
	if existing, err := resolveHetznerToken(); err == nil {
		if err := dial(existing).checkToken(ctx); err == nil {
			say("  ✓ already stored and accepted")
			return nil
		}
		say("  ! the stored token was rejected — asking for a new one")
	}

	say("  Create one in the Hetzner console: Security → API tokens → Read & Write")
	_, _ = fmt.Fprint(out, "  Token (not shown as you type): ")

	token, err := readSecret(in)
	// The newline the user's Return did not echo, so what prints next starts on
	// its own line.
	_, _ = fmt.Fprintln(out)
	if err != nil {
		say("  ✗ %v", err)
		return err
	}
	if token == "" {
		say("  ✗ nothing entered")
		return errors.New("no Hetzner token entered")
	}

	if err := dial(token).checkToken(ctx); err != nil {
		say("  ✗ %v — not saved", err)
		return err
	}
	if err := writeToken("hetzner-token", token); err != nil {
		say("  ✗ could not save it: %v", err)
		return err
	}
	path, _ := tokenFile("hetzner-token")
	say("  ✓ saved to %s", path)
	return nil
}

// indent lines up a multi-line error under the marker that introduces it.
func indent(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "\n", "\n  ")
}

// readSecret reads one line without echoing it.
//
// Echo is turned off with stty rather than a terminal library, to keep this from
// being the one feature that adds a dependency. The deferred restore runs on
// every path out, including an interrupted read — a terminal left with echo off
// is a shell that looks broken.
func readSecret(in *os.File) (string, error) {
	if restore, ok := disableEcho(in); ok {
		defer restore()
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("reading the token: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// disableEcho turns terminal echo off, returning the function that puts it back
// and whether it managed at all. Input that is not a terminal — a pipe, a file —
// has no echo to disable, and reading from it is still fine.
func disableEcho(in *os.File) (restore func(), ok bool) {
	info, err := in.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return nil, false
	}
	set := func(arg string) error {
		cmd := exec.Command("stty", arg)
		cmd.Stdin = in
		return cmd.Run()
	}
	if err := set("-echo"); err != nil {
		return nil, false
	}
	return func() { _ = set("echo") }, true
}
