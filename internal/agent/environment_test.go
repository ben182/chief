package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/loop"
)

// argsOf runs the provider's loop command and returns its arguments, so a test
// can assert on the command line the CLI would actually be started with.
func argsOf(p *ClaudeProvider) []string {
	return p.LoopCommand(context.Background(), "prompt", "/work").Args
}

// hasFlag reports whether args contains flag, and returns the value following it
// when there is one.
func hasFlag(args []string, flag string) (string, bool) {
	for i, a := range args {
		if a != flag {
			continue
		}
		if i+1 < len(args) {
			return args[i+1], true
		}
		return "", true
	}
	return "", false
}

func TestClaudeProvider_inheritsMCPByDefault(t *testing.T) {
	args := argsOf(NewClaudeProvider("/bin/claude"))
	for _, flag := range []string{"--strict-mcp-config", "--mcp-config", "--disable-slash-commands"} {
		if _, ok := hasFlag(args, flag); ok {
			t.Errorf("unconfigured provider passes %s; the default must leave the machine's setup alone: %v", flag, args)
		}
	}
}

func TestClaudeProvider_MCPNone(t *testing.T) {
	p := NewClaudeProvider("/bin/claude")
	p.SetMCP(true, "")
	args := argsOf(p)

	if _, ok := hasFlag(args, "--strict-mcp-config"); !ok {
		t.Errorf("agent.mcp: none did not pass --strict-mcp-config: %v", args)
	}
	if _, ok := hasFlag(args, "--mcp-config"); ok {
		t.Errorf("agent.mcp: none passed a --mcp-config file: %v", args)
	}
}

func TestClaudeProvider_MCPFile(t *testing.T) {
	p := NewClaudeProvider("/bin/claude")
	p.SetMCP(true, "/project/.chief/mcp.json")
	args := argsOf(p)

	if _, ok := hasFlag(args, "--strict-mcp-config"); !ok {
		t.Errorf("a configured MCP file must come with --strict-mcp-config, or the machine's servers are merged in anyway: %v", args)
	}
	got, ok := hasFlag(args, "--mcp-config")
	if !ok || got != "/project/.chief/mcp.json" {
		t.Errorf("--mcp-config = %q (present: %v), want /project/.chief/mcp.json: %v", got, ok, args)
	}
}

func TestClaudeProvider_skillsDisabled(t *testing.T) {
	p := NewClaudeProvider("/bin/claude")
	p.SetSkillsDisabled(true)

	if _, ok := hasFlag(argsOf(p), "--disable-slash-commands"); !ok {
		t.Errorf("agent.skills: none did not pass --disable-slash-commands: %v", argsOf(p))
	}

	// A phase that needs a skill asks for one back, and must not disturb the
	// provider the build agent keeps using.
	withSkills, ok := p.WithSkills(true).(*ClaudeProvider)
	if !ok {
		t.Fatalf("WithSkills returned %T, want *ClaudeProvider", p.WithSkills(true))
	}
	if _, ok := hasFlag(argsOf(withSkills), "--disable-slash-commands"); ok {
		t.Errorf("WithSkills(true) still disables skills: %v", argsOf(withSkills))
	}
	if _, ok := hasFlag(argsOf(p), "--disable-slash-commands"); !ok {
		t.Error("WithSkills(true) modified the receiver instead of returning a copy")
	}
}

func TestClaudeProvider_interactiveSessionIsUntouched(t *testing.T) {
	p := NewClaudeProvider("/bin/claude")
	p.SetMCP(true, "/project/.chief/mcp.json")
	p.SetSkillsDisabled(true)

	args := p.InteractiveCommand("/work", "interview").Args
	for _, flag := range []string{"--strict-mcp-config", "--mcp-config", "--disable-slash-commands"} {
		if _, ok := hasFlag(args, flag); ok {
			t.Errorf("interactive session got %s; a person sitting in front of it may want those: %v", flag, args)
		}
	}
}

// resolveClaude resolves cfg and asserts the result is the Claude provider.
func resolveClaude(t *testing.T, cfg *config.Config) *ClaudeProvider {
	t.Helper()
	p, err := Resolve("", "", cfg)
	if err != nil {
		t.Fatalf("Resolve: unexpected error: %v", err)
	}
	claude, ok := p.(*ClaudeProvider)
	if !ok {
		t.Fatalf("Resolve returned %T, want *ClaudeProvider", p)
	}
	return claude
}

func TestResolve_MCPRelativePathIsProjectRelative(t *testing.T) {
	// A run works inside a worktree, so a path written relative to the project
	// has to be anchored to the project before the CLI ever sees it.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".chief"), 0o755); err != nil {
		t.Fatal(err)
	}
	mcpPath := filepath.Join(root, ".chief", "mcp.json")
	if err := os.WriteFile(mcpPath, []byte(`{"mcpServers":{"boost":{"command":"php","args":["artisan","boost:mcp"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, &config.Config{Agent: config.AgentConfig{MCP: ".chief/mcp.json"}}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	got, ok := hasFlag(argsOf(resolveClaude(t, cfg)), "--mcp-config")
	if !ok {
		t.Fatalf("no --mcp-config in %v", argsOf(resolveClaude(t, cfg)))
	}
	if got != mcpPath {
		t.Errorf("--mcp-config = %q, want the project-anchored %q", got, mcpPath)
	}
}

func TestResolve_MCPMissingFileAborts(t *testing.T) {
	root := t.TempDir()
	if err := config.Save(root, &config.Config{Agent: config.AgentConfig{MCP: ".chief/mcp.json"}}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Resolve("", "", cfg)
	if err == nil {
		t.Fatal("a missing MCP config file started the run anyway; the agent would silently lose every server")
	}
	if !strings.Contains(err.Error(), "agent.mcp") {
		t.Errorf("error %q does not name the key that caused it", err)
	}
}

func TestResolve_MCPFileWithNoServersAborts(t *testing.T) {
	// The file is handed to the CLI with --strict-mcp-config, so an empty one
	// silently takes every server away instead of doing nothing.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, &config.Config{Agent: config.AgentConfig{MCP: "mcp.json"}}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Resolve("", "", cfg); err == nil {
		t.Fatal("an MCP file naming no servers was accepted")
	}
}

func TestResolve_MCPFileWithBrokenJSONAborts(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), []byte(`{"mcpServers":`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, &config.Config{Agent: config.AgentConfig{MCP: "mcp.json"}}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Resolve("", "", cfg)
	if err == nil {
		t.Fatal("an MCP file that is not valid JSON was accepted")
	}
	if !strings.Contains(err.Error(), "agent.mcp") {
		t.Errorf("error %q does not name the key that caused it", err)
	}
}

func TestResolve_MCPSettingIsInertForOtherProviders(t *testing.T) {
	cfg := &config.Config{Agent: config.AgentConfig{Provider: "codex", MCP: "none", Skills: "none"}}
	p, err := Resolve("", "", cfg)
	if err != nil {
		t.Fatalf("Resolve: unexpected error: %v", err)
	}
	if _, ok := p.(loop.SkillSwitcher); ok {
		t.Error("codex provider claims it can switch skills")
	}
	if p.Name() != "Codex" {
		t.Errorf("Name = %q, want Codex", p.Name())
	}
}
