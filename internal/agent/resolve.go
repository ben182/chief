package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/loop"
)

// Resolve returns the agent Provider using priority: flagAgent > CHIEF_AGENT env > config > "claude".
// flagPath overrides the CLI path when non-empty (flag > CHIEF_AGENT_PATH > config agent.cliPath).
// An optional flagModel overrides the model (flag > CHIEF_MODEL > config agent.model); it applies
// to the Claude provider only.
// Returns an error if the resolved provider name is not recognised.
func Resolve(flagAgent, flagPath string, cfg *config.Config, flagModel ...string) (loop.Provider, error) {
	var cfgProvider, cfgPath, cfgModel string
	if cfg != nil {
		cfgProvider = cfg.Agent.Provider
		cfgPath = cfg.Agent.CLIPath
		cfgModel = cfg.Agent.Model
	}

	// Each setting follows the same precedence: flag > env > config, first
	// non-empty wins. Provider names are additionally lowercased and default to
	// "claude" when nothing is set.
	providerName := "claude"
	if v := firstNonEmpty(flagAgent, "CHIEF_AGENT", cfgProvider); v != "" {
		providerName = strings.ToLower(v)
	}

	cliPath := firstNonEmpty(flagPath, "CHIEF_AGENT_PATH", cfgPath)

	flagModelVal := ""
	if len(flagModel) > 0 {
		flagModelVal = flagModel[0]
	}
	model := firstNonEmpty(flagModelVal, "CHIEF_MODEL", cfgModel)

	switch providerName {
	case "claude":
		provider := NewClaudeProvider(cliPath, model)
		if err := applyEnvironment(provider, cfg); err != nil {
			return nil, err
		}
		return provider, nil
	case "codex":
		return NewCodexProvider(cliPath), nil
	case "opencode":
		return NewOpenCodeProvider(cliPath), nil
	case "cursor":
		return NewCursorProvider(cliPath), nil
	case "gemini":
		return NewGeminiProvider(cliPath), nil
	default:
		return nil, fmt.Errorf("unknown agent provider %q: expected \"claude\", \"codex\", \"opencode\", \"cursor\", or \"gemini\"", providerName)
	}
}

// applyEnvironment hands the Claude provider the two project settings that shape
// what a loop iteration is given before it starts: agent.mcp and agent.skills.
//
// A configured MCP file is resolved here rather than in the provider, for two
// reasons. The project root is known at this point and not later — the CLI runs
// with its working directory inside a worktree, where a path written relative to
// the project would mean something else or nothing at all. And a path that isn't
// there has to fail now: an MCP file the CLI cannot read costs the run its
// servers silently, and `laravel-boost` quietly missing from every iteration is
// exactly the kind of failure nobody notices until the run is over.
func applyEnvironment(provider *ClaudeProvider, cfg *config.Config) error {
	if cfg == nil {
		return nil
	}

	strict, path := cfg.Agent.MCPSetting()
	if path != "" {
		if !filepath.IsAbs(path) {
			path = filepath.Join(cfg.BaseDir(), path)
		}
		if err := checkMCPFile(path); err != nil {
			return err
		}
	}
	provider.SetMCP(strict, path)
	provider.SetSkillsDisabled(cfg.Agent.SkillsDisabled())
	return nil
}

// checkMCPFile reads the configured MCP file far enough to be sure the run will
// get servers out of it. Existence alone is not enough: the file is handed to
// the CLI together with --strict-mcp-config, so a file that parses but names
// nothing takes every server away without a word, and the run finds out by
// behaving slightly worse for an hour.
func checkMCPFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("agent.mcp: no MCP config file at %s (set it to \"inherit\", \"none\", or a path to a .mcp.json-shaped file)", path)
		}
		return fmt.Errorf("agent.mcp: cannot read %s: %w", path, err)
	}

	var parsed struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("agent.mcp: %s is not valid JSON: %w", path, err)
	}
	if len(parsed.Servers) == 0 {
		return fmt.Errorf("agent.mcp: %s names no servers under \"mcpServers\" — use \"none\" if that is what you meant", path)
	}
	return nil
}

// firstNonEmpty returns the first non-empty value, after trimming surrounding
// whitespace, of: flag, the environment variable named envKey, then cfgVal.
// Returns "" when all three are empty. It encodes chief's flag > env > config
// precedence for a single string setting.
func firstNonEmpty(flag, envKey, cfgVal string) string {
	if v := strings.TrimSpace(flag); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
		return v
	}
	return strings.TrimSpace(cfgVal)
}

// CheckInstalled verifies that the provider's CLI binary is found in PATH (or at cliPath).
func CheckInstalled(p loop.Provider) error {
	_, err := exec.LookPath(p.CLIPath())
	if err != nil {
		return fmt.Errorf("%s CLI not found in PATH. Install it or set agent.cliPath in .chief/config.yaml", p.Name())
	}
	return nil
}
