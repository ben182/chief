package git

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// fetchTimeout caps how long a preflight fetch may block. The check runs before a
// run starts, so an unreachable remote must degrade into "can't tell" quickly
// rather than stalling the start.
const fetchTimeout = 15 * time.Second

// BranchSync describes how a local branch relates to its counterpart on origin.
// Behind counts commits that exist on the remote but not locally; Ahead counts
// local commits the remote hasn't seen.
type BranchSync struct {
	RemoteExists bool
	Behind       int
	Ahead        int
}

// Diverged reports whether the remote holds commits the local branch lacks. This
// is exactly the condition that makes `git push` fail with a non-fast-forward
// rejection, which is worth knowing before a run starts rather than after it.
func (s BranchSync) Diverged() bool { return s.RemoteExists && s.Behind > 0 }

// FastForwardable reports whether the local branch can simply absorb the remote
// commits, having none of its own to replay. Reconciling it cannot conflict.
func (s BranchSync) FastForwardable() bool { return s.Diverged() && s.Ahead == 0 }

// CheckBranchSync compares branch against its counterpart on origin, fetching
// that one ref first so the answer reflects the actual remote rather than a
// possibly stale tracking ref — divergence on a chief/<prd> branch typically
// comes from another machine having pushed the same branch.
//
// Every failure mode (no origin, no network, a branch the remote has never seen,
// a branch that doesn't exist locally) is reported as a zero BranchSync, i.e. "no
// divergence proven". The check informs a preflight warning, so being unable to
// answer must never stop a run from starting.
func CheckBranchSync(dir, branch string) BranchSync {
	if branch == "" || IsProtectedBranch(branch) {
		return BranchSync{}
	}
	if exists, err := BranchExists(dir, branch); err != nil || !exists {
		return BranchSync{}
	}
	if _, err := runGit(dir, "remote", "get-url", "origin"); err != nil {
		return BranchSync{}
	}
	if err := fetchBranch(dir, branch); err != nil {
		return BranchSync{}
	}

	// FETCH_HEAD rather than origin/<branch>: it is written by the fetch we just
	// ran, so it can't be stale, and it is set even in repos whose refspec doesn't
	// maintain a remote-tracking branch for this ref.
	counts, err := runGit(dir, "rev-list", "--left-right", "--count", "FETCH_HEAD..."+branch)
	if err != nil {
		return BranchSync{}
	}
	behind, ahead, ok := parseRevListCounts(counts)
	if !ok {
		return BranchSync{}
	}
	return BranchSync{RemoteExists: true, Behind: behind, Ahead: ahead}
}

// SyncBranchToRemote brings branch in line with origin/<branch> so a later push
// fast-forwards. A branch that is strictly behind absorbs the remote commits; one
// that carries local commits as well is rebased onto the remote tip so those
// commits end up on top. A rebase that hits conflicts is aborted, leaving the
// branch exactly as it was — resolving them is the user's call, not chief's.
func SyncBranchToRemote(dir, branch string) error {
	if err := fetchBranch(dir, branch); err != nil {
		return err
	}
	sync := CheckBranchSync(dir, branch)
	if !sync.Diverged() {
		return nil
	}
	if sync.FastForwardable() {
		return runGitChecked(dir, "failed to fast-forward branch", "merge", "--ff-only", "FETCH_HEAD")
	}
	if err := runGitChecked(dir, "failed to rebase onto origin", "rebase", "FETCH_HEAD"); err != nil {
		// Best-effort: if the abort itself fails the repo is mid-rebase and the
		// original error is still the useful one to report.
		_ = runGitChecked(dir, "", "rebase", "--abort")
		return err
	}
	return nil
}

// fetchBranch fetches a single branch from origin under fetchTimeout.
func fetchBranch(dir, branch string) error {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "fetch", "origin", branch)
	cmd.Dir = dir
	return cmd.Run()
}

// parseRevListCounts splits the "<left>\t<right>" output of
// `git rev-list --left-right --count <left>...<right>`.
func parseRevListCounts(out string) (left, right int, ok bool) {
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, false
	}
	l, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, false
	}
	r, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, false
	}
	return l, r, true
}
