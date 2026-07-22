package agent

import (
	"fmt"
	"os"
	"os/exec"
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
		return NewClaudeProvider(cliPath, model), nil
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
