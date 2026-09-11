package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initTestRepo creates a temporary git repository with an initial commit and returns its path.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "checkout", "-b", "main"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup command %v failed: %s", args, string(out))
		}
	}

	// Create an initial commit so branches can be created
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("# Test\n"), 0644); err != nil {
		t.Fatalf("failed to create README: %v", err)
	}
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %s", string(out))
	}
	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %s", string(out))
	}

	return dir
}

func TestGetDefaultBranch(t *testing.T) {
	t.Run("detects main branch", func(t *testing.T) {
		dir := initTestRepo(t)
		branch, err := GetDefaultBranch(dir)
		if err != nil {
			t.Fatalf("GetDefaultBranch() error = %v", err)
		}
		if branch != "main" {
			t.Errorf("GetDefaultBranch() = %q, want %q", branch, "main")
		}
	})

	t.Run("detects master branch", func(t *testing.T) {
		dir := t.TempDir()
		cmds := [][]string{
			{"git", "init"},
			{"git", "config", "user.email", "test@test.com"},
			{"git", "config", "user.name", "Test"},
			{"git", "checkout", "-b", "master"},
		}
		for _, args := range cmds {
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("setup command %v failed: %s", args, string(out))
			}
		}
		readme := filepath.Join(dir, "README.md")
		if err := os.WriteFile(readme, []byte("# Test\n"), 0644); err != nil {
			t.Fatalf("failed to create README: %v", err)
		}
		cmd := exec.Command("git", "add", ".")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git add failed: %s", string(out))
		}
		cmd = exec.Command("git", "commit", "-m", "initial commit")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git commit failed: %s", string(out))
		}

		branch, err := GetDefaultBranch(dir)
		if err != nil {
			t.Fatalf("GetDefaultBranch() error = %v", err)
		}
		if branch != "master" {
			t.Errorf("GetDefaultBranch() = %q, want %q", branch, "master")
		}
	})
}

