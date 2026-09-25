package headless

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/loop"
)

func TestLoggerStampsEveryLineAndFoldsContinuations(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(&buf, false)
	l.event("run", "first line\nsecond line")

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), buf.String())
	}
	if !strings.Contains(lines[0], "run") || !strings.HasSuffix(lines[0], "first line") {
		t.Errorf("first line = %q", lines[0])
	}
	// The continuation carries no timestamp and no kind, so it reads as part of
	// the line above rather than as its own event.
	if strings.Contains(lines[1], "run") {
		t.Errorf("continuation repeats the kind: %q", lines[1])
	}
	if !strings.HasSuffix(lines[1], "second line") {
		t.Errorf("continuation = %q", lines[1])
	}
	// Both start with a prefix of the same width, so the text columns line up.
	if a, b := strings.Index(lines[0], "first"), strings.Index(lines[1], "second"); a != b {
		t.Errorf("columns do not align (text starts at %d and %d):\n%q\n%q", a, b, lines[0], lines[1])
	}
}

func TestLoggerDetailIsVerboseOnly(t *testing.T) {
	var quiet, loud bytes.Buffer
	newLogger(&quiet, false).detail("tool", "Read foo.go")
	newLogger(&loud, true).detail("tool", "Read foo.go")

	if quiet.Len() != 0 {
		t.Errorf("a quiet run wrote a detail line: %q", quiet.String())
	}
	if !strings.Contains(loud.String(), "Read foo.go") {
		t.Errorf("a verbose run dropped the detail line: %q", loud.String())
	}
}

func TestLoggerWithoutAWriterIsSilent(t *testing.T) {
	l := newLogger(nil, true)
	l.event("run", "this goes nowhere") // must not panic
	l.detail("tool", "nor this")
}

func TestLoggerSkipsEmptyMessages(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(&buf, true)
	l.event("setup", "   \n  ")
	if buf.Len() != 0 {
		t.Errorf("expected nothing for a blank message, got %q", buf.String())
	}
}

// reportLine runs one event through the reporter and returns what it logged.
func reportLine(t *testing.T, verbose bool, ev loop.Event, story string) string {
	t.Helper()
	var buf bytes.Buffer
	l := newLogger(&buf, verbose)
	report(l, ev, &story)
	return buf.String()
}

func TestReportKeepsTheRunsSkeletonAndDropsTheNoise(t *testing.T) {
	always := []struct {
		name string
		ev   loop.Event
		want string
	}{
		{"story done", loop.Event{Type: loop.EventStoryDone, StoryID: "US-002"}, "US-002 done"},
		{"parked", loop.Event{Type: loop.EventStoryNeedsReview, StoryID: "US-003"}, "parked for human review"},
		{"no commit", loop.Event{Type: loop.EventStoryNoCommit, StoryID: "US-004"}, "committed nothing"},
		{"review", loop.Event{Type: loop.EventReviewStart, StoryID: "US-005"}, "reviewing US-005"},
		{"consolidate", loop.Event{Type: loop.EventConsolidateStart}, "refactoring"},
		{"retry", loop.Event{Type: loop.EventRetrying, RetryCount: 2, RetryMax: 3}, "attempt 2 of 3"},
		{"watchdog", loop.Event{Type: loop.EventWatchdogTimeout}, "went silent"},
		{"complete", loop.Event{Type: loop.EventComplete}, "all stories resolved"},
		{"max iterations", loop.Event{Type: loop.EventMaxIterationsReached}, "max iterations"},
		{"error", loop.Event{Type: loop.EventError, Err: errors.New("boom")}, "boom"},
		{"no git repo", loop.Event{Type: loop.EventNoGitRepo}, "not a git repository"},
	}
	for _, tc := range always {
		t.Run(tc.name, func(t *testing.T) {
			got := reportLine(t, false, tc.ev, "")
			if !strings.Contains(got, tc.want) {
				t.Errorf("a quiet run dropped %s: got %q, want it to mention %q", tc.name, got, tc.want)
			}
		})
	}

	// The events a five-hour run emits tens of thousands of. Keeping them would
	// bury everything above.
	noise := []struct {
		name string
		ev   loop.Event
	}{
		{"assistant text", loop.Event{Type: loop.EventAssistantText, Text: "Let me look at that file"}},
		{"tool call", loop.Event{Type: loop.EventToolStart, Tool: "Read", ToolInput: map[string]any{"file_path": "a.go"}}},
		{"per-iteration cost", loop.Event{Type: loop.EventResult, Cost: 0.12}},
	}
	for _, tc := range noise {
		t.Run(tc.name+" is verbose-only", func(t *testing.T) {
			if got := reportLine(t, false, tc.ev, ""); got != "" {
				t.Errorf("a quiet run kept %s: %q", tc.name, got)
			}
			if got := reportLine(t, true, tc.ev, ""); got == "" {
				t.Errorf("a verbose run dropped %s", tc.name)
			}
		})
	}
}

