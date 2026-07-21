// Package git provides Git utility functions for Chief.
package git

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// GetCurrentBranch returns the current git branch name for a directory.
func GetCurrentBranch(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
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
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, cerr := cmd.CombinedOutput(); cerr != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

// BranchExists returns true if a branch with the given name exists.
func BranchExists(dir, branchName string) (bool, error) {
	cmd := exec.Command("git", "rev-parse", "--verify", branchName)
	cmd.Dir = dir
	err := cmd.Run()
	if err != nil {
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
	cmd := exec.Command("git", "rev-list", "--count", defaultBranch+".."+branch)
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0
	}
	return count
}

// GetDiff returns the git diff output for the working directory.
// It shows the diff between the current branch and its merge base with the default branch.
// If on main/master or if merge-base fails, it shows the last few commits' diff.
func GetDiff(dir string) (string, error) {
	branch, err := GetCurrentBranch(dir)
	if err != nil {
		return "", err
	}

	// If on a feature branch, diff against merge-base with main/master
	if !IsProtectedBranch(branch) {
		baseBranch, err := GetDefaultBranch(dir)
		if err == nil && baseBranch != "" {
			mergeBase, err := getMergeBase(dir, baseBranch, "HEAD")
			if err == nil && mergeBase != "" {
				return getDiffOutput(dir, mergeBase, "HEAD")
			}
		}
	}

	// Fallback: show diff of recent commits (last 10)
	return getDiffOutput(dir, "HEAD~10", "HEAD")
}

// GetDiffStats returns a short diffstat summary.
func GetDiffStats(dir string) (string, error) {
	branch, err := GetCurrentBranch(dir)
	if err != nil {
		return "", err
	}

	if !IsProtectedBranch(branch) {
		baseBranch, err := GetDefaultBranch(dir)
		if err == nil && baseBranch != "" {
			mergeBase, err := getMergeBase(dir, baseBranch, "HEAD")
			if err == nil && mergeBase != "" {
				cmd := exec.Command("git", "diff", "--stat", mergeBase, "HEAD")
				cmd.Dir = dir
				output, err := cmd.Output()
				if err != nil {
					return "", err
				}
				return strings.TrimSpace(string(output)), nil
			}
		}
	}

	cmd := exec.Command("git", "diff", "--stat", "HEAD~10", "HEAD")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// GetDiffForCommit returns the diff for a single commit using git show.
func GetDiffForCommit(dir, commitHash string) (string, error) {
	cmd := exec.Command("git", "show", "--format=", commitHash)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// GetDiffStatsForCommit returns the diffstat for a single commit.
func GetDiffStatsForCommit(dir, commitHash string) (string, error) {
	cmd := exec.Command("git", "show", "--format=", "--stat", commitHash)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// FindCommitForStory searches the git log for a commit whose subject line
// matches the chief commit format "feat: <storyID> - <title>".
// Both the story ID and title are required to avoid false positives from
// previous PRD runs that may reuse the same story IDs.
// Returns the commit hash if found, empty string otherwise.
func FindCommitForStory(dir, storyID, title string) (string, error) {
	cmd := exec.Command("git", "log", "--fixed-strings", "--grep=feat: "+storyID+" - "+title, "--format=%H", "-1")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	hash := strings.TrimSpace(string(output))
	return hash, nil
}

// CommitLog returns a one-line-per-commit log (`<short-hash> <subject>`) of the
// commits on branch that are not on the default branch, oldest first. It is used
// to feed the run summary generator the list of work that landed. Returns an
// empty string (no error) when the range can't be determined or is empty, so the
// caller can simply skip summarizing when there is nothing to describe.
func CommitLog(repoDir, branch string) (string, error) {
	defaultBranch, err := GetDefaultBranch(repoDir)
	if err != nil {
		return "", err
	}
	cmd := exec.Command("git", "log", "--reverse", "--format=%h %s", defaultBranch+".."+branch)
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// CommitPaths stages the given paths and commits them with message. Paths are
// force-added (`git add -f`) so a file lives under an otherwise-gitignored
// directory (e.g. `.chief/`) is still committed. Paths may be absolute or
// relative to dir. Returns an error if nothing was staged or the commit fails.
func CommitPaths(dir, message string, paths ...string) error {
	if len(paths) == 0 {
		return fmt.Errorf("no paths to commit")
	}
	addArgs := append([]string{"add", "-f", "--"}, paths...)
	add := exec.Command("git", addArgs...)
	add.Dir = dir
	if out, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("git add failed: %s", strings.TrimSpace(string(out)))
	}
	commit := exec.Command("git", append([]string{"commit", "-m", message, "--"}, paths...)...)
	commit.Dir = dir
	if out, err := commit.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// HeadSubject returns the subject line (first line of the message) of the
// current HEAD commit. It errors on a repo with no commits yet, letting callers
// treat "no commit to inspect" the same as "HEAD isn't what I expected".
func HeadSubject(dir string) (string, error) {
	cmd := exec.Command("git", "log", "-1", "--format=%s")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
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
	addArgs := append([]string{"add", "-f", "--"}, paths...)
	add := exec.Command("git", addArgs...)
	add.Dir = dir
	if out, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("git add failed: %s", strings.TrimSpace(string(out)))
	}
	commit := exec.Command("git", append([]string{"commit", "--amend", "--no-edit", "--"}, paths...)...)
	commit.Dir = dir
	if out, err := commit.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit --amend failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// getMergeBase returns the merge base commit between two refs.
func getMergeBase(dir, ref1, ref2 string) (string, error) {
	cmd := exec.Command("git", "merge-base", ref1, ref2)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// getDiffOutput returns the full diff between two refs.
func getDiffOutput(dir, from, to string) (string, error) {
	cmd := exec.Command("git", "diff", from, to)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}
