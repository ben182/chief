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
	Review     ReviewConfig     `yaml:"review"`
}

// ReviewConfig holds the per-project code-quality review step that the agent
// runs at the end of each iteration, before committing.
type ReviewConfig struct {
	// Skill is the name of a skill the agent must run to review the changes it
	// made for the current story (e.g. "/code-quality"). When set, chief injects
	// an instruction into every iteration prompt telling the agent to run the
	// skill, fix anything it flags, and only then commit. Empty disables it.
	Skill string `yaml:"skill"`
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
	// Summary, when true, generates a human-facing, timestamped summary file
	// (SUMMARY-<date>-<time>.md) next to the PRD once the run finishes (what was
	// built, how to test it, where the new functionality lives, open follow-ups)
	// and commits it so it rides along in the push/PR. Runs both on full
	// completion and when max iterations is hit, as long as the branch has
	// commits to describe.
	Summary bool `yaml:"summary"`
}

// Default returns a Config with default values. Notify and Summary default to
// true so a walk-away run both pings the user and leaves a summary when it
// finishes; yaml.Unmarshal only overrides keys that are present, so an explicit
// `notify: false` / `summary: false` still disables them.
func Default() *Config {
	return &Config{
		OnComplete: OnCompleteConfig{Notify: true, Summary: true},
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