func TestCreateWorktree(t *testing.T) {
	t.Run("creates worktree and branch", func(t *testing.T) {
		dir := initTestRepo(t)
		wtPath := filepath.Join(dir, "worktrees", "test-prd")

		_, err := CreateWorktree(CreateWorktreeOptions{RepoDir: dir, WorktreePath: wtPath, Branch: "chief/test-prd"})
		if err != nil {
			t.Fatalf("CreateWorktree() error = %v", err)
		}

		// Verify worktree exists and is on the right branch
		branch, err := GetCurrentBranch(wtPath)
		if err != nil {
			t.Fatalf("GetCurrentBranch() error = %v", err)
		}
		if branch != "chief/test-prd" {
			t.Errorf("branch = %q, want %q", branch, "chief/test-prd")
		}
	})

	t.Run("reuses existing valid worktree", func(t *testing.T) {
		dir := initTestRepo(t)
		wtPath := filepath.Join(dir, "worktrees", "test-prd")

		// Create worktree first time
		if _, err := CreateWorktree(CreateWorktreeOptions{RepoDir: dir, WorktreePath: wtPath, Branch: "chief/test-prd"}); err != nil {
			t.Fatalf("first CreateWorktree() error = %v", err)
		}

		// Create a file in the worktree to verify it's reused (not recreated)
		marker := filepath.Join(wtPath, "marker.txt")
		if err := os.WriteFile(marker, []byte("marker"), 0644); err != nil {
			t.Fatalf("failed to create marker: %v", err)
		}

		// Create again - should reuse
		if _, err := CreateWorktree(CreateWorktreeOptions{RepoDir: dir, WorktreePath: wtPath, Branch: "chief/test-prd"}); err != nil {
			t.Fatalf("second CreateWorktree() error = %v", err)
		}

		// Marker should still exist
		if _, err := os.Stat(marker); err != nil {
			t.Error("marker file was removed - worktree was not reused")
		}
	})

	t.Run("recreates stale worktree with wrong branch", func(t *testing.T) {
		dir := initTestRepo(t)
		wtPath := filepath.Join(dir, "worktrees", "test-prd")

		// Create worktree with one branch
		if _, err := CreateWorktree(CreateWorktreeOptions{RepoDir: dir, WorktreePath: wtPath, Branch: "chief/branch-a"}); err != nil {
			t.Fatalf("first CreateWorktree() error = %v", err)
		}

		// Create again with a different branch - should remove and recreate
		if _, err := CreateWorktree(CreateWorktreeOptions{RepoDir: dir, WorktreePath: wtPath, Branch: "chief/branch-b"}); err != nil {
			t.Fatalf("second CreateWorktree() error = %v", err)
		}

		branch, err := GetCurrentBranch(wtPath)
		if err != nil {
			t.Fatalf("GetCurrentBranch() error = %v", err)
		}
		if branch != "chief/branch-b" {
			t.Errorf("branch = %q, want %q", branch, "chief/branch-b")
		}
	})

	t.Run("runs the teardown inside the stale worktree before removing it", func(t *testing.T) {
		dir := initTestRepo(t)
		wtPath := filepath.Join(dir, "worktrees", "test-prd")

		if _, err := CreateWorktree(CreateWorktreeOptions{RepoDir: dir, WorktreePath: wtPath, Branch: "chief/branch-a"}); err != nil {
			t.Fatalf("first CreateWorktree() error = %v", err)
		}

		// The teardown lists its working directory into a log outside the
		// worktree. README.md is only listed while the worktree still exists, so
		// a log naming it proves the command ran in the worktree and before the
		// removal.
		logPath := filepath.Join(t.TempDir(), "teardown.log")
		if _, err := CreateWorktree(CreateWorktreeOptions{RepoDir: dir, WorktreePath: wtPath, Branch: "chief/branch-b", Teardown: "ls > " + logPath}); err != nil {
			t.Fatalf("second CreateWorktree() error = %v", err)
		}

		out, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("teardown did not run: %v", err)
		}
		if !strings.Contains(string(out), "README.md") {
			t.Errorf("teardown ran outside the worktree, it saw: %q", out)
		}
	})

	t.Run("tells the stale teardown which PRD and branch it is tearing down", func(t *testing.T) {
		dir := initTestRepo(t)
		wtPath := filepath.Join(dir, "worktrees", "test-prd")

		if _, err := CreateWorktree(CreateWorktreeOptions{RepoDir: dir, WorktreePath: wtPath, Branch: "chief/branch-a", PRDName: "test-prd"}); err != nil {
			t.Fatalf("first CreateWorktree() error = %v", err)
		}

		// The resources the teardown is about to drop belong to the branch the
		// stale worktree is standing on, not to the one replacing it.
		logPath := filepath.Join(t.TempDir(), "teardown.log")
		if _, err := CreateWorktree(CreateWorktreeOptions{
			RepoDir:      dir,
			WorktreePath: wtPath,
			Branch:       "chief/branch-b",
			PRDName:      "test-prd",
			Teardown:     envProbe + " > " + logPath,
		}); err != nil {
			t.Fatalf("second CreateWorktree() error = %v", err)
		}

		out, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("teardown did not run: %v", err)
		}
		want := strings.Join([]string{"test-prd", "chief/branch-a", "main", wtPath, dir}, "|")
		if strings.TrimSpace(string(out)) != want {
			t.Errorf("teardown saw %q, want %q", strings.TrimSpace(string(out)), want)
		}
	})

	t.Run("skips the teardown when the worktree is reused", func(t *testing.T) {
		dir := initTestRepo(t)
		wtPath := filepath.Join(dir, "worktrees", "test-prd")

		if _, err := CreateWorktree(CreateWorktreeOptions{RepoDir: dir, WorktreePath: wtPath, Branch: "chief/test-prd"}); err != nil {
			t.Fatalf("first CreateWorktree() error = %v", err)
		}

		marker := filepath.Join(t.TempDir(), "teardown-ran")
		if _, err := CreateWorktree(CreateWorktreeOptions{RepoDir: dir, WorktreePath: wtPath, Branch: "chief/test-prd", Teardown: "touch " + marker}); err != nil {
			t.Fatalf("second CreateWorktree() error = %v", err)
		}

		if _, err := os.Stat(marker); err == nil {
			t.Error("teardown ran for a worktree that was kept")
		}
	})

	t.Run("keeps the stale worktree when the teardown fails", func(t *testing.T) {
		dir := initTestRepo(t)
		wtPath := filepath.Join(dir, "worktrees", "test-prd")

		if _, err := CreateWorktree(CreateWorktreeOptions{RepoDir: dir, WorktreePath: wtPath, Branch: "chief/branch-a"}); err != nil {
			t.Fatalf("first CreateWorktree() error = %v", err)
		}

		logDir := t.TempDir()
		_, err := CreateWorktree(CreateWorktreeOptions{RepoDir: dir, WorktreePath: wtPath, Branch: "chief/branch-b", Teardown: "echo boom >&2; exit 1", LogDir: logDir})
		if err == nil {
			t.Fatal("expected CreateWorktree() to fail on a failing teardown")
		}
		// The output belongs in the log, and the error belongs on one line of a
		// modal — so the error says where to read the rest.
		if strings.Contains(err.Error(), "boom") {
			t.Errorf("the error carries the teardown output instead of pointing at the log: %v", err)
		}
		logs, _ := filepath.Glob(filepath.Join(logDir, "teardown-*.log"))
		if len(logs) != 1 {
			t.Fatalf("teardown logs = %v, want exactly one", logs)
		}
		if !strings.Contains(err.Error(), logs[0]) {
			t.Errorf("error = %v, want it to name %s", err, logs[0])
		}
		data, readErr := os.ReadFile(logs[0])
		if readErr != nil {
			t.Fatalf("reading the teardown log: %v", readErr)
		}
		if !strings.Contains(string(data), "boom") {
			t.Errorf("teardown log = %q, want the command output in it", string(data))
		}

		branch, branchErr := GetCurrentBranch(wtPath)
		if branchErr != nil {
			t.Fatalf("worktree is gone: %v", branchErr)
		}
		if branch != "chief/branch-a" {
			t.Errorf("branch = %q, want the stale worktree untouched on %q", branch, "chief/branch-a")
		}
	})
}

