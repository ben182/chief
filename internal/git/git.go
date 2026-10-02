// Package git provides Git utility functions for Chief.
package git

import (
	"fmt"
	"os/exec"
	"path/filepath"
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
//
// A freshly created branch records the branch it was cut from (see
// RecordBaseBranch) so a pull request for it later targets that branch — cutting
// from develop and then opening the PR against main is the wrong merge. An
// existing branch keeps whatever origin it was created with.
//
// A branch that exists only on origin is picked up from there rather than cut
// anew: that is an earlier run's work — on a box, a fresh clone of a branch the
// previous box pushed — and a branch cut from here would start the PRD over and
// then fail to push over it.
func CreateBranch(dir, branchName string) error {
	exists, err := BranchExists(dir, branchName)
	if err != nil {
		return err
	}
	if exists {
		return runGitChecked(dir, "", "checkout", branchName)
	}
	base, _ := GetCurrentBranch(dir) // "" or "HEAD" (detached) is simply not recorded
	args := []string{"checkout", "-b", branchName}
	if remoteRef, ok := OriginRef(dir, branchName); ok {
		args = []string{"checkout", "--track", "-b", branchName, remoteRef}
	}
	if err := runGitChecked(dir, "", args...); err != nil {
		return err
	}
	RecordBaseBranch(dir, branchName, base)
	return nil
}

// OriginRef returns the ref origin's copy of branch has in this repository, and
// whether there is one. It only looks at what the last fetch brought: asking
// origin itself costs a round trip, and a clone has just fetched everything.
func OriginRef(dir, branch string) (string, bool) {
	ref := "refs/remotes/origin/" + branch
	if exists, err := BranchExists(dir, ref); err == nil && exists {
		return ref, true
	}
	return "", false
}

// BranchExists returns true if a branch with the given name exists.
func BranchExists(dir, branchName string) (bool, error) {
	cmd := exec.Command("git", "rev-parse", "--verify", branchName)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		// A non-zero rev-parse is the answer, not a failure: the branch does not
		// exist. The error return is reserved for genuine problems.
		return false, nil //nolint:nilerr // exit status is the signal
	}
	return true, nil
}

// IsGitRepo returns true if the directory is inside a git repository.
func IsGitRepo(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = dir
	return cmd.Run() == nil
}

// RepoRoot returns the top of the working tree dir belongs to — the checkout
// itself, or the worktree when dir is inside one.
func RepoRoot(dir string) (string, error) {
	return runGit(dir, "rev-parse", "--show-toplevel")
}

// HasOrigin reports whether the repository at dir has a remote named origin.
func HasOrigin(dir string) bool {
	_, err := runGit(dir, "remote", "get-url", "origin")
	return err == nil
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

// FindCommitForStory searches the git log for the commit chief authored for a
// story. It first looks for the namespaced format "feat: <prdName>/<storyID> - "
// (matched as a prefix, so a later title edit in prd.md never loses the commit).
// Because prdName is the PRD's unique directory name, prdName+storyID is a stable
// key even when two PRDs reuse the same story ID (e.g. both start at US-001).
//
// When no namespaced commit exists it falls back to the legacy format
// "feat: <storyID> - <title>" (which needs the exact title, since the legacy
// subject carried no PRD namespace) so commits authored before this format
// change are still found.
//
// When sinceRef is non-empty the search is scoped to sinceRef..HEAD, so a
// followup run only finds the commit if it landed during this run and not on an
// earlier one that already committed the same story on this branch.
// Returns the commit hash if found, empty string otherwise.
//
// Only a subject that starts with the format counts. A `wip:` commit — the
// finished part of a story that was then blocked — is not the story being done,
// and neither is a revert, or a body that quotes the subject.
func FindCommitForStory(dir, prdName, storyID, title, sinceRef string) (string, error) {
	if prdName != "" {
		prefix := "feat: " + prdName + "/" + storyID + " - "
		hash, err := grepCommit(dir, prefix, sinceRef)
		if err != nil {
			return "", err
		}
		if hash != "" {
			return hash, nil
		}
	}
	// Legacy fallback: subject without the PRD namespace.
	return grepCommit(dir, "feat: "+storyID+" - "+title, sinceRef)
}

// grepCommit returns the newest commit hash whose subject starts with prefix,
// optionally scoped to sinceRef..HEAD. Empty string when none. git's --grep
// narrows the log to messages containing prefix anywhere; the subject check
// keeps only the ones that begin with it.
func grepCommit(dir, prefix, sinceRef string) (string, error) {
	args := []string{"log", "--fixed-strings", "--grep=" + prefix, "--format=%H %s"}
	if sinceRef != "" {
		args = append(args, sinceRef+"..HEAD")
	}
	out, err := runGit(dir, args...)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		hash, subject, ok := strings.Cut(line, " ")
		if ok && strings.HasPrefix(subject, prefix) {
			return hash, nil
		}
	}
	return "", nil
}

