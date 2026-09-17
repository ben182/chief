package headless

import (
	"context"
	"errors"
	"fmt"
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
func finish(ctx context.Context, log *logger, opts Options, manager *loop.Manager, name string, p *prd.PRD, res *Result) {
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
	if cfg.OnComplete.Push {
		res.Actions["push"] = push(log, res)
		// A pull request needs the branch on the remote, so a failed push takes
		// the PR with it rather than producing a confusing second error.
		if cfg.OnComplete.CreatePR && res.Actions["push"] == nil {
			res.Actions["pr"] = createPR(log, opts, name, res)
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
func createPR(log *logger, opts Options, name string, res *Result) error {
	if installed, authed, err := git.CheckGHCLI(); err != nil || !installed || !authed {
		log.event("pr", "skipped: the gh CLI is not installed and authenticated on this machine")
		return fmt.Errorf("gh CLI unavailable")
	}

	p, err := prd.LoadPRD(opts.PRDPath)
	if err != nil {
		log.event("pr", "failed to read the PRD: %v", err)
		return err
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