func TestRemoveWorktree(t *testing.T) {
	t.Run("removes existing worktree", func(t *testing.T) {
		dir := initTestRepo(t)
		wtPath := filepath.Join(dir, "worktrees", "test-prd")

		if _, err := CreateWorktree(CreateWorktreeOptions{RepoDir: dir, WorktreePath: wtPath, Branch: "chief/test-prd"}); err != nil {
			t.Fatalf("CreateWorktree() error = %v", err)
		}

		err := RemoveWorktree(dir, wtPath)
		if err != nil {
			t.Fatalf("RemoveWorktree() error = %v", err)
		}

		// Verify the directory is gone
		if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
			t.Error("worktree directory still exists after removal")
		}
	})
}

func TestListWorktrees(t *testing.T) {
	t.Run("lists worktrees including main", func(t *testing.T) {
		dir := initTestRepo(t)
		wtPath := filepath.Join(dir, "worktrees", "test-prd")

		if _, err := CreateWorktree(CreateWorktreeOptions{RepoDir: dir, WorktreePath: wtPath, Branch: "chief/test-prd"}); err != nil {
			t.Fatalf("CreateWorktree() error = %v", err)
		}

		worktrees, err := ListWorktrees(dir)
		if err != nil {
			t.Fatalf("ListWorktrees() error = %v", err)
		}

		if len(worktrees) < 2 {
			t.Fatalf("expected at least 2 worktrees, got %d", len(worktrees))
		}

		// Find our worktree
		found := false
		for _, wt := range worktrees {
			if wt.Branch == "chief/test-prd" {
				found = true
				if wt.HEAD == "" {
					t.Error("worktree HEAD is empty")
				}
			}
		}
		if !found {
			t.Error("worktree with branch chief/test-prd not found in list")
		}
	})
}

func TestIsWorktree(t *testing.T) {
	t.Run("returns true for valid worktree", func(t *testing.T) {
		dir := initTestRepo(t)
		wtPath := filepath.Join(dir, "worktrees", "test-prd")

		if _, err := CreateWorktree(CreateWorktreeOptions{RepoDir: dir, WorktreePath: wtPath, Branch: "chief/test-prd"}); err != nil {
			t.Fatalf("CreateWorktree() error = %v", err)
		}

		if !IsWorktree(wtPath) {
			t.Error("IsWorktree() = false, want true")
		}
	})

	t.Run("returns false for non-existent path", func(t *testing.T) {
		if IsWorktree("/nonexistent/path") {
			t.Error("IsWorktree() = true for non-existent path")
		}
	})

	t.Run("returns false for plain directory", func(t *testing.T) {
		dir := t.TempDir()
		if IsWorktree(dir) {
			t.Error("IsWorktree() = true for plain directory")
		}
	})
}

