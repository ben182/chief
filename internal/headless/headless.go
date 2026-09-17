// Package headless runs a PRD to completion without a terminal UI.
//
// It exists for the run nobody watches: a machine that is not the one the user
// sits at, reached over SSH, where the TUI's alternate screen is the wrong
// shape entirely — it needs a TTY to draw into, it dies with the connection
// that owns it, and its whole value is the interactivity a detached run has no
// use for. What is left when the screen goes away is a log: one line per thing
// worth knowing, timestamped, on stdout, where systemd or a redirect can keep
// it.
//
// Everything below the screen is shared with the TUI rather than reimplemented:
// the same loop.Manager drives the run, the same git package creates the
// worktree and runs its setup, the same summary package writes the run summary,
// and the same config decides whether to push and open a pull request. This
// package is the part that decides without asking — where the work happens, and
// what to do when a run ends — because there is nobody to ask.
package headless

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/loop"
	"github.com/ben182/chief/internal/prd"
)

// Options describes the run. Provider and PRDPath are required; everything else
// has a working default, because a headless run is normally started by a script
// that passes a PRD name and nothing more.
type Options struct {
	// PRDPath is the project's copy of the PRD to run (.chief/prds/<name>/prd.md).
	PRDPath string
	// BaseDir is the project root. Empty means the directory holding the PRD's
	// .chief, which is what a run started from inside the project wants.
	BaseDir string
	// Provider is the agent CLI the run drives, already resolved and configured
	// (MCP, skills, model) by the caller.
	Provider loop.Provider
	// Config is the project config. Nil runs with chief's defaults: no review,
	// no consolidation, no push.
	Config *config.Config
	// MaxIterations caps the run. Zero lets the loop size it from the PRD.
	MaxIterations int
	// Worktree runs the PRD in its own git worktree, created and set up the way
	// an interactive run's dialog would. False works in the current checkout.
	Worktree bool
	// NoRetry disables the loop's automatic retry after an agent crash.
	NoRetry bool
	// Verbose adds the agent's own narration and every tool call to the log.
	// Off, the log carries the run's skeleton — iterations, stories, reviews,
	// costs, failures — which is what five hours of output should amount to.
	Verbose bool
	// Out is where the log goes. Nil means os.Stdout is the caller's job to
	// pass; this package writes nowhere on its own.
	Out io.Writer
}

// Result reports what the run did, for a caller that turns it into an exit code.
type Result struct {
	// PRDName is the PRD that ran.
	PRDName string
	// Branch is the branch the work landed on, empty when the run made none.
	Branch string
	// WorkDir is the directory the agent worked in — the worktree for a worktree
	// run, the project root otherwise.
	WorkDir string
	// Completed is true when every story is resolved: built, or parked for human
	// review after failing its attempts. It is the run's own verdict, not a
	// promise that the work is good.
	Completed bool
	// Stories counts the PRD's stories and how many of them pass at the end.
	Stories, Passing int
	// Parked lists the stories that ended up needing a human.
	Parked []string
	// Cost is what the run spent, in USD, as the provider reported it.
	Cost float64
	// Duration is the wall-clock time from start to finish, waiting out rate
	// limits included.
	Duration time.Duration
	// Summary, Push and PR name the post-completion actions that ran, with the
	// error each one ended with. Absent from the map means it was not attempted.
	Actions map[string]error
	// PRURL is the pull request the run opened or found, empty when none.
	PRURL string
}

