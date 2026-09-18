package git

import (
	"os/exec"
	"testing"
)

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
