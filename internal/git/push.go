package git

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ben182/chief/internal/prd"
)

// CheckGHCLI validates that the GitHub CLI is installed and authenticated.
func CheckGHCLI() (installed bool, authenticated bool, err error) {
	// Check if gh is installed
	// A lookup or auth-status failure is the answer this function reports through
	// its bools, so err stays nil for both.
	_, err = exec.LookPath("gh")
	if err != nil {
		return false, false, nil //nolint:nilerr // "not installed" is a result, not an error
	}

	// Check if gh is authenticated
	cmd := exec.Command("gh", "auth", "status")
	if err := cmd.Run(); err != nil {
		return true, false, nil //nolint:nilerr // "not authenticated" is a result, not an error
	}

	return true, true, nil
}

// PushBranch pushes the branch to origin.
func PushBranch(dir, branch string) error {
	return runGitChecked(dir, "failed to push branch", "push", "-u", "origin", branch)
}

// PR describes the pull request a finished run ends up pointing at, whether
// this run opened it or an earlier one did.
type PR struct {
	URL   string
	Title string
	// Base is the branch the PR merges into. Empty when `gh` was left to pick the
	// repository default.
	Base string
	// AlreadyExisted is true when an open PR for the branch was found instead of
	// created, which is the normal case for a followup run on the same branch.
	AlreadyExisted bool
}

// EnsurePR makes sure an open pull request exists for branch and returns it.
//
// A followup run pushes more commits to a branch that already has a PR; opening
// a second one is impossible, so an existing open PR is reported as-is (its
// commits are already updated by the push) rather than treated as an error.
//
// A new PR targets the branch the feature branch was cut from — recorded at
// branch creation, otherwise inferred from history — because a branch cut from
// develop must not be merged into main. baseOverride (from
// onComplete.prBaseBranch) wins over both. A base origin doesn't know is dropped
// rather than passed on, leaving `gh` to fall back to the repository default.
func EnsurePR(dir, branch, title, body, baseOverride string) (PR, error) {
	if existing, err := FindOpenPR(dir, branch); err == nil && existing.URL != "" {
		return existing, nil
	}

	base := strings.TrimSpace(baseOverride)
	if base == "" {
		base = BaseBranchFor(dir, branch)
	}
	if base != "" && !RemoteBranchExists(dir, base) {
		base = ""
	}

	url, err := createPR(dir, branch, base, title, body)
	if err != nil {
		return PR{}, err
	}
	return PR{URL: url, Title: title, Base: base}, nil
}

// FindOpenPR returns the open pull request whose head is branch, or a zero PR
// when there is none. An error means the question couldn't be answered (no `gh`,
// not authenticated, no GitHub remote), which callers treat as "don't know"
// rather than "none".
func FindOpenPR(dir, branch string) (PR, error) {
	cmd := exec.Command("gh", "pr", "list",
		"--head", branch,
		"--state", "open",
		"--limit", "1",
		"--json", "url,title,baseRefName",
	)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return PR{}, fmt.Errorf("failed to list pull requests: %w", err)
	}
	var prs []struct {
		URL         string `json:"url"`
		Title       string `json:"title"`
		BaseRefName string `json:"baseRefName"`
	}
	if err := json.Unmarshal(out, &prs); err != nil {
		return PR{}, fmt.Errorf("failed to parse pull request list: %w", err)
	}
	if len(prs) == 0 {
		return PR{}, nil
	}
	return PR{URL: prs[0].URL, Title: prs[0].Title, Base: prs[0].BaseRefName, AlreadyExisted: true}, nil
}

// createPR runs `gh pr create` and returns the PR URL it prints.
func createPR(dir, branch, base, title, body string) (string, error) {
	cmd := exec.Command("gh", prCreateArgs(branch, base, title, body)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to create PR: %s", strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// prCreateArgs builds the `gh pr create` argument list. An empty base is left
// out entirely so `gh` applies the repository default.
func prCreateArgs(branch, base, title, body string) []string {
	args := []string{"pr", "create", "--head", branch}
	if base != "" {
		args = append(args, "--base", base)
	}
	return append(args, "--title", title, "--body", body)
}

// PRTitleFromPRD generates a conventional-commits title for a PR.
// Format: feat(<prd-name>): <project name>
func PRTitleFromPRD(prdName string, p *prd.PRD) string {
	return fmt.Sprintf("feat(%s): %s", prdName, p.Project)
}

// PRBodyFromPRD generates a PR body with a summary and list of completed stories.
func PRBodyFromPRD(p *prd.PRD) string {
	var b strings.Builder

	b.WriteString("## Summary\n\n")
	b.WriteString(p.Description)
	b.WriteString("\n\n")

	b.WriteString("## Changes\n\n")
	for _, story := range p.Completed() {
		b.WriteString(fmt.Sprintf("- %s: %s\n", story.ID, story.Title))
	}

	return b.String()
}

// DeleteBranch deletes a local branch.
func DeleteBranch(repoDir, branch string) error {
	return runGitChecked(repoDir, "failed to delete branch", "branch", "-D", branch)
}
