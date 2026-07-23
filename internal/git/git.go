// Package git provides Git utility functions for Chief.
package git

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// runGit runs a git command in dir and returns its trimmed stdout. Use it for
// read-only queries where only stdout matters.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(string(out)), nil
}

// runGitRaw is like runGit but returns stdout verbatim (no trimming), for diffs
// and other output where surrounding whitespace is meaningful.
func runGitRaw(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(out), nil
}

// runGitChecked runs a mutating git command and, on failure, returns an error
// carrying the trimmed combined output so the caller sees git's own message. A
// non-empty what is prefixed to that message (e.g. "git add failed").
func runGitChecked(dir, what string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if what == "" {
			what = "git " + args[0]
		}
		// Wrap the underlying exec error with %w so errors.Is/As work across the
		// package boundary, while still surfacing git's own message (msg) when it
		// produced one.
		if msg != "" {
			return fmt.Errorf("%s: %s: %w", what, msg, err)
		}
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

// GetCurrentBranch returns the current git branch name for a directory.
func GetCurrentBranch(dir string) (string, error) {
	return runGit(dir, "rev-parse", "--abbrev-ref", "HEAD")
}

// IsProtectedBranch returns true if the branch name is main or master.
func IsProtectedBranch(branch string) bool {
	return branch == "main" || branch == "master"
}

// CreateBranch switches to branchName, creating it if it doesn't exist yet.
// Idempotent: re-running a PRD whose branch already exists just checks it out
// instead of failing like plain `git checkout -b` would.
func CreateBranch(dir, branchName string) error {
	exists, err := BranchExists(dir, branchName)
	if err != nil {
		return err
	}
	args := []string{"checkout", "-b", branchName}
	if exists {
		args = []string{"checkout", branchName}
	}
	return runGitChecked(dir, "", args...)
}

// BranchExists returns true if a branch with the given name exists.
func BranchExists(dir, branchName string) (bool, error) {
	cmd := exec.Command("git", "rev-parse", "--verify", branchName)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		// Branch doesn't exist
		return false, nil
	}
	return true, nil
}

// IsGitRepo returns true if the directory is inside a git repository.
func IsGitRepo(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = dir
	return cmd.Run() == nil
}

// CommitCount returns the number of commits on branch that are not on the default branch.
// Returns 0 if the count cannot be determined.
func CommitCount(repoDir, branch string) int {
	defaultBranch, err := GetDefaultBranch(repoDir)
	if err != nil {
		return 0
	}
	out, err := runGit(repoDir, "rev-list", "--count", defaultBranch+".."+branch)
	if err != nil {
		return 0
	}
	count, err := strconv.Atoi(out)
	if err != nil {
		return 0
	}
	return count
}

// diffRange returns the [base, head] revision range chief shows as "the work on
// this branch". On a feature branch that's merge-base(defaultBranch, HEAD)..HEAD;
// on a protected branch, or when the merge base can't be determined, it falls
// back to the last 10 commits (HEAD~10..HEAD). GetDiff and GetDiffStats share it
// so both always describe the same range.
func diffRange(dir string) (base, head string, err error) {
	branch, err := GetCurrentBranch(dir)
	if err != nil {
		return "", "", err
	}

	// If on a feature branch, diff against merge-base with main/master.
	if !IsProtectedBranch(branch) {
		if baseBranch, err := GetDefaultBranch(dir); err == nil && baseBranch != "" {
			if mergeBase, err := getMergeBase(dir, baseBranch, "HEAD"); err == nil && mergeBase != "" {
				return mergeBase, "HEAD", nil
			}
		}
	}

	// Fallback: recent commits (last 10).
	return "HEAD~10", "HEAD", nil
}

// GetDiff returns the git diff output for the working directory.
// It shows the diff between the current branch and its merge base with the default branch.
// If on main/master or if merge-base fails, it shows the last few commits' diff.
func GetDiff(dir string) (string, error) {
	base, head, err := diffRange(dir)
	if err != nil {
		return "", err
	}
	return runGitRaw(dir, "diff", base, head)
}

// GetDiffStats returns a short diffstat summary.
func GetDiffStats(dir string) (string, error) {
	base, head, err := diffRange(dir)
	if err != nil {
		return "", err
	}
	return runGit(dir, "diff", "--stat", base, head)
}

// GetDiffForCommit returns the diff for a single commit using git show.
func GetDiffForCommit(dir, commitHash string) (string, error) {
	return runGitRaw(dir, "show", "--format=", commitHash)
}

// GetDiffStatsForCommit returns the diffstat for a single commit.
func GetDiffStatsForCommit(dir, commitHash string) (string, error) {
	return runGit(dir, "show", "--format=", "--stat", commitHash)
}

