package loop

import (
	"context"
	"os/exec"
)

// Provider is the interface for an agent CLI (e.g. Claude, Codex).
// Implementations live in internal/agent to avoid import cycles.
type Provider interface {
	Name() string
	CLIPath() string
	LoopCommand(ctx context.Context, prompt, workDir string) *exec.Cmd
	InteractiveCommand(workDir, prompt string) *exec.Cmd
	// SupportsInteractiveQuestions reports whether the provider's interactive
	// session can render a native multiple-choice question UI (as Claude Code's
	// question tool does). When false, prompts fall back to lettered options in
	// plain text. Used by the PRD new/edit prompts to pick the clearest format.
	SupportsInteractiveQuestions() bool
	// CleanOutput extracts JSON from the provider's output format (e.g., NDJSON).
	// Returns the original output if no cleaning needed.
	CleanOutput(output string) string
	ParseLine(line string) *Event
	LogFileName() string
}

// ModelSwitcher is implemented by providers whose model can be picked per agent
// invocation — today only Claude, whose CLI takes --model. It lets the loop run
// the review and consolidation agents on a cheaper model than the build agent
// without touching the build agent's own configuration: WithModel returns a copy
// of the provider running on the given model and leaves the receiver alone.
//
// Providers that don't implement it (codex, opencode, cursor, gemini) simply run
// every phase on whatever model their CLI picks, so a configured phase model is
// inert there rather than an error.
type ModelSwitcher interface {
	WithModel(model string) Provider
}
