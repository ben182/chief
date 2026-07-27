package loop

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ben182/chief/internal/prd"
)

// promptLog records the prompt of every agent invocation, so a test can observe
// what chief actually hands the agent.
type promptLog struct {
	mu      sync.Mutex
	prompts []string
}

func (l *promptLog) record(prompt string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prompts = append(l.prompts, prompt)
}

func (l *promptLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.prompts...)
}

// newProgressPromptLoop builds the same one-story run as newPhaseModelLoop, but
// records the prompts instead of the models and seeds progress.md with the given
// content first (empty content = the file does not exist yet).
func newProgressPromptLoop(t *testing.T, progressContent string) (*Loop, *promptLog) {
	t.Helper()

	dir := t.TempDir()
	gitInit(t, dir)

	prdDir := filepath.Join(dir, ".chief", "prds", "myprd")
	if err := os.MkdirAll(prdDir, 0o755); err != nil {
		t.Fatalf("mkdir prd dir: %v", err)
	}
	prdPath := filepath.Join(prdDir, "prd.md")
	if err := os.WriteFile(prdPath, []byte(phaseModelPRD), 0o644); err != nil {
		t.Fatalf("write prd: %v", err)
	}
	if progressContent != "" {
		if err := os.WriteFile(prd.ProgressPath(prdPath), []byte(progressContent), 0o644); err != nil {
			t.Fatalf("write progress: %v", err)
		}
	}

	scriptPath := filepath.Join(dir, "mock-claude")
	script := "#!/bin/bash\n" +
		"echo content > " + filepath.Join(dir, "impl.txt") + "\n" +
		"git -C " + dir + " add impl.txt >/dev/null 2>&1\n" +
		"git -C " + dir + " commit -m 'feat: myprd/US-001 - Test Story' >/dev/null 2>&1\n" +
		`echo '{"type":"assistant","message":{"content":[{"type":"text","text":"done <chief-done/>"}]}}'` + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	prompts := &promptLog{}
	l := NewLoopWithWorkDir(prdPath, dir, "", 10, &mockProvider{cliPath: scriptPath, prompts: prompts})
	l.buildPrompt = promptBuilderForPRD(prdPath, false)
	l.DisableRetry()
	return l, prompts
}

// oldProgress is a progress.md as it looks after a few stories: a patterns
// section plus story reports whose bodies chief must never carry into a prompt.
const oldProgress = `# Progress: myprd

## Codebase Patterns
- PATTERN-MARKER: config lives in internal/config

## 2026-07-01 10:00 - US-000
- ENTRY-MARKER: implemented the thing
---
`

// TestBuildPrompt_DoesNotInlineProgress is the guard for chief's side of the
// progress.md diet: chief passes the *path* and nothing else. Inlining any of the
// file would defeat the point of telling the agent to read it selectively.
func TestBuildPrompt_DoesNotInlineProgress(t *testing.T) {
	l, prompts := newProgressPromptLoop(t, oldProgress)

	runPhases(t, l)

	got := prompts.all()
	if len(got) == 0 {
		t.Fatal("expected at least the build invocation to be recorded")
	}
	build := got[0]

	for _, marker := range []string{"PATTERN-MARKER", "ENTRY-MARKER"} {
		if strings.Contains(build, marker) {
			t.Errorf("build prompt inlines progress.md content (%s); it must only pass the path", marker)
		}
	}
	if !strings.Contains(build, prd.ProgressPath(l.prdPath)) {
		t.Error("build prompt must name the progress.md path")
	}
}

// TestBuildPrompt_NoProgressFileYet verifies the first story of a fresh project is
// unaffected: the read instruction is conditional, so a missing progress.md
// neither errors nor changes the prompt chief builds.
func TestBuildPrompt_NoProgressFileYet(t *testing.T) {
	l, prompts := newProgressPromptLoop(t, "")

	if _, err := os.Stat(prd.ProgressPath(l.prdPath)); !os.IsNotExist(err) {
		t.Fatalf("expected no progress.md before the run, got err=%v", err)
	}

	runPhases(t, l)

	got := prompts.all()
	if len(got) == 0 {
		t.Fatal("expected the build invocation to be recorded")
	}
	if !strings.Contains(got[0], prd.ProgressPath(l.prdPath)) {
		t.Error("build prompt must name the progress.md path even when the file is absent")
	}

	p, err := prd.LoadPRD(l.prdPath)
	if err != nil {
		t.Fatalf("reload prd: %v", err)
	}
	if !p.UserStories[0].Passes {
		t.Error("the story must complete when there is no progress.md yet")
	}
}
