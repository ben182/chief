package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// commitFile writes a file and commits it on the current branch.
func commitFile(t *testing.T, dir, name, content, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	for _, args := range [][]string{{"add", name}, {"commit", "-m", msg}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, string(out))
		}
	}
}

func TestCommitLog(t *testing.T) {
	dir := initTestRepo(t)
	if err := CreateBranch(dir, "chief/feature"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	commitFile(t, dir, "a.txt", "a", "feat: S1 - add a")
	commitFile(t, dir, "b.txt", "b", "feat: S2 - add b")

	log, err := CommitLog(dir, "chief/feature")
	if err != nil {
		t.Fatalf("CommitLog: %v", err)
	}

	lines := strings.Split(log, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 commits, got %d: %q", len(lines), log)
	}
	// Oldest first.
	if !strings.Contains(lines[0], "feat: S1 - add a") {
		t.Errorf("first line = %q, want S1 first", lines[0])
	}
	if !strings.Contains(lines[1], "feat: S2 - add b") {
		t.Errorf("second line = %q, want S2 second", lines[1])
	}
}

func TestCommitLog_EmptyWhenNoCommits(t *testing.T) {
	dir := initTestRepo(t)
	if err := CreateBranch(dir, "chief/empty"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	log, err := CommitLog(dir, "chief/empty")
	if err != nil {
		t.Fatalf("CommitLog: %v", err)
	}
	if log != "" {
		t.Errorf("expected empty log for a branch with no new commits, got %q", log)
	}
}

func TestCommitPaths_ForceAddsGitignoredFile(t *testing.T) {
	dir := initTestRepo(t)
	// Ignore .chief, then write a summary underneath it.
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".chief/\n"), 0644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	prdDir := filepath.Join(dir, ".chief", "prds", "default")
	if err := os.MkdirAll(prdDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	summaryPath := filepath.Join(prdDir, "SUMMARY.md")
	if err := os.WriteFile(summaryPath, []byte("# Summary\n"), 0644); err != nil {
		t.Fatalf("write summary: %v", err)
	}

	if err := CommitPaths(dir, "docs: add run summary", summaryPath); err != nil {
		t.Fatalf("CommitPaths: %v", err)
	}

	// The gitignored file must now be tracked.
	cmd := exec.Command("git", "ls-files", "--", summaryPath)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	if strings.TrimSpace(string(out)) == "" {
		t.Errorf("summary was not committed despite force-add; ls-files empty")
	}
}
