package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/prd"
)

// The two kinds of worktree command, spelled the way git names their log files
// so a message and its log agree.
const (
	worktreeKindSetup    = "setup"
	worktreeKindTeardown = "teardown"
)

// WorktreeOptions contains configuration for the worktree subcommands.
type WorktreeOptions struct {
	Name    string         // PRD name, required — this is the <prd> argument
	BaseDir string         // Main checkout holding .chief/ (default: current directory)
	Config  *config.Config // Loaded from BaseDir when nil
	Stdout  io.Writer      // Where the command's output goes (default: os.Stdout)
}

// worktreeTarget is what a manual setup or teardown run needs to know: the
// worktree it runs against and the command it runs there.
type worktreeTarget struct {
	wt      git.WorktreeContext
	command string
	// logDir and timeout are the same ones a run started from the TUI uses, so
	// a manual setup ends up in the same PRD directory as the automatic one.
	logDir  string
	timeout time.Duration
}

// RunWorktreeSetup runs the configured worktree.setup command against a PRD's
// existing worktree.
func RunWorktreeSetup(opts WorktreeOptions) error {
	return runWorktreeSubcommand(opts, worktreeKindSetup)
}

// RunWorktreeTeardown runs the configured worktree.teardown command against a
// PRD's worktree without removing it. Removal belongs to chief's clean flow,
// which runs the teardown itself; this is the way to re-run a teardown that
// failed, or to drop a worktree's outside resources while keeping the checkout.
func RunWorktreeTeardown(opts WorktreeOptions) error {
	return runWorktreeSubcommand(opts, worktreeKindTeardown)
}

// runWorktreeSubcommand is the shared body of the setup and teardown
// subcommands: resolve the worktree, run the command, report where the output
// went.
func runWorktreeSubcommand(opts WorktreeOptions, kind string) error {
	target, err := resolveWorktreeTarget(&opts, kind)
	if err != nil {
		return err
	}

	out := opts.Stdout
	if out == nil {
		out = os.Stdout
	}
	// Writing to the terminal has no recovery path, so the write errors are
	// deliberately dropped throughout.
	_, _ = fmt.Fprintf(out, "Running worktree %s for PRD %q in %s\n", kind, opts.Name, target.wt.WorktreePath)
	_, _ = fmt.Fprintf(out, "$ %s\n\n", target.command)

	run := git.RunSetup
	if kind == worktreeKindTeardown {
		run = git.RunTeardown
	}
	// OnLine is called from the goroutine reading the command's output, which
	// the runner joins before it returns — so out is only ever written by one
	// goroutine at a time and needs no lock of its own.
	res, runErr := run(target.wt, target.command, git.RunOptions{
		LogDir:  target.logDir,
		Timeout: target.timeout,
		OnLine: func(line string) {
			_, _ = fmt.Fprintln(out, line)
		},
	})
	if res.LogPath != "" {
		_, _ = fmt.Fprintf(out, "\nFull output in %s\n", res.LogPath)
	}
	if runErr != nil {
		return fmt.Errorf("worktree %s failed: %w", kind, runErr)
	}
	_, _ = fmt.Fprintf(out, "Worktree %s complete.\n", kind)
	return nil
}

// resolveWorktreeTarget resolves the PRD's worktree the same way the TUI does
// and picks the command for kind out of the config. It fails when the worktree
// isn't there: a setup command belongs in a checkout, and running it in a
// directory that doesn't exist would fail with a shell error nobody can act on.
func resolveWorktreeTarget(opts *WorktreeOptions, kind string) (worktreeTarget, error) {
	baseDir, err := resolveBaseDir(opts.BaseDir)
	if err != nil {
		return worktreeTarget{}, err
	}
	opts.BaseDir = baseDir

	if opts.Name == "" {
		return worktreeTarget{}, fmt.Errorf("no PRD given: run 'chief worktree %s <prd>'", kind)
	}

	cfg := opts.Config
	if cfg == nil {
		cfg, err = config.Load(baseDir)
		if err != nil {
			return worktreeTarget{}, fmt.Errorf("failed to load .chief/config.yaml: %w", err)
		}
		opts.Config = cfg
	}

	branch := git.BranchForPRD(opts.Name)
	worktreePath, err := git.WorktreePathForPRD(baseDir, strings.TrimSpace(cfg.Worktree.Dir), opts.Name, branch)
	if err != nil {
		return worktreeTarget{}, err
	}
	if info, statErr := os.Stat(worktreePath); statErr != nil || !info.IsDir() {
		return worktreeTarget{}, fmt.Errorf(
			"no worktree for PRD %q at %s: start the PRD in chief to create it",
			opts.Name, worktreePath)
	}

	command := strings.TrimSpace(cfg.Worktree.Setup)
	timeout := time.Duration(0)
	if kind == worktreeKindTeardown {
		command = strings.TrimSpace(cfg.Worktree.Teardown)
	} else if cfg.Worktree.SetupTimeoutSeconds > 0 {
		timeout = time.Duration(cfg.Worktree.SetupTimeoutSeconds) * time.Second
	}
	if command == "" {
		return worktreeTarget{}, fmt.Errorf("no worktree.%s command configured in .chief/config.yaml", kind)
	}

	return worktreeTarget{
		wt: git.WorktreeContext{
			PRDName:      opts.Name,
			Branch:       branch,
			BaseBranch:   git.RecordedBaseBranch(baseDir, branch),
			WorktreePath: worktreePath,
			RepoDir:      baseDir,
		},
		command: command,
		logDir:  prd.PRDDir(baseDir, opts.Name),
		timeout: timeout,
	}, nil
}