func TestPruneWorktrees(t *testing.T) {
	t.Run("prune succeeds on clean repo", func(t *testing.T) {
		dir := initTestRepo(t)
		err := PruneWorktrees(dir)
		if err != nil {
			t.Fatalf("PruneWorktrees() error = %v", err)
		}
	})
}

func TestMergeBranch(t *testing.T) {
	t.Run("fast-forward merge succeeds", func(t *testing.T) {
		dir := initTestRepo(t)

		// Create a branch with a commit
		cmd := exec.Command("git", "checkout", "-b", "feature")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("checkout failed: %s", string(out))
		}

		featureFile := filepath.Join(dir, "feature.txt")
		if err := os.WriteFile(featureFile, []byte("feature\n"), 0644); err != nil {
			t.Fatalf("failed to create feature file: %v", err)
		}
		cmd = exec.Command("git", "add", ".")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git add failed: %s", string(out))
		}
		cmd = exec.Command("git", "commit", "-m", "add feature")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git commit failed: %s", string(out))
		}

		// Switch back to main and merge
		cmd = exec.Command("git", "checkout", "main")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("checkout main failed: %s", string(out))
		}

		conflicts, err := MergeBranch(dir, "feature")
		if err != nil {
			t.Fatalf("MergeBranch() error = %v", err)
		}
		if len(conflicts) > 0 {
			t.Errorf("expected no conflicts, got %v", conflicts)
		}

		// Verify feature file exists on main
		if _, err := os.Stat(featureFile); err != nil {
			t.Error("feature.txt not present after merge")
		}
	})

	t.Run("merge conflict returns conflicting files", func(t *testing.T) {
		dir := initTestRepo(t)

		// Create conflicting changes on two branches
		conflictFile := filepath.Join(dir, "conflict.txt")
		if err := os.WriteFile(conflictFile, []byte("main content\n"), 0644); err != nil {
			t.Fatalf("failed to create conflict file: %v", err)
		}
		cmd := exec.Command("git", "add", ".")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git add failed: %s", string(out))
		}
		cmd = exec.Command("git", "commit", "-m", "main change")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git commit failed: %s", string(out))
		}

		// Create feature branch from parent commit
		cmd = exec.Command("git", "checkout", "-b", "feature", "HEAD~1")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("checkout failed: %s", string(out))
		}
		if err := os.WriteFile(conflictFile, []byte("feature content\n"), 0644); err != nil {
			t.Fatalf("failed to create conflict file: %v", err)
		}
		cmd = exec.Command("git", "add", ".")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git add failed: %s", string(out))
		}
		cmd = exec.Command("git", "commit", "-m", "feature change")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git commit failed: %s", string(out))
		}

		// Switch to main and try to merge
		cmd = exec.Command("git", "checkout", "main")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("checkout main failed: %s", string(out))
		}

		conflicts, err := MergeBranch(dir, "feature")
		if err == nil {
			t.Fatal("MergeBranch() expected error for conflict, got nil")
		}
		if len(conflicts) == 0 {
			t.Fatal("expected conflict files, got none")
		}

		foundConflict := false
		for _, f := range conflicts {
			if strings.Contains(f, "conflict.txt") {
				foundConflict = true
			}
		}
		if !foundConflict {
			t.Errorf("expected conflict.txt in conflicts, got %v", conflicts)
		}

		// Verify the merge was aborted (clean state)
		cmd = exec.Command("git", "status", "--porcelain")
		cmd.Dir = dir
		output, _ := cmd.Output()
		if strings.TrimSpace(string(output)) != "" {
			t.Errorf("expected clean working tree after merge abort, got: %s", string(output))
		}
	})
}

