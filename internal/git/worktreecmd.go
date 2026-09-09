package git

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// WorktreeContext is what a worktree setup or teardown command is told about the
// worktree it runs against. Every field reaches the command as a CHIEF_*
// environment variable, so a script can derive a database name, a hostname or a
// slug from the PRD and the branch instead of guessing them from the directory
// it happens to sit in.
type WorktreeContext struct {
	PRDName string
	Branch  string
	// BaseBranch is the branch Branch was cut from, as recorded by
	// RecordBaseBranch, and empty when chief never recorded one. Empty stays
	// empty rather than falling back to the default branch: a script diffing
	// against a guessed base is worse off than one that can see it doesn't know.
	BaseBranch string
	// WorktreePath is also the working directory of the command. RepoDir is the
	// main checkout, which a worktree script needs for whatever is shared — an
	// .env to copy, a seeded database to clone. Both are handed over absolute.
	WorktreePath string
	RepoDir      string
}

// RunOptions says what to do with a setup or teardown command while it runs. A
// zero RunOptions is the old behaviour: run to completion, keep the output, tell
// nobody about it until it is over.
type RunOptions struct {
	// LogDir is where the command's full output is written, one file per run,
	// named "<setup|teardown>-<timestamp>.log". Empty writes no log. Chief
	// points it at the PRD directory, whose .gitignore this keeps up to date.
	LogDir string
	// Timeout aborts a command that outlives it, killing its whole process
	// group so a hung `npm install` takes its children along. Zero — the
	// default — waits forever, which is the right answer for a setup that is
	// slow rather than stuck.
	Timeout time.Duration
	// OnLine is called with every output line the moment it arrives, so a
	// caller can show progress during the minutes a setup takes. It runs on the
	// goroutine reading the command's output, not the caller's, so an
	// implementation has to be safe to call from another goroutine.
	OnLine func(line string)
}

// RunResult is what is left of a setup or teardown command once it is over. The
// full output is in the log file; Output only carries the tail, because the
// callers that show it — an error dialog, a spinner — have room for a handful of
// lines and a `composer install` produces thousands.
type RunResult struct {
	// Output is the last retainedOutputLines lines of the command's combined
	// stdout and stderr, trimmed.
	Output string
	// LogPath is the file the full output went to, empty when no LogDir was
	// given or the file could not be opened.
	LogPath string
	// TimedOut says the command was killed by RunOptions.Timeout rather than
	// exiting on its own.
	TimedOut bool
}

// retainedOutputLines caps how much of a command's output RunResult carries
// back. The log file has all of it; this is only what an error message or a
// dialog can show, and the tail is where the failure is.
const retainedOutputLines = 200

// logTimestampLayout names one log file per run without a separator that needs
// quoting in a shell. Same layout as the loop's run logs, so a PRD directory
// sorts sensibly.
const logTimestampLayout = "2006-01-02-150405"

// readerGrace is how long output is still collected after the command itself has
// exited, for the case where something it spawned still holds the pipe.
const readerGrace = 2 * time.Second

// env returns the environment for a setup or teardown command: everything the
// command inherits from chief anyway, plus the CHIEF_* variables. Ours come
// last, because a later entry wins: a CHIEF_* variable that happens to sit in
// chief's own environment must not shadow the worktree actually being handled.
func (c WorktreeContext) env() []string {
	return append(os.Environ(),
		"CHIEF_PRD_NAME="+c.PRDName,
		"CHIEF_BRANCH="+c.Branch,
		"CHIEF_BASE_BRANCH="+c.BaseBranch,
		"CHIEF_WORKTREE_PATH="+absOrSelf(c.WorktreePath),
		"CHIEF_REPO_DIR="+absOrSelf(c.RepoDir),
	)
}

