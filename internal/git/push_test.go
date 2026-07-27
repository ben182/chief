package git

import (
	"os/exec"
	"testing"

	"github.com/ben182/chief/internal/prd"
)

func TestCheckGHCLI(t *testing.T) {
	t.Run("returns result without error", func(t *testing.T) {
		installed, _, err := CheckGHCLI()
		if err != nil {
			t.Fatalf("CheckGHCLI() error = %v", err)
		}
		// We can't guarantee gh is installed in CI, but we can verify the function runs
		_ = installed
	})
}

func TestPushBranch(t *testing.T) {
	t.Run("fails on repo without remote", func(t *testing.T) {
		dir := initTestRepo(t)
		err := PushBranch(dir, "main")
		if err == nil {
			t.Error("PushBranch() expected error for repo without remote, got nil")
		}
	})
}

func TestPRCreateArgs(t *testing.T) {
	t.Run("targets the base branch it was given", func(t *testing.T) {
		got := prCreateArgs("chief/auth", "develop", "feat: auth", "body")
		want := []string{"pr", "create", "--head", "chief/auth", "--base", "develop", "--title", "feat: auth", "--body", "body"}
		if !equalArgs(got, want) {
			t.Errorf("prCreateArgs() = %v, want %v", got, want)
		}
	})

	t.Run("omits --base entirely when there is none", func(t *testing.T) {
		got := prCreateArgs("chief/auth", "", "feat: auth", "body")
		want := []string{"pr", "create", "--head", "chief/auth", "--title", "feat: auth", "--body", "body"}
		if !equalArgs(got, want) {
			t.Errorf("prCreateArgs() = %v, want %v", got, want)
		}
	})
}

func TestFindOpenPR(t *testing.T) {
	// Without a GitHub remote (or without gh at all) the question can't be
	// answered — that has to surface as an error, not as "no PR exists", so
	// EnsurePR doesn't silently skip an existing one.
	t.Run("errors on a repo gh cannot resolve", func(t *testing.T) {
		dir := initTestRepo(t)
		pr, err := FindOpenPR(dir, "chief/auth")
		if err == nil {
			t.Error("FindOpenPR() expected an error for a repo without a GitHub remote, got nil")
		}
		if pr.URL != "" {
			t.Errorf("FindOpenPR() URL = %q, want empty on error", pr.URL)
		}
	})
}

func TestEnsurePR(t *testing.T) {
	t.Run("reports the create failure on a repo without a GitHub remote", func(t *testing.T) {
		dir := initTestRepo(t)
		if _, err := EnsurePR(dir, "chief/auth", "title", "body", ""); err == nil {
			t.Error("EnsurePR() expected an error for a repo without a GitHub remote, got nil")
		}
	})
}

func equalArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDeleteBranch(t *testing.T) {
	t.Run("deletes existing branch", func(t *testing.T) {
		dir := initTestRepo(t)

		// Create a branch
		cmd := exec.Command("git", "branch", "feature-branch")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git branch failed: %s", string(out))
		}

		// Verify it exists
		exists, err := BranchExists(dir, "feature-branch")
		if err != nil {
			t.Fatalf("BranchExists() error = %v", err)
		}
		if !exists {
			t.Fatal("branch should exist before deletion")
		}

		// Delete the branch
		err = DeleteBranch(dir, "feature-branch")
		if err != nil {
			t.Fatalf("DeleteBranch() error = %v", err)
		}

		// Verify it's gone
		exists, err = BranchExists(dir, "feature-branch")
		if err != nil {
			t.Fatalf("BranchExists() error = %v", err)
		}
		if exists {
			t.Error("branch still exists after deletion")
		}
	})

	t.Run("fails for non-existent branch", func(t *testing.T) {
		dir := initTestRepo(t)
		err := DeleteBranch(dir, "nonexistent-branch")
		if err == nil {
			t.Error("DeleteBranch() expected error for non-existent branch, got nil")
		}
	})
}

func TestPRTitleFromPRD(t *testing.T) {
	p := &prd.PRD{
		Project: "Git Worktree Support",
	}
	got := PRTitleFromPRD("worktrees", p)
	want := "feat(worktrees): Git Worktree Support"
	if got != want {
		t.Errorf("PRTitleFromPRD() = %q, want %q", got, want)
	}
}

func TestPRBodyFromPRD(t *testing.T) {
	t.Run("includes summary and completed stories", func(t *testing.T) {
		p := &prd.PRD{
			Project:     "Test Project",
			Description: "This is a test project description.",
			UserStories: []prd.UserStory{
				{ID: "US-001", Title: "Config System", Passes: true},
				{ID: "US-002", Title: "Git Worktree Primitives", Passes: true},
				{ID: "US-003", Title: "Incomplete Story", Passes: false},
			},
		}

		body := PRBodyFromPRD(p)

		// Check summary section
		if got := body; got == "" {
			t.Fatal("PRBodyFromPRD() returned empty string")
		}
		if !contains(body, "## Summary") {
			t.Error("body missing ## Summary header")
		}
		if !contains(body, "This is a test project description.") {
			t.Error("body missing project description")
		}

		// Check changes section
		if !contains(body, "## Changes") {
			t.Error("body missing ## Changes header")
		}
		if !contains(body, "US-001: Config System") {
			t.Error("body missing completed story US-001")
		}
		if !contains(body, "US-002: Git Worktree Primitives") {
			t.Error("body missing completed story US-002")
		}

		// Incomplete stories should not be listed
		if contains(body, "Incomplete Story") {
			t.Error("body should not include incomplete stories")
		}
	})

	t.Run("empty stories produces changes header only", func(t *testing.T) {
		p := &prd.PRD{
			Project:     "Empty Project",
			Description: "No stories yet.",
			UserStories: []prd.UserStory{},
		}

		body := PRBodyFromPRD(p)
		if !contains(body, "## Summary") {
			t.Error("body missing ## Summary header")
		}
		if !contains(body, "## Changes") {
			t.Error("body missing ## Changes header")
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
