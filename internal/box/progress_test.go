package box

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// The run writes to the worktree's copy of the PRD, so that is the one the
// probe has to find — not the stale one in the checkout.
func TestPRDProbeReadsTheNewestCopy(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	repo := filepath.Join(dir, "project")
	wt := filepath.Join(dir, "elsewhere", "wt")
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git("init", "-q")
	git("commit", "-q", "--allow-empty", "-m", "init")
	git("worktree", "add", "-q", "-b", "run", wt)

	name := "it's mine"
	write(filepath.Join(repo, ".chief", "prds", name, "prd.md"), "old")
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(repo, ".chief", "prds", name, "prd.md"), past, past); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(wt, ".chief", "prds", name, "prd.md"), "new")

	out, err := exec.CommandContext(context.Background(), "sh", "-c", prdProbe(repo, name)).CombinedOutput()
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, out)
	}
	if string(out) != "new" {
		t.Errorf("probe read %q, want the worktree's copy", out)
	}
}

func TestRoundMinutes(t *testing.T) {
	for d, want := range map[time.Duration]string{
		20 * time.Second:             "<1m",
		35 * time.Minute:             "35m",
		2 * time.Hour:                "2h",
		2*time.Hour + 10*time.Minute: "2h10m",
	} {
		if got := roundMinutes(d); got != want {
			t.Errorf("roundMinutes(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestUnitRunning(t *testing.T) {
	for in, want := range map[string]bool{
		"activating\nsuccess": true, // a one-shot run, as Status asks for it
		"active":              true,
		"activating":          true,
		"inactive\nsuccess":   false,
		"failed\nexit-code":   false,
		"":                    false,
	} {
		if got := unitRunning(in); got != want {
			t.Errorf("unitRunning(%q) = %v, want %v", in, got, want)
		}
	}
}
