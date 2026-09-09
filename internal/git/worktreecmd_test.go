package git

import (
	"path/filepath"
	"strings"
	"testing"
)

// envProbe prints every variable a setup or teardown command is promised, in a
// fixed order, so one run of the command answers the whole contract at once.
const envProbe = `echo "$CHIEF_PRD_NAME|$CHIEF_BRANCH|$CHIEF_BASE_BRANCH|$CHIEF_WORKTREE_PATH|$CHIEF_REPO_DIR"`

// worktreeFixture creates a worktree for a PRD the way chief does and returns
// the repo directory, the worktree path and the context a command would get.
func worktreeFixture(t *testing.T, prdName, branch string) (string, string, WorktreeContext) {
	t.Helper()
	dir := initTestRepo(t)
	wtPath := filepath.Join(dir, "worktrees", prdName)
	opts := CreateWorktreeOptions{
		RepoDir:      dir,
		WorktreePath: wtPath,
		Branch:       branch,
		PRDName:      prdName,
	}
	if err := CreateWorktree(opts); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	return dir, wtPath, WorktreeContext{
		PRDName:      prdName,
		Branch:       branch,
		BaseBranch:   RecordedBaseBranch(dir, branch),
		WorktreePath: wtPath,
		RepoDir:      dir,
	}
}

func TestRunSetupPassesTheContextAsEnvironment(t *testing.T) {
	dir, wtPath, wt := worktreeFixture(t, "auth", "chief/auth")

	out, err := RunSetup(wt, envProbe)
	if err != nil {
		t.Fatalf("RunSetup() error = %v (%s)", err, out)
	}

	// initTestRepo commits on main, so main is the branch chief cut chief/auth
	// from and recorded as its base.
	want := strings.Join([]string{"auth", "chief/auth", "main", wtPath, dir}, "|")
	if out != want {
		t.Errorf("setup saw %q, want %q", out, want)
	}
}

func TestRunTeardownPassesTheContextAsEnvironment(t *testing.T) {
	dir, wtPath, wt := worktreeFixture(t, "auth", "chief/auth")

	out, err := RunTeardown(wt, envProbe)
	if err != nil {
		t.Fatalf("RunTeardown() error = %v (%s)", err, out)
	}

	want := strings.Join([]string{"auth", "chief/auth", "main", wtPath, dir}, "|")
	if out != want {
		t.Errorf("teardown saw %q, want %q", out, want)
	}
}

// A branch chief did not create has no recorded base. The teardown then has to
// see an empty CHIEF_BASE_BRANCH rather than a guessed one, so a script can tell
// "unknown" apart from a real branch name.
func TestRunTeardownLeavesAnUnknownBaseBranchEmpty(t *testing.T) {
	dir := initTestRepo(t)
	if err := runGitChecked(dir, "failed to create branch", "branch", "chief/auth", "main"); err != nil {
		t.Fatalf("branch setup failed: %v", err)
	}
	wtPath := filepath.Join(dir, "worktrees", "auth")
	if err := CreateWorktree(CreateWorktreeOptions{
		RepoDir:      dir,
		WorktreePath: wtPath,
		Branch:       "chief/auth",
		PRDName:      "auth",
	}); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}

	wt := WorktreeContext{
		PRDName:      "auth",
		Branch:       "chief/auth",
		BaseBranch:   RecordedBaseBranch(dir, "chief/auth"),
		WorktreePath: wtPath,
		RepoDir:      dir,
	}
	out, err := RunTeardown(wt, `echo "[$CHIEF_BASE_BRANCH]"`)
	if err != nil {
		t.Fatalf("RunTeardown() error = %v (%s)", err, out)
	}
	if out != "[]" {
		t.Errorf("CHIEF_BASE_BRANCH = %s, want an empty value", out)
	}
}

// The paths are promised absolute, so a script can hand them to a tool with a
// working directory of its own.
func TestWorktreeCommandPathsAreAbsolute(t *testing.T) {
	dir, wtPath, _ := worktreeFixture(t, "auth", "chief/auth")

	wt := WorktreeContext{
		WorktreePath: dir + "/worktrees/../worktrees/auth",
		RepoDir:      dir + "/.",
	}
	out, err := RunSetup(wt, `echo "$CHIEF_WORKTREE_PATH|$CHIEF_REPO_DIR"`)
	if err != nil {
		t.Fatalf("RunSetup() error = %v (%s)", err, out)
	}
	if want := wtPath + "|" + dir; out != want {
		t.Errorf("paths = %q, want %q", out, want)
	}
}

func TestRunSetupRunsInsideTheWorktree(t *testing.T) {
	_, _, wt := worktreeFixture(t, "auth", "chief/auth")

	out, err := RunSetup(wt, "ls")
	if err != nil {
		t.Fatalf("RunSetup() error = %v (%s)", err, out)
	}
	if !strings.Contains(out, "README.md") {
		t.Errorf("setup ran outside the worktree, it saw: %q", out)
	}
}

func TestRunSetupWithoutACommandIsANoOp(t *testing.T) {
	out, err := RunSetup(WorktreeContext{WorktreePath: "/nonexistent"}, "   ")
	if err != nil {
		t.Fatalf("RunSetup() error = %v", err)
	}
	if out != "" {
		t.Errorf("RunSetup() output = %q, want empty", out)
	}
}