func TestDetectOrphanedWorktrees(t *testing.T) {
	t.Run("returns nil outside a git repository", func(t *testing.T) {
		result := DetectOrphanedWorktrees(t.TempDir(), "")
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("returns no entries for a repository without worktrees", func(t *testing.T) {
		dir := initTestRepo(t)
		result := DetectOrphanedWorktrees(dir, "")
		if len(result) != 0 {
			t.Errorf("expected no entries, got %v", result)
		}
	})

	t.Run("detects worktrees at the default location", func(t *testing.T) {
		dir := initTestRepo(t)
		for _, name := range []string{"auth", "payments"} {
			if _, err := CreateWorktree(CreateWorktreeOptions{
				RepoDir:      dir,
				WorktreePath: filepath.Join(dir, ".chief", "worktrees", name),
				Branch:       "chief/" + name,
			}); err != nil {
				t.Fatalf("CreateWorktree(%s) error = %v", name, err)
			}
		}

		result := DetectOrphanedWorktrees(dir, "")
		if len(result) != 2 {
			t.Fatalf("expected 2 entries, got %d: %v", len(result), result)
		}
		for _, name := range []string{"auth", "payments"} {
			want := filepath.Join(dir, ".chief", "worktrees", name)
			if result[name] != want {
				t.Errorf("result[%q] = %q, want %q", name, result[name], want)
			}
		}
	})

	t.Run("detects worktrees at a configured location outside the checkout", func(t *testing.T) {
		dir := initTestRepo(t)
		template := "../{repo}-worktrees/{branch}"
		path := filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-worktrees", "chief-auth")
		if _, err := CreateWorktree(CreateWorktreeOptions{
			RepoDir:      dir,
			WorktreePath: path,
			Branch:       "chief/auth",
		}); err != nil {
			t.Fatalf("CreateWorktree() error = %v", err)
		}

		// {prd} is missing from this template, so nothing can be attributed.
		if result := DetectOrphanedWorktrees(dir, template); len(result) != 0 {
			t.Errorf("expected no entries for a template without {prd}, got %v", result)
		}

		// The same worktree under a template that does name the PRD.
		named := filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-worktrees", "auth")
		if _, err := CreateWorktree(CreateWorktreeOptions{
			RepoDir:      dir,
			WorktreePath: named,
			Branch:       "chief/named",
		}); err != nil {
			t.Fatalf("CreateWorktree() error = %v", err)
		}
		result := DetectOrphanedWorktrees(dir, "../{repo}-worktrees/{prd}")
		if len(result) != 2 {
			t.Fatalf("expected 2 entries, got %d: %v", len(result), result)
		}
		if result["auth"] != named {
			t.Errorf("result[\"auth\"] = %q, want %q", result["auth"], named)
		}
		if result["chief-auth"] != path {
			t.Errorf("result[\"chief-auth\"] = %q, want %q", result["chief-auth"], path)
		}
	})

	t.Run("ignores worktrees outside the template", func(t *testing.T) {
		dir := initTestRepo(t)
		outside := filepath.Join(t.TempDir(), "somewhere-else")
		if _, err := CreateWorktree(CreateWorktreeOptions{
			RepoDir:      dir,
			WorktreePath: outside,
			Branch:       "chief/auth",
		}); err != nil {
			t.Fatalf("CreateWorktree() error = %v", err)
		}

		result := DetectOrphanedWorktrees(dir, "")
		if len(result) != 0 {
			t.Errorf("expected no entries, got %v", result)
		}
	})

	t.Run("ignores the main checkout even when the template matches it", func(t *testing.T) {
		// "../{prd}" puts worktrees beside the checkout, so the checkout's own
		// directory matches the pattern with {prd} = its basename.
		dir := initTestRepo(t)
		result := DetectOrphanedWorktrees(dir, "../{prd}")
		if len(result) != 0 {
			t.Errorf("expected the main checkout to be ignored, got %v", result)
		}
	})
}

// runGitIn runs a git command in dir and fails the test if it doesn't succeed.
func runGitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s failed: %s", args, dir, string(out))
	}
}

// addBranchWithMarker creates branch off the current HEAD, commits a file that
// exists nowhere else, and returns to the branch it started on. A worktree that
// contains the marker file can only have been cut from this branch.
func addBranchWithMarker(t *testing.T, dir, branch, marker string) {
	t.Helper()
	start, err := GetCurrentBranch(dir)
	if err != nil {
		t.Fatalf("GetCurrentBranch() error = %v", err)
	}
	runGitIn(t, dir, "checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(dir, marker), []byte("marker\n"), 0644); err != nil {
		t.Fatalf("failed to write marker: %v", err)
	}
	runGitIn(t, dir, "add", marker)
	runGitIn(t, dir, "commit", "-m", "add "+marker)
	runGitIn(t, dir, "checkout", start)
}