// Run executes the PRD and returns once the loop has stopped and every
// configured post-completion action has been tried.
//
// Cancelling ctx stops the run the way the TUI's stop does: the agent is killed,
// the commits it already made stay, and the post-completion actions are skipped
// — an interrupted run has not finished, and pushing it as though it had would
// be a lie told to whatever is watching the branch.
func Run(ctx context.Context, opts Options) (Result, error) {
	res := Result{Actions: map[string]error{}}

	if opts.Provider == nil {
		return res, errors.New("headless: no agent provider")
	}
	if strings.TrimSpace(opts.PRDPath) == "" {
		return res, errors.New("headless: no PRD")
	}

	log := newLogger(opts.Out, opts.Verbose)

	prdPath, err := filepath.Abs(opts.PRDPath)
	if err != nil {
		return res, fmt.Errorf("headless: resolving the PRD path: %w", err)
	}
	baseDir := opts.BaseDir
	if baseDir == "" {
		baseDir = projectRootFor(prdPath)
	}
	baseDir, err = filepath.Abs(baseDir)
	if err != nil {
		return res, fmt.Errorf("headless: resolving the project root: %w", err)
	}

	name := prdNameFrom(prdPath)
	res.PRDName = name

	p, err := prd.LoadPRD(prdPath)
	if err != nil {
		return res, fmt.Errorf("headless: reading %s: %w", prdPath, err)
	}
	res.Stories = len(p.UserStories)

	log.event("run", "%s · %d stories, %d already passing", name, len(p.UserStories), p.CompletedCount())
	log.event("run", "agent %s · project %s", opts.Provider.Name(), baseDir)

	// Where the work happens is settled before the loop starts, because a
	// headless run has no dialog to settle it in.
	home, err := prepareWorkspace(ctx, log, opts, baseDir, prdPath, name)
	if err != nil {
		return res, err
	}
	res.Branch, res.WorkDir = home.branch, home.workDir

	// A headless run gets the same iteration budget an interactive one is given
	// when none is named: enough for every unfinished story to use its attempts.
	maxIter := opts.MaxIterations
	if maxIter <= 0 {
		maxIter = loop.DefaultMaxIterations(p)
	}

	manager := loop.NewManager(maxIter, opts.Provider)
	manager.SetBaseDir(baseDir)
	manager.SetConfig(opts.Config)
	if opts.NoRetry {
		manager.DisableRetry()
	}

	if home.worktree != "" {
		err = manager.RegisterWithWorktree(name, prdPath, home.worktree, home.branch)
	} else {
		err = manager.Register(name, prdPath)
	}
	if err != nil {
		return res, fmt.Errorf("headless: registering %s: %w", name, err)
	}
	// A run in the current checkout still owns a branch, and push/PR need to
	// know which — the manager only learns it from a worktree otherwise.
	if home.worktree == "" && home.branch != "" {
		_ = manager.UpdateWorktreeInfo(name, "", home.branch)
	}

	started := time.Now()
	if err := manager.Start(name); err != nil {
		return res, fmt.Errorf("headless: starting %s: %w", name, err)
	}
	log.event("run", "started · up to %d iterations", maxIter)

	state, cost := drain(ctx, log, manager, name)
	res.Duration = time.Since(started)
	res.Cost = cost

	// Re-read the PRD the run actually wrote to: a worktree run records its
	// progress in its own copy, and the project's still says what it said before.
	livePRD := p
	if live, err := prd.LoadPRD(livePRDPath(baseDir, prdPath, home.worktree)); err == nil {
		livePRD = live
	}
	res.Passing = livePRD.CompletedCount()
	res.Parked = parkedLabels(livePRD)
	res.Completed = state == loop.LoopStateComplete

	log.event("run", "%s after %s · %d/%d stories, $%.2f",
		verdict(state), round(res.Duration), res.Passing, res.Stories, res.Cost)
	for _, s := range res.Parked {
		log.event("parked", "%s", s)
	}

	if inst := manager.GetInstance(name); inst != nil && inst.Error != nil {
		log.event("error", "%v", inst.Error)
	}

	// An interrupted run is not a finished one, so nothing gets pushed on its
	// behalf. Everything it committed is on the branch either way.
	if ctx.Err() != nil {
		log.event("run", "interrupted — skipping post-completion actions")
		return res, nil //nolint:nilerr // an interrupted run reports what it got done; it did not fail
	}

	finish(ctx, log, opts, manager, name, livePRD, &res)
	return res, nil
}

// verdict turns the loop's final state into the word the log ends on.
func verdict(state loop.LoopState) string {
	switch state {
	case loop.LoopStateComplete:
		return "complete"
	case loop.LoopStateError:
		return "failed"
	case loop.LoopStateStopped:
		return "stopped"
	default:
		return "ended"
	}
}

// parkedLabels names the stories that ended up needing a human, the way the
// completion screen lists them.
func parkedLabels(p *prd.PRD) []string {
	var out []string
	for _, s := range p.UserStories {
		if s.NeedsReview {
			out = append(out, s.ID+" - "+s.Title)
		}
	}
	return out
}

// livePRDPath is the copy of the PRD the run wrote its progress into: the
// worktree's when the run had one and the file is really there, the project's
// otherwise.
func livePRDPath(baseDir, prdPath, worktree string) string {
	if worktree == "" {
		return prdPath
	}
	mapped, ok := prd.PathIn(baseDir, prdPath, worktree)
	if !ok {
		return prdPath
	}
	return mapped
}

// prdNameFrom returns the PRD's name — the directory holding its prd.md — which
// is what the manager, the branch and the story commits are keyed on.
func prdNameFrom(prdPath string) string {
	return filepath.Base(filepath.Dir(prdPath))
}

// projectRootFor walks up from a PRD to the project that owns it: the directory
// holding the .chief the PRD lives under. A PRD kept somewhere else falls back
// to its own directory, which is the only root it has.
func projectRootFor(prdPath string) string {
	dir := filepath.Dir(prdPath)
	for d := dir; ; {
		parent := filepath.Dir(d)
		if parent == d {
			return dir
		}
		if filepath.Base(d) == ".chief" {
			return parent
		}
		d = parent
	}
}

// round trims a duration to whole seconds, which is all a run lasting hours
// needs to report.
func round(d time.Duration) time.Duration { return d.Round(time.Second) }

