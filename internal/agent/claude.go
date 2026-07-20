package agent

import (
	"context"
	"os/exec"

	"github.com/minicodemonkey/chief/internal/loop"
)

// ClaudeProvider implements loop.Provider for the Claude Code CLI.
type ClaudeProvider struct {
	cliPath string
	model   string
}

// NewClaudeProvider returns a Provider for the Claude CLI.
// If cliPath is empty, "claude" is used. An optional model is passed to the
// CLI via --model (needed when Claude Code's -p mode ignores the configured
// model, e.g. local models via LM Studio).
func NewClaudeProvider(cliPath string, model ...string) *ClaudeProvider {
	if cliPath == "" {
		cliPath = "claude"
	}
	m := ""
	if len(model) > 0 {
		m = model[0]
	}
	return &ClaudeProvider{cliPath: cliPath, model: m}
}

// Name implements loop.Provider.
func (p *ClaudeProvider) Name() string { return "Claude" }

// SupportsInteractiveQuestions implements loop.Provider. Claude Code renders a
// native multiple-choice question UI, so the PRD prompts use it instead of
// lettered text options.
func (p *ClaudeProvider) SupportsInteractiveQuestions() bool { return true }

// CLIPath implements loop.Provider.
func (p *ClaudeProvider) CLIPath() string { return p.cliPath }

// LoopCommand implements loop.Provider.
func (p *ClaudeProvider) LoopCommand(ctx context.Context, prompt, workDir string) *exec.Cmd {
	args := []string{
		"--dangerously-skip-permissions",
		"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
	}
	if p.model != "" {
		args = append(args, "--model", p.model)
	}
	cmd := exec.CommandContext(ctx, p.cliPath, args...)
	cmd.Dir = workDir
	return cmd
}

// InteractiveCommand implements loop.Provider.
func (p *ClaudeProvider) InteractiveCommand(workDir, prompt string) *exec.Cmd {
	cmd := exec.Command(p.cliPath, prompt)
	cmd.Dir = workDir
	return cmd
}

// ParseLine implements loop.Provider.
func (p *ClaudeProvider) ParseLine(line string) *loop.Event {
	return loop.ParseLine(line)
}

// LogFileName implements loop.Provider.
func (p *ClaudeProvider) LogFileName() string { return "claude.log" }

// CleanOutput implements loop.Provider - Claude doesn't use a special format.
func (p *ClaudeProvider) CleanOutput(output string) string { return output }