func TestCreateWorktreeBaseBranch(t *testing.T) {
	t.Run("cuts the branch from a local base branch", func(t *testing.T) {
		dir := initTestRepo(t)
		addBranchWithMarker(t, dir, "develop", "only-on-develop.txt")
		wtPath := filepath.Join(dir, "worktrees", "test-prd")

		if _, err := CreateWorktree(CreateWorktreeOptions{
			RepoDir:      dir,
			WorktreePath: wtPath,
			Branch:       "chief/test-prd",
			BaseBranch:   "develop",
		}); err != nil {
			t.Fatalf("CreateWorktree() error = %v", err)
		}

		if _, err := os.Stat(filepath.Join(wtPath, "only-on-develop.txt")); err != nil {
			t.Errorf("worktree was not cut from develop: %v", err)
		}
		if got := RecordedBaseBranch(dir, "chief/test-prd"); got != "develop" {
			t.Errorf("RecordedBaseBranch() = %q, want %q", got, "develop")
		}
	})

	t.Run("falls back to origin when the base branch is only on the remote", func(t *testing.T) {
		upstream := initTestRepo(t)
		addBranchWithMarker(t, upstream, "develop", "only-on-develop.txt")

		clone := filepath.Join(t.TempDir(), "clone")
		runGitIn(t, upstream, "clone", upstream, clone)
		runGitIn(t, clone, "config", "user.email", "test@test.com")
		runGitIn(t, clone, "config", "user.name", "Test")
		if exists, _ := BranchExists(clone, "refs/heads/develop"); exists {
			t.Fatal("clone was expected to have no local develop branch")
		}

		wtPath := filepath.Join(clone, "worktrees", "test-prd")
		if _, err := CreateWorktree(CreateWorktreeOptions{
			RepoDir:      clone,
			WorktreePath: wtPath,
			Branch:       "chief/test-prd",
			BaseBranch:   "develop",
		}); err != nil {
			t.Fatalf("CreateWorktree() error = %v", err)
		}

		if _, err := os.Stat(filepath.Join(wtPath, "only-on-develop.txt")); err != nil {
			t.Errorf("worktree was not cut from origin/develop: %v", err)
		}
		// The plain name, not origin/develop: this is what a pull request has
		// to target.
		if got := RecordedBaseBranch(clone, "chief/test-prd"); got != "develop" {
			t.Errorf("RecordedBaseBranch() = %q, want %q", got, "develop")
		}
	})

	t.Run("refuses to start when the configured base branch is nowhere", func(t *testing.T) {
		dir := initTestRepo(t)
		wtPath := filepath.Join(dir, "worktrees", "test-prd")

		_, err := CreateWorktree(CreateWorktreeOptions{
			RepoDir:      dir,
			WorktreePath: wtPath,
			Branch:       "chief/test-prd",
			BaseBranch:   "develop",
		})
		if err == nil {
			t.Fatal("expected CreateWorktree() to fail on a missing base branch")
		}
		for _, want := range []string{"worktree.baseBranch", "develop"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %v does not mention %q", err, want)
			}
		}
		if exists, _ := BranchExists(dir, "refs/heads/chief/test-prd"); exists {
			t.Error("branch was created despite the missing base branch")
		}
		if _, statErr := os.Stat(wtPath); statErr == nil {
			t.Error("worktree was created despite the missing base branch")
		}
	})

	t.Run("uses the detected default branch when nothing is configured", func(t *testing.T) {
		dir := initTestRepo(t)
		addBranchWithMarker(t, dir, "develop", "only-on-develop.txt")
		wtPath := filepath.Join(dir, "worktrees", "test-prd")

		if _, err := CreateWorktree(CreateWorktreeOptions{
			RepoDir:      dir,
			WorktreePath: wtPath,
			Branch:       "chief/test-prd",
		}); err != nil {
			t.Fatalf("CreateWorktree() error = %v", err)
		}

		if _, err := os.Stat(filepath.Join(wtPath, "only-on-develop.txt")); err == nil {
			t.Error("worktree was cut from develop, want the default branch")
		}
		if got := RecordedBaseBranch(dir, "chief/test-prd"); got != "main" {
			t.Errorf("RecordedBaseBranch() = %q, want %q", got, "main")
		}
	})
}

