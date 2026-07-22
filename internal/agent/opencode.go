package agent

import (
	"context"
	"os/exec"
	"strings"

	"github.com/ben182/chief/internal/loop"
)

type OpenCodeProvider struct {
	cliPath string
}

func NewOpenCodeProvider(cliPath string) *OpenCodeProvider {
	if cliPath == "" {
		cliPath = "opencode"
	}
	return &OpenCodeProvider{cliPath: cliPath}
}

func (p *OpenCodeProvider) Name() string { return "OpenCode" }

// SupportsInteractiveQuestions implements loop.Provider. OpenCode has no native
// question UI, so the PRD prompts fall back to lettered text options.
func (p *OpenCodeProvider) SupportsInteractiveQuestions() bool { return false }

func (p *OpenCodeProvider) CLIPath() string { return p.cliPath }

func (p *OpenCodeProvider) LoopCommand(ctx context.Context, prompt, workDir string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, p.cliPath, "run", "--format", "json", prompt)
	cmd.Dir = workDir
	return cmd
}

func (p *OpenCodeProvider) InteractiveCommand(workDir, prompt string) *exec.Cmd {
	cmd := exec.Command(p.cliPath, "--prompt", prompt)
	cmd.Dir = workDir
	return cmd
}

func (p *OpenCodeProvider) ParseLine(line string) *loop.Event {
	return loop.ParseLineOpenCode(line)
}

func (p *OpenCodeProvider) LogFileName() string { return "opencode.log" }

// openCodeTextEvent is one NDJSON line of opencode's output; CleanOutput reads
// the text parts from these.
type openCodeTextEvent struct {
	Type string `json:"type"`
	Part struct {
		Text string `json:"text"`
	} `json:"part"`
}

// CleanOutput extracts JSON from opencode's NDJSON output format.
// It looks for the last "text" event line and returns its part.text content.
func (p *OpenCodeProvider) CleanOutput(output string) string {
	output = strings.TrimSpace(output)
	if !strings.Contains(output, "\n") {
		return output
	}

	if text := lastMatchingLine(output, func(ev openCodeTextEvent) (string, bool) {
		return ev.Part.Text, ev.Type == "text" && ev.Part.Text != ""
	}); text != "" {
		return text
	}
	return output
}
