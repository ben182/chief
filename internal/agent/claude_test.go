package agent

import (
	"context"
	"testing"

	"github.com/minicodemonkey/chief/internal/loop"
)

func TestClaudeProvider_Name(t *testing.T) {
	p := NewClaudeProvider("")
	if p.Name() != "Claude" {
		t.Errorf("Name() = %q, want Claude", p.Name())
	}
}

func TestClaudeProvider_CLIPath(t *testing.T) {
	p := NewClaudeProvider("")
	if p.CLIPath() != "claude" {
		t.Errorf("CLIPath() empty arg = %q, want claude", p.CLIPath())
	}
	p2 := NewClaudeProvider("/usr/local/bin/claude")
	if p2.CLIPath() != "/usr/local/bin/claude" {
		t.Errorf("CLIPath() custom = %q, want /usr/local/bin/claude", p2.CLIPath())
	}
}

func TestClaudeProvider_LogFileName(t *testing.T) {
	p := NewClaudeProvider("")
	if p.LogFileName() != "claude.log" {
		t.Errorf("LogFileName() = %q, want claude.log", p.LogFileName())
	}
}

func TestClaudeProvider_LoopCommand(t *testing.T) {
	ctx := context.Background()
	p := NewClaudeProvider("/bin/claude")
	cmd := p.LoopCommand(ctx, "hello world", "/work/dir")

	if cmd.Path != "/bin/claude" {
		t.Errorf("LoopCommand Path = %q, want /bin/claude", cmd.Path)
	}
	wantArgs := []string{"/bin/claude", "--dangerously-skip-permissions", "-p", "hello world", "--output-format", "stream-json", "--verbose"}
	if len(cmd.Args) != len(wantArgs) {
		t.Fatalf("LoopCommand Args len = %d, want %d: %v", len(cmd.Args), len(wantArgs), cmd.Args)
	}
	for i, w := range wantArgs {
		if cmd.Args[i] != w {
			t.Errorf("LoopCommand Args[%d] = %q, want %q", i, cmd.Args[i], w)
		}
	}
	if cmd.Dir != "/work/dir" {
		t.Errorf("LoopCommand Dir = %q, want /work/dir", cmd.Dir)
	}
}

func TestClaudeProvider_LoopCommandWithModel(t *testing.T) {
	ctx := context.Background()
	p := NewClaudeProvider("/bin/claude", "my-local-model")
	cmd := p.LoopCommand(ctx, "hi", "/work")

	wantArgs := []string{"/bin/claude", "--dangerously-skip-permissions", "-p", "hi", "--output-format", "stream-json", "--verbose", "--model", "my-local-model"}
	if len(cmd.Args) != len(wantArgs) {
		t.Fatalf("LoopCommand Args = %v, want %v", cmd.Args, wantArgs)
	}
	for i, w := range wantArgs {
		if cmd.Args[i] != w {
			t.Errorf("LoopCommand Args[%d] = %q, want %q", i, cmd.Args[i], w)
		}
	}
}

func TestClaudeProvider_InteractiveCommand(t *testing.T) {
	p := NewClaudeProvider("/bin/claude")
	cmd := p.InteractiveCommand("/work", "my prompt")
	if cmd.Dir != "/work" {
		t.Errorf("InteractiveCommand Dir = %q, want /work", cmd.Dir)
	}
	want := []string{"/bin/claude", "--dangerously-skip-permissions", "my prompt"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("InteractiveCommand Args = %v, want %v", cmd.Args, want)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Errorf("InteractiveCommand Args[%d] = %q, want %q", i, cmd.Args[i], want[i])
		}
	}
}

func TestClaudeProvider_InteractiveCommandWithModel(t *testing.T) {
	p := NewClaudeProvider("/bin/claude", "fable")
	cmd := p.InteractiveCommand("/work", "my prompt")
	want := []string{"/bin/claude", "--dangerously-skip-permissions", "my prompt", "--model", "fable"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("InteractiveCommand Args = %v, want %v", cmd.Args, want)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Errorf("InteractiveCommand Args[%d] = %q, want %q", i, cmd.Args[i], want[i])
		}
	}
}

func TestClaudeProvider_SetModel(t *testing.T) {
	p := NewClaudeProvider("/bin/claude")
	if p.Model() != "" {
		t.Errorf("Model() = %q, want empty", p.Model())
	}
	p.SetModel("opus")
	if p.Model() != "opus" {
		t.Errorf("Model() = %q, want opus", p.Model())
	}
	if cmd := p.InteractiveCommand("/w", "x"); len(cmd.Args) != 5 || cmd.Args[4] != "opus" {
		t.Errorf("after SetModel, InteractiveCommand Args = %v", cmd.Args)
	}
}

func TestClaudeProvider_ParseLine(t *testing.T) {
	p := NewClaudeProvider("")
	// Valid assistant text event
	line := `{"type":"assistant","message":{"type":"assistant","content":[{"type":"text","text":"hello"}]}}`
	e := p.ParseLine(line)
	if e == nil {
		t.Fatal("ParseLine(assistant text) returned nil")
	}
	if e.Type != loop.EventAssistantText {
		t.Errorf("ParseLine(assistant text) Type = %v, want EventAssistantText", e.Type)
	}
}

func TestClaudeProvider_CleanOutput(t *testing.T) {
	p := NewClaudeProvider("")
	input := "some output"
	if p.CleanOutput(input) != input {
		t.Errorf("CleanOutput should return input unchanged")
	}
}
