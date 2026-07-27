package git

import (
	"os/exec"
	"strings"
	"testing"
)

// gitOut runs a git command in dir and returns its trimmed stdout, failing the
// test when git does.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %s", strings.Join(args, " "), string(out))
	}
	return strings.TrimSpace(string(out))
}

func TestRecordBaseBranch(t *testing.T) {
	t.Run("round-trips through git config", func(t *testing.T) {
		dir := initTestRepo(t)
		runInDir(t, dir, "git", "branch", "develop")

		RecordBaseBranch(dir, "chief/auth", "develop")

		if got := recordedBaseBranch(dir, "chief/auth"); got != "develop" {
			t.Errorf("recordedBaseBranch() = %q, want %q", got, "develop")
		}
	})

	t.Run("ignores a base that no longer exists", func(t *testing.T) {
		dir := initTestRepo(t)
		RecordBaseBranch(dir, "chief/auth", "deleted-branch")

		if got := recordedBaseBranch(dir, "chief/auth"); got != "" {
			t.Errorf("recordedBaseBranch() = %q, want \"\" for a deleted base", got)
		}
	})

	t.Run("records nothing for a detached HEAD or self-reference", func(t *testing.T) {
		dir := initTestRepo(t)
		RecordBaseBranch(dir, "chief/auth", "HEAD")
		RecordBaseBranch(dir, "chief/auth", "chief/auth")
		RecordBaseBranch(dir, "chief/auth", "")

		if got := recordedBaseBranch(dir, "chief/auth"); got != "" {
			t.Errorf("recordedBaseBranch() = %q, want \"\"", got)
		}
	})
}

func TestCreateBranchRecordsBase(t *testing.T) {
	t.Run("records the branch it was cut from", func(t *testing.T) {
		dir := initTestRepo(t)
		runInDir(t, dir, "git", "checkout", "-b", "develop")
		commitFile(t, dir, "dev.txt", "dev\n", "dev work")

		if err := CreateBranch(dir, "chief/auth"); err != nil {
			t.Fatalf("CreateBranch() error = %v", err)
		}

		if got := recordedBaseBranch(dir, "chief/auth"); got != "develop" {
			t.Errorf("recorded base = %q, want %q", got, "develop")
		}
	})

	t.Run("keeps the original base when the branch already exists", func(t *testing.T) {
		dir := initTestRepo(t)
		runInDir(t, dir, "git", "checkout", "-b", "develop")
		commitFile(t, dir, "dev.txt", "dev\n", "dev work")
		if err := CreateBranch(dir, "chief/auth"); err != nil {
			t.Fatalf("CreateBranch() error = %v", err)
		}

		// A followup run checks the branch out again, this time from main.
		runInDir(t, dir, "git", "checkout", "main")
		if err := CreateBranch(dir, "chief/auth"); err != nil {
			t.Fatalf("CreateBranch() second call error = %v", err)
		}

		if got := recordedBaseBranch(dir, "chief/auth"); got != "develop" {
			t.Errorf("recorded base = %q, want it to stay %q", got, "develop")
		}
	})
}

func TestCreateWorktreeRecordsBase(t *testing.T) {
	dir := initTestRepo(t)
	runInDir(t, dir, "git", "checkout", "-b", "develop")
	commitFile(t, dir, "dev.txt", "dev\n", "dev work")

	worktree := t.TempDir() + "/wt"
	if err := CreateWorktree(dir, worktree, "chief/auth"); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}

	// Worktree branches are cut from the default branch, not from whatever the
	// main checkout happens to sit on.
	if got := recordedBaseBranch(dir, "chief/auth"); got != "main" {
		t.Errorf("recorded base = %q, want %q", got, "main")
	}
	// The record lives in the shared repo config, so the worktree sees it too.
	if got := recordedBaseBranch(worktree, "chief/auth"); got != "main" {
		t.Errorf("recorded base seen from worktree = %q, want %q", got, "main")
	}
}

func TestInferBaseBranch(t *testing.T) {
	t.Run("picks the closest ancestor, not the default branch", func(t *testing.T) {
		dir := initTestRepo(t)
		runInDir(t, dir, "git", "checkout", "-b", "develop")
		commitFile(t, dir, "dev1.txt", "1\n", "dev 1")
		commitFile(t, dir, "dev2.txt", "2\n", "dev 2")
		runInDir(t, dir, "git", "checkout", "-b", "chief/auth")
		commitFile(t, dir, "auth.txt", "auth\n", "auth work")

		if got := inferBaseBranch(dir, "chief/auth"); got != "develop" {
			t.Errorf("inferBaseBranch() = %q, want %q", got, "develop")
		}
	})

	t.Run("falls back to the default branch when that is the origin", func(t *testing.T) {
		dir := initTestRepo(t)
		runInDir(t, dir, "git", "checkout", "-b", "chief/auth")
		commitFile(t, dir, "auth.txt", "auth\n", "auth work")

		if got := inferBaseBranch(dir, "chief/auth"); got != "main" {
			t.Errorf("inferBaseBranch() = %q, want %q", got, "main")
		}
	})

	t.Run("prefers the default branch over a sibling cut from the same point", func(t *testing.T) {
		dir := initTestRepo(t)
		runInDir(t, dir, "git", "checkout", "-b", "chief/other")
		commitFile(t, dir, "other.txt", "other\n", "other work")
		runInDir(t, dir, "git", "checkout", "main")
		runInDir(t, dir, "git", "checkout", "-b", "chief/auth")
		commitFile(t, dir, "auth.txt", "auth\n", "auth work")

		if got := inferBaseBranch(dir, "chief/auth"); got != "main" {
			t.Errorf("inferBaseBranch() = %q, want %q", got, "main")
		}
	})

	t.Run("ignores branches that contain the branch itself", func(t *testing.T) {
		dir := initTestRepo(t)
		runInDir(t, dir, "git", "checkout", "-b", "chief/auth")
		commitFile(t, dir, "auth.txt", "auth\n", "auth work")
		// A branch cut *from* chief/auth is a descendant, never its base.
		runInDir(t, dir, "git", "branch", "downstream")

		if got := inferBaseBranch(dir, "chief/auth"); got != "main" {
			t.Errorf("inferBaseBranch() = %q, want %q", got, "main")
		}
	})
}

