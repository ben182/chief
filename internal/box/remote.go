package box

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// remote runs commands on a box over SSH.
//
// It shells out to ssh and rsync rather than speaking either protocol in Go.
// That is not laziness about the libraries: the local ssh already knows the
// user's agent, their keys, their ~/.ssh/config and their jump hosts, and
// reimplementing a fraction of that would mean a box is reachable from the
// terminal but not from chief, for reasons the user cannot see. Both binaries
// ship with macOS and every Linux worth running this from.
type remote struct {
	// user and host say where to connect. host is an IP: a throwaway box has no
	// name anyone has heard of.
	user, host string
	// knownHosts is the file holding the host key this box was built with. Empty
	// falls back to accepting whatever answers, which is what a box created
	// before chief pinned host keys has to be reached with.
	knownHosts string
}

// sshArgs are the options every connection uses.
//
// The host key is checked against the one chief generated and put into the box
// through cloud-init, in a known_hosts file belonging to this project rather
// than the user's own. That keeps both halves of the usual trade: the address
// of a machine created eleven minutes ago cannot collide with an entry for a
// machine somebody cares about, and chief still refuses to hand its tokens to a
// host that is not the one it created.
//
// Without a pinned key — a box from before this existed — it falls back to
// accepting whatever answers. Being unable to read the log of a running box
// would protect nothing: that box was handed its secrets hours ago.
func (r remote) sshArgs(extra ...string) []string {
	args := []string{
		"-o", "LogLevel=ERROR",
		"-o", "ServerAliveInterval=30",
	}
	if r.knownHosts != "" {
		args = append(args,
			"-o", "StrictHostKeyChecking=yes",
			"-o", "UserKnownHostsFile="+r.knownHosts,
		)
	} else {
		args = append(args,
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
		)
	}
	return append(args, extra...)
}

// target is the user@host ssh takes.
func (r remote) target() string { return r.user + "@" + r.host }

// run executes a command on the box and returns its combined output.
func (r remote) run(ctx context.Context, script string) (string, error) {
	args := append(r.sshArgs(), r.target(), script)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	out := strings.TrimSpace(buf.String())
	if err != nil {
		return out, fmt.Errorf("%w: %s", err, firstLines(out, 5))
	}
	return out, nil
}

// runWith executes a command on the box with input on its stdin. It is how a
// secret gets there: written into a file by the command rather than passed as
// an argument, so it never appears in a process list or a shell history.
func (r remote) runWith(ctx context.Context, script, input string) error {
	args := append(r.sshArgs(), r.target(), script)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = strings.NewReader(input)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, firstLines(strings.TrimSpace(buf.String()), 5))
	}
	return nil
}

// stream runs a command on the box with its output going straight to out, and
// the local terminal's signals reaching it. It is what following a log needs.
func (r remote) stream(ctx context.Context, script string, out io.Writer) error {
	args := append(r.sshArgs(), r.target(), script)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdout, cmd.Stderr = out, out
	return cmd.Run()
}

// shell opens an interactive session, replacing this process's terminal for the
// duration.
func (r remote) shell(ctx context.Context, dir string) error {
	args := append(r.sshArgs("-t"), r.target(), "cd "+shellQuote(dir)+" 2>/dev/null; exec bash -l")
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// copyFile puts a local file on the box.
func (r remote) copyFile(ctx context.Context, local, remotePath string) error {
	args := append(r.sshArgs("-q"), local, r.target()+":"+remotePath)
	cmd := exec.CommandContext(ctx, "scp", args...)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("copying %s: %w: %s", local, err, firstLines(buf.String(), 3))
	}
	return nil
}

// sync mirrors a local directory (or file) onto the box. excludes are rsync
// patterns; a run's own log files are the reason it takes any.
func (r remote) sync(ctx context.Context, local, remotePath string, excludes ...string) error {
	args := []string{"-az", "-e", "ssh " + strings.Join(r.sshArgs(), " ")}
	for _, e := range excludes {
		args = append(args, "--exclude", e)
	}
	args = append(args, local, r.target()+":"+remotePath)
	cmd := exec.CommandContext(ctx, "rsync", args...)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("copying %s: %w: %s", local, err, firstLines(buf.String(), 3))
	}
	return nil
}

// waitReachable returns once the box accepts an SSH connection, or when the
// deadline passes.
func (r remote) waitReachable(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		probe, cancel := context.WithTimeout(ctx, 10*time.Second)
		args := append(r.sshArgs("-o", "ConnectTimeout=5"), r.target(), "true")
		err := exec.CommandContext(probe, "ssh", args...).Run()
		cancel()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the box never accepted an SSH connection")
		}
		if !sleep(ctx, 3*time.Second) {
			return ctx.Err()
		}
	}
}

// waitFile returns once a path exists on the box. It is how the end of
// provisioning is detected: cloud-init writes its marker last, so the file
// existing means every step before it finished.
func (r remote) waitFile(ctx context.Context, path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		probe, cancel := context.WithTimeout(ctx, 20*time.Second)
		args := append(r.sshArgs(), r.target(), "test -f "+shellQuote(path))
		err := exec.CommandContext(probe, "ssh", args...).Run()
		cancel()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("provisioning did not finish within %s", timeout)
		}
		if !sleep(ctx, 5*time.Second) {
			return ctx.Err()
		}
	}
}

// waitProvisioned returns once the ready marker exists, and returns an error
// the moment the failed marker does instead. A provisioning step that fails
// leaves the second and never the first, and waiting twenty minutes to find
// that out is the difference between a box that reports and one that stalls.
func (r remote) waitProvisioned(ctx context.Context, ready, failed string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	script := "if test -f " + shellQuote(ready) + "; then echo ready; elif test -f " + shellQuote(failed) + "; then echo failed; fi"
	for {
		probe, cancel := context.WithTimeout(ctx, 20*time.Second)
		out, err := r.run(probe, script)
		cancel()
		if err == nil {
			switch strings.TrimSpace(out) {
			case "ready":
				return nil
			case "failed":
				return fmt.Errorf("a provisioning step failed on the box")
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("provisioning did not finish within %s", timeout)
		}
		if !sleep(ctx, 5*time.Second) {
			return ctx.Err()
		}
	}
}

// sleep waits for d, reporting false when the context ended first.
func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// shellQuote wraps s so a remote shell sees it as one literal argument.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// firstLines returns at most n lines of s, for an error message that has to
// carry a hint of what went wrong without pasting a provisioning log into it.
func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "; ")
}
