package git

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreePathForPRD(t *testing.T) {
	repo := filepath.Join(string(filepath.Separator), "home", "user", "project")

	t.Run("empty template uses the default location", func(t *testing.T) {
		got, err := WorktreePathForPRD(repo, "", "auth", "chief/auth")
		if err != nil {
			t.Fatalf("WorktreePathForPRD() error = %v", err)
		}
		want := filepath.Join(repo, ".chief", "worktrees", "auth")
		if got != want {
			t.Errorf("WorktreePathForPRD() = %q, want %q", got, want)
		}
	})

	t.Run("expands prd, repo and branch placeholders", func(t *testing.T) {
		got, err := WorktreePathForPRD(repo, "../wt/{repo}/{prd}/{branch}", "auth", "chief/auth")
		if err != nil {
			t.Fatalf("WorktreePathForPRD() error = %v", err)
		}
		want := filepath.Join(string(filepath.Separator), "home", "user", "wt", "project", "auth", "chief-auth")
		if got != want {
			t.Errorf("WorktreePathForPRD() = %q, want %q", got, want)
		}
	})

	t.Run("absolute template is used as is", func(t *testing.T) {
		tmpl := filepath.Join(string(filepath.Separator), "tmp", "chief-worktrees", "{prd}")
		got, err := WorktreePathForPRD(repo, tmpl, "payments", "chief/payments")
		if err != nil {
			t.Fatalf("WorktreePathForPRD() error = %v", err)
		}
		want := filepath.Join(string(filepath.Separator), "tmp", "chief-worktrees", "payments")
		if got != want {
			t.Errorf("WorktreePathForPRD() = %q, want %q", got, want)
		}
	})

	t.Run("rejects a template pointing at the main checkout", func(t *testing.T) {
		_, err := WorktreePathForPRD(repo, ".", "auth", "chief/auth")
		if err == nil {
			t.Fatal("expected an error for a template pointing at the main checkout")
		}
		if !strings.Contains(err.Error(), "main checkout") {
			t.Errorf("error %q does not mention the main checkout", err)
		}
	})

	t.Run("rejects a template pointing at the parent directory", func(t *testing.T) {
		_, err := WorktreePathForPRD(repo, "..", "auth", "chief/auth")
		if err == nil {
			t.Fatal("expected an error for a template pointing at the parent directory")
		}
		if !strings.Contains(err.Error(), "parent") {
			t.Errorf("error %q does not mention the parent directory", err)
		}
	})

	t.Run("accepts a sibling directory next to the main checkout", func(t *testing.T) {
		got, err := WorktreePathForPRD(repo, "../{repo}-worktrees/{prd}", "auth", "chief/auth")
		if err != nil {
			t.Fatalf("WorktreePathForPRD() error = %v", err)
		}
		want := filepath.Join(string(filepath.Separator), "home", "user", "project-worktrees", "auth")
		if got != want {
			t.Errorf("WorktreePathForPRD() = %q, want %q", got, want)
		}
	})
}