func TestReportAnnouncesAStoryOnlyWhenItChanges(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(&buf, false)
	story := ""

	// Three iterations, the first two on one story and the third on the next.
	report(l, loop.Event{Type: loop.EventIterationStart, StoryID: "US-001", Iteration: 1}, &story)
	report(l, loop.Event{Type: loop.EventIterationStart, StoryID: "US-001", Iteration: 2}, &story)
	report(l, loop.Event{Type: loop.EventIterationStart, StoryID: "US-002", Iteration: 3}, &story)

	if got := strings.Count(buf.String(), "US-001 started"); got != 1 {
		t.Errorf("US-001 announced %d times, want 1:\n%s", got, buf.String())
	}
	if !strings.Contains(buf.String(), "US-002 started") {
		t.Errorf("the next story was never announced:\n%s", buf.String())
	}
	if story != "US-002" {
		t.Errorf("current story = %q, want US-002", story)
	}
}

func TestReportFallsBackToTheCurrentStory(t *testing.T) {
	// The loop does not always name the story on the event that ends it.
	got := reportLine(t, false, loop.Event{Type: loop.EventStoryDone}, "US-007")
	if !strings.Contains(got, "US-007 done") {
		t.Errorf("got %q, want it to name the story the run was on", got)
	}
	// With nothing to fall back on it must still read as a sentence.
	got = reportLine(t, false, loop.Event{Type: loop.EventStoryDone}, "")
	if !strings.Contains(got, "story done") {
		t.Errorf("got %q, want a readable fallback", got)
	}
}

func TestReportSaysWhenARateLimitPauseEnds(t *testing.T) {
	resets := time.Now().Add(90 * time.Minute)
	got := reportLine(t, false, loop.Event{
		Type:      loop.EventRateLimitWait,
		RateLimit: &loop.RateLimitInfo{Status: "rejected", ResetsAt: resets},
	}, "")
	if !strings.Contains(got, resets.Format("15:04")) {
		t.Errorf("got %q, want the time the window resets", got)
	}

	// Without a reported reset time it still has to say why nothing is moving.
	got = reportLine(t, false, loop.Event{Type: loop.EventRateLimitWait}, "")
	if !strings.Contains(got, "usage limit") {
		t.Errorf("got %q, want it to name the usage limit", got)
	}
}

