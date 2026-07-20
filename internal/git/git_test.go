package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAddChiefToGitignore(t *testing.T) {
	t.Run("creates new gitignore", func(t *testing.T) {
		dir := t.TempDir()
		gitignorePath := filepath.Join(dir, ".gitignore")

		err := AddChiefToGitignore(dir)
		if err != nil {
			t.Fatalf("AddChiefToGitignore() error = %v", err)
		}

		content, err := os.ReadFile(gitignorePath)
		if err != nil {
			t.Fatalf("failed to read .gitignore: %v", err)
		}

		if string(content) != ".chief/\n" {
			t.Errorf("got %q, want %q", string(content), ".chief/\n")
		}
	})

	t.Run("appends to existing gitignore", func(t *testing.T) {
		dir := t.TempDir()
		gitignorePath := filepath.Join(dir, ".gitignore")

		// Create existing .gitignore
		if err := os.WriteFile(gitignorePath, []byte("node_modules/\n"), 0644); err != nil {
			t.Fatalf("failed to create .gitignore: %v", err)
		}

		err := AddChiefToGitignore(dir)
		if err != nil {
			t.Fatalf("AddChiefToGitignore() error = %v", err)
		}

		content, err := os.ReadFile(gitignorePath)
		if err != nil {
			t.Fatalf("failed to read .gitignore: %v", err)
		}

		expected := "node_modules/\n.chief/\n"
		if string(content) != expected {
			t.Errorf("got %q, want %q", string(content), expected)
		}
	})

	t.Run("appends newline if missing", func(t *testing.T) {
		dir := t.TempDir()
		gitignorePath := filepath.Join(dir, ".gitignore")

		// Create existing .gitignore without trailing newline
		if err := os.WriteFile(gitignorePath, []byte("node_modules/"), 0644); err != nil {
			t.Fatalf("failed to create .gitignore: %v", err)
		}

		err := AddChiefToGitignore(dir)
		if err != nil {
			t.Fatalf("AddChiefToGitignore() error = %v", err)
		}

		content, err := os.ReadFile(gitignorePath)
		if err != nil {
			t.Fatalf("failed to read .gitignore: %v", err)
		}

		expected := "node_modules/\n.chief/\n"
		if string(content) != expected {
			t.Errorf("got %q, want %q", string(content), expected)
		}
	})

	t.Run("skips if already present", func(t *testing.T) {
		dir := t.TempDir()
		gitignorePath := filepath.Join(dir, ".gitignore")

		// Create existing .gitignore with .chief already present
		original := "node_modules/\n.chief/\n"
		if err := os.WriteFile(gitignorePath, []byte(original), 0644); err != nil {
			t.Fatalf("failed to create .gitignore: %v", err)
		}

		err := AddChiefToGitignore(dir)
		if err != nil {
			t.Fatalf("AddChiefToGitignore() error = %v", err)
		}

		content, err := os.ReadFile(gitignorePath)
		if err != nil {
			t.Fatalf("failed to read .gitignore: %v", err)
		}

		// Should remain unchanged
		if string(content) != original {
			t.Errorf("got %q, want %q", string(content), original)
		}
	})

	t.Run("skips if .chief without slash present", func(t *testing.T) {
		dir := t.TempDir()
		gitignorePath := filepath.Join(dir, ".gitignore")

		// Create existing .gitignore with .chief (no slash)
		original := "node_modules/\n.chief\n"
		if err := os.WriteFile(gitignorePath, []byte(original), 0644); err != nil {
			t.Fatalf("failed to create .gitignore: %v", err)
		}

		err := AddChiefToGitignore(dir)
		if err != nil {
			t.Fatalf("AddChiefToGitignore() error = %v", err)
		}

		content, err := os.ReadFile(gitignorePath)
		if err != nil {
			t.Fatalf("failed to read .gitignore: %v", err)
		}

		// Should remain unchanged
		if string(content) != original {
			t.Errorf("got %q, want %q", string(content), original)
		}
	})
}

func TestIsProtectedBranch(t *testing.T) {
	tests := []struct {
		branch   string
		expected bool
	}{
		{"main", true},
		{"master", true},
		{"develop", false},
		{"feature/foo", false},
		{"chief/my-prd", false},
	}

	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			result := IsProtectedBranch(tt.branch)
			if result != tt.expected {
				t.Errorf("IsProtectedBranch(%q) = %v, want %v", tt.branch, result, tt.expected)
			}
		})
	}
}

func TestCreateBranch(t *testing.T) {
	t.Run("creates and switches to new branch", func(t *testing.T) {
		dir := initTestRepo(t)
		if err := CreateBranch(dir, "chief/foo"); err != nil {
			t.Fatalf("CreateBranch() error = %v", err)
		}
		branch, _ := GetCurrentBranch(dir)
		if branch != "chief/foo" {
			t.Errorf("current branch = %q, want %q", branch, "chief/foo")
		}
	})

	t.Run("idempotent when branch already exists", func(t *testing.T) {
		dir := initTestRepo(t)
		if err := CreateBranch(dir, "chief/foo"); err != nil {
			t.Fatalf("first CreateBranch() error = %v", err)
		}
		// Switch away, then re-run: plain `checkout -b` would fail here.
		cmd := exec.Command("git", "checkout", "main")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("checkout main failed: %s", string(out))
		}
		if err := CreateBranch(dir, "chief/foo"); err != nil {
			t.Fatalf("second CreateBranch() should be idempotent, got error = %v", err)
		}
		branch, _ := GetCurrentBranch(dir)
		if branch != "chief/foo" {
			t.Errorf("current branch = %q, want %q", branch, "chief/foo")
		}
	})
}
