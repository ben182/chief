package box

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ben182/chief/internal/prd"
)

func TestProgressLines(t *testing.T) {
	p := &prd.PRD{UserStories: []prd.UserStory{
		{ID: "US-1", Passes: true},
		{ID: "US-2", Passes: true},
		{ID: "US-3", InProgress: true, Title: "Login"},
		{ID: "US-4", NeedsReview: true},
		{ID: "US-5"},
		{ID: "US-6"},
	}}
	lines := progressLines(p)
	if len(lines) != 2 {
		t.Fatalf("want bar and current story, got %q", lines)
	}
	want := strings.Repeat("█", 10) + strings.Repeat("░", 20) + " 2/6 stories (33%), 1 parked for review"
	if lines[0] != want {
		t.Errorf("bar:\n got %q\nwant %q", lines[0], want)
	}
	if lines[1] != "working on US-3: Login" {
		t.Errorf("current: %q", lines[1])
	}

	for i := range p.UserStories {
		p.UserStories[i] = prd.UserStory{ID: p.UserStories[i].ID, Passes: true}
	}
	lines = progressLines(p)
	if len(lines) != 1 || !strings.HasPrefix(lines[0], strings.Repeat("█", progressWidth)+" 6/6 stories (100%)") {
		t.Errorf("finished run: %q", lines)
	}
}

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
