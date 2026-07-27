package loop

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// consolidateTestPRD is a two-story PRD used by the consolidation tests. The
// story titles must match the commit subjects the tests create.
const consolidateTestPRD = `# PRD: Test Project

A project for consolidation tests.

## User Stories

### US-001: add a
As a developer, I need a.

**Priority:** 1
**Status:** done

- [x] a exists

### US-002: add b
As a developer, I need b.

**Priority:** 2
**Status:** done

- [x] b exists
`

// writeConsolidatePRD writes consolidateTestPRD to <repo>/.chief/prds/<name>/prd.md
// and returns its path. The PRD's directory name becomes the PRD name, which
// namespaces the story commits the consolidation pass looks for.
func writeConsolidatePRD(t *testing.T, repo, name string) string {
	t.Helper()
	prdDir := filepath.Join(repo, ".chief", "prds", name)
	if err := os.MkdirAll(prdDir, 0o755); err != nil {
		t.Fatalf("mkdir prd dir: %v", err)
	}
	prdPath := filepath.Join(prdDir, "prd.md")
	if err := os.WriteFile(prdPath, []byte(consolidateTestPRD), 0644); err != nil {
		t.Fatalf("write prd: %v", err)
	}
	return prdPath
}

// gitCommitFile commits a single file in dir with the given subject.
func gitCommitFile(t *testing.T, dir, name, content, subject string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	for _, args := range [][]string{{"add", name}, {"commit", "-m", subject}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, string(out))
		}
	}
}

