package loop

import (
	"context"
	"os/exec"
	"testing"
)

// skillAwareProvider is a Provider that can switch both its model and its skill
// catalogue, the way the Claude provider does, and remembers which it was asked
// for. Clones share nothing: the point of the test is that the receiver is left
// alone.
type skillAwareProvider struct {
	model     string
	skillsOff bool
}

func (p *skillAwareProvider) WithModel(model string) Provider {
	clone := *p
	clone.model = model
	return &clone
}

func (p *skillAwareProvider) WithSkills(enabled bool) Provider {
	clone := *p
	clone.skillsOff = !enabled
	return &clone
}

func (p *skillAwareProvider) Name() string                             { return "Test" }
func (p *skillAwareProvider) CLIPath() string                          { return "claude" }
func (p *skillAwareProvider) InteractiveCommand(_, _ string) *exec.Cmd { return exec.Command("true") }
func (p *skillAwareProvider) SupportsInteractiveQuestions() bool       { return true }
func (p *skillAwareProvider) CleanOutput(output string) string         { return output }
func (p *skillAwareProvider) ParseLine(line string) *Event             { return ParseLine(line) }
func (p *skillAwareProvider) LogFileName() string                      { return "claude.log" }
func (p *skillAwareProvider) LoopCommand(_ context.Context, _, _ string) *exec.Cmd {
	return exec.Command("true")
}

// modelOnlyProvider can switch models but knows nothing about skills, like a
// provider whose CLI has no equivalent flag. It deliberately does not implement
// SkillSwitcher: it must come back from providerForMode unharmed, with its phase
// model still applied.
type modelOnlyProvider struct{ model string }

func (p *modelOnlyProvider) WithModel(model string) Provider {
	clone := *p
	clone.model = model
	return &clone
}

func (p *modelOnlyProvider) Name() string                             { return "Fixed" }
func (p *modelOnlyProvider) CLIPath() string                          { return "fixed" }
func (p *modelOnlyProvider) InteractiveCommand(_, _ string) *exec.Cmd { return exec.Command("true") }
func (p *modelOnlyProvider) SupportsInteractiveQuestions() bool       { return false }
func (p *modelOnlyProvider) CleanOutput(output string) string         { return output }
func (p *modelOnlyProvider) ParseLine(line string) *Event             { return ParseLine(line) }
func (p *modelOnlyProvider) LogFileName() string                      { return "fixed.log" }
func (p *modelOnlyProvider) LoopCommand(_ context.Context, _, _ string) *exec.Cmd {
	return exec.Command("true")
}

func TestProviderForMode_buildKeepsConfiguredSkillSetting(t *testing.T) {
	provider := &skillAwareProvider{skillsOff: true}
	l := NewLoop("prd.md", "prompt", 1, provider)

	got, ok := l.providerForMode(modeBuild).(*skillAwareProvider)
	if !ok {
		t.Fatalf("build provider has type %T, want *skillAwareProvider", l.providerForMode(modeBuild))
	}
	if !got.skillsOff {
		t.Error("build iteration got the skill catalogue back; agent.skills: none should hold for it")
	}
}

func TestProviderForMode_reviewAndConsolidateGetSkillsBack(t *testing.T) {
	for _, mode := range []iterationMode{modeReview, modeConsolidate} {
		provider := &skillAwareProvider{skillsOff: true}
		l := NewLoop("prd.md", "prompt", 1, provider)

		got, ok := l.providerForMode(mode).(*skillAwareProvider)
		if !ok {
			t.Fatalf("mode %v: provider has type %T, want *skillAwareProvider", mode, l.providerForMode(mode))
		}
		if got.skillsOff {
			t.Errorf("mode %v ran without the skill catalogue; its review.skill/consolidate.skill could not run", mode)
		}
		// The phase model still applies — switching skills must not drop it.
		if got.model != defaultPhaseModel {
			t.Errorf("mode %v ran on model %q, want %q", mode, got.model, defaultPhaseModel)
		}
		if provider.skillsOff != true || provider.model != "" {
			t.Error("the build agent's own provider was modified; phases must work on a copy")
		}
	}
}

func TestProviderForMode_providerWithoutSkillSwitchIsUntouched(t *testing.T) {
	provider := &modelOnlyProvider{}
	l := NewLoop("prd.md", "prompt", 1, provider)

	got, ok := l.providerForMode(modeReview).(*modelOnlyProvider)
	if !ok {
		t.Fatalf("provider has type %T, want *modelOnlyProvider", l.providerForMode(modeReview))
	}
	if got.model != defaultPhaseModel {
		t.Errorf("review ran on model %q, want %q", got.model, defaultPhaseModel)
	}
}
