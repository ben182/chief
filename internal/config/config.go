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
	// Enabled is the hard switch for the pass, and it always wins: `enabled: true`
	// runs it even with no other config, `enabled: false` keeps it off even when a
	// skill or instructions are configured. Left out of the config entirely it is
	// nil, and then a non-empty Skill or Instructions turns the pass on by itself.
	Enabled *bool `yaml:"enabled,omitempty"`
	// Model is the model the consolidation agent runs on (e.g. "haiku", "opus").
	// Empty — the default — runs the pass on Sonnet: consolidation is a large share
	// of a run's cost and does not need the build agent's model. Claude-specific;
	// providers whose CLI takes no model ignore it. Set it to the same value as
	// `agent.model` when the build agent runs on a model of its own that the
	// Sonnet default would not reach (e.g. a local model).
	Model string `yaml:"model,omitempty"`
	// Skill is the name of a project skill the consolidation agent should run as
	// part of its pass (e.g. "/code-quality"). Claude-specific; other providers
	// ignore it. Optional — setting it also enables the pass unless Enabled says
	// otherwise.
	Skill string `yaml:"skill"`
	// Instructions is free-form guidance for the consolidation agent (e.g. "we
	// keep all HTTP clients in internal/transport"). Works with any provider.
	// Optional — setting it also enables the pass unless Enabled says otherwise.
	Instructions string `yaml:"instructions"`
}

// Active reports whether the consolidation pass should run at the end of a run.
// An explicit `enabled` decides on its own, either way; without it, a configured
// skill or free-form instructions turn the pass on.
func (c ConsolidateConfig) Active() bool {
	if c.Enabled != nil {
		return *c.Enabled
	}
	return strings.TrimSpace(c.Skill) != "" ||
		strings.TrimSpace(c.Instructions) != ""
}

// ReviewConfig holds the per-project code review that runs after a story's
// build agent has committed. When enabled, chief spawns a *separate* agent with
// a fresh context (it never sees the build agent's reasoning) that adversarially
// reviews the story's changes, fixes anything it finds, and amends the commit —
// a second pair of eyes rather than the author checking their own work.
type ReviewConfig struct {
	// Enabled is the hard switch for the review, and it always wins: `enabled: true`
	// runs it with just the built-in review prompt (the two-axis Spec/Standards
	// review and code-smell baseline), `enabled: false` keeps it off even when a
	// skill or instructions are configured. Left out of the config entirely it is
	// nil, and then a non-empty Skill or Instructions turns the review on by itself.
	Enabled *bool `yaml:"enabled,omitempty"`
	// Model is the model the review agent runs on (e.g. "haiku", "opus"). Empty —
	// the default — runs the review on Sonnet: reviewing a single story's diff is a
	// large share of a run's cost and does not need the build agent's model.
	// Claude-specific; providers whose CLI takes no model ignore it. Set it to the
	// same value as `agent.model` when the build agent runs on a model of its own
	// that the Sonnet default would not reach (e.g. a local model).
	Model string `yaml:"model,omitempty"`
	// Skill is the name of a project skill the review agent should run as part of
	// its review (e.g. "/code-quality"). Claude-specific; other providers ignore
	// it. Optional — setting it also enables the review unless Enabled says
	// otherwise.
	Skill string `yaml:"skill"`
	// Instructions is free-form guidance for the review agent (e.g. "watch for
	// N+1 queries and missing tests"). Works with any provider. Optional — setting
	// it also enables the review unless Enabled says otherwise.
	Instructions string `yaml:"instructions"`
}

// Active reports whether a review agent should run after a story commits. An
// explicit `enabled` decides on its own, either way; without it, a configured
// skill or free-form instructions turn the review on.
func (r ReviewConfig) Active() bool {
	if r.Enabled != nil {
		return *r.Enabled
	}
	return strings.TrimSpace(r.Skill) != "" ||
		strings.TrimSpace(r.Instructions) != ""
}

// Bool returns a pointer to b, for setting the tri-state `enabled` switches
// (nil = not configured) from code rather than from YAML.
func Bool(b bool) *bool { return &b }

// LoopConfig holds agent-loop tuning knobs.
type LoopConfig struct {
	// WatchdogTimeoutSeconds is the silence duration before a hung agent is
	// killed. <= 0 uses the built-in default. Raise it when the agent runs long
	// silent builds/tests that would otherwise trip the watchdog.
	WatchdogTimeoutSeconds int `yaml:"watchdogTimeoutSeconds"`
	// KeepAwake stops the machine from going to sleep while a loop is running.
	// A run is a walk-away workflow, so nobody touches the keyboard for an hour
	// and the OS suspends the machine mid-story. Currently macOS only
	// (caffeinate); a no-op elsewhere. A running loop re-reads this within a few
	// seconds, so switching it on halfway through a run applies to that run.
	KeepAwake bool `yaml:"keepAwake"`
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
	// PRBaseBranch forces the branch pull requests are opened against. Empty (the
	// default) lets chief use the branch the run's branch was cut from — recorded
	// when chief created it, inferred from history otherwise — which is what you
	// want in a repo where feature branches come off develop rather than main.
	// Set this only when that answer is wrong for your workflow.
	PRBaseBranch string `yaml:"prBaseBranch"`
}

// Default returns a Config with default values. Notify, Summary and KeepAwake
// default to true so a walk-away run keeps the machine up while it works, then
// pings the user and leaves a summary when it finishes; yaml.Unmarshal only
// overrides keys that are present, so an explicit `notify: false` /
// `summary: false` / `keepAwake: false` still disables them.
func Default() *Config {
	return &Config{
		OnComplete: OnCompleteConfig{Notify: true, Summary: true},
		Loop:       LoopConfig{KeepAwake: true},
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