func TestBaseBranchFor(t *testing.T) {
	t.Run("recorded base wins over inference", func(t *testing.T) {
		dir := initTestRepo(t)
		runInDir(t, dir, "git", "branch", "staging")
		runInDir(t, dir, "git", "checkout", "-b", "chief/auth")
		commitFile(t, dir, "auth.txt", "auth\n", "auth work")
		RecordBaseBranch(dir, "chief/auth", "staging")

		if got := BaseBranchFor(dir, "chief/auth"); got != "staging" {
			t.Errorf("BaseBranchFor() = %q, want %q", got, "staging")
		}
	})

	t.Run("infers the base for a branch chief did not create", func(t *testing.T) {
		dir := initTestRepo(t)
		runInDir(t, dir, "git", "checkout", "-b", "develop")
		commitFile(t, dir, "dev.txt", "dev\n", "dev work")
		runInDir(t, dir, "git", "checkout", "-b", "chief/auth")
		commitFile(t, dir, "auth.txt", "auth\n", "auth work")

		if got := BaseBranchFor(dir, "chief/auth"); got != "develop" {
			t.Errorf("BaseBranchFor() = %q, want %q", got, "develop")
		}
	})

	t.Run("returns the default branch when there is nothing to infer from", func(t *testing.T) {
		dir := initTestRepo(t)
		runInDir(t, dir, "git", "checkout", "-b", "chief/auth")

		if got := BaseBranchFor(dir, "chief/auth"); got != "main" {
			t.Errorf("BaseBranchFor() = %q, want %q", got, "main")
		}
	})

	t.Run("returns empty for an empty branch", func(t *testing.T) {
		dir := initTestRepo(t)
		if got := BaseBranchFor(dir, ""); got != "" {
			t.Errorf("BaseBranchFor() = %q, want \"\"", got)
		}
	})
}

func TestRemoteBranchExists(t *testing.T) {
	t.Run("true for a branch pushed to origin", func(t *testing.T) {
		dir, _ := initTestRepoWithRemote(t)
		if !RemoteBranchExists(dir, "main") {
			t.Error("RemoteBranchExists() = false for a pushed branch, want true")
		}
	})

	t.Run("false for a local-only branch", func(t *testing.T) {
		dir, _ := initTestRepoWithRemote(t)
		runInDir(t, dir, "git", "branch", "local-only")
		if RemoteBranchExists(dir, "local-only") {
			t.Error("RemoteBranchExists() = true for a local-only branch, want false")
		}
	})

	t.Run("false without a remote", func(t *testing.T) {
		dir := initTestRepo(t)
		if RemoteBranchExists(dir, "main") {
			t.Error("RemoteBranchExists() = true without a remote, want false")
		}
	})

	t.Run("false for an empty branch name", func(t *testing.T) {
		dir, _ := initTestRepoWithRemote(t)
		if RemoteBranchExists(dir, "") {
			t.Error("RemoteBranchExists() = true for an empty name, want false")
		}
	})
}

// A base branch is only usable if the local checkout, the recorded value and the
// remote agree — this walks the whole chain the way a real run does.
func TestBaseBranchEndToEnd(t *testing.T) {
	dir, _ := initTestRepoWithRemote(t)
	runInDir(t, dir, "git", "checkout", "-b", "develop")
	commitFile(t, dir, "dev.txt", "dev\n", "dev work")
	runInDir(t, dir, "git", "push", "-u", "origin", "develop")

	if err := CreateBranch(dir, "chief/auth"); err != nil {
		t.Fatalf("CreateBranch() error = %v", err)
	}
	commitFile(t, dir, "auth.txt", "auth\n", "auth work")

	base := BaseBranchFor(dir, "chief/auth")
	if base != "develop" {
		t.Fatalf("BaseBranchFor() = %q, want %q", base, "develop")
	}
	if !RemoteBranchExists(dir, base) {
		t.Errorf("RemoteBranchExists(%q) = false, want true", base)
	}
	if head := gitOut(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); head != "chief/auth" {
		t.Errorf("HEAD = %q, want chief/auth", head)
	}
}
