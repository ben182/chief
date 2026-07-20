package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const configFile = ".chief/config.yaml"

// Config holds project-level settings for Chief.
type Config struct {
	Worktree   WorktreeConfig   `yaml:"worktree"`
	OnComplete OnCompleteConfig `yaml:"onComplete"`
	Agent      AgentConfig      `yaml:"agent"`
	Loop       LoopConfig       `yaml:"loop"`
}

// LoopConfig holds agent-loop tuning knobs.
type LoopConfig struct {
	// WatchdogTimeoutSeconds is the silence duration before a hung agent is
	// killed. <= 0 uses the built-in default. Raise it when the agent runs long
	// silent builds/tests that would otherwise trip the watchdog.
	WatchdogTimeoutSeconds int `yaml:"watchdogTimeoutSeconds"`
}

// AgentConfig holds agent CLI settings (Claude, Codex, OpenCode, or Cursor).
type AgentConfig struct {
	Provider string `yaml:"provider"` // "claude" (default) | "codex" | "opencode" | "cursor" | "gemini"
	CLIPath  string `yaml:"cliPath"`  // optional custom path to CLI binary
	Model    string `yaml:"model"`    // optional model passed to the CLI via --model (Claude only)
}

// WorktreeConfig holds worktree-related settings.
type WorktreeConfig struct {
	Setup string `yaml:"setup"`
}

// OnCompleteConfig holds post-completion automation settings.
type OnCompleteConfig struct {
	Push     bool `yaml:"push"`
	CreatePR bool `yaml:"createPR"`
	Notify   bool `yaml:"notify"`
}

// Default returns a Config with default values. Notify defaults to true so a
// walk-away run pings the user when it finishes; yaml.Unmarshal only overrides
// keys that are present, so an explicit `notify: false` still disables it.
func Default() *Config {
	return &Config{
		OnComplete: OnCompleteConfig{Notify: true},
	}
}

// configPath returns the full path to the config file.
func configPath(baseDir string) string {
	return filepath.Join(baseDir, configFile)
}

// Exists checks if the config file exists.
func Exists(baseDir string) bool {
	_, err := os.Stat(configPath(baseDir))
	return err == nil
}

// Load reads the config from .chief/config.yaml.
// Returns Default() when the file doesn't exist (no error).
func Load(baseDir string) (*Config, error) {
	path := configPath(baseDir)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return nil, err
	}

	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Save writes the config to .chief/config.yaml.
func Save(baseDir string, cfg *Config) error {
	path := configPath(baseDir)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
}
