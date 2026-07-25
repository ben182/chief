package config

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const configFile = ".chief/config.yaml"

// Config holds project-level settings for Chief.
type Config struct {
	Worktree    WorktreeConfig    `yaml:"worktree"`
	OnComplete  OnCompleteConfig  `yaml:"onComplete"`
	Agent       AgentConfig       `yaml:"agent"`
	Loop        LoopConfig        `yaml:"loop"`
	Review      ReviewConfig      `yaml:"review"`
	Consolidate ConsolidateConfig `yaml:"consolidate"`
}

// ConsolidateConfig holds the consolidation pass that runs once at the end of a
// run, after every story has been built and reviewed.
//
// The review agent judges one story at a time and never sees the others. That
// leaves a blind spot no per-story check can cover: because each story is built
// by a separate agent with a fresh context, two stories can each grow their own
// helper for the same job, or introduce competing patterns for one concern, and
// both commits still look correct in isolation. The consolidation agent is the
// only one that ever sees the whole run — it refactors those seams away in a
// single separate commit, scoped strictly to this run's commits (StartRef..HEAD)
// so an earlier run's shipped work is never touched. It is a pure refactor:
// behavior must not change.
type ConsolidateConfig struct {
	// Enabled turns the consolidation pass on with just the built-in prompt, no
	// extra config. A non-empty Skill or Instructions also enables it on its own,
	// so this flag is only needed to run the bare baseline with neither of those set.
	Enabled bool `yaml:"enabled"`
	// Skill is the name of a project skill the consolidation agent should run as
	// part of its pass (e.g. "/code-quality"). Claude-specific; other providers
	// ignore it. Optional — setting it also enables the pass.
	Skill string `yaml:"skill"`
	// Instructions is free-form guidance for the consolidation agent (e.g. "we
	// keep all HTTP clients in internal/transport"). Works with any provider.
	// Optional — setting it also enables the pass.
	Instructions string `yaml:"instructions"`
}

// Active reports whether the consolidation pass should run at the end of a run:
// true when explicitly enabled, or when a skill or free-form instructions are
// configured (either of which enables it on its own).
func (c ConsolidateConfig) Active() bool {
	return c.Enabled ||
		strings.TrimSpace(c.Skill) != "" ||
		strings.TrimSpace(c.Instructions) != ""
}

// ReviewConfig holds the per-project code review that runs after a story's
// build agent has committed. When enabled, chief spawns a *separate* agent with
// a fresh context (it never sees the build agent's reasoning) that adversarially
// reviews the story's changes, fixes anything it finds, and amends the commit —
// a second pair of eyes rather than the author checking their own work.
type ReviewConfig struct {
	// Enabled turns the review agent on with just the built-in review prompt (the
	// two-axis Spec/Standards review and code-smell baseline), no extra config. A
	// non-empty Skill or Instructions also enables the review on its own, so this
	// flag is only needed to run the bare baseline with neither of those set.
	Enabled bool `yaml:"enabled"`
	// Skill is the name of a project skill the review agent should run as part of
	// its review (e.g. "/code-quality"). Claude-specific; other providers ignore
	// it. Optional — setting it also enables the review.
	Skill string `yaml:"skill"`
	// Instructions is free-form guidance for the review agent (e.g. "watch for
	// N+1 queries and missing tests"). Works with any provider. Optional — setting
	// it also enables the review.
	Instructions string `yaml:"instructions"`
}

// Active reports whether a review agent should run: true when the review is
// explicitly enabled, or when a skill or free-form instructions are configured
// (either of which enables it on its own).
func (r ReviewConfig) Active() bool {
	return r.Enabled ||
		strings.TrimSpace(r.Skill) != "" ||
		strings.TrimSpace(r.Instructions) != ""
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
	// (summary-<date>-<time>.md) next to the PRD once the run finishes (what was
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
