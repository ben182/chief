package agent

import (
	"context"
	"os/exec"

	"github.com/ben182/chief/internal/loop"
)

// ClaudeProvider implements loop.Provider for the Claude Code CLI.
type ClaudeProvider struct {
	cliPath string
	model   string
	// strictMCP and mcpConfig carry agent.mcp: strictMCP makes the CLI ignore
	// every MCP server configured on the machine, mcpConfig names a file of
	// servers to load instead (absolute by the time it gets here). Both zero
	// means the run inherits the machine's servers, which is what runs did
	// before the setting existed.
	strictMCP bool
	mcpConfig string
	// skillsOff keeps the machine's skill catalogue out of the session. It holds
	// for build iterations only: WithSkills(true) hands a copy back to the
	// review and consolidation passes, which are driven by a skill.
	skillsOff bool
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

// Model returns the model the provider passes to the CLI via --model, or "" when
// none is configured (the CLI then uses its own default).
func (p *ClaudeProvider) Model() string { return p.model }

// SetModel overrides the model passed to the CLI via --model. An empty string
// clears it so the CLI falls back to its own default. Used by the PRD new/edit
// flows to apply an interactively chosen model.
func (p *ClaudeProvider) SetModel(model string) { p.model = model }

// WithModel implements loop.ModelSwitcher: it returns a copy of the provider that
// passes model to the CLI via --model, leaving the receiver untouched so the build
// agent keeps running on its own model while a phase (review, consolidation) runs
// on another.
func (p *ClaudeProvider) WithModel(model string) loop.Provider {
	clone := *p
	clone.model = model
	return &clone
}

// SetMCP configures which MCP servers the loop's agents start with. strict makes
// the CLI ignore everything configured on the machine; configPath, when
// non-empty, names a JSON file of servers to load in its place and must already
// be absolute, because the CLI resolves relative paths against the working
// directory and a run inside a worktree does not share the project's.
func (p *ClaudeProvider) SetMCP(strict bool, configPath string) {
	p.strictMCP = strict
	p.mcpConfig = configPath
}

// SetSkillsDisabled decides whether build iterations run without the machine's
// skill catalogue. It never reaches the review or consolidation passes; see
// WithSkills.
func (p *ClaudeProvider) SetSkillsDisabled(disabled bool) { p.skillsOff = disabled }

// WithSkills implements loop.SkillSwitcher: it returns a copy of the provider
// that runs with or without the machine's skill catalogue, leaving the receiver
// untouched so the build agent keeps the setting the project configured while a
// phase that needs a skill gets one of its own.
func (p *ClaudeProvider) WithSkills(enabled bool) loop.Provider {
	clone := *p
	clone.skillsOff = !enabled
	return &clone
}

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
	args = append(args, p.environmentArgs()...)
	cmd := exec.CommandContext(ctx, p.cliPath, args...)
	cmd.Dir = workDir
	return cmd
}

// environmentArgs returns the flags that shape what the session is handed before
// it starts: which MCP servers it may reach and whether the machine's skill
// catalogue is loaded. They apply to the autonomous loop only — an interactive
// `chief new` or `chief edit` has a person sitting in front of it who may well
// want to reach Notion or a skill mid-interview, and nothing is being paid per
// turn for a hundred tool definitions there.
func (p *ClaudeProvider) environmentArgs() []string {
	var args []string
	if p.strictMCP {
		args = append(args, "--strict-mcp-config")
	}
	if p.mcpConfig != "" {
		args = append(args, "--mcp-config", p.mcpConfig)
	}
	if p.skillsOff {
		args = append(args, "--disable-slash-commands")
	}
	return args
}

// InteractiveCommand implements loop.Provider. It launches the interactive
// Claude session (used by `chief new`/`chief edit`) with
// --dangerously-skip-permissions so the PRD interview can read the repository
// and write prd.md without a permission prompt on every tool call, matching the
// autonomous LoopCommand.
func (p *ClaudeProvider) InteractiveCommand(workDir, prompt string) *exec.Cmd {
	args := []string{"--dangerously-skip-permissions", prompt}
	if p.model != "" {
		args = append(args, "--model", p.model)
	}
	cmd := exec.Command(p.cliPath, args...)
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
