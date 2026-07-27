package git

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
)

// baseBranchConfigKey returns the git-config key chief records a branch's origin
// under. It lives in the repo's own config (shared by every worktree), so the
// answer survives a chief restart and is readable from the worktree a run
// happens in.
func baseBranchConfigKey(branch string) string {
	return "branch." + branch + ".chiefbase"
}

// RecordBaseBranch remembers which branch branch was cut from, so a pull request
// opened for it later targets that branch instead of the repository default.
// Called at the moment the branch is created, when the answer is still known for
// certain; inferring it afterwards from history is guesswork (see
// inferBaseBranch). Best-effort: a repo that refuses the config write just falls
// back to inference.
func RecordBaseBranch(dir, branch, base string) {
	if branch == "" || base == "" || base == branch || base == "HEAD" {
		return
	}
	_ = runGitChecked(dir, "", "config", "--local", baseBranchConfigKey(branch), base)
}

// BaseBranchFor returns the branch a pull request for branch should merge into.
// It prefers the origin chief recorded when it created the branch, falls back to
// inferring it from history, and finally to the repository's default branch.
// Returns "" only when even the default branch can't be determined, which leaves
// the choice to `gh`.
func BaseBranchFor(dir, branch string) string {
	if branch == "" {
		return ""
	}
	if recorded := recordedBaseBranch(dir, branch); recorded != "" {
		return recorded
	}
	if inferred := inferBaseBranch(dir, branch); inferred != "" {
		return inferred
	}
	def, err := GetDefaultBranch(dir)
	if err != nil || def == branch {
		return ""
	}
	return def
}

// recordedBaseBranch reads back what RecordBaseBranch stored, ignoring a branch
// that has since been deleted (locally and on origin) — a stale name would make
// `gh pr create --base` fail outright, where inference still has a chance.
func recordedBaseBranch(dir, branch string) string {
	base, err := runGit(dir, "config", "--get", baseBranchConfigKey(branch))
	if err != nil || base == "" || base == branch {
		return ""
	}
	if exists, err := BranchExists(dir, base); err == nil && exists {
		return base
	}
	if exists, err := BranchExists(dir, "origin/"+base); err == nil && exists {
		return base
	}
	return ""
}

// baseCandidate is a branch a feature branch could have been cut from, tracked
// with both the name a pull request needs (develop) and the ref history lookups
// need (develop, or origin/develop when there is no local copy).
type baseCandidate struct {
	name string
	ref  string
}

// inferBaseBranch guesses where branch was cut from by asking, for every other
// branch in the repo, how much history branch has added since it last shared a
// commit with that branch. The branch that answers with the fewest commits is
// the closest ancestor and therefore the one branch was most likely cut from: a
// chief/x branch cut from develop is a handful of commits past develop, but
// those commits *plus* everything develop gained since it left main past main.
//
// Ties go to the default branch, so two feature branches cut from the same point
// don't nominate each other. Returns "" when nothing qualifies.
func inferBaseBranch(dir, branch string) string {
	tip, err := runGit(dir, "rev-parse", branch)
	if err != nil {
		return ""
	}
	defaultBranch, _ := GetDefaultBranch(dir)

	best := ""
	bestDistance := -1
	for _, cand := range baseCandidates(dir, branch) {
		mergeBase, err := getMergeBase(dir, cand.ref, branch)
		if err != nil || mergeBase == "" || mergeBase == tip {
			// No shared history, or branch is fully contained in the candidate —
			// neither describes a branch that was cut from it.
			continue
		}
		distance, err := commitDistance(dir, mergeBase, branch)
		if err != nil || distance == 0 {
			continue
		}
		if bestDistance == -1 || distance < bestDistance ||
			(distance == bestDistance && cand.name == defaultBranch) {
			best, bestDistance = cand.name, distance
		}
	}
	return best
}

// baseCandidates lists every local branch and every origin branch except branch
// itself, deduplicated by name (a local branch shadows its origin counterpart)
// and with local branches first, so ordering is deterministic.
func baseCandidates(dir, branch string) []baseCandidate {
	out, err := runGit(dir, "for-each-ref", "--format=%(refname:short)", "refs/heads", "refs/remotes/origin")
	if err != nil {
		return nil
	}
	seen := map[string]bool{branch: true, "": true, "HEAD": true}
	var candidates []baseCandidate
	for _, ref := range strings.Split(out, "\n") {
		ref = strings.TrimSpace(ref)
		name := strings.TrimPrefix(ref, "origin/")
		if seen[name] {
			continue
		}
		seen[name] = true
		candidates = append(candidates, baseCandidate{name: name, ref: ref})
	}
	return candidates
}

// commitDistance counts the commits on branch that from isn't an ancestor of,
// i.e. how far branch has moved since from.
func commitDistance(dir, from, branch string) (int, error) {
	out, err := runGit(dir, "rev-list", "--count", from+".."+branch)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(out)
}

// RemoteBranchExists reports whether origin has a branch of this name. A pull
// request can only target a branch GitHub knows about, so a base that exists
// only locally has to be dropped rather than passed to `gh`. The local
// remote-tracking ref answers without network; only when that is missing does it
// ask origin directly, under the same timeout as the preflight fetch.
func RemoteBranchExists(dir, branch string) bool {
	if branch == "" {
		return false
	}
	if exists, err := BranchExists(dir, "refs/remotes/origin/"+branch); err == nil && exists {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--exit-code", "--heads", "origin", branch)
	cmd.Dir = dir
	return cmd.Run() == nil
}
