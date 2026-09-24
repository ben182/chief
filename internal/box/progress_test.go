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

func TestEstimateLine(t *testing.T) {
	// Setup takes 10 minutes and must not count; two stories finish 20 and 40
	// minutes into the first one; the third has been going for 5 minutes.
	journal := strings.Join([]string{
		"4500",
		"1000.123 chief[1]: 2026-09-24 10:00:00  story      US-1 started (iteration 1)",
		"2200.5 chief[1]: 2026-09-24 10:20:00  story      US-1 done",
		"2201 chief[1]: 2026-09-24 10:20:01  story      US-2 started (iteration 2)",
		"3400 chief[1]: 2026-09-24 10:40:00  story      US-2 parked for human review after too many failed attempts",
		"4200 chief[1]: 2026-09-24 10:45:00  story      US-3 started (iteration 3)",
	}, "\n")
	line, ok := estimateLine(journal, 3)
	// 20m per story × 3 left − 18m20s since the last one ended ≈ 42m.
	if !ok || line != "about 42m left (about 20m per story, 3 stories to go)" {
		t.Errorf("got %q, %v", line, ok)
	}

	line, ok = estimateLine("1600\n1000 x: s  story      US-1 started (iteration 1)", 2)
	if !ok || line != "no estimate yet — the first story has been running for 10m" {
		t.Errorf("before the first story ends: %q, %v", line, ok)
	}

	line, _ = estimateLine("99999\n1000 x: s  story      US-1 started (iteration 1)\n2200 x: s  story      US-1 done", 1)
	if !strings.HasPrefix(line, "should be done any minute") {
		t.Errorf("overdue: %q", line)
	}

	if _, ok := estimateLine("1600", 2); ok {
		t.Error("a run with no story events yet has nothing to say")
	}
	if _, ok := estimateLine(journal, 0); ok {
		t.Error("nothing left means no estimate")
	}
}

func TestRoundMinutes(t *testing.T) {
	for d, want := range map[time.Duration]string{
		20 * time.Second:             "under a minute",
		35 * time.Minute:             "35m",
		2 * time.Hour:                "2h",
		2*time.Hour + 10*time.Minute: "2h10m",
	} {
		if got := roundMinutes(d); got != want {
			t.Errorf("roundMinutes(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestSpentLine(t *testing.T) {
	journal := strings.Join([]string{
		"4500",
		"2200 chief[1]: 2026-09-24 10:20:00  cost       $1.23 so far",
		"3400 chief[1]: 2026-09-24 10:40:00  cost       $2.50 so far",
	}, "\n")
	if got := spentLine(0.04, true, journal); got != "cost so far: machine 4 cents, agent $2.50" {
		t.Errorf("running: %q", got)
	}
	ended := journal + "\n5000 chief[1]: x  run        demo after 3h · 6/6 stories, $4.56"
	if got := spentLine(0.04, true, ended); got != "cost so far: machine 4 cents, agent $4.56" {
		t.Errorf("ended: %q", got)
	}
	// A box started by an older chief logs no running total.
	if got := spentLine(0.04, true, "4500"); got != "cost so far: machine 4 cents" {
		t.Errorf("no agent total: %q", got)
	}
	if got := spentLine(0, false, ""); got != "cost so far: machine cost unknown" {
		t.Errorf("unpriced: %q", got)
	}
}
