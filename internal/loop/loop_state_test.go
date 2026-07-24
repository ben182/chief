package loop

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoop_PauseResume verifies the pause flag transitions reported by
// IsPaused across Pause/Resume.
func TestLoop_PauseResume(t *testing.T) {
	l := NewLoop("/test/prd.json", "test", 5, testProvider)

	if l.IsPaused() {
		t.Error("expected a fresh loop to not be paused")
	}
	l.Pause()
	if !l.IsPaused() {
		t.Error("expected IsPaused to be true after Pause")
	}
	l.Resume()
	if l.IsPaused() {
		t.Error("expected IsPaused to be false after Resume")
	}
}

// TestLoop_IsStopped verifies IsStopped reflects the stopped flag set by Stop.
func TestLoop_IsStopped(t *testing.T) {
	l := NewLoop("/test/prd.json", "test", 5, testProvider)

	if l.IsStopped() {
		t.Error("expected a fresh loop to not be stopped")
	}
	l.Stop()
	if !l.IsStopped() {
		t.Error("expected IsStopped to be true after Stop")
	}
}

// TestLoop_IsRunning verifies IsRunning is false until an agent process exists.
func TestLoop_IsRunning(t *testing.T) {
	l := NewLoop("/test/prd.json", "test", 5, testProvider)
	if l.IsRunning() {
		t.Error("expected IsRunning to be false with no agent process")
	}
}

// TestNewLoopWithEmbeddedPrompt verifies the constructor wires up an empty
// prompt and installs a per-PRD prompt builder that inlines the next story.
func TestNewLoopWithEmbeddedPrompt(t *testing.T) {
	dir := t.TempDir()
	prdPath := createTestPRD(t, dir, false) // US-001, not complete

	l := NewLoopWithEmbeddedPrompt(prdPath, 5, testProvider)

	if l.prdPath != prdPath {
		t.Errorf("expected prdPath %q, got %q", prdPath, l.prdPath)
	}
	if l.prompt != "" {
		t.Errorf("expected empty initial prompt, got %q", l.prompt)
	}
	if l.buildPrompt == nil {
		t.Fatal("expected a prompt builder to be installed")
	}

	prompt, storyID, storyTitle, err := l.buildPrompt()
	if err != nil {
		t.Fatalf("buildPrompt returned error: %v", err)
	}
	if storyID != "US-001" {
		t.Errorf("expected next story US-001, got %q", storyID)
	}
	if storyTitle == "" {
		t.Error("expected a non-empty story title")
	}
	if prompt == "" {
		t.Error("expected a non-empty built prompt")
	}
}

// TestNewLoopWithEmbeddedPrompt_AllComplete verifies the installed builder
// reports an error when there is no actionable story left, which is how Run
// detects completion.
func TestNewLoopWithEmbeddedPrompt_AllComplete(t *testing.T) {
	dir := t.TempDir()
	prdPath := filepath.Join(dir, "prd.md")
	// A PRD whose only story is already done leaves nothing actionable.
	md := "# Test Project\n\nDesc\n\n### US-001: Story One\n**Status:** done\n- [x] works\n"
	if err := os.WriteFile(prdPath, []byte(md), 0644); err != nil {
		t.Fatalf("write prd: %v", err)
	}

	l := NewLoopWithEmbeddedPrompt(prdPath, 5, testProvider)
	if _, _, _, err := l.buildPrompt(); err == nil {
		t.Error("expected an error when all stories are complete")
	}
}
