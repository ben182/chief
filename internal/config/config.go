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
	// baseDir is the project root the config was read from. It is kept so that a
	// setting holding a path (agent.mcp) can be resolved relative to the project
	// rather than to whatever directory the process happens to sit in — a run in
	// a worktree has a different working directory than the config it obeys.
	// Unexported on purpose: it is not a setting, and it must never be written
	// back to the file.
	baseDir string

	Box         BoxConfig         `yaml:"box,omitempty"`
	Worktree    WorktreeConfig    `yaml:"worktree"`
	OnComplete  OnCompleteConfig  `yaml:"onComplete"`
	Agent       AgentConfig       `yaml:"agent"`
	Loop        LoopConfig        `yaml:"loop"`
	Review      ReviewConfig      `yaml:"review"`
	Consolidate ConsolidateConfig `yaml:"consolidate"`
}

// BoxConfig holds where and on what a `chief box` run happens.
//
// It is answered once per project, by `chief box config`, rather than passed as
// flags on every run. Two of these settings are decisions rather than
// preferences: the location decides which country the project's source and its
// .env spend the run in, and the type decides what the run costs. A flag that
// has to be remembered for both is a flag that will be forgotten.
//
// Every field is optional, and an empty one takes chief's default. A project
// that never runs `chief box config` behaves exactly as it did before this
// existed.
type BoxConfig struct {
	// Type is the Hetzner server type, e.g. "cpx32".
	Type string `yaml:"type,omitempty"`
	// Location is the Hetzner location, e.g. "fsn1" for Falkenstein.
	Location string `yaml:"location,omitempty"`
	// Image is the Hetzner image the box is built from. Rarely worth setting:
	// the default is the current Ubuntu LTS, and the cloud-config that provisions
	// the box assumes the package names that LTS ships.
	Image string `yaml:"image,omitempty"`
	// PHP pins the PHP series the box installs, e.g. "8.3". Empty lets chief
	// work it out: the version Herd isolates this site to, else the php on this
	// machine, else the lowest composer.json accepts.
	PHP string `yaml:"php,omitempty"`
	// Node pins the Node major the box installs, e.g. "22". Empty lets chief
	// work it out the same way: this machine's node, else the project's .nvmrc,
	// else the lowest package.json accepts.
	Node string `yaml:"node,omitempty"`
	// Packages are apt packages this project needs on top of what chief worked
	// out from the project, for the dependency no manifest declares.
	Packages []string `yaml:"packages,omitempty"`
	// Files are paths, relative to the project, that git does not carry but the
	// run needs. Empty means ".env" alone, which is the case for most projects.
	Files []string `yaml:"files,omitempty"`
	// Keep leaves the box running after its run has finished. By default a box
	// destroys itself once the work is safely on origin, because the alternative
	// is a machine billing until somebody notices — and the person who would
	// notice is asleep, which is the whole reason the run is on a box.
	//
	// Set this for the project whose boxes you want to inspect afterwards, and
	// remember that 'chief box down' is then the only thing that stops the bill.
	Keep bool `yaml:"keep,omitempty"`
	// MaxHours is the box's outside limit: however the run is going, the box
	// stops it and destroys itself this many hours after it booted. Zero takes
	// chief's default. It is the backstop for the run that hangs rather than
	// ends — a box nothing ever finishes on is a box nothing ever destroys.
	MaxHours int `yaml:"maxHours,omitempty"`
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
	// MCP decides which MCP servers an unattended iteration starts with. Empty
	// or "inherit" — the default — keeps what every run did before this key
	// existed: everything the machine has configured, which is the repository's
	// .mcp.json plus the user's own servers plus every account-level connector.
	// "none" starts the agent with no MCP server at all. Anything else is a path
	// to a JSON file in the same shape as .mcp.json, and then the agent sees
	// exactly the servers named there and nothing else.
	//
	// It exists because a loop nobody is watching inherits a whole desktop. In
	// one measured project that was 169 tools from 25 servers — mail, calendars,
	// task trackers, a hosting API — of which twenty runs of build agents called
	// five, all from one server. The cost of carrying the rest is small and the
	// reach is not: those runs pass --dangerously-skip-permissions.
	//
	// A relative path is resolved against the project root rather than the
	// working directory, so a run inside a worktree finds the same file. The
	// path is checked when the provider is built: a name that is not there
	// aborts the start instead of failing inside the first iteration.
	// Claude-specific; providers whose CLI has no equivalent ignore it.
	MCP string `yaml:"mcp,omitempty"`
	// Skills decides whether the machine's skill catalogue — Claude Code's
	// skills and slash commands — is loaded into a **build** iteration. Empty or
	// "inherit" (the default) loads it; "none" leaves it out, which keeps the
	// catalogue out of the context of every turn, not just the first.
	//
	// Review and consolidation always get the catalogue back, whatever this
	// says: `review.skill` / `consolidate.skill` name a skill those passes run,
	// and switching skills off would quietly disable them. Claude-specific.
	Skills string `yaml:"skills,omitempty"`
}