func TestToolLineNamesWhatTheToolWasPointedAt(t *testing.T) {
	cases := []struct {
		in   map[string]any
		want string
	}{
		{map[string]any{"file_path": "src/Auth.php"}, "Read src/Auth.php"},
		{map[string]any{"command": "composer install"}, "Read composer install"},
		{map[string]any{"irrelevant": "x"}, "Read"},
		{nil, "Read"},
	}
	for _, tc := range cases {
		if got := toolLine(loop.Event{Tool: "Read", ToolInput: tc.in}); got != tc.want {
			t.Errorf("toolLine(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCollapseFoldsAndTruncates(t *testing.T) {
	if got := collapse("  a\n\n  b   c  "); got != "a b c" {
		t.Errorf("collapse = %q, want %q", got, "a b c")
	}
	long := strings.Repeat("x", maxDetailLine+50)
	got := collapse(long)
	if len([]rune(got)) != maxDetailLine+1 { // the ellipsis is one rune
		t.Errorf("collapse kept %d runes, want %d plus an ellipsis", len([]rune(got)), maxDetailLine)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a truncated value does not say so: %q", got)
	}
}

func TestProjectRootForFindsTheProjectAboveChief(t *testing.T) {
	root := filepath.Join("home", "ben", "app")
	prdPath := filepath.Join(root, ".chief", "prds", "auth", "prd.md")
	if got := projectRootFor(prdPath); got != root {
		t.Errorf("projectRootFor = %q, want %q", got, root)
	}

	// The legacy layout, where the PRD sits directly in .chief.
	if got := projectRootFor(filepath.Join(root, ".chief", "prd.md")); got != root {
		t.Errorf("legacy layout: projectRootFor = %q, want %q", got, root)
	}

	// A PRD kept outside any project has only its own directory to fall back on.
	loose := filepath.Join("tmp", "scratch", "prd.md")
	if got := projectRootFor(loose); got != filepath.Join("tmp", "scratch") {
		t.Errorf("loose PRD: projectRootFor = %q", got)
	}
}

func TestPRDNameFromIsTheDirectoryThatHoldsIt(t *testing.T) {
	if got := prdNameFrom(filepath.Join("p", ".chief", "prds", "billing", "prd.md")); got != "billing" {
		t.Errorf("prdNameFrom = %q, want billing", got)
	}
}

func TestSummaryDirFollowsTheRunIntoItsWorktree(t *testing.T) {
	base := filepath.Join("repo")
	prdPath := filepath.Join(base, ".chief", "prds", "auth", "prd.md")

	// A run in the project itself writes the summary beside the PRD.
	if got := summaryDir(base, prdPath, base); got != filepath.Join(base, ".chief", "prds", "auth") {
		t.Errorf("project run: summaryDir = %q", got)
	}

	// A worktree run writes it into the worktree's own copy, which is the one
	// its branch carries.
	wt := filepath.Join("elsewhere", "wt")
	if got := summaryDir(base, prdPath, wt); got != filepath.Join(wt, ".chief", "prds", "auth") {
		t.Errorf("worktree run: summaryDir = %q", got)
	}
}

func TestLivePRDPathPrefersTheCopyTheRunWroteTo(t *testing.T) {
	base := filepath.Join("repo")
	prdPath := filepath.Join(base, ".chief", "prds", "auth", "prd.md")

	if got := livePRDPath(base, prdPath, ""); got != prdPath {
		t.Errorf("without a worktree: %q, want the project's copy", got)
	}
	wt := filepath.Join("elsewhere", "wt")
	want := filepath.Join(wt, ".chief", "prds", "auth", "prd.md")
	if got := livePRDPath(base, prdPath, wt); got != want {
		t.Errorf("with a worktree: %q, want %q", got, want)
	}
}

func TestVerdictNamesHowTheRunEnded(t *testing.T) {
	cases := map[loop.LoopState]string{
		loop.LoopStateComplete: "complete",
		loop.LoopStateError:    "failed",
		loop.LoopStateStopped:  "stopped",
		loop.LoopStatePaused:   "ended",
	}
	for state, want := range cases {
		if got := verdict(state); got != want {
			t.Errorf("verdict(%v) = %q, want %q", state, got, want)
		}
	}
}

func TestInterruptStopsTheRunAndSkipsPostCompletionActions(t *testing.T) {
	dir, prdPath := project(t, "US-001", "Test Story")
	runGit(t, dir, "checkout", "-b", "work")

	// An agent that commits and then keeps running: the run is interrupted while
	// it works, so there is committed work that must not be pushed.
	script := filepath.Join(t.TempDir(), "mock-agent")
	body := "#!/bin/bash\n" +
		"echo impl >> impl.txt\n" +
		"git add impl.txt >/dev/null 2>&1\n" +
		"git commit -m 'feat: demo/US-001 - Test Story' >/dev/null 2>&1\n" +
		`echo '{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}'` + "\n" +
		"sleep 60\n"
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{}
	cfg.OnComplete.Push = true // would fail loudly if it were ever attempted

	var log bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(2 * time.Second)
		cancel()
	}()

	res, err := Run(ctx, Options{
		PRDPath:  prdPath,
		BaseDir:  dir,
		Provider: &testProvider{script: script},
		Config:   cfg,
		Out:      &log,
	})
	if err != nil {
		t.Fatalf("an interrupted run must report what it got done, not fail: %v", err)
	}
	if res.Completed {
		t.Error("an interrupted run is not a completed one")
	}
	if len(res.Actions) != 0 {
		t.Errorf("post-completion actions ran on an interrupted run: %v", res.Actions)
	}
	if !strings.Contains(log.String(), "interrupted") {
		t.Errorf("the log does not say the run was interrupted:\n%s", log.String())
	}
	// The work the agent did commit is still on the branch.
	if out := runGit(t, dir, "log", "--oneline"); !strings.Contains(out, "US-001") {
		t.Errorf("the interrupted run lost its commit:\n%s", out)
	}
}

// On a box an interruption is the deadline, and the machine is destroyed right
// after it. The log has to be committed on the way out, or the reaper's rescue
// push leaves without it and the journal dies with the box.
func TestAnInterruptedRunStillCommitsItsLog(t *testing.T) {
	dir, prdPath := project(t, "US-001", "Test Story")
	runGit(t, dir, "checkout", "-b", "work")

	script := filepath.Join(t.TempDir(), "mock-agent")
	body := "#!/bin/bash\n" +
		"echo impl >> impl.txt\n" +
		"git add impl.txt >/dev/null 2>&1\n" +
		"git commit -m 'feat: demo/US-001 - Test Story' >/dev/null 2>&1\n" +
		`echo '{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}'` + "\n" +
		"sleep 60\n"
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(2 * time.Second)
		cancel()
	}()
	var log bytes.Buffer
	if _, err := Run(ctx, Options{
		PRDPath:     prdPath,
		BaseDir:     dir,
		Provider:    &testProvider{script: script},
		Config:      &config.Config{},
		Out:         &log,
		LogToBranch: true,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	files := runGit(t, dir, "show", "--name-only", "--format=%s", "HEAD")
	if !strings.HasPrefix(files, "docs: add run log") || !strings.Contains(files, ".chief/prds/demo/run-") {
		t.Fatalf("the interrupted run did not commit its log; HEAD is:\n%s\nlog:\n%s", files, log.String())
	}
	name := strings.TrimSpace(files[strings.LastIndex(files, "\n")+1:])
	committed := runGit(t, dir, "show", "HEAD:"+name)
	if !strings.Contains(committed, "interrupted") {
		t.Errorf("the committed log does not say the run was interrupted:\n%s", committed)
	}
}

// chief box status reads the running total off these lines, so their shape is
// what it parses: "cost", then "$<amount> so far".
func TestReportSpentOnlyWhenAStoryEnds(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(&buf, false)
	reportSpent(l, loop.Event{Type: loop.EventUsage, Cost: 0.5}, 0.5)
	if buf.Len() != 0 {
		t.Fatalf("a usage event logged a total: %q", buf.String())
	}
	reportSpent(l, loop.Event{Type: loop.EventStoryDone}, 1.234)
	reportSpent(l, loop.Event{Type: loop.EventStoryNeedsReview}, 2.5)
	out := buf.String()
	for _, want := range []string{"cost       $1.23 so far", "cost       $2.50 so far"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