// FindCommitForStory searches the git log for a commit whose subject line
// matches the chief commit format "feat: <storyID> - <title>".
// Both the story ID and title are required to avoid false positives from
// previous PRD runs that may reuse the same story IDs.
// When sinceRef is non-empty the search is scoped to sinceRef..HEAD, so a
// followup run only finds the commit if it landed during this run and not on an
// earlier one that already committed the same story on this branch.
// Returns the commit hash if found, empty string otherwise.
func FindCommitForStory(dir, storyID, title, sinceRef string) (string, error) {
	args := []string{"log", "--fixed-strings", "--grep=feat: " + storyID + " - " + title, "--format=%H", "-1"}
	if sinceRef != "" {
		args = append(args, sinceRef+"..HEAD")
	}
	return runGit(dir, args...)
}

// HeadHash returns the full commit hash of the current HEAD. It errors on a repo
// with no commits yet. Callers capture this at the start of a run so the summary
// can be scoped to only the commits that run adds (HeadHash..HEAD at the end).
func HeadHash(dir string) (string, error) {
	return runGit(dir, "rev-parse", "HEAD")
}

// StoryRef identifies a story by the fields that make up its chief commit
// subject ("feat: <ID> - <Title>"). It scopes the run summary to the commits
// chief actually authored for a specific PRD.
type StoryRef struct {
	ID    string
	Title string
}

// CommitLogForStories returns a one-line-per-commit log (`<short-hash> <subject>`)
// of the commits chief authored for the given stories, in the order the stories
// are passed (PRD order, oldest first). Each commit is matched by its exact
// "feat: <ID> - <Title>" subject, so the result contains only this PRD's work and
// excludes unrelated commits sitting on the same branch — including same-numbered
// stories from other PRDs, since the title must match too. Stories with no
// matching commit are skipped. Returns an empty string (no error) when none match.
//
// When sinceRef is non-empty the match is scoped to sinceRef..HEAD, so a followup
// run's summary describes only the stories that run completed and not the ones an
// earlier run already landed on the same branch.
func CommitLogForStories(repoDir string, stories []StoryRef, sinceRef string) (string, error) {
	var hashes []string
	for _, s := range stories {
		hash, err := FindCommitForStory(repoDir, s.ID, s.Title, sinceRef)
		if err != nil {
			return "", err
		}
		if hash != "" {
			hashes = append(hashes, hash)
		}
	}
	if len(hashes) == 0 {
		return "", nil
	}
	// --no-walk=unsorted lists exactly the named commits in the order given (PRD
	// order, i.e. oldest story first), rather than walking history and dragging in
	// everything reachable, or re-sorting by commit date (the --no-walk default).
	args := append([]string{"log", "--no-walk=unsorted", "--format=%h %s"}, hashes...)
	return runGit(repoDir, args...)
}

// CommitPaths stages the given paths and commits them with message. Paths are
// force-added (`git add -f`) so a file lives under an otherwise-gitignored
// directory (e.g. `.chief/`) is still committed. Paths may be absolute or
// relative to dir. Returns an error if nothing was staged or the commit fails.
func CommitPaths(dir, message string, paths ...string) error {
	if len(paths) == 0 {
		return fmt.Errorf("no paths to commit")
	}
	if err := runGitChecked(dir, "git add failed", append([]string{"add", "-f", "--"}, paths...)...); err != nil {
		return err
	}
	return runGitChecked(dir, "git commit failed", append([]string{"commit", "-m", message, "--"}, paths...)...)
}

// HeadSubject returns the subject line (first line of the message) of the
// current HEAD commit. It errors on a repo with no commits yet, letting callers
// treat "no commit to inspect" the same as "HEAD isn't what I expected".
func HeadSubject(dir string) (string, error) {
	return runGit(dir, "log", "-1", "--format=%s")
}

// AmendPaths force-adds the given paths and folds them into the current HEAD
// commit without opening an editor or changing its message. It attaches chief's
// own working files (prd.md, progress.md) to the story commit the agent just
// made, so a completed story's tracked progress travels with its code in one
// commit and survives an interrupted run. Only the listed paths are amended in;
// other unstaged changes are left untouched. Force-add (`-f`) keeps it working
// when the PRD dir sits under an otherwise-gitignored `.chief/`. Paths may be
// absolute or relative to dir.
func AmendPaths(dir string, paths ...string) error {
	if len(paths) == 0 {
		return fmt.Errorf("no paths to amend")
	}
	if err := runGitChecked(dir, "git add failed", append([]string{"add", "-f", "--"}, paths...)...); err != nil {
		return err
	}
	return runGitChecked(dir, "git commit --amend failed", append([]string{"commit", "--amend", "--no-edit", "--"}, paths...)...)
}

// getMergeBase returns the merge base commit between two refs.
func getMergeBase(dir, ref1, ref2 string) (string, error) {
	return runGit(dir, "merge-base", ref1, ref2)
}
