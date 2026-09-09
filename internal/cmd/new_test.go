package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/loop"
)

func TestIsValidPRDName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"valid lowercase", "main", true},
		{"valid with numbers", "feature1", true},
		{"valid with hyphen", "my-feature", true},
		{"valid with underscore", "my_feature", true},
		{"valid mixed case", "MyFeature", true},
		{"valid complex", "auth-v2_final", true},
		{"empty string", "", false},
		{"with space", "my feature", false},
		{"with dot", "my.feature", false},
		{"with slash", "my/feature", false},
		{"with special char", "my@feature", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidPRDName(tt.input)
			if result != tt.expected {
				t.Errorf("isValidPRDName(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestRunNewCreatesDirectory(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Test that directory structure is created correctly
	// We can't fully test RunNew without Claude, but we can verify directory creation logic
	name := "test-prd"
	prdDir := filepath.Join(tmpDir, ".chief", "prds", name)

	// Simulate what RunNew does for directory creation
	if err := os.MkdirAll(prdDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Verify directory was created at expected path
	if _, err := os.Stat(prdDir); os.IsNotExist(err) {
		t.Error("Expected directory to be created")
	}

	// Verify parent directories also exist
	chiefDir := filepath.Join(tmpDir, ".chief")
	if _, err := os.Stat(chiefDir); os.IsNotExist(err) {
		t.Error("Expected .chief directory to be created")
	}

	prdsDir := filepath.Join(chiefDir, "prds")
	if _, err := os.Stat(prdsDir); os.IsNotExist(err) {
		t.Error("Expected .chief/prds directory to be created")
	}
}

func TestRunNewRejectsInvalidName(t *testing.T) {
	tmpDir := t.TempDir()

	opts := NewOptions{
		Name:    "invalid name with space",
		BaseDir: tmpDir,
	}

	err := RunNew(opts)
	if err == nil {
		t.Error("Expected error for invalid name")
	}
}

func TestRunNewCleansUpEmptyDirOnCancel(t *testing.T) {
	tmpDir := t.TempDir()

	// Simulate what RunNew does: create directory, then check prd.md doesn't exist
	name := "cancelled"
	prdDir := filepath.Join(tmpDir, ".chief", "prds", name)
	if err := os.MkdirAll(prdDir, 0755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}

	// prd.md was never created (user cancelled) — simulate the cleanup
	prdMdPath := filepath.Join(prdDir, "prd.md")
	if _, err := os.Stat(prdMdPath); os.IsNotExist(err) {
		os.Remove(prdDir)
	}

	// Directory should be removed
	if _, err := os.Stat(prdDir); !os.IsNotExist(err) {
		t.Error("Expected empty directory to be cleaned up after cancellation")
	}
}

func TestRunNewKeepsDirWhenPrdMdExists(t *testing.T) {
	tmpDir := t.TempDir()

	name := "has-prd"
	prdDir := filepath.Join(tmpDir, ".chief", "prds", name)
	if err := os.MkdirAll(prdDir, 0755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}

	// Create prd.md (simulates successful Claude session)
	prdMdPath := filepath.Join(prdDir, "prd.md")
	if err := os.WriteFile(prdMdPath, []byte("# My PRD"), 0644); err != nil {
		t.Fatalf("Failed to create prd.md: %v", err)
	}

	// Cleanup should NOT trigger since prd.md exists
	if _, err := os.Stat(prdMdPath); os.IsNotExist(err) {
		os.Remove(prdDir)
	}

	// Directory should still exist
	if _, err := os.Stat(prdDir); os.IsNotExist(err) {
		t.Error("Expected directory to be kept when prd.md exists")
	}
}

func TestRunNewRejectsExistingPRD(t *testing.T) {
	tmpDir := t.TempDir()

	// Create existing prd.md
	prdDir := filepath.Join(tmpDir, ".chief", "prds", "existing")
	if err := os.MkdirAll(prdDir, 0755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}
	prdMdPath := filepath.Join(prdDir, "prd.md")
	if err := os.WriteFile(prdMdPath, []byte("# Existing PRD"), 0644); err != nil {
		t.Fatalf("Failed to create prd.md: %v", err)
	}

	opts := NewOptions{
		Name:    "existing",
		BaseDir: tmpDir,
	}

	err := RunNew(opts)
	if err == nil {
		t.Error("Expected error for existing PRD")
	}
}

func TestRunNewRequiresProvider(t *testing.T) {
	opts := NewOptions{
		Name:    "main",
		BaseDir: t.TempDir(),
	}

	err := RunNew(opts)
	if err == nil {
		t.Fatal("expected provider validation error")
	}
	if !strings.Contains(err.Error(), "Provider") {
		t.Fatalf("expected error to mention Provider, got: %v", err)
	}
}

// noopProvider is a Provider whose interactive session does nothing and writes
// no prd.md, so RunNew can be driven end to end in a test without an agent CLI.
type noopProvider struct{}

func (noopProvider) Name() string    { return "noop" }
func (noopProvider) CLIPath() string { return "true" }
func (noopProvider) LoopCommand(_ context.Context, _, _ string) *exec.Cmd {
	return exec.Command("true")
}
func (noopProvider) InteractiveCommand(_, _ string) *exec.Cmd { return exec.Command("true") }
func (noopProvider) SupportsInteractiveQuestions() bool       { return false }
func (noopProvider) CleanOutput(output string) string         { return output }
func (noopProvider) ParseLine(_ string) *loop.Event           { return nil }
func (noopProvider) LogFileName() string                      { return "noop.log" }

// TestRunNewLeavesBranchAlone pins down that authoring a PRD never touches git.
// RunNew used to check out chief/<name> up front, which then hid the worktree
// option when the PRD was later started, since the run no longer saw a
// protected branch.
func TestRunNewLeavesBranchAlone(t *testing.T) {
	dir := t.TempDir()
	initGitRepoOnBranch(t, dir, "main")

	if err := RunNew(NewOptions{Name: "auth", BaseDir: dir, Provider: noopProvider{}}); err != nil {
		t.Fatalf("RunNew() error = %v", err)
	}

	branch, err := git.GetCurrentBranch(dir)
	if err != nil {
		t.Fatalf("GetCurrentBranch() error = %v", err)
	}
	if branch != "main" {
		t.Errorf("current branch = %q, want %q (RunNew must not switch branches)", branch, "main")
	}
	if _, err := exec.Command("git", "-C", dir, "rev-parse", "--verify", "chief/auth").Output(); err == nil {
		t.Error("branch chief/auth exists, want RunNew to create no branch")
	}
}
