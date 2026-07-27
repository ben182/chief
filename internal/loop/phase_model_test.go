package loop

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ben182/chief/internal/prd"
)

// phaseModelPRD is a PRD with one open story, so a run builds it, reviews it and
// then — with no stories left — consolidates.
const phaseModelPRD = `# PRD: Test Project

A project for phase-model tests.

## User Stories

### US-001: Test Story
As a developer, I need it.

**Priority:** 1

- [ ] It works
`

// runPhases drives one full run to completion, draining its events, so the
// recorded invocations cover every phase the loop went through.
func runPhases(t *testing.T, l *Loop) {
	t.Helper()

	drained := make(chan struct{})
	go func() {
		for range l.Events() {
		}
		close(drained)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := l.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	<-drained
}

// newPhaseModelLoop sets up a git repo, a one-story PRD and a mock agent that
// commits the story and signals done, and returns the loop plus the log of models
// its invocations ran on.
func newPhaseModelLoop(t *testing.T) (*Loop, *modelLog) {
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

	// Mock agent: implement + commit the story (idempotent, so the review and
	// consolidation calls re-commit nothing), then signal done.
	scriptPath := filepath.Join(dir, "mock-claude")
	script := "#!/bin/bash\n" +
		"echo content > " + filepath.Join(dir, "impl.txt") + "\n" +
		"git -C " + dir + " add impl.txt >/dev/null 2>&1\n" +
		"git -C " + dir + " commit -m 'feat: myprd/US-001 - Test Story' >/dev/null 2>&1\n" +
		`echo '{"type":"assistant","message":{"content":[{"type":"text","text":"done <chief-done/>"}]}}'` + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	models := &modelLog{}
	l := NewLoopWithWorkDir(prdPath, dir, "", 10, &mockProvider{cliPath: scriptPath, models: models})
	l.buildPrompt = promptBuilderForPRD(prdPath, false)
	l.DisableRetry()
	return l, models
}

// TestPhaseModels_DefaultToSonnet pins the cost fix down: with nothing configured,
// the review and consolidation agents run on Sonnet while the build agent keeps
// running on whatever the provider was set up with (empty = the CLI's own choice).
func TestPhaseModels_DefaultToSonnet(t *testing.T) {
	l, models := newPhaseModelLoop(t)
	l.SetReview(true, "", "check the implementation carefully")
	l.SetConsolidate(true, "", "")

	runPhases(t, l)

	got := models.all()
	want := []string{"", "sonnet", "sonnet"} // build, review, consolidation
	assertModels(t, got, want)
}

// TestPhaseModels_ConfiguredModelWins verifies an explicitly configured model is
// used for its phase, and only for its phase.
func TestPhaseModels_ConfiguredModelWins(t *testing.T) {
	l, models := newPhaseModelLoop(t)
	l.SetReview(true, "", "check the implementation carefully")
	l.SetReviewModel("haiku")
	l.SetConsolidate(true, "", "")
	l.SetConsolidateModel("opus")

	runPhases(t, l)

	assertModels(t, models.all(), []string{"", "haiku", "opus"})
}

// TestPhaseModels_BuildModelUntouched verifies the build agent never gets a model
// of chief's choosing: a run without review or consolidation invokes the provider
// exactly as before, so an empty agent.model still means "no --model at all".
func TestPhaseModels_BuildModelUntouched(t *testing.T) {
	l, models := newPhaseModelLoop(t)
	// No review, no consolidation.

	runPhases(t, l)

	assertModels(t, models.all(), []string{""})
}

// TestPhaseModels_ProviderWithoutModelSupport verifies the phase models are inert
// for a provider whose CLI takes no model (codex, opencode, cursor, gemini): the
// review and consolidation agents still run, on the provider's own invocation, and
// nothing errors out.
func TestPhaseModels_ProviderWithoutModelSupport(t *testing.T) {
	l, models := newPhaseModelLoop(t)
	// Swap the recording provider for one that cannot switch models but shares the
	// same log, so the invocations are still observable.
	base, ok := l.provider.(*mockProvider)
	if !ok {
		t.Fatalf("expected a *mockProvider, got %T", l.provider)
	}
	l.provider = &fixedModelProvider{inner: base}
	l.SetReview(true, "", "check the implementation carefully")
	l.SetReviewModel("haiku")
	l.SetConsolidate(true, "", "")

	runPhases(t, l)

	// Three invocations happened (build, review, consolidation) and none of them
	// carried a model.
	assertModels(t, models.all(), []string{"", "", ""})

	p, err := prd.LoadPRD(l.prdPath)
	if err != nil {
		t.Fatalf("reload prd: %v", err)
	}
	if !p.UserStories[0].Passes {
		t.Error("the story must still complete on a provider that takes no model")
	}
}

func assertModels(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %d agent invocations %v, got %d %v", len(want), want, len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("invocation %d ran on model %q, want %q (all: %v)", i+1, got[i], want[i], got)
		}
	}
}

// fixedModelProvider is a Provider whose model cannot be chosen per invocation,
// like every non-Claude provider chief supports. It deliberately does not
// implement ModelSwitcher; everything else is delegated to the mock.
type fixedModelProvider struct{ inner *mockProvider }

func (p *fixedModelProvider) Name() string    { return p.inner.Name() }
func (p *fixedModelProvider) CLIPath() string { return p.inner.CLIPath() }
func (p *fixedModelProvider) LoopCommand(ctx context.Context, prompt, workDir string) *exec.Cmd {
	return p.inner.LoopCommand(ctx, prompt, workDir)
}
func (p *fixedModelProvider) InteractiveCommand(workDir, prompt string) *exec.Cmd {
	return p.inner.InteractiveCommand(workDir, prompt)
}
func (p *fixedModelProvider) SupportsInteractiveQuestions() bool {
	return p.inner.SupportsInteractiveQuestions()
}
func (p *fixedModelProvider) CleanOutput(output string) string { return p.inner.CleanOutput(output) }
func (p *fixedModelProvider) ParseLine(line string) *Event     { return p.inner.ParseLine(line) }
func (p *fixedModelProvider) LogFileName() string              { return p.inner.LogFileName() }
