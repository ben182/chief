package box

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Login reads the Hetzner API token from the terminal, checks it against the
// API, and saves it.
//
// It exists because the obvious alternative — telling someone to write the token
// into a file with a shell one-liner — goes wrong in a specific and silent way:
// paste a multi-line block into a shell and `read` consumes the *next line of
// the paste* as its input, so the file ends up holding `chmod 600 ...` and the
// failure surfaces later as a rejected token. A program that owns the prompt
// cannot make that mistake.
//
// The token is checked before it is saved, so a typo is a sentence here rather
// than a confusing failure the next time a box is created.
func Login(ctx context.Context, in *os.File, out io.Writer) error {
	return loginWith(ctx, in, out, newHetzner)
}

// loginWith is Login with the API client injected, so a test can check the
// token-is-verified-before-it-is-saved rule without a Hetzner account.
func loginWith(ctx context.Context, in *os.File, out io.Writer, dial func(token string) *hetzner) error {
	_, _ = fmt.Fprintln(out, "Paste your Hetzner Cloud API token (Read & Write).")
	_, _ = fmt.Fprintln(out, "It will not be shown as you type, and it is not written to your shell history.")
	_, _ = fmt.Fprint(out, "\nToken: ")

	token, err := readSecret(in)
	// The newline the user's Return did not echo, so whatever prints next starts
	// on its own line.
	_, _ = fmt.Fprintln(out)
	if err != nil {
		return err
	}
	if token == "" {
		return fmt.Errorf("no token entered")
	}

	_, _ = fmt.Fprintln(out, "Checking it against the Hetzner API...")
	if err := dial(token).checkToken(ctx); err != nil {
		return fmt.Errorf("%w\n  The token was not saved", err)
	}

	if err := writeToken("hetzner-token", token); err != nil {
		return fmt.Errorf("saving the token: %w", err)
	}
	path, _ := tokenFile("hetzner-token")
	_, _ = fmt.Fprintf(out, "Saved to %s\n", path)
	_, _ = fmt.Fprintln(out, "\nYou are set up. Create a box with: chief box up <prd>")
	return nil
}

// readSecret reads one line without echoing it.
//
// Echo is turned off with stty rather than a terminal library, to keep this
// from being the one feature that adds a dependency. The deferred restore runs
// on every path out, including a signal-interrupted read — a terminal left with
// echo off is a shell that looks broken.
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
