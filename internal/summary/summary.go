// Package summary generates a human-facing run summary once a PRD finishes.
// It gathers the commits that landed on the branch, has the configured agent
// write a timestamped SUMMARY-<time>.md describing what was built and how to
// test it, and commits that file so it rides along in the push/PR. Each run
// writes its own timestamped file, so a PRD keeps a history of its runs.
package summary

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ben182/chief/embed"
	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/loop"
)

// FilePrefix is the prefix of each timestamped run-summary file. Every run
// writes its own SUMMARY-<timestamp>.md instead of overwriting a single file,
// so a PRD keeps a readable, sortable history of its runs.
const FilePrefix = "SUMMARY-"

// timestampLayout is the sortable, filesystem-safe timestamp used in the
// summary file name (local time, second precision to avoid collisions).
const timestampLayout = "2006-01-02-150405"

// FileNameFor returns the summary file name for a run that finished at t.
func FileNameFor(t time.Time) string {
	return FilePrefix + t.Format(timestampLayout) + ".md"
}

// Result reports the outcome of a summary generation.
type Result struct {
	Path      string // absolute path to the written timestamped summary file
	Committed bool   // whether the summary was committed
}

// ErrNothingToSummarize is returned when the branch has no commits to describe,
// so callers can treat it as a benign skip rather than a failure.
var ErrNothingToSummarize = fmt.Errorf("no commits to summarize")

// Generate produces and commits the run summary.
//
//   - provider: the agent CLI to run headless (same one that ran the loop).
//   - gitDir:   the working dir whose branch the commits live on and where the
//     summary is committed (worktree dir when set, else project root).
//   - prdDir:   the directory the PRD lives in; the summary file is written here.
//   - branch:   the branch whose commits (vs. the default branch) are summarized.
//   - parked:   stories left for human review, surfaced under "Offene Punkte".
//
// It returns ErrNothingToSummarize when there are no commits to describe. The
// agent writes the file; Generate then force-adds and commits it so it lands
// even when the PRD dir sits under a gitignored `.chief/`.
func Generate(ctx context.Context, provider loop.Provider, gitDir, prdDir, branch string, parked []string) (Result, error) {
	return generateAt(ctx, provider, gitDir, prdDir, branch, parked, time.Now())
}

// generateAt is Generate with an injectable clock so the timestamped file name
// is deterministic in tests.
func generateAt(ctx context.Context, provider loop.Provider, gitDir, prdDir, branch string, parked []string, now time.Time) (Result, error) {
	if provider == nil {
		return Result{}, fmt.Errorf("summary requires a provider")
	}

	commits, err := git.CommitLog(gitDir, branch)
	if err != nil || commits == "" {
		// No range / no commits: nothing worth summarizing. Not an error.
		return Result{}, ErrNothingToSummarize
	}

	if err := os.MkdirAll(prdDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("failed to create summary dir: %w", err)
	}

	summaryPath := filepath.Join(prdDir, FileNameFor(now))
	prompt := embed.GetSummaryPrompt(summaryPath, commits, parked)

	// Run the agent headless, reusing the provider's loop command (which already
	// carries the right skip-permissions flags per provider). We don't parse the
	// output — the agent writes the file itself — but stdout/stderr must still be
	// drained so the process doesn't block on a full pipe.
	cmd := provider.LoopCommand(ctx, prompt, gitDir)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		// A context cancellation is a clean stop, not a failure to report loudly.
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		// The agent may exit non-zero yet still have written the file; fall through
		// to the existence check before deciding this is fatal.
		if _, statErr := os.Stat(summaryPath); statErr != nil {
			return Result{}, fmt.Errorf("%s exited without writing a summary: %w", provider.Name(), err)
		}
	}

	if _, err := os.Stat(summaryPath); err != nil {
		return Result{}, fmt.Errorf("summary file was not created at %s", summaryPath)
	}

	res := Result{Path: summaryPath}

	// Commit deterministically rather than trusting the agent to. Force-add so a
	// summary under a gitignored `.chief/` is still tracked. The same commit
	// sweeps chief's own working files (prd.md, progress.md and the scoped
	// .gitignore) so a finished run never leaves them behind as uncommitted
	// changes — this is the backstop for the per-story commits, catching the last
	// story's timing and any status the concurrent writes raced past. Only files
	// that exist are included.
	if git.IsGitRepo(gitDir) {
		msg := "docs: add run summary"
		paths := []string{summaryPath}
		for _, name := range []string{"prd.md", "progress.md", ".gitignore"} {
			p := filepath.Join(prdDir, name)
			if _, statErr := os.Stat(p); statErr == nil {
				paths = append(paths, p)
			}
		}
		if err := git.CommitPaths(gitDir, msg, paths...); err != nil {
			// The file exists and is useful even if the commit failed (e.g. nothing
			// staged because it was already committed). Report the file, note the
			// commit miss via Committed=false, and let the caller decide.
			return res, fmt.Errorf("summary written but not committed: %w", err)
		}
		res.Committed = true
	}

	return res, nil
}