// workspace is the answer to where a run works: the directory the agent runs in
// and the branch its commits land on.
type workspace struct {
	// worktree is the dedicated checkout, empty for a run in the project itself.
	worktree string
	// workDir is where the agent runs — the worktree, or the project root.
	workDir string
	// branch is what the run commits to, empty when the run is not in a git repo.
	branch string
}

// prepareWorkspace decides and builds the place the run happens.
//
// With --worktree it does what the interactive dialog's worktree answer does:
// create the branch and checkout, run the configured setup command, and copy
// the PRD's working files in. Without it the run works in the checkout it was
// started from — but never on main: a protected branch gets the PRD's own
// branch cut and checked out first, because the alternative is an unattended
// agent committing hours of work onto main, and because nothing downstream will
// push a protected branch anyway.
func prepareWorkspace(ctx context.Context, log *logger, opts Options, baseDir, prdPath, name string) (workspace, error) {
	if !git.IsGitRepo(baseDir) {
		log.event("git", "not a git repository — work will not be committed between iterations")
		return workspace{workDir: baseDir}, nil
	}

	branch := git.BranchForPRD(name)

	if !opts.Worktree {
		current, err := git.GetCurrentBranch(baseDir)
		if err != nil {
			return workspace{}, fmt.Errorf("headless: reading the current branch: %w", err)
		}
		if !git.IsProtectedBranch(current) {
			log.event("git", "working on %s", current)
			return workspace{workDir: baseDir, branch: current}, nil
		}
		if err := git.CreateBranch(baseDir, branch); err != nil {
			return workspace{}, fmt.Errorf("headless: creating %s off %s: %w", branch, current, err)
		}
		log.event("git", "checked out %s (was on %s)", branch, current)
		return workspace{workDir: baseDir, branch: branch}, nil
	}

	dirTemplate := ""
	var cfg config.WorktreeConfig
	if opts.Config != nil {
		cfg = opts.Config.Worktree
		dirTemplate = strings.TrimSpace(cfg.Dir)
	}
	path, err := git.WorktreePathForPRD(baseDir, dirTemplate, name, branch)
	if err != nil {
		return workspace{}, fmt.Errorf("headless: resolving the worktree path: %w", err)
	}

	created, err := git.CreateWorktree(git.CreateWorktreeOptions{
		RepoDir:      baseDir,
		WorktreePath: path,
		Branch:       branch,
		PRDName:      name,
		Teardown:     cfg.Teardown,
		BaseBranch:   cfg.BaseBranch,
		LogDir:       filepath.Dir(prdPath),
		PRDDir:       filepath.Dir(prdPath),
	})
	if err != nil {
		return workspace{}, fmt.Errorf("headless: creating the worktree: %w", err)
	}
	if created.Reused {
		log.event("worktree", "reusing %s on %s", path, branch)
	} else {
		log.event("worktree", "created %s on %s", path, branch)
	}

	if err := runSetup(ctx, log, cfg, created.Reused, git.WorktreeContext{
		PRDName:      name,
		Branch:       branch,
		BaseBranch:   git.RecordedBaseBranch(baseDir, branch),
		WorktreePath: path,
		RepoDir:      baseDir,
	}, filepath.Dir(prdPath)); err != nil {
		return workspace{}, err
	}

	if err := prd.SeedWorktree(baseDir, prdPath, path, created.Reused); err != nil {
		return workspace{}, fmt.Errorf("headless: %w", err)
	}

	return workspace{worktree: path, workDir: path, branch: branch}, nil
}

// runSetup runs the configured worktree setup command, streaming its output
// into the run log so a `composer install` that takes four minutes is visible
// as it happens rather than as a gap. A setup that fails aborts the run: the
// checkout it was supposed to make workable is not, and an agent turned loose
// in it would spend its iterations discovering that.
func runSetup(ctx context.Context, log *logger, cfg config.WorktreeConfig, reused bool, wt git.WorktreeContext, logDir string) error {
	command := strings.TrimSpace(cfg.Setup)
	if command == "" {
		return nil
	}
	// A worktree picked up as it stands has been set up before. Whether it needs
	// setting up again is the project's call, the same one the TUI reads.
	if reused && cfg.SetupOnReuse != nil && !*cfg.SetupOnReuse {
		log.event("setup", "skipped (worktree reused, setupOnReuse is false)")
		return nil
	}

	log.event("setup", "running: %s", command)
	started := time.Now()
	timeout := time.Duration(cfg.SetupTimeoutSeconds) * time.Second
	res, err := git.RunSetup(wt, command, git.RunOptions{
		LogDir:  logDir,
		Timeout: timeout,
		OnLine:  func(line string) { log.detail("setup", "%s", line) },
	})
	if err != nil {
		if res.TimedOut {
			return fmt.Errorf("headless: worktree setup timed out after %s (log: %s)", timeout, res.LogPath)
		}
		return fmt.Errorf("headless: worktree setup failed: %w (log: %s)", err, res.LogPath)
	}
	log.event("setup", "done in %s", round(time.Since(started)))
	_ = ctx
	return nil
}