// inheritSetting is the word a per-project setting uses for "leave the machine's
// own configuration alone", and the empty value means the same thing.
const inheritSetting = "inherit"

// noneSetting is the word that empties a catalogue the CLI would otherwise load.
const noneSetting = "none"

// MCPSetting normalises agent.mcp into the two answers a provider needs: whether
// to ignore the machine's MCP configuration, and which file to load instead.
// It returns (false, "") for the inherited default, (true, "") for "none", and
// (true, path) for a configured file. The path is returned as written; resolving
// it against the project root is the caller's job, because only the caller knows
// where that is.
func (a AgentConfig) MCPSetting() (strict bool, path string) {
	value := strings.TrimSpace(a.MCP)
	switch {
	case value == "" || strings.EqualFold(value, inheritSetting):
		return false, ""
	case strings.EqualFold(value, noneSetting):
		return true, ""
	default:
		return true, value
	}
}

// SkillsDisabled reports whether build iterations should run without the
// machine's skill catalogue.
func (a AgentConfig) SkillsDisabled() bool {
	return strings.EqualFold(strings.TrimSpace(a.Skills), noneSetting)
}

// WorktreeConfig holds worktree-related settings.
type WorktreeConfig struct {
	// Setup is a shell command run inside a freshly created worktree, for
	// whatever a checkout needs before it can be worked in — dependencies, an
	// .env, a database. Empty (the default) skips the step. Like Teardown it
	// gets the worktree's context in its environment, see git.WorktreeContext.
	Setup string `yaml:"setup"`
	// SetupOnReuse decides whether Setup also runs when chief picks up a
	// worktree that is already there — a run resumed after a crash, a PRD
	// started again. Unset (the default) runs it, which is what every run did
	// before this key existed and what a setup that only installs dependencies
	// wants. Set it to false when the setup is expensive or not idempotent; then
	// only a freshly created worktree gets one.
	SetupOnReuse *bool `yaml:"setupOnReuse,omitempty"`
	// Teardown is a shell command run inside a worktree right before chief
	// removes it, so resources living outside git — databases, web-server
	// links, containers — disappear along with the directory. Empty (the
	// default) keeps removal a pure git operation. A failing teardown aborts
	// the removal so nothing is lost silently.
	Teardown string `yaml:"teardown"`
	// BaseBranch is the branch worktree branches are cut from, and therefore
	// the branch a pull request for them targets. Empty (the default) uses the
	// repository's detected default branch, which is what a repo whose work
	// starts at main wants; set it to develop in a repo where it doesn't. The
	// branch has to exist locally or on origin, otherwise the run refuses to
	// start rather than branch off the wrong place.
	BaseBranch string `yaml:"baseBranch"`
	// SetupTimeoutSeconds aborts a setup command that outlives it, killing the
	// whole process tree it started. Zero — the default — waits forever, which
	// is right for a setup that is merely slow; a number is for the one that
	// hangs on a prompt or a lock and would otherwise block the run for good.
	SetupTimeoutSeconds int `yaml:"setupTimeoutSeconds"`
	// Dir is a path template saying where a PRD's worktree lives. Empty (the
	// default) means .chief/worktrees/{prd}, which keeps worktrees inside the
	// project; point it outside when a tool in the repo — a bundler, a test
	// runner, an editor index — trips over checkouts nested in the checkout.
	// Placeholders are {prd}, {repo} and {branch}; see git.WorktreePathForPRD.
	Dir string `yaml:"dir"`
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
			cfg := Default()
			cfg.baseDir = baseDir
			return cfg, nil
		}
		return nil, err
	}

	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	cfg.baseDir = baseDir

	return cfg, nil
}

// BaseDir returns the project root this config was loaded from, or "" for a
// config that never came from a file (Default, or one built in a test).
func (c *Config) BaseDir() string {
	if c == nil {
		return ""
	}
	return c.baseDir
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
