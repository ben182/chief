package loop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ben182/chief/internal/prd"
)

// researchMarker is the sentence that opens the research-delegation block. It is
// the block's identity as far as these tests are concerned: present in the build
// prompt of a subagent-capable provider, absent everywhere else.
const researchMarker = "Delegate broad codebase research to a subagent"

// newResearchRepo lays out a git repo with a one-story PRD ("myprd") and a mock
// agent that commits the story and signals done, and returns the repo root, the
// PRD path and the agent script.
func newResearchRepo(t *testing.T) (dir, prdPath, scriptPath string) {
	t.Helper()

	dir = t.TempDir()
	gitInit(t, dir)

	prdDir := filepath.Join(dir, ".chief", "prds", "myprd")
	if err := os.MkdirAll(prdDir, 0o755); err != nil {
		t.Fatalf("mkdir prd dir: %v", err)
	}
	prdPath = filepath.Join(prdDir, "prd.md")
	if err := os.WriteFile(prdPath, []byte(phaseModelPRD), 0o644); err != nil {
		t.Fatalf("write prd: %v", err)
	}

	scriptPath = filepath.Join(dir, "mock-claude")
	script := "#!/bin/bash\n" +
		"echo content > " + filepath.Join(dir, "impl.txt") + "\n" +
		"git -C " + dir + " add impl.txt >/dev/null 2>&1\n" +
		"git -C " + dir + " commit -m 'feat: myprd/US-001 - Test Story' >/dev/null 2>&1\n" +
		`echo '{"type":"assistant","message":{"content":[{"type":"text","text":"done <chief-done/>"}]}}'` + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return dir, prdPath, scriptPath
}

// newResearchPromptLoop builds the same one-story run as newPhaseModelLoop with
// review and consolidation enabled, so a full run records all three phases'
// prompts. native says whether the provider behind it looks like Claude.
func newResearchPromptLoop(t *testing.T, native bool) (*Loop, *promptLog) {
	t.Helper()

	dir, prdPath, scriptPath := newResearchRepo(t)

	prompts := &promptLog{}
	provider := &mockProvider{cliPath: scriptPath, prompts: prompts, native: native}
	l := NewLoopWithWorkDir(prdPath, dir, "", 10, provider)
	l.buildPrompt = promptBuilderForPRD(prdPath, provider.SupportsInteractiveQuestions())
	l.SetReview(true, "", "check the implementation carefully")
	l.SetConsolidate(true, "", "")
	l.DisableRetry()
	return l, prompts
}

// TestBuildPrompt_DelegatesResearchOnClaude verifies the block reaches the build
// agent of a subagent-capable provider — and only the build agent. Review and
// consolidation already work off a diff and must stay as they are.
func TestBuildPrompt_DelegatesResearchOnClaude(t *testing.T) {
	l, prompts := newResearchPromptLoop(t, true)

	runPhases(t, l)

	got := prompts.all()
	if len(got) != 3 {
		t.Fatalf("expected build, review and consolidate invocations, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], researchMarker) {
		t.Error("build prompt must tell the Claude agent to delegate research to a subagent")
	}
	if !strings.Contains(got[0], "Read tool") {
		t.Error("build prompt must point the Claude agent at the Read tool")
	}
	for i, phase := range []string{"review", "consolidate"} {
		if strings.Contains(got[i+1], researchMarker) {
			t.Errorf("%s prompt must not carry the research-delegation block", phase)
		}
	}
}

// TestBuildPrompt_NoResearchBlockForOtherProviders verifies the gating at the loop
// seam: a provider without subagents gets the build prompt it got before, and the
// story still completes.
func TestBuildPrompt_NoResearchBlockForOtherProviders(t *testing.T) {
	l, prompts := newResearchPromptLoop(t, false)

	runPhases(t, l)

	got := prompts.all()
	if len(got) == 0 {
		t.Fatal("expected at least the build invocation to be recorded")
	}
	for i, prompt := range got {
		if strings.Contains(prompt, researchMarker) {
			t.Errorf("invocation %d carries the research-delegation block on a provider without subagents", i+1)
		}
	}

	p, err := prd.LoadPRD(l.prdPath)
	if err != nil {
		t.Fatalf("reload prd: %v", err)
	}
	if !p.UserStories[0].Passes {
		t.Error("the story must still complete on a provider without subagents")
	}
}

// TestManagerGatesResearchBlockOnProvider covers the hop from the configured
// provider to the build prompt. Manager.Start installs its own prompt builder, so
// a provider capability read only in NewLoopWithEmbeddedPrompt would never reach a
// real run — every run started from the TUI goes through the manager.
func TestManagerGatesResearchBlockOnProvider(t *testing.T) {
	buildPromptFor := func(t *testing.T, native bool) string {
		t.Helper()
		dir, prdPath, scriptPath := newResearchRepo(t)

		prompts := &promptLog{}
		m := NewManager(10, &mockProvider{cliPath: scriptPath, prompts: prompts, native: native})
		m.SetBaseDir(dir)
		m.DisableRetry()

		// Let the run finish on its own (the mock agent signals done) instead of
		// stopping it mid-flight, and drain the manager's event channel so a full
		// buffer can never stall it.
		done := make(chan struct{})
		m.SetCompletionCallback(func(string) { close(done) })
		go func() {
			for range m.Events() {
			}
		}()

		if err := m.Register("myprd", prdPath); err != nil {
			t.Fatalf("register failed: %v", err)
		}
		if err := m.Start("myprd"); err != nil {
			t.Fatalf("start failed: %v", err)
		}

		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Fatal("the manager run never completed")
		}

		// Observe what the run actually handed the agent, rather than reaching into
		// the loop for the builder it installed.
		got := prompts.all()
		if len(got) == 0 {
			t.Fatal("the started run never invoked the agent")
		}
		return got[0]
	}

	if got := buildPromptFor(t, true); !strings.Contains(got, researchMarker) {
		t.Error("a manager run on a subagent-capable provider must hand the build agent the research block")
	}
	if got := buildPromptFor(t, false); strings.Contains(got, researchMarker) {
		t.Error("a manager run on a provider without subagents must not carry the research block")
	}
}
