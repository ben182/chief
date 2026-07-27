package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.Worktree.Setup != "" {
		t.Errorf("expected empty setup, got %q", cfg.Worktree.Setup)
	}
	if cfg.OnComplete.Push {
		t.Error("expected Push to be false")
	}
	if cfg.OnComplete.CreatePR {
		t.Error("expected CreatePR to be false")
	}
	// Empty means "use the branch the run's branch was cut from", not "main".
	if cfg.OnComplete.PRBaseBranch != "" {
		t.Errorf("expected PRBaseBranch to be empty, got %q", cfg.OnComplete.PRBaseBranch)
	}
}

func TestLoadPRBaseBranch(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".chief"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	yaml := "onComplete:\n  createPR: true\n  prBaseBranch: develop\n"
	if err := os.WriteFile(filepath.Join(dir, ".chief", "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.OnComplete.PRBaseBranch != "develop" {
		t.Errorf("expected PRBaseBranch %q, got %q", "develop", cfg.OnComplete.PRBaseBranch)
	}
}

func TestLoadNonExistent(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Worktree.Setup != "" {
		t.Errorf("expected empty setup, got %q", cfg.Worktree.Setup)
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()

	cfg := &Config{
		Worktree: WorktreeConfig{
			Setup: "npm install",
		},
		OnComplete: OnCompleteConfig{
			Push:     true,
			CreatePR: true,
		},
	}

	if err := Save(dir, cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.Worktree.Setup != "npm install" {
		t.Errorf("expected setup %q, got %q", "npm install", loaded.Worktree.Setup)
	}
	if !loaded.OnComplete.Push {
		t.Error("expected Push to be true")
	}
	if !loaded.OnComplete.CreatePR {
		t.Error("expected CreatePR to be true")
	}
}

func TestReviewConfigEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  ReviewConfig
		want bool
	}{
		{"empty", ReviewConfig{}, false},
		{"enabled flag only", ReviewConfig{Enabled: Bool(true)}, true},
		{"skill only", ReviewConfig{Skill: "/code-quality"}, true},
		{"instructions only", ReviewConfig{Instructions: "watch for N+1"}, true},
		{"both", ReviewConfig{Skill: "/cq", Instructions: "x"}, true},
		{"whitespace only", ReviewConfig{Skill: "  ", Instructions: "\n\t"}, false},
		{"disabled flag, whitespace fields", ReviewConfig{Enabled: Bool(false), Skill: "  "}, false},
		// enabled: false is a hard off switch: a leftover skill or instructions
		// block must not resurrect a review the project explicitly turned off.
		{"disabled flag beats skill", ReviewConfig{Enabled: Bool(false), Skill: "/code-quality"}, false},
		{"disabled flag beats instructions", ReviewConfig{Enabled: Bool(false), Instructions: "watch for N+1"}, false},
	}
	for _, tt := range tests {
		if got := tt.cfg.Active(); got != tt.want {
			t.Errorf("%s: Active() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestReviewConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{Review: ReviewConfig{Skill: "/code-quality", Instructions: "watch for N+1 queries"}}
	if err := Save(dir, cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Review.Skill != "/code-quality" {
		t.Errorf("expected skill to round-trip, got %q", loaded.Review.Skill)
	}
	if loaded.Review.Instructions != "watch for N+1 queries" {
		t.Errorf("expected instructions to round-trip, got %q", loaded.Review.Instructions)
	}
}

func TestConsolidateConfigEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  ConsolidateConfig
		want bool
	}{
		{"empty", ConsolidateConfig{}, false},
		{"enabled flag only", ConsolidateConfig{Enabled: Bool(true)}, true},
		{"skill only", ConsolidateConfig{Skill: "/code-quality"}, true},
		{"instructions only", ConsolidateConfig{Instructions: "one HTTP client"}, true},
		{"both", ConsolidateConfig{Skill: "/cq", Instructions: "x"}, true},
		{"whitespace only", ConsolidateConfig{Skill: "  ", Instructions: "\n\t"}, false},
		{"disabled flag, whitespace fields", ConsolidateConfig{Enabled: Bool(false), Skill: "  "}, false},
		// enabled: false is a hard off switch, skill or instructions notwithstanding.
		{"disabled flag beats skill", ConsolidateConfig{Enabled: Bool(false), Skill: "/code-quality"}, false},
		{"disabled flag beats instructions", ConsolidateConfig{Enabled: Bool(false), Instructions: "one HTTP client"}, false},
	}
	for _, tt := range tests {
		if got := tt.cfg.Active(); got != tt.want {
			t.Errorf("%s: Active() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestConsolidateDefaultsOff verifies the consolidation pass is opt-in: it
// refactors already-committed, already-reviewed code, so it must never turn itself
// on for a project that didn't ask for it.
func TestConsolidateDefaultsOff(t *testing.T) {
	if Default().Consolidate.Active() {
		t.Error("consolidation must be off by default")
	}
	dir := t.TempDir()
	loaded, err := Load(dir) // no config file at all
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Consolidate.Active() {
		t.Error("consolidation must be off when there is no config file")
	}
}

func TestConsolidateConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{Consolidate: ConsolidateConfig{Enabled: Bool(true), Skill: "/code-quality", Instructions: "one HTTP client only"}}
	if err := Save(dir, cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Consolidate.Enabled == nil || !*loaded.Consolidate.Enabled {
		t.Error("expected Enabled to round-trip")
	}
	if loaded.Consolidate.Skill != "/code-quality" {
		t.Errorf("expected skill to round-trip, got %q", loaded.Consolidate.Skill)
	}
	if loaded.Consolidate.Instructions != "one HTTP client only" {
		t.Errorf("expected instructions to round-trip, got %q", loaded.Consolidate.Instructions)
	}
}

// TestEnabledFalseOverridesSkillFromYAML pins the hard-switch semantics down on
// the path a project actually uses: a config file. Someone who turns the review
// or the consolidation pass off but leaves the skill and instructions in place —
// to keep them around for later — must get no agent, not an agent driven by the
// leftover config.
func TestEnabledFalseOverridesSkillFromYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".chief"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yaml := `review:
  enabled: false
  skill: "/code-quality"
  instructions: "watch for N+1 queries"
consolidate:
  enabled: false
  skill: "/code-quality"
  instructions: "one HTTP client only"
`
	if err := os.WriteFile(filepath.Join(dir, configFile), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Review.Active() {
		t.Error("review: enabled: false must keep the review off despite skill and instructions")
	}
	if loaded.Consolidate.Active() {
		t.Error("consolidate: enabled: false must keep the pass off despite skill and instructions")
	}
	// The fields themselves still round-trip, so flipping enabled back to true
	// restores the configured review rather than the bare baseline.
	if loaded.Review.Skill != "/code-quality" {
		t.Errorf("expected the skill to survive being disabled, got %q", loaded.Review.Skill)
	}
}

// TestSkillWithoutEnabledKeyStillActivates verifies the omitted-key case keeps
// working: a config that only sets a skill (no `enabled` at all) still runs.
func TestSkillWithoutEnabledKeyStillActivates(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".chief"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yaml := `review:
  skill: "/code-quality"
consolidate:
  instructions: "one HTTP client only"
`
	if err := os.WriteFile(filepath.Join(dir, configFile), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if !loaded.Review.Active() {
		t.Error("a skill with no enabled key must still enable the review")
	}
	if !loaded.Consolidate.Active() {
		t.Error("instructions with no enabled key must still enable the pass")
	}
}

// TestSaveOmitsUnsetEnabled verifies a config that never touched `enabled`
// doesn't gain an `enabled: null` line when it is written back out, which would
// look like an off switch to anyone reading the file.
func TestSaveOmitsUnsetEnabled(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, &Config{Review: ReviewConfig{Skill: "/code-quality"}}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, configFile))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(data), "enabled: null") {
		t.Errorf("unset enabled must not be written out:\n%s", string(data))
	}
}

// TestPhaseModelsFromYAML verifies both phases pick their model up from the
// config file, independently of each other and of the build agent's model.
func TestPhaseModelsFromYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".chief"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yaml := `agent:
  model: opus
review:
  enabled: true
  model: haiku
consolidate:
  enabled: true
  model: opus
`
	if err := os.WriteFile(filepath.Join(dir, configFile), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Review.Model != "haiku" {
		t.Errorf("expected review.model %q, got %q", "haiku", loaded.Review.Model)
	}
	if loaded.Consolidate.Model != "opus" {
		t.Errorf("expected consolidate.model %q, got %q", "opus", loaded.Consolidate.Model)
	}
	if loaded.Agent.Model != "opus" {
		t.Errorf("expected agent.model to stay untouched, got %q", loaded.Agent.Model)
	}
}

// TestPhaseModelsUnsetByDefault pins the empty default: chief resolves the
// Sonnet default for the review and consolidation agents when it spawns them, so
// the config itself must stay empty — otherwise the file would claim a model the
// user never chose.
func TestPhaseModelsUnsetByDefault(t *testing.T) {
	loaded, err := Load(t.TempDir()) // no config file at all
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Review.Model != "" {
		t.Errorf("expected review.model to be unset, got %q", loaded.Review.Model)
	}
	if loaded.Consolidate.Model != "" {
		t.Errorf("expected consolidate.model to be unset, got %q", loaded.Consolidate.Model)
	}
}

// TestPhaseModelRoundTrip verifies a configured model survives Save/Load, and an
// unconfigured one leaves no `model:` line behind that would read as a choice.
func TestPhaseModelRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		Review:      ReviewConfig{Enabled: Bool(true), Model: "haiku"},
		Consolidate: ConsolidateConfig{Enabled: Bool(true), Model: "opus"},
	}
	if err := Save(dir, cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Review.Model != "haiku" {
		t.Errorf("expected review.model to round-trip, got %q", loaded.Review.Model)
	}
	if loaded.Consolidate.Model != "opus" {
		t.Errorf("expected consolidate.model to round-trip, got %q", loaded.Consolidate.Model)
	}

	bare := t.TempDir()
	if err := Save(bare, &Config{Review: ReviewConfig{Skill: "/code-quality"}}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(bare, configFile))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var raw map[string]map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse saved config: %v", err)
	}
	for _, section := range []string{"review", "consolidate"} {
		if _, ok := raw[section]["model"]; ok {
			t.Errorf("unset %s.model must not be written out:\n%s", section, string(data))
		}
	}
}

func TestExists(t *testing.T) {
	dir := t.TempDir()

	if Exists(dir) {
		t.Error("expected Exists to return false for missing config")
	}

	// Create the config
	chiefDir := filepath.Join(dir, ".chief")
	if err := os.MkdirAll(chiefDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chiefDir, "config.yaml"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !Exists(dir) {
		t.Error("expected Exists to return true for existing config")
	}
}