// StashUncommitted puts every uncommitted change in the repository at dir —
// modified, staged and untracked files alike — into a stash named message,
// except the files under the paths in keep (absolute, or relative to dir; a
// directory keeps everything below it). Nothing is deleted: the stash holds it
// all, and `git stash list` shows it by name. It reports whether there was
// anything to stash.
//
// The files are named to git one by one instead of as pathspec exclusions: an
// exclusion that matches an ignored path (chief's run log, a .chief/ the
// project ignores) makes `git stash push` fail halfway, after it has saved the
// stash and before it has cleaned the tree.
func StashUncommitted(dir, message string, keep ...string) (bool, error) {
	root, err := RepoRoot(dir)
	if err != nil {
		return false, err
	}
	var kept []string
	for _, k := range keep {
		if !filepath.IsAbs(k) {
			k = filepath.Join(dir, k)
		}
		// dir may itself be relative — a PRD found from the working directory
		// is ".chief/prds/<name>/prd.md" — and filepath.Rel cannot relate a
		// relative path to the absolute root: the keep would silently drop and
		// prd.md and progress.md would go into the stash with the leftovers.
		if abs, err := filepath.Abs(k); err == nil {
			k = abs
		}
		if rel, ok := relativeInRepo(root, k); ok {
			kept = append(kept, rel)
		}
	}
	status, err := runGitRaw(root, "status", "--porcelain", "-z", "--untracked-files=all", "--no-renames")
	if err != nil {
		return false, err
	}
	var files []string
	for _, entry := range strings.Split(status, "\x00") {
		if len(entry) < 4 {
			continue
		}
		path := entry[3:]
		if !underAny(path, kept) {
			files = append(files, path)
		}
	}
	if len(files) == 0 {
		return false, nil
	}
	cmd := exec.Command("git", "--literal-pathspecs", "stash", "push", "--include-untracked", "-m", message,
		"--pathspec-from-file=-", "--pathspec-file-nul")
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(strings.Join(files, "\x00"))
	if out, err := cmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("git stash failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return true, nil
}

// relativeInRepo expresses path relative to the repository root, in git's
// slash form, following symlinks on both (a temp dir on macOS is /var and
// /private/var at once). False when path lies outside the repository.
func relativeInRepo(root, path string) (string, bool) {
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	// The path itself may not exist; its directory usually does.
	if d, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		path = filepath.Join(d, filepath.Base(path))
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// underAny reports whether the slash path p is one of dirs or lies below one.
func underAny(p string, dirs []string) bool {
	for _, d := range dirs {
		if d == "." || p == d || strings.HasPrefix(p, strings.TrimSuffix(d, "/")+"/") {
			return true
		}
	}
	return false
}

// HeadHash returns the full commit hash of the current HEAD. It errors on a repo
// with no commits yet. Callers capture this at the start of a run so the summary
// can be scoped to only the commits that run adds (HeadHash..HEAD at the end).
func HeadHash(dir string) (string, error) {
	return runGit(dir, "rev-parse", "HEAD")
}

// IsAncestor reports whether commit ancestor is reachable from commit
// descendant (a commit counts as its own ancestor). Any failure — an unknown
// hash, no repository — answers false.
func IsAncestor(dir, ancestor, descendant string) bool {
	_, err := runGit(dir, "merge-base", "--is-ancestor", ancestor, descendant)
	return err == nil
}

// StoryRef identifies a story by the fields that make up its chief commit
// subject ("feat: <PRDName>/<ID> - <Title>"). It scopes the run summary to the
// commits chief actually authored for a specific PRD. PRDName is the PRD's
// unique directory name, so it disambiguates same-numbered stories across PRDs.
type StoryRef struct {
	PRDName string
	ID      string
	Title   string
}

// CommitLogForStories returns a one-line-per-commit log (`<short-hash> <subject>`)
// of the commits chief authored for the given stories, in the order the stories
// are passed (PRD order, oldest first). Each commit is matched by its
// "feat: <PRDName>/<ID> - " subject prefix, so the result contains only this
// PRD's work and excludes unrelated commits sitting on the same branch —
// including same-numbered stories from other PRDs, which carry a different
// PRDName. Stories with no matching commit are skipped. Returns an empty string
// (no error) when none match.
//
// When sinceRef is non-empty the match is scoped to sinceRef..HEAD, so a followup
// run's summary describes only the stories that run completed and not the ones an
// earlier run already landed on the same branch.
func CommitLogForStories(repoDir string, stories []StoryRef, sinceRef string) (string, error) {
	var hashes []string
	for _, s := range stories {
		hash, err := FindCommitForStory(repoDir, s.PRDName, s.ID, s.Title, sinceRef)
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

// CommittablePaths filters paths down to those a commit may include without
// overriding the user's .gitignore: a path git ignores is dropped unless it is
// already tracked — gitignore has no effect on tracked files, and dropping one
// would let its changes pile up uncommitted forever. Callers that must land a
// file despite an ignore rule (the run summary, by design) skip this filter
// and rely on CommitPaths' force-add instead. Paths may be absolute or
// relative to dir. When git itself can't answer, the path is kept: the add
// downstream decides.
func CommittablePaths(dir string, paths ...string) []string {
	var keep []string
	for _, p := range paths {
		if isIgnored(dir, p) && !isTracked(dir, p) {
			continue
		}
		keep = append(keep, p)
	}
	return keep
}

// isIgnored reports whether git ignores path in dir (check-ignore exits 0 only
// for an ignored path; "not ignored" and "not a repo" both land on the
// not-ignored side).
func isIgnored(dir, path string) bool {
	cmd := exec.Command("git", "check-ignore", "-q", "--", path)
	cmd.Dir = dir
	return cmd.Run() == nil
}

// isTracked reports whether path is in the index.
func isTracked(dir, path string) bool {
	cmd := exec.Command("git", "ls-files", "--error-unmatch", "--", path)
	cmd.Dir = dir
	return cmd.Run() == nil
}

// CommitPaths stages the given paths and commits them with message. Paths are
// force-added (`git add -f`) so a file lives under an otherwise-gitignored
// directory (e.g. `.chief/`) is still committed. Callers who must respect the
// user's choice to gitignore `.chief/` filter with CommittablePaths first.
// Paths may be absolute or relative to dir. Returns an error if nothing was
// staged or the commit fails.
func CommitPaths(dir, message string, paths ...string) error {
	if len(paths) == 0 {
		return fmt.Errorf("no paths to commit")
	}
	if err := runGitChecked(dir, "git add failed", append([]string{"add", "-f", "--"}, paths...)...); err != nil {
		return err
	}
	return runGitChecked(dir, "git commit failed", append([]string{"commit", "-m", message, "--"}, paths...)...)
}

// HasUncommitted reports whether any of paths differs from HEAD, staged or
// not, untracked included. It tells a commit that failed because there was
// nothing to commit from one that failed and left the changes behind. Paths
// may be absolute or relative to dir. When git cannot answer it says true:
// assuming the changes are still there is the safe side.
func HasUncommitted(dir string, paths ...string) bool {
	out, err := runGitRaw(dir, append([]string{"status", "--porcelain", "--untracked-files=all", "--"}, paths...)...)
	if err != nil {
		return true
	}
	return strings.TrimSpace(out) != ""
}

// HeadSubject returns the subject line (first line of the message) of the
// current HEAD commit. It errors on a repo with no commits yet, letting callers
// treat "no commit to inspect" the same as "HEAD isn't what I expected".
func HeadSubject(dir string) (string, error) {
	return runGit(dir, "log", "-1", "--format=%s")
}

// HeadIsPushed reports whether the current HEAD commit is on any remote-tracking
// branch, which is what makes amending it a rewrite of published history: the
// next push of the branch would be refused as a non-fast-forward. It answers
// true when git cannot say, since not amending is the safe side of that doubt.
func HeadIsPushed(dir string) bool {
	out, err := runGit(dir, "branch", "-r", "--contains", "HEAD")
	if err != nil {
		return true
	}
	return strings.TrimSpace(out) != ""
}

// AmendPaths force-adds the given paths and folds them into the current HEAD
// commit without opening an editor or changing its message. It attaches chief's
// own working files (prd.md, progress.md) to the story commit the agent just
// made, so a completed story's tracked progress travels with its code in one
// commit and survives an interrupted run. Only the listed paths are amended in;
// other unstaged changes are left untouched. Force-add (`-f`) keeps the add
// itself from failing on edge cases; callers who must respect the user's
// gitignore filter with CommittablePaths first. Paths may be absolute or
// relative to dir.
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
