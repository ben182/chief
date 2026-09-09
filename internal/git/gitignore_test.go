package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestIsChiefIgnored(t *testing.T) {
	t.Run("false in a repo that does not ignore .chief", func(t *testing.T) {
		dir := initTestRepo(t)
		if IsChiefIgnored(dir) {
			t.Error("IsChiefIgnored() = true, want false (no .gitignore entry)")
		}
	})

	t.Run("true once .chief is added to .gitignore", func(t *testing.T) {
		dir := initTestRepo(t)
		if err := AddChiefToGitignore(dir); err != nil {
			t.Fatalf("AddChiefToGitignore() error = %v", err)
		}
		// The pattern AddChiefToGitignore writes is ".chief/" (directory-only), so
		// git check-ignore only matches when .chief actually exists as a directory.
		if err := os.MkdirAll(filepath.Join(dir, ".chief"), 0755); err != nil {
			t.Fatal(err)
		}
		if !IsChiefIgnored(dir) {
			t.Error("IsChiefIgnored() = false, want true after adding .chief/ to .gitignore")
		}
	})
}

// addWorktreeForPRD creates a worktree the way a chief run does: resolve the
// worktree.dir template, then hand the result to CreateWorktree.
func addWorktreeForPRD(t *testing.T, dir, template, prdName, branch string) string {
	t.Helper()
	worktreePath, err := WorktreePathForPRD(dir, template, prdName, branch)
	if err != nil {
		t.Fatalf("WorktreePathForPRD(%q) error = %v", template, err)
	}
	if err := CreateWorktree(CreateWorktreeOptions{
		RepoDir:      dir,
		WorktreePath: worktreePath,
		Branch:       branch,
		PRDName:      prdName,
	}); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	return worktreePath
}

// gitignoreLines returns the trimmed lines of dir's .gitignore, or nil when the
// file does not exist.
func gitignoreLines(t *testing.T, dir string) []string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("failed to read .gitignore: %v", err)
	}
	var lines []string
	for _, line := range strings.Split(string(content), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

func TestCreateWorktreeIgnoresItsLocation(t *testing.T) {
	t.Run("adds .chief/worktrees/ for the default location", func(t *testing.T) {
		dir := initTestRepo(t)

		addWorktreeForPRD(t, dir, "", "my-prd", "chief/my-prd")

		if lines := gitignoreLines(t, dir); !slices.Contains(lines, ".chief/worktrees/") {
			t.Errorf(".gitignore = %q, want it to contain %q", lines, ".chief/worktrees/")
		}
		if status := gitStatus(t, dir); strings.Contains(status, ".chief") {
			t.Errorf("git status = %q, want no mention of the worktree directory", status)
		}
	})

	t.Run("leaves .gitignore alone when the location is already ignored", func(t *testing.T) {
		dir := initTestRepo(t)
		if err := AddChiefToGitignore(dir); err != nil {
			t.Fatalf("AddChiefToGitignore() error = %v", err)
		}

		addWorktreeForPRD(t, dir, "", "my-prd", "chief/my-prd")

		want := []string{".chief/"}
		if got := gitignoreLines(t, dir); !slices.Equal(got, want) {
			t.Errorf(".gitignore = %q, want %q — .chief/ already covers the worktree", got, want)
		}
	})

	t.Run("writes nothing for a location outside the repository", func(t *testing.T) {
		dir := initTestRepo(t)

		addWorktreeForPRD(t, dir, "../{repo}-worktrees/{prd}", "my-prd", "chief/my-prd")

		if got := gitignoreLines(t, dir); got != nil {
			t.Errorf(".gitignore = %q, want no file — the worktree is outside the repository", got)
		}
	})

	t.Run("ignores the parent of a configured location inside the repository", func(t *testing.T) {
		dir := initTestRepo(t)

		addWorktreeForPRD(t, dir, "build/trees/{prd}", "my-prd", "chief/my-prd")

		if lines := gitignoreLines(t, dir); !slices.Contains(lines, "build/trees/") {
			t.Errorf(".gitignore = %q, want it to contain %q", lines, "build/trees/")
		}
		if status := gitStatus(t, dir); strings.Contains(status, "build") {
			t.Errorf("git status = %q, want no mention of the worktree directory", status)
		}
	})

	t.Run("writes nothing when the worktree sits in the repository root", func(t *testing.T) {
		dir := initTestRepo(t)

		addWorktreeForPRD(t, dir, "{prd}-worktree", "my-prd", "chief/my-prd")

		if got := gitignoreLines(t, dir); got != nil {
			t.Errorf(".gitignore = %q, want no file — ignoring the parent would ignore the whole project", got)
		}
	})
}

// gitStatus returns the porcelain status of dir's working tree.
func gitStatus(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status failed: %s", string(out))
	}
	return string(out)
}
