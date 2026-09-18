package headless

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/loop"
	"github.com/ben182/chief/internal/notify"
	"github.com/ben182/chief/internal/prd"
	"github.com/ben182/chief/internal/summary"
)

// summaryTimeout caps the run summary. It is an agent reading a diff and
// writing a page, and one that has not managed it in ten minutes is stuck; the
// commits it was meant to describe are already safe either way.
const summaryTimeout = 10 * time.Minute

// finish runs the post-completion actions the project configured, in the order
// their outputs depend on each other: the summary first so its commit rides
// along in what is pushed, then the push, then the pull request that needs the
// branch to exist on the remote.
//
// Every action is best-effort and reported rather than fatal. The run's value
// is the commits on the branch, and those are already made; a push that fails
// because the machine has no credentials for the remote is a thing to tell
// someone about, not a reason to call an afternoon of work a failure.
func finish(ctx context.Context, log *logger, opts Options, manager *loop.Manager, name string, p *prd.PRD, res *Result, transcript string) {
	if opts.Config == nil {
		return
	}
	cfg := opts.Config

	// Post-completion actions only mean anything with committed work. A run can
	// end with none — every story parked, or the agent never committing — and
	// there is then nothing to summarise, push, or open a pull request for.
	if res.Branch == "" {
		log.event("done", "no branch — nothing to push")
		notifyDone(cfg.OnComplete.Notify, name, res)
		return
	}
	commits := git.CommitCount(res.WorkDir, res.Branch)
	if commits == 0 {
		log.event("done", "no commits on %s — nothing to push", res.Branch)
		notifyDone(cfg.OnComplete.Notify, name, res)
		return
	}
	log.event("done", "%d commits on %s", commits, res.Branch)

	if cfg.OnComplete.Summary {
		res.Actions["summary"] = writeSummary(ctx, log, opts, manager, name, p, res)
	}
	// Before the push, because the point of the log is to be in what gets
	// pushed. What it therefore cannot contain is the push and the pull request
	// that come after it — and neither is a loss: a push that worked is visible
	// on the remote, and a push that failed took the whole commit with it.
	if transcript != "" {
		res.Actions["log"] = commitRunLog(log, opts, res, transcript)
	}
	if cfg.OnComplete.Push {
		res.Actions["push"] = push(log, res)
		// A pull request needs the branch on the remote, so a failed push takes
		// the PR with it rather than producing a confusing second error.
		if cfg.OnComplete.CreatePR && res.Actions["push"] == nil {
			res.Actions["pr"] = createPR(log, opts, name, p, res)
		}
	} else if cfg.OnComplete.CreatePR {
		log.event("pr", "skipped: onComplete.createPR needs onComplete.push")
	}

	notifyDone(cfg.OnComplete.Notify, name, res)
}

// writeSummary generates and commits the run's summary file next to the PRD.
func writeSummary(ctx context.Context, log *logger, opts Options, manager *loop.Manager, name string, p *prd.PRD, res *Result) error {
	log.event("summary", "writing the run summary")

	sinceRef := ""
	if inst := manager.GetInstance(name); inst != nil {
		sinceRef = inst.StartRef
	}

	refs := make([]git.StoryRef, 0, len(p.UserStories))
	for _, s := range p.UserStories {
		refs = append(refs, git.StoryRef{PRDName: name, ID: s.ID, Title: s.Title})
	}

	ctx, cancel := context.WithTimeout(ctx, summaryTimeout)
	defer cancel()

	out, err := summary.Generate(ctx, opts.Provider, res.WorkDir,
		summaryDir(opts.BaseDir, opts.PRDPath, res.WorkDir), refs, res.Parked, sinceRef)
	if errors.Is(err, summary.ErrNothingToSummarize) {
		log.event("summary", "nothing to summarise")
		return nil
	}
	if err != nil {
		log.event("summary", "failed: %v", err)
		return err
	}
	log.event("summary", "wrote %s", filepath.Base(out.Path))
	return nil
}

// summaryDir is the directory the summary file belongs in: the PRD's own
// directory inside the checkout the run worked in, which for a worktree run is
// the worktree's copy rather than the project's.
func summaryDir(baseDir, prdPath, workDir string) string {
	dir := filepath.Dir(prdPath)
	if prd.IsUnder(workDir, dir) {
		return dir
	}
	if mapped, ok := prd.PathIn(baseDir, dir, workDir); ok {
		return mapped
	}
	return dir
}

// commitRunLog puts the run's log next to the PRD and commits it, so what the
// run said survives the machine it said it on.
//
// Force-added, the same way the summary is: `.chief/` is gitignored in most
// projects, and the PRD directory carries its own `*.log` rule on top. Both are
// right for the log files a run leaves on a developer's machine, and both would
// otherwise silently drop the one file that is meant to travel.
func commitRunLog(log *logger, opts Options, res *Result, transcript string) error {
	dir := summaryDir(opts.BaseDir, opts.PRDPath, res.WorkDir)
	dest := filepath.Join(dir, "run-"+time.Now().Format("2006-01-02-1504")+".log")

	data, err := os.ReadFile(transcript) //nolint:gosec // the temporary file this run has been writing to
	if err != nil {
		log.event("log", "could not be kept: %v", err)
		return err
	}
	if err := os.WriteFile(dest, data, 0o600); err != nil {
		log.event("log", "could not be kept: %v", err)
		return err
	}
	if err := git.CommitPaths(res.WorkDir, "docs: add run log", dest); err != nil {
		log.event("log", "written but not committed: %v", err)
		return err
	}
	log.event("log", "committed %s", filepath.Base(dest))
	return nil
}

// push sends the run's branch to the remote.
func push(log *logger, res *Result) error {
	log.event("push", "pushing %s", res.Branch)
	if err := git.PushBranch(res.WorkDir, res.Branch); err != nil {
		log.event("push", "failed: %v", err)
		return err
	}
	log.event("push", "pushed")
	return nil
}

// createPR opens a pull request for the run's branch, or finds the one that is
// already open for it.
// p must be the PRD the run wrote its progress into — the worktree's copy for a
// worktree run. Re-reading the project's copy here, as this used to, produces a
// pull request whose "Changes" section is empty: the project's copy still says
// every story is todo, because the run never touched it.
func createPR(log *logger, opts Options, name string, p *prd.PRD, res *Result) error {
	if installed, authed, err := git.CheckGHCLI(); err != nil || !installed || !authed {
		log.event("pr", "skipped: the gh CLI is not installed and authenticated on this machine")
		return fmt.Errorf("gh CLI unavailable")
	}

	base := ""
	if opts.Config != nil {
		base = opts.Config.OnComplete.PRBaseBranch
	}
	pr, err := git.EnsurePR(res.WorkDir, res.Branch, git.PRTitleFromPRD(name, p), git.PRBodyFromPRD(p), base)
	if err != nil {
		log.event("pr", "failed: %v", err)
		return err
	}
	res.PRURL = pr.URL
	log.event("pr", "%s", pr.URL)
	return nil
}

// notifyDone fires the desktop notification the project asked for. On a server
// there is usually nothing to receive it, which is why it is best-effort in the
// notify package and unconditional here: a machine that does have a notifier
// running should still get the ping.
func notifyDone(enabled bool, name string, res *Result) {
	if !enabled {
		return
	}
	title := "chief: " + name
	body := fmt.Sprintf("%d/%d stories in %s", res.Passing, res.Stories, round(res.Duration))
	if res.Cost > 0 {
		body += fmt.Sprintf(" ($%.2f)", res.Cost)
	}
	notify.Send(title, body)
}