// gitHead returns the current HEAD hash of dir.
func gitHead(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func TestConsolidator_Active(t *testing.T) {
	tests := []struct {
		name string
		c    consolidator
		want bool
	}{
		{"all unset", consolidator{}, false},
		{"explicitly enabled", consolidator{enabled: true}, true},
		// Whether a skill or instructions alone enable the pass is decided upstream
		// in config.ConsolidateConfig.Active(); here enabled is the whole decision,
		// so an off switch can't be undone by a leftover skill.
		{"skill alone does not enable", consolidator{skill: "/code-quality"}, false},
		{"instructions alone do not enable", consolidator{instructions: "watch for dupes"}, false},
		{"disabled beats skill and instructions", consolidator{skill: "/cq", instructions: "watch for dupes"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.active(); got != tt.want {
				t.Errorf("active() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestIterationMode_IsStoryAgent pins down that only the build agent's
// <chief-done/> counts as story completion. If the review or consolidation agent
// ever started reporting as the story agent, their done signal would mark a story
// complete (or emit a spurious second "story done") — the premature-completion bug.
func TestIterationMode_IsStoryAgent(t *testing.T) {
	if !modeBuild.isStoryAgent() {
		t.Error("modeBuild must be the story agent")
	}
	if modeReview.isStoryAgent() {
		t.Error("modeReview must not count as the story agent")
	}
	if modeConsolidate.isStoryAgent() {
		t.Error("modeConsolidate must not count as the story agent")
	}
}

// TestBuildConsolidatePrompt_ScopesToThisRun is the crux of the feature: the
// consolidation pass must only ever see the commits *this* run landed. A followup
// run on a branch that already carries an earlier run's story commits must not
// drag those into the refactor — that work was already reviewed, summarized and
// possibly pushed, and refactoring it would make the commit unreviewable.
func TestBuildConsolidatePrompt_ScopesToThisRun(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	prdPath := writeConsolidatePRD(t, repo, "myprd")

	// An earlier run already landed US-001 on this branch.
	gitCommitFile(t, repo, "a.txt", "a", "feat: myprd/US-001 - add a")

	// This run starts here...
	startRef := gitHead(t, repo)

	// ...and lands US-002.
	gitCommitFile(t, repo, "b.txt", "b", "feat: myprd/US-002 - add b")

	l := NewLoopWithWorkDir(prdPath, repo, "", 1, testProvider)
	l.SetConsolidate(true, "", "")
	l.SetStartRef(startRef)

	prompt, err := l.buildConsolidatePrompt()
	if err != nil {
		t.Fatalf("buildConsolidatePrompt: %v", err)
	}

	if !strings.Contains(prompt, "add b") {
		t.Errorf("prompt must contain this run's commit (US-002 'add b'):\n%s", prompt)
	}
	if strings.Contains(prompt, "add a") {
		t.Errorf("prompt leaked the earlier run's commit (US-001 'add a') — the pass must be scoped to startRef..HEAD:\n%s", prompt)
	}
	// The diff range handed to the agent must be scoped too, not "everything".
	if !strings.Contains(prompt, startRef+"..HEAD") {
		t.Errorf("prompt must offer the scoped diff range %s..HEAD:\n%s", startRef, prompt)
	}
}

// TestBuildConsolidatePrompt_SkipsWithoutStartRef verifies the pass refuses to run
// unscoped. Without a start ref there is no way to tell this run's work from the
// whole branch history, and consolidating the entire branch is far worse than
// consolidating nothing.
func TestBuildConsolidatePrompt_SkipsWithoutStartRef(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	prdPath := writeConsolidatePRD(t, repo, "myprd")
	gitCommitFile(t, repo, "a.txt", "a", "feat: myprd/US-001 - add a")

	l := NewLoopWithWorkDir(prdPath, repo, "", 1, testProvider)
	l.SetConsolidate(true, "", "")
	// No SetStartRef.

	if _, err := l.buildConsolidatePrompt(); err == nil {
		t.Fatal("expected an error (and thus a skip) when there is no start ref to scope to")
	}
}

// TestBuildConsolidatePrompt_SkipsWhenRunLandedNothing verifies a run that
// committed no stories of its own is skipped rather than sending an agent off with
// an empty commit list.
func TestBuildConsolidatePrompt_SkipsWhenRunLandedNothing(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	prdPath := writeConsolidatePRD(t, repo, "myprd")
	gitCommitFile(t, repo, "a.txt", "a", "feat: myprd/US-001 - add a")

	// The run starts *after* the only story commit, so its window is empty.
	l := NewLoopWithWorkDir(prdPath, repo, "", 1, testProvider)
	l.SetConsolidate(true, "", "")
	l.SetStartRef(gitHead(t, repo))

	_, err := l.buildConsolidatePrompt()
	if err == nil {
		t.Fatal("expected an error (and thus a skip) when the run landed no story commits")
	}
	if !strings.Contains(err.Error(), "no story commits") {
		t.Errorf("error should explain the empty run, got: %v", err)
	}
}

// TestBuildConsolidatePrompt_SkipsOutsideGitRepo verifies the pass is skipped when
// commits can't be identified at all.
func TestBuildConsolidatePrompt_SkipsOutsideGitRepo(t *testing.T) {
	dir := t.TempDir()
	prdPath := writeConsolidatePRD(t, dir, "myprd")

	l := NewLoopWithWorkDir(prdPath, dir, "", 1, testProvider)
	l.SetConsolidate(true, "", "")
	l.SetStartRef("deadbeef")

	if _, err := l.buildConsolidatePrompt(); err == nil {
		t.Fatal("expected an error (and thus a skip) outside a git repo")
	}
}

// TestBuildConsolidatePrompt_IncludesSkillAndInstructions verifies the optional
// configuration reaches the prompt, the same way the reviewer's does.
func TestBuildConsolidatePrompt_IncludesSkillAndInstructions(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	prdPath := writeConsolidatePRD(t, repo, "myprd")
	startRef := gitHead(t, repo)
	gitCommitFile(t, repo, "b.txt", "b", "feat: myprd/US-002 - add b")

	l := NewLoopWithWorkDir(prdPath, repo, "", 1, testProvider)
	l.SetConsolidate(true, "/code-quality", "keep all HTTP clients in internal/transport")
	l.SetStartRef(startRef)

	prompt, err := l.buildConsolidatePrompt()
	if err != nil {
		t.Fatalf("buildConsolidatePrompt: %v", err)
	}
	if !strings.Contains(prompt, "/code-quality") {
		t.Error("prompt should carry the configured skill")
	}
	if !strings.Contains(prompt, "internal/transport") {
		t.Error("prompt should carry the configured instructions")
	}
	// The commit subject the agent is told to use must name the PRD.
	if !strings.Contains(prompt, "refactor: consolidate myprd run") {
		t.Errorf("prompt should specify the PRD-named refactor commit subject:\n%s", prompt)
	}
}

// TestRunConsolidation_DisabledIsNoOp verifies a disabled pass emits nothing at
// all, so runs that don't opt in are completely unaffected.
func TestRunConsolidation_DisabledIsNoOp(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	prdPath := writeConsolidatePRD(t, repo, "myprd")

	l := NewLoopWithWorkDir(prdPath, repo, "", 1, testProvider)
	// SetConsolidate is never called: the pass is off.

	l.runConsolidation(context.Background(), 1)

	select {
	case ev := <-l.Events():
		t.Fatalf("disabled consolidation must emit no events, got %v: %q", ev.Type, ev.Text)
	default:
	}
}

// TestRunConsolidation_SkipStillReportsDone verifies that a pass which can't run
// (here: nothing to consolidate) still emits EventConsolidateDone. A start event
// without a matching done would leave the UI showing a phase that never finishes;
// skipping before the start event must still be reported so the reason is visible.
func TestRunConsolidation_SkipStillReportsDone(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	prdPath := writeConsolidatePRD(t, repo, "myprd")
	gitCommitFile(t, repo, "a.txt", "a", "feat: myprd/US-001 - add a")

	l := NewLoopWithWorkDir(prdPath, repo, "", 1, testProvider)
	l.SetConsolidate(true, "", "")
	l.SetStartRef(gitHead(t, repo)) // empty window -> skip

	l.runConsolidation(context.Background(), 3)

	select {
	case ev := <-l.Events():
		if ev.Type != EventConsolidateDone {
			t.Fatalf("expected EventConsolidateDone, got %v", ev.Type)
		}
		if !strings.Contains(ev.Text, "skipped") {
			t.Errorf("skip event should say it was skipped, got %q", ev.Text)
		}
	default:
		t.Fatal("expected a done event so the UI never shows a hanging pass")
	}
}

// TestRunConsolidation_StoppedIsNoOp verifies a run being torn down doesn't kick
// off a fresh agent. Consolidation is the most optional thing chief does; it must
// never delay a stop.
func TestRunConsolidation_StoppedIsNoOp(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	prdPath := writeConsolidatePRD(t, repo, "myprd")
	startRef := gitHead(t, repo)
	gitCommitFile(t, repo, "b.txt", "b", "feat: myprd/US-002 - add b")

	l := NewLoopWithWorkDir(prdPath, repo, "", 1, testProvider)
	l.SetConsolidate(true, "", "")
	l.SetStartRef(startRef)
	l.Stop()

	l.runConsolidation(context.Background(), 1)

	select {
	case ev := <-l.Events():
		// Stop() itself may emit nothing; any consolidation event is a failure here.
		if ev.Type == EventConsolidateStart || ev.Type == EventConsolidateDone {
			t.Fatalf("a stopped loop must not start consolidating, got %v", ev.Type)
		}
	default:
	}
}

// donePRD is a PRD whose only story already passed, so buildPrompt reports "all
// stories complete" on the first iteration and Run takes its completion path
// without ever spawning an agent.
const donePRD = `# PRD: Done Project

Everything is already built.

## User Stories

### US-001: add a
As a developer, I need a.

**Priority:** 1
**Status:** done

- [x] a exists
`

// TestRun_ConsolidatesBeforeCompleting pins down the ordering the feature depends
// on: the consolidation pass must finish *before* EventComplete is emitted. The
// TUI kicks off its post-completion actions on EventComplete — the run summary,
// the push, the PR — so a refactor that landed after it would be described by
// nothing and, worse, could be pushed as an afterthought or missed entirely.
func TestRun_ConsolidatesBeforeCompleting(t *testing.T) {
	dir := t.TempDir()
	prdDir := filepath.Join(dir, ".chief", "prds", "myprd")
	if err := os.MkdirAll(prdDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	prdPath := filepath.Join(prdDir, "prd.md")
	if err := os.WriteFile(prdPath, []byte(donePRD), 0644); err != nil {
		t.Fatalf("write prd: %v", err)
	}

	l := NewLoopWithEmbeddedPrompt(prdPath, 5, testProvider)
	l.workDir = dir // not a git repo: the pass skips, but still reports
	l.SetConsolidate(true, "", "")

	go func() {
		if err := l.Run(context.Background()); err != nil {
			t.Errorf("Run: %v", err)
		}
	}()

	var order []EventType
	for ev := range l.Events() {
		switch ev.Type {
		case EventConsolidateStart, EventConsolidateDone, EventComplete:
			order = append(order, ev.Type)
		}
	}

	// The pass is skipped here (no git repo), so only its done event appears — but
	// it must appear, and it must come first.
	if len(order) < 2 {
		t.Fatalf("expected a consolidation event and a completion event, got %v", order)
	}
	if order[len(order)-1] != EventComplete {
		t.Errorf("EventComplete must come last, got order %v", order)
	}
	if order[0] != EventConsolidateDone && order[0] != EventConsolidateStart {
		t.Errorf("consolidation must be reported before completion, got order %v", order)
	}
}

// TestRun_ConsolidatesWhenMaxIterationsHit verifies a run cut short by the
// iteration cap still consolidates what it did land. Such a run is over and its
// summary/push happen just the same, so skipping the pass would leave exactly the
// seams the feature exists to remove.
func TestRun_ConsolidatesWhenMaxIterationsHit(t *testing.T) {
	dir := t.TempDir()
	prdDir := filepath.Join(dir, ".chief", "prds", "myprd")
	if err := os.MkdirAll(prdDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	prdPath := filepath.Join(prdDir, "prd.md")
	// A story still open, so the run ends on the iteration cap rather than on
	// "all stories complete".
	if err := os.WriteFile(prdPath, []byte(consolidateTestPRD), 0644); err != nil {
		t.Fatalf("write prd: %v", err)
	}

	l := NewLoopWithWorkDir(prdPath, dir, "test", 0, testProvider) // maxIter 0: cap hit immediately
	l.SetConsolidate(true, "", "")

	go func() {
		if err := l.Run(context.Background()); err != nil {
			t.Errorf("Run: %v", err)
		}
	}()

	var sawConsolidate, sawMax bool
	var consolidateFirst bool
	for ev := range l.Events() {
		switch ev.Type {
		case EventConsolidateStart, EventConsolidateDone:
			sawConsolidate = true
			if !sawMax {
				consolidateFirst = true
			}
		case EventMaxIterationsReached:
			sawMax = true
		}
	}

	if !sawMax {
		t.Fatal("expected EventMaxIterationsReached")
	}
	if !sawConsolidate {
		t.Error("a run that hit the iteration cap must still consolidate what it landed")
	}
	if !consolidateFirst {
		t.Error("consolidation must be reported before the max-iterations event")
	}
}

// TestRunConsolidation_CancelledContextIsNoOp verifies a cancelled context skips
// the pass cleanly.
func TestRunConsolidation_CancelledContextIsNoOp(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	prdPath := writeConsolidatePRD(t, repo, "myprd")
	startRef := gitHead(t, repo)
	gitCommitFile(t, repo, "b.txt", "b", "feat: myprd/US-002 - add b")

	l := NewLoopWithWorkDir(prdPath, repo, "", 1, testProvider)
	l.SetConsolidate(true, "", "")
	l.SetStartRef(startRef)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	l.runConsolidation(ctx, 1)

	select {
	case ev := <-l.Events():
		if ev.Type == EventConsolidateStart {
			t.Fatal("a cancelled context must not start consolidating")
		}
	default:
	}
}
