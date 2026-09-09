package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// WorktreeContext is what a worktree setup or teardown command is told about the
// worktree it runs against. Every field reaches the command as a CHIEF_*
// environment variable, so a script can derive a database name, a hostname or a
// slug from the PRD and the branch instead of guessing them from the directory
// it happens to sit in.
type WorktreeContext struct {
	PRDName string
	Branch  string
	// BaseBranch is the branch Branch was cut from, as recorded by
	// RecordBaseBranch, and empty when chief never recorded one. Empty stays
	// empty rather than falling back to the default branch: a script diffing
	// against a guessed base is worse off than one that can see it doesn't know.
	BaseBranch string
	// WorktreePath is also the working directory of the command. RepoDir is the
	// main checkout, which a worktree script needs for whatever is shared — an
	// .env to copy, a seeded database to clone. Both are handed over absolute.
	WorktreePath string
	RepoDir      string
}

// env returns the environment for a setup or teardown command: everything the
// command inherits from chief anyway, plus the CHIEF_* variables. Ours come
// last, because a later entry wins: a CHIEF_* variable that happens to sit in
// chief's own environment must not shadow the worktree actually being handled.
func (c WorktreeContext) env() []string {
	return append(os.Environ(),
		"CHIEF_PRD_NAME="+c.PRDName,
		"CHIEF_BRANCH="+c.Branch,
		"CHIEF_BASE_BRANCH="+c.BaseBranch,
		"CHIEF_WORKTREE_PATH="+absOrSelf(c.WorktreePath),
		"CHIEF_REPO_DIR="+absOrSelf(c.RepoDir),
	)
}

// absOrSelf resolves p against the working directory, falling back to p itself
// when that fails. An empty path stays empty: "" means "not known", and the
// process working directory would be a confidently wrong answer instead.
func absOrSelf(p string) string {
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// RunSetup runs the configured worktree setup command inside wt.WorktreePath
// and returns its combined output. An empty command is a no-op.
func RunSetup(wt WorktreeContext, setup string) (string, error) {
	return runWorktreeCommand(wt, setup)
}

// RunTeardown runs the configured worktree teardown command inside
// wt.WorktreePath and returns its combined output. An empty command is a no-op,
// which is what keeps removal a pure git operation for projects that configure
// nothing.
//
// Callers run this before removing a worktree and must not remove it when this
// returns an error: the teardown owns resources git knows nothing about —
// databases, web-server links — and removing the directory anyway would orphan
// them.
func RunTeardown(wt WorktreeContext, teardown string) (string, error) {
	return runWorktreeCommand(wt, teardown)
}

// runWorktreeCommand runs one of the configured worktree commands with `sh -c`
// in the worktree, with the context in its environment. Both commands are
// written by the same person for the same worktree, so they get the same shell,
// the same working directory and the same variables.
func runWorktreeCommand(wt WorktreeContext, command string) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", nil
	}

	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = absOrSelf(wt.WorktreePath)
	cmd.Env = wt.env()
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
