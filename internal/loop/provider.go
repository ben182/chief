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