// absOrSelf resolves p against the working directory, falling back to p itself
// when that fails. An empty path stays empty: "" means "not known", and the
// process working directory would be a confidently wrong answer instead.
func absOrSelf(p string) string {
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// RunSetup runs the configured worktree setup command inside wt.WorktreePath.
// An empty command is a no-op.
func RunSetup(wt WorktreeContext, setup string, opts RunOptions) (RunResult, error) {
	return runWorktreeCommand(wt, setup, "setup", opts)
}

// RunTeardown runs the configured worktree teardown command inside
// wt.WorktreePath. An empty command is a no-op, which is what keeps removal a
// pure git operation for projects that configure nothing.
//
// Callers run this before removing a worktree and must not remove it when this
// returns an error: the teardown owns resources git knows nothing about —
// databases, web-server links — and removing the directory anyway would orphan
// them.
func RunTeardown(wt WorktreeContext, teardown string, opts RunOptions) (RunResult, error) {
	return runWorktreeCommand(wt, teardown, "teardown", opts)
}

// runWorktreeCommand runs one of the configured worktree commands with `sh -c`
// in the worktree, with the context in its environment. Both commands are
// written by the same person for the same worktree, so they get the same shell,
// the same working directory and the same variables.
//
// stdout and stderr share one pipe, so the log and the live output read in the
// order the command produced them — which is the order the person who wrote the
// script expects to read them in.
func runWorktreeCommand(wt WorktreeContext, command, kind string, opts RunOptions) (RunResult, error) {
	if strings.TrimSpace(command) == "" {
		return RunResult{}, nil
	}

	logFile, logPath := openCommandLog(opts.LogDir, kind, command)
	if logFile != nil {
		defer func() { _ = logFile.Close() }()
	}
	result := RunResult{LogPath: logPath}

	pr, pw, err := os.Pipe()
	if err != nil {
		return result, fmt.Errorf("failed to capture %s output: %w", kind, err)
	}

	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = absOrSelf(wt.WorktreePath)
	cmd.Env = wt.env()
	cmd.Stdout = pw
	cmd.Stderr = pw
	setProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		_ = pr.Close()
		return result, fmt.Errorf("failed to start %s command: %w", kind, err)
	}
	// The child holds the only writing end that matters now; keeping ours open
	// would mean the reader below never sees EOF.
	_ = pw.Close()

	var tail []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(pr)
		// A dependency installer prints single lines far longer than the
		// scanner's default 64 KiB token; a truncated log is worse than a wide one.
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if logFile != nil {
				_, _ = logFile.WriteString(line + "\n")
			}
			if opts.OnLine != nil {
				opts.OnLine(line)
			}
			tail = append(tail, line)
			if len(tail) > retainedOutputLines {
				tail = tail[len(tail)-retainedOutputLines:]
			}
		}
	}()

	// AfterFunc fires at most once, so the close needs no guard.
	killed := make(chan struct{})
	if opts.Timeout > 0 {
		timer := time.AfterFunc(opts.Timeout, func() {
			close(killed)
			killProcessGroup(cmd.Process)
		})
		defer timer.Stop()
	}

	waitErr := cmd.Wait()
	// A setup that leaves a daemon behind hands it the pipe as well, so waiting
	// for end-of-output would mean waiting for the daemon. Once the command
	// itself is over, the reader gets a moment to drain what is already there
	// and then has the pipe closed out from under it.
	select {
	case <-done:
	case <-time.After(readerGrace):
		_ = pr.Close()
		<-done
	}
	_ = pr.Close()

	result.Output = strings.TrimSpace(strings.Join(tail, "\n"))
	select {
	case <-killed:
		result.TimedOut = true
		note := fmt.Sprintf("chief: %s timed out after %s and was killed", kind, opts.Timeout)
		if logFile != nil {
			_, _ = logFile.WriteString(note + "\n")
		}
		return result, fmt.Errorf("%s timed out after %s", kind, opts.Timeout)
	default:
	}
	return result, waitErr
}

// openCommandLog opens this run's log file and makes sure it stays out of
// version control. Logging is best effort: a directory that cannot be written
// to costs the log, never the setup.
func openCommandLog(logDir, kind, command string) (*os.File, string) {
	if logDir == "" {
		return nil, ""
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, ""
	}
	IgnoreLogsIn(logDir)

	path := filepath.Join(logDir, kind+"-"+time.Now().Format(logTimestampLayout)+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, ""
	}
	// The command is the first thing you want to know when reading this file
	// weeks later, and it is not in the output.
	_, _ = f.WriteString("$ " + command + "\n")
	return f, path
}

// logHint points at a log file in a sentence, or says nothing when there is no
// log. Errors from setup and teardown end up in modals with room for a line or
// two, so the output stays in the file and the message says where the file is.
func logHint(logPath string) string {
	if logPath == "" {
		return ""
	}
	return " (full output in " + logPath + ")"
}