// Whether the worktree was created or picked up decides whether the caller
// still has to set it up, so CreateWorktree has to say which of the two it did.
func TestCreateWorktreeReportsReuse(t *testing.T) {
	dir := initTestRepo(t)
	wtPath := filepath.Join(dir, "worktrees", "test-prd")

	res, err := CreateWorktree(CreateWorktreeOptions{RepoDir: dir, WorktreePath: wtPath, Branch: "chief/branch-a"})
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	if res.Reused {
		t.Error("a freshly created worktree was reported as reused")
	}

	res, err = CreateWorktree(CreateWorktreeOptions{RepoDir: dir, WorktreePath: wtPath, Branch: "chief/branch-a"})
	if err != nil {
		t.Fatalf("second CreateWorktree() error = %v", err)
	}
	if !res.Reused {
		t.Error("an existing worktree on the expected branch was not reported as reused")
	}

	// A stale worktree is torn down and replaced, so what the caller ends up
	// with is a new checkout, not the one that was standing there.
	res, err = CreateWorktree(CreateWorktreeOptions{RepoDir: dir, WorktreePath: wtPath, Branch: "chief/branch-b"})
	if err != nil {
		t.Fatalf("third CreateWorktree() error = %v", err)
	}
	if res.Reused {
		t.Error("a recreated stale worktree was reported as reused")
	}
}

// Replacing a stale worktree removes a directory that holds the PRD's working
// files — the record of whatever ran in it. In a project that gitignores
// `.chief/` the branch never carried them, so that copy is the only one there
// is, and it comes home before the directory goes.
func TestCreateWorktreeReclaimsThePRDFilesOfAStaleWorktree(t *testing.T) {
	dir := initTestRepo(t)
	wtPath := filepath.Join(dir, "worktrees", "auth")
	prdDir := filepath.Join(dir, ".chief", "prds", "auth")

	// The case reclaiming exists for: a project that gitignores `.chief/`, so the
	// branch never carried the PRD files and the worktree's copy is the only one.
	// (Ignored files also don't hold up `git worktree remove`, where tracked
	// changes would — git refuses those on their own.)
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".chief/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "add", ".gitignore")
	runGitIn(t, dir, "commit", "-m", "ignore .chief")

	if err := os.MkdirAll(prdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prdDir, "prd.md"), []byte("# PRD\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := CreateWorktreeOptions{RepoDir: dir, WorktreePath: wtPath, Branch: "chief/auth", PRDDir: prdDir}
	if _, err := CreateWorktree(opts); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}

	// What a run in that worktree would have recorded.
	runPRDDir := filepath.Join(wtPath, ".chief", "prds", "auth")
	if err := os.MkdirAll(runPRDDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runPRDDir, "prd.md"), []byte("# PRD\n**Status:** done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runPRDDir, "progress.md"), []byte("# progress\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A different branch makes the worktree stale, so it is replaced.
	opts.Branch = "chief/auth-v2"
	if _, err := CreateWorktree(opts); err != nil {
		t.Fatalf("second CreateWorktree() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(prdDir, "prd.md"))
	if err != nil || string(data) != "# PRD\n**Status:** done\n" {
		t.Errorf("the project's prd.md = %q (err %v), want the replaced worktree's state", string(data), err)
	}
	if _, err := os.Stat(filepath.Join(prdDir, "progress.md")); err != nil {
		t.Errorf("progress.md was not brought home: %v", err)
	}
}

// A branch lives in one worktree at a time, which is what stands between a
// caller and a checkout that fails with "already used by worktree".
func TestWorktreeForBranch(t *testing.T) {
	dir := initTestRepo(t)
	wtPath := filepath.Join(dir, "worktrees", "auth")
	if _, err := CreateWorktree(CreateWorktreeOptions{RepoDir: dir, WorktreePath: wtPath, Branch: "chief/auth"}); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}

	if got := WorktreeForBranch(dir, "chief/auth"); normalizePath(got) != normalizePath(wtPath) {
		t.Errorf("WorktreeForBranch(chief/auth) = %q, want %q", got, wtPath)
	}
	if got := WorktreeForBranch(dir, "chief/nobody"); got != "" {
		t.Errorf("WorktreeForBranch(chief/nobody) = %q, want no worktree", got)
	}
	// The main checkout's own branch is not a conflict: it is already there.
	if got := WorktreeForBranch(dir, "main"); got != "" {
		t.Errorf("WorktreeForBranch(main) = %q, want the main checkout ignored", got)
	}
}
