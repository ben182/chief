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

func TestCommitLogForStories(t *testing.T) {
	dir := initTestRepo(t)
	if err := CreateBranch(dir, "chief/feature"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	commitFile(t, dir, "a.txt", "a", "feat: US-001 - add a")
	commitFile(t, dir, "b.txt", "b", "feat: US-002 - add b")

	stories := []StoryRef{
		{ID: "US-001", Title: "add a"},
		{ID: "US-002", Title: "add b"},
	}
	log, err := CommitLogForStories(dir, stories)
	if err != nil {
		t.Fatalf("CommitLogForStories: %v", err)
	}

	lines := strings.Split(log, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 commits, got %d: %q", len(lines), log)
	}
	if !strings.Contains(lines[0], "feat: US-001 - add a") {
		t.Errorf("first line = %q, want US-001 first", lines[0])
	}
	if !strings.Contains(lines[1], "feat: US-002 - add b") {
		t.Errorf("second line = %q, want US-002 second", lines[1])
	}

	// Order follows the passed slice, not commit date: passing the newer commit
	// (US-002) first must put it first, even though it is the more recent commit.
	rev, err := CommitLogForStories(dir, []StoryRef{{ID: "US-002", Title: "add b"}, {ID: "US-001", Title: "add a"}})
	if err != nil {
		t.Fatalf("CommitLogForStories (reversed): %v", err)
	}
	if first := strings.Split(rev, "\n")[0]; !strings.Contains(first, "feat: US-002 - add b") {
		t.Errorf("reversed first line = %q, want US-002 first (order must follow the slice)", first)
	}
}

// TestCommitLogForStories_ExcludesUnrelatedWork is the crux: a branch may carry
// commits from other PRDs (even reusing the same story IDs) plus unrelated
// churn. Only commits whose "feat: <ID> - <Title>" matches a story of *this*
// PRD may appear — a same-numbered story with a different title must not leak in.
func TestCommitLogForStories_ExcludesUnrelatedWork(t *testing.T) {
	dir := initTestRepo(t)
	if err := CreateBranch(dir, "chief/feature"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	// Prior work from another PRD that reuses US-001, plus noise.
	commitFile(t, dir, "other.txt", "x", "feat: US-001 - some other feature")
	commitFile(t, dir, "noise.txt", "y", "chore: unrelated cleanup")
	// This PRD's actual work.
	commitFile(t, dir, "mine.txt", "z", "feat: US-001 - my real feature")

	log, err := CommitLogForStories(dir, []StoryRef{{ID: "US-001", Title: "my real feature"}})
	if err != nil {
		t.Fatalf("CommitLogForStories: %v", err)
	}

	if !strings.Contains(log, "my real feature") {
		t.Errorf("log missing this PRD's commit: %q", log)
	}
	if strings.Contains(log, "some other feature") || strings.Contains(log, "unrelated cleanup") {
		t.Errorf("log leaked unrelated commits: %q", log)
	}
	if lines := strings.Split(log, "\n"); len(lines) != 1 {
		t.Errorf("expected exactly 1 commit, got %d: %q", len(lines), log)
	}
}

func TestCommitLogForStories_EmptyWhenNoMatch(t *testing.T) {
	dir := initTestRepo(t)
	if err := CreateBranch(dir, "chief/empty"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	log, err := CommitLogForStories(dir, []StoryRef{{ID: "US-001", Title: "never committed"}})
	if err != nil {
		t.Fatalf("CommitLogForStories: %v", err)
	}
	if log != "" {
		t.Errorf("expected empty log when no story commit matches, got %q", log)
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
	summaryPath := filepath.Join(prdDir, "summary.md")
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
