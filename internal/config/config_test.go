package config

import (
	"os"
	"path/filepath"
	"testing"
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
		{"enabled flag only", ReviewConfig{Enabled: true}, true},
		{"skill only", ReviewConfig{Skill: "/code-quality"}, true},
		{"instructions only", ReviewConfig{Instructions: "watch for N+1"}, true},
		{"both", ReviewConfig{Skill: "/cq", Instructions: "x"}, true},
		{"whitespace only", ReviewConfig{Skill: "  ", Instructions: "\n\t"}, false},
		{"disabled flag, whitespace fields", ReviewConfig{Enabled: false, Skill: "  "}, false},
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
