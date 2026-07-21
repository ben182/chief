package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func headCount(t *testing.T, dir string) int {
	t.Helper()
	cmd := exec.Command("git", "rev-list", "--count", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-list: %v", err)
	}
	n := 0
	for _, r := range strings.TrimSpace(string(out)) {
		n = n*10 + int(r-'0')
	}
	return n
}

func fileInHead(t *testing.T, dir, name string) bool {
	t.Helper()
	cmd := exec.Command("git", "cat-file", "-e", "HEAD:"+name)
	cmd.Dir = dir
	return cmd.Run() == nil
}

func TestHeadSubject(t *testing.T) {
	dir := initTestRepo(t)
	subj, err := HeadSubject(dir)
	if err != nil {
		t.Fatalf("HeadSubject() error = %v", err)
	}
	if subj != "initial commit" {
		t.Errorf("HeadSubject() = %q, want %q", subj, "initial commit")
	}
}

func TestAmendPaths(t *testing.T) {
	t.Run("folds paths into HEAD without adding a commit", func(t *testing.T) {
		dir := initTestRepo(t)
		commitFile(t, dir, "app.go", "package main\n", "feat: US-001 - a story")
		before := headCount(t, dir)

		// Simulate chief's working files appearing after the story commit.
		if err := os.WriteFile(filepath.Join(dir, "prd.md"), []byte("# PRD\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "progress.md"), []byte("# progress\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := AmendPaths(dir, filepath.Join(dir, "prd.md"), filepath.Join(dir, "progress.md")); err != nil {
			t.Fatalf("AmendPaths() error = %v", err)
		}

		if got := headCount(t, dir); got != before {
			t.Errorf("commit count = %d, want %d (amend must not add a commit)", got, before)
		}
		if subj, _ := HeadSubject(dir); subj != "feat: US-001 - a story" {
			t.Errorf("HEAD subject = %q, want unchanged story subject", subj)
		}
		if !fileInHead(t, dir, "prd.md") || !fileInHead(t, dir, "progress.md") {
			t.Error("prd.md/progress.md should be tracked in the amended HEAD commit")
		}
	})

	t.Run("force-adds a path under a gitignored dir", func(t *testing.T) {
		dir := initTestRepo(t)
		if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".chief/\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		commitFile(t, dir, ".gitignore", ".chief/\n", "feat: US-002 - ignore chief")
		chiefDir := filepath.Join(dir, ".chief", "prds", "x")
		if err := os.MkdirAll(chiefDir, 0o755); err != nil {
			t.Fatal(err)
		}
		prdMd := filepath.Join(chiefDir, "prd.md")
		if err := os.WriteFile(prdMd, []byte("# PRD\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := AmendPaths(dir, prdMd); err != nil {
			t.Fatalf("AmendPaths() error = %v", err)
		}
		if !fileInHead(t, dir, ".chief/prds/x/prd.md") {
			t.Error("force-add should track prd.md even though .chief/ is gitignored")
		}
	})

	t.Run("errors with no paths", func(t *testing.T) {
		dir := initTestRepo(t)
		if err := AmendPaths(dir); err == nil {
			t.Error("AmendPaths() with no paths should error")
		}
	})
}

func TestIgnoreLogsIn(t *testing.T) {
	t.Run("creates .gitignore when missing", func(t *testing.T) {
		dir := t.TempDir()
		IgnoreLogsIn(dir)
		data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
		if err != nil {
			t.Fatalf("expected .gitignore to be created: %v", err)
		}
		if !strings.Contains(string(data), "*.log") {
			t.Errorf(".gitignore = %q, want it to contain *.log", string(data))
		}
	})

	t.Run("appends pattern to existing .gitignore", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, ".gitignore")
		if err := os.WriteFile(path, []byte("node_modules\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		IgnoreLogsIn(dir)
		data, _ := os.ReadFile(path)
		if !strings.Contains(string(data), "node_modules") || !strings.Contains(string(data), "*.log") {
			t.Errorf(".gitignore = %q, want both node_modules and *.log", string(data))
		}
	})

	t.Run("is idempotent", func(t *testing.T) {
		dir := t.TempDir()
		IgnoreLogsIn(dir)
		IgnoreLogsIn(dir)
		data, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
		if got := strings.Count(string(data), "*.log"); got != 1 {
			t.Errorf("*.log appears %d times, want exactly 1", got)
		}
	})

	t.Run("actually ignores a timestamped log", func(t *testing.T) {
		dir := initTestRepo(t)
		IgnoreLogsIn(dir)
		logName := "claude-2026-07-21-180640.log"
		if err := os.WriteFile(filepath.Join(dir, logName), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("git", "check-ignore", "-q", logName)
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			t.Errorf("check-ignore: timestamped log %q should be ignored", logName)
		}
	})
}
