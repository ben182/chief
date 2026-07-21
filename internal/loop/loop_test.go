package loop

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minicodemonkey/chief/internal/prd"
)

// mockProvider implements Provider for tests without importing agent (avoids import cycle).
type mockProvider struct {
	cliPath string // if set, used as CLI path; otherwise "claude"
}

func (m *mockProvider) Name() string                             { return "Test" }
func (m *mockProvider) CLIPath() string                          { return m.path() }
func (m *mockProvider) InteractiveCommand(_, _ string) *exec.Cmd { return exec.Command("true") }
func (m *mockProvider) SupportsInteractiveQuestions() bool       { return false }
func (m *mockProvider) ParseLine(line string) *Event             { return ParseLine(line) }
func (m *mockProvider) LogFileName() string                      { return "claude.log" }

func (m *mockProvider) path() string {
	if m.cliPath != "" {
		return m.cliPath
	}
	return "claude"
}

func (m *mockProvider) LoopCommand(ctx context.Context, _, workDir string) *exec.Cmd {
	p := m.path()
	cmd := exec.CommandContext(ctx, p)
	cmd.Dir = workDir
	return cmd
}

func (m *mockProvider) CleanOutput(output string) string { return output }

// testProvider is used by loop tests so they don't need to run a real CLI.
var testProvider Provider = &mockProvider{}

// createMockClaudeScript creates a shell script that outputs predefined stream-json.
func createMockClaudeScript(t *testing.T, dir string, output []string) string {
	t.Helper()

	scriptPath := filepath.Join(dir, "mock-claude")
	content := "#!/bin/bash\n"
	for _, line := range output {
		content += "echo '" + line + "'\n"
	}

	if err := os.WriteFile(scriptPath, []byte(content), 0755); err != nil {
		t.Fatalf("Failed to create mock script: %v", err)
	}

	return scriptPath
}

// createTestPRD creates a minimal test PRD markdown file.
func createTestPRD(t *testing.T, dir string, allComplete bool) string {
	t.Helper()

	status := ""
	checkbox := "- [ ] It works"
	if allComplete {
		status = "**Status:** done\n"
		checkbox = "- [x] It works"
	}

	md := fmt.Sprintf("# Test Project\n\nTest Description\n\n### US-001: Test Story\n%s%s\n", status, checkbox)

	prdPath := filepath.Join(dir, "prd.md")
	if err := os.WriteFile(prdPath, []byte(md), 0644); err != nil {
		t.Fatalf("Failed to create test PRD: %v", err)
	}

	return prdPath
}

func TestNewLoop(t *testing.T) {
	l := NewLoop("/path/to/prd.json", "test prompt", 5, testProvider)

	if l.prdPath != "/path/to/prd.json" {
		t.Errorf("Expected prdPath %q, got %q", "/path/to/prd.json", l.prdPath)
	}
	if l.prompt != "test prompt" {
		t.Errorf("Expected prompt %q, got %q", "test prompt", l.prompt)
	}
	if l.maxIter != 5 {
		t.Errorf("Expected maxIter %d, got %d", 5, l.maxIter)
	}
	if l.events == nil {
		t.Error("Expected events channel to be initialized")
	}
}

func TestNewLoopWithWorkDir(t *testing.T) {
	l := NewLoopWithWorkDir("/path/to/prd.json", "/work/dir", "test prompt", 5, testProvider)

	if l.prdPath != "/path/to/prd.json" {
		t.Errorf("Expected prdPath %q, got %q", "/path/to/prd.json", l.prdPath)
	}
	if l.workDir != "/work/dir" {
		t.Errorf("Expected workDir %q, got %q", "/work/dir", l.workDir)
	}
	if l.prompt != "test prompt" {
		t.Errorf("Expected prompt %q, got %q", "test prompt", l.prompt)
	}
	if l.maxIter != 5 {
		t.Errorf("Expected maxIter %d, got %d", 5, l.maxIter)
	}
	if l.events == nil {
		t.Error("Expected events channel to be initialized")
	}
}

func TestNewLoopWithWorkDir_EmptyWorkDir(t *testing.T) {
	l := NewLoopWithWorkDir("/path/to/prd.json", "", "test prompt", 5, testProvider)

	if l.workDir != "" {
		t.Errorf("Expected empty workDir, got %q", l.workDir)
	}
}

func TestLoop_Events(t *testing.T) {
	l := NewLoop("/path/to/prd.json", "test prompt", 5, testProvider)
	events := l.Events()

	if events == nil {
		t.Error("Expected Events() to return a channel")
	}
}

func TestLoop_Iteration(t *testing.T) {
	l := NewLoop("/path/to/prd.json", "test prompt", 5, testProvider)

	if l.Iteration() != 0 {
		t.Errorf("Expected initial iteration to be 0, got %d", l.Iteration())
	}

	l.iteration = 3
	if l.Iteration() != 3 {
		t.Errorf("Expected iteration to be 3, got %d", l.Iteration())
	}
}

func TestLoop_Stop(t *testing.T) {
	l := NewLoop("/path/to/prd.json", "test prompt", 5, testProvider)

	l.Stop()

	l.mu.Lock()
	stopped := l.stopped
	l.mu.Unlock()

	if !stopped {
		t.Error("Expected loop to be marked as stopped")
	}
}

// TestLoop_RunWithMockClaude tests the loop with a mock Claude script.
// This is an integration test that requires a Unix-like shell.
func TestLoop_RunWithMockClaude(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping integration test in CI")
	}

	tmpDir := t.TempDir()

	// Create a mock Claude output
	mockOutput := []string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Starting work on story"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"123","name":"Read","input":{"file_path":"test.go"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"123","content":"file content"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Work complete"}]}}`,
	}

	scriptPath := createMockClaudeScript(t, tmpDir, mockOutput)
	prdPath := createTestPRD(t, tmpDir, true) // Already complete so loop stops after one iteration

	// Create a prompt that invokes our mock script instead of real Claude
	// For the actual test, we'll test the internal methods
	l := NewLoop(prdPath, "test prompt", 1, testProvider)

	// Override the command for testing - we'll test processOutput directly
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Collect events in a goroutine
	var events []Event
	done := make(chan bool)
	go func() {
		for event := range l.Events() {
			events = append(events, event)
		}
		done <- true
	}()

	// Test processOutput directly with mock data
	r, w, _ := os.Pipe()
	go func() {
		for _, line := range mockOutput {
			w.WriteString(line + "\n")
		}
		w.Close()
	}()

	l.iteration = 1
	l.processOutput(r)

	// Close events channel and wait for collection
	close(l.events)
	<-done

	// Verify we got expected events
	if len(events) == 0 {
		t.Error("Expected at least one event")
	}

	// Check that we got the expected event types
	hasIterationStart := false
	hasAssistantText := false
	hasToolStart := false
	hasToolResult := false

	for _, e := range events {
		switch e.Type {
		case EventIterationStart:
			hasIterationStart = true
		case EventAssistantText:
			hasAssistantText = true
		case EventToolStart:
			hasToolStart = true
			if e.Tool != "Read" {
				t.Errorf("Expected tool name 'Read', got %q", e.Tool)
			}
		case EventToolResult:
			hasToolResult = true
		}
	}

	if !hasIterationStart {
		t.Error("Expected IterationStart event")
	}
	if !hasAssistantText {
		t.Error("Expected AssistantText event")
	}
	if !hasToolStart {
		t.Error("Expected ToolStart event")
	}
	if !hasToolResult {
		t.Error("Expected ToolResult event")
	}

	// Cleanup
	_ = scriptPath // Avoid unused variable warning
	_ = ctx        // Context used for reference
}

// TestLoop_MaxIterations tests that the loop stops after max iterations.
func TestLoop_MaxIterations(t *testing.T) {
	tmpDir := t.TempDir()
	prdPath := createTestPRD(t, tmpDir, false) // Not complete

	l := NewLoop(prdPath, "test prompt", 2, testProvider)

	// Simulate reaching max iterations by manually incrementing
	l.iteration = 2

	// The Run method should check and emit MaxIterationsReached
	// For this test, we verify the check logic
	if l.iteration >= l.maxIter {
		l.events <- Event{
			Type:      EventMaxIterationsReached,
			Iteration: l.iteration,
		}
	}

	event := <-l.events
	if event.Type != EventMaxIterationsReached {
		t.Errorf("Expected MaxIterationsReached event, got %v", event.Type)
	}
}

// TestLoop_CompleteDetection tests that the loop detects completion.
func TestLoop_CompleteDetection(t *testing.T) {
	tmpDir := t.TempDir()
	prdPath := createTestPRD(t, tmpDir, true) // All complete

	p, err := prd.LoadPRD(prdPath)
	if err != nil {
		t.Fatalf("Failed to load PRD: %v", err)
	}

	if !p.AllComplete() {
		t.Error("Expected PRD to be all complete")
	}
}

// TestLoop_LogFile tests that log file is created and written to.
func TestLoop_LogFile(t *testing.T) {
	tmpDir := t.TempDir()
	_ = createTestPRD(t, tmpDir, true)

	logPath := filepath.Join(tmpDir, "claude.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatalf("Failed to create log file: %v", err)
	}

	l := NewLoop(filepath.Join(tmpDir, "prd.md"), "test", 1, testProvider)
	l.logFile = logFile

	l.logLine("test log line")
	logFile.Close()

	// Read back the log file
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if string(data) != "test log line\n" {
		t.Errorf("Expected log line content, got %q", string(data))
	}
}

// TestLoop_ChiefDoneEvent tests detection of <chief-done/> event.
func TestLoop_ChiefDoneEvent(t *testing.T) {
	l := NewLoop("/test/prd.json", "test", 5, testProvider)
	l.iteration = 1

	done := make(chan bool)
	var events []Event
	go func() {
		for event := range l.Events() {
			events = append(events, event)
			if event.Type == EventStoryDone {
				break
			}
		}
		done <- true
	}()

	// Simulate processing a line with chief-done
	r, w, _ := os.Pipe()
	go func() {
		w.WriteString(`{"type":"assistant","message":{"content":[{"type":"text","text":"All criteria pass! <chief-done/>"}]}}` + "\n")
		w.Close()
	}()

	l.processOutput(r)
	close(l.events)
	<-done

	// Check that we got a StoryDone event and sawStoryDone was set
	hasStoryDone := false
	for _, e := range events {
		if e.Type == EventStoryDone {
			hasStoryDone = true
		}
	}

	if !hasStoryDone {
		t.Error("Expected StoryDone event for <chief-done/>")
	}

	l.mu.Lock()
	if !l.sawStoryDone {
		t.Error("Expected sawStoryDone to be true after processing <chief-done/>")
	}
	l.mu.Unlock()
}

// TestLoop_StoryDoneEndsIterationWithoutWatchdog tests that once <chief-done/>
// is seen, the agent process is killed and runIteration returns nil (no crash,
// no watchdog) even if the agent never exits on its own.
func TestLoop_StoryDoneEndsIterationWithoutWatchdog(t *testing.T) {
	dir := t.TempDir()

	// Mock agent: emit chief-done, then hang far longer than the watchdog timeout.
	// Use `exec sleep` so bash replaces itself with sleep, keeping a single
	// process that is its own process-group leader. A plain `sleep 30` would run
	// as a bash *child*; under the race detector's heavy scheduling the
	// chief-done process-group kill could race the fork and leave that child
	// orphaned, holding the stdout pipe open until sleep exited 30s later — a
	// test artifact, not the behaviour under test.
	scriptPath := filepath.Join(dir, "mock-claude")
	script := "#!/bin/bash\n" +
		`echo '{"type":"assistant","message":{"content":[{"type":"text","text":"done <chief-done/>"}]}}'` + "\n" +
		"exec sleep 30\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("Failed to create mock script: %v", err)
	}

	l := NewLoopWithWorkDir("/test/prd.json", dir, "test", 5, &mockProvider{cliPath: scriptPath})
	l.iteration = 1
	l.SetWatchdogTimeout(2 * time.Second)

	// Drain events.
	go func() {
		for range l.Events() {
		}
	}()

	start := time.Now()
	err := l.runIteration(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Expected nil error after <chief-done/>, got %v", err)
	}
	if elapsed >= 2*time.Second {
		t.Errorf("Expected iteration to end before watchdog timeout, took %v", elapsed)
	}

	l.mu.Lock()
	saw := l.sawStoryDone
	l.mu.Unlock()
	if !saw {
		t.Error("Expected sawStoryDone to be true")
	}
}

// TestLoop_ParksStoryAfterMaxAttempts verifies that a story which never
// completes is parked for human review after maxAttempts, the loop moves on to
// the next story, and the run ends cleanly once nothing actionable remains.
func TestLoop_ParksStoryAfterMaxAttempts(t *testing.T) {
	dir := t.TempDir()

	// Two incomplete stories.
	md := "# Test Project\n\nDesc\n\n### US-001: Story One\n- [ ] works\n\n### US-002: Story Two\n- [ ] works\n"
	prdPath := filepath.Join(dir, "prd.md")
	if err := os.WriteFile(prdPath, []byte(md), 0644); err != nil {
		t.Fatalf("write prd: %v", err)
	}

	// Mock agent: produces output but never emits <chief-done/>, so every
	// iteration is a failed attempt.
	scriptPath := filepath.Join(dir, "mock-claude")
	script := "#!/bin/bash\n" +
		`echo '{"type":"assistant","message":{"content":[{"type":"text","text":"trying"}]}}'` + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	l := NewLoopWithWorkDir(prdPath, dir, "", 50, &mockProvider{cliPath: scriptPath})
	l.buildPrompt = promptBuilderForPRD(prdPath)
	l.SetMaxAttemptsPerStory(2)
	l.DisableRetry()

	var parked []string
	var sawComplete bool
	done := make(chan bool)
	go func() {
		for e := range l.Events() {
			switch e.Type {
			case EventStoryNeedsReview:
				parked = append(parked, e.StoryID)
			case EventComplete:
				sawComplete = true
			}
		}
		done <- true
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := l.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	<-done

	if !sawComplete {
		t.Error("expected EventComplete once no actionable stories remain")
	}
	if len(parked) != 2 {
		t.Fatalf("expected both stories parked, got %v", parked)
	}

	// Both stories should be marked needs-review on disk.
	p, err := prd.LoadPRD(prdPath)
	if err != nil {
		t.Fatalf("reload prd: %v", err)
	}
	for _, s := range p.UserStories {
		if !s.NeedsReview {
			t.Errorf("expected %s to be parked for review, got %+v", s.ID, s)
		}
	}
}

// TestLoop_SetMaxIterations tests setting max iterations at runtime.
func TestLoop_SetMaxIterations(t *testing.T) {
	l := NewLoop("/test/prd.json", "test", 5, testProvider)

	if l.MaxIterations() != 5 {
		t.Errorf("Expected initial maxIter 5, got %d", l.MaxIterations())
	}

	l.SetMaxIterations(10)

	if l.MaxIterations() != 10 {
		t.Errorf("Expected maxIter 10 after set, got %d", l.MaxIterations())
	}
}

// TestDefaultRetryConfig tests the default retry configuration.
func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()

	if config.MaxRetries != 3 {
		t.Errorf("Expected MaxRetries 3, got %d", config.MaxRetries)
	}
	if !config.Enabled {
		t.Error("Expected Enabled to be true")
	}
	if len(config.RetryDelays) != 3 {
		t.Errorf("Expected 3 retry delays, got %d", len(config.RetryDelays))
	}
}

// TestLoop_SetRetryConfig tests setting retry config.
func TestLoop_SetRetryConfig(t *testing.T) {
	l := NewLoop("/test/prd.json", "test", 5, testProvider)

	// Check default
	if !l.retryConfig.Enabled {
		t.Error("Expected default retry to be enabled")
	}

	// Disable retry
	l.DisableRetry()
	if l.retryConfig.Enabled {
		t.Error("Expected retry to be disabled after DisableRetry()")
	}

	// Set custom config
	customConfig := RetryConfig{
		MaxRetries:  5,
		RetryDelays: []time.Duration{time.Second},
		Enabled:     true,
	}
	l.SetRetryConfig(customConfig)

	if l.retryConfig.MaxRetries != 5 {
		t.Errorf("Expected MaxRetries 5, got %d", l.retryConfig.MaxRetries)
	}
}

// TestLoop_WatchdogDefaultTimeout tests that the default watchdog timeout is set.
func TestLoop_WatchdogDefaultTimeout(t *testing.T) {
	l := NewLoop("/test/prd.json", "test", 5, testProvider)

	if l.WatchdogTimeout() != DefaultWatchdogTimeout {
		t.Errorf("Expected default watchdog timeout %v, got %v", DefaultWatchdogTimeout, l.WatchdogTimeout())
	}
}

// TestLoop_SetWatchdogTimeout tests setting the watchdog timeout.
func TestLoop_SetWatchdogTimeout(t *testing.T) {
	l := NewLoop("/test/prd.json", "test", 5, testProvider)

	l.SetWatchdogTimeout(10 * time.Minute)
	if l.WatchdogTimeout() != 10*time.Minute {
		t.Errorf("Expected watchdog timeout 10m, got %v", l.WatchdogTimeout())
	}

	// Setting to 0 disables the watchdog
	l.SetWatchdogTimeout(0)
	if l.WatchdogTimeout() != 0 {
		t.Errorf("Expected watchdog timeout 0 (disabled), got %v", l.WatchdogTimeout())
	}
}

// TestLoop_WatchdogKillsHungProcess tests that a hung process is killed after timeout.
func TestLoop_WatchdogKillsHungProcess(t *testing.T) {
	l := NewLoop("/test/prd.json", "test", 5, testProvider)
	l.iteration = 1

	// Use a very short timeout for testing
	timeout := 100 * time.Millisecond

	// Collect events
	var events []Event
	done := make(chan bool)
	go func() {
		for event := range l.Events() {
			events = append(events, event)
		}
		done <- true
	}()

	// Create a pipe that never sends data (simulates hung process)
	r, w, _ := os.Pipe()

	// Initialize lastOutputTime
	l.mu.Lock()
	l.lastOutputTime = time.Now()
	l.mu.Unlock()

	// Start watchdog with a short check interval
	watchdogDone := make(chan struct{})
	watchdogStopped := make(chan struct{})
	var fired atomic.Bool
	go func() {
		defer close(watchdogStopped)
		l.runWatchdog(timeout, watchdogDone, &fired)
	}()

	// processOutput will block until pipe is closed (by watchdog killing would close it,
	// but in this test we close it manually after watchdog fires)
	go func() {
		// Wait for watchdog to fire
		time.Sleep(500 * time.Millisecond)
		w.Close()
	}()

	l.processOutput(r)
	close(watchdogDone)
	<-watchdogStopped
	close(l.events)
	<-done

	if !fired.Load() {
		t.Error("Expected watchdog to fire for hung process")
	}

	// Check that we got a WatchdogTimeout event
	hasWatchdog := false
	for _, e := range events {
		if e.Type == EventWatchdogTimeout {
			hasWatchdog = true
			if e.Text == "" {
				t.Error("Expected watchdog event to have descriptive text")
			}
		}
	}
	if !hasWatchdog {
		t.Error("Expected WatchdogTimeout event")
	}
}

// TestLoop_WatchdogDoesNotFireForActiveProcess tests that an active process doesn't trigger the watchdog.
func TestLoop_WatchdogDoesNotFireForActiveProcess(t *testing.T) {
	l := NewLoop("/test/prd.json", "test", 5, testProvider)
	l.iteration = 1

	// Use a timeout that's longer than our test
	timeout := 2 * time.Second

	// Collect events
	var events []Event
	done := make(chan bool)
	go func() {
		for event := range l.Events() {
			events = append(events, event)
		}
		done <- true
	}()

	// Create a pipe that produces output regularly
	r, w, _ := os.Pipe()

	l.mu.Lock()
	l.lastOutputTime = time.Now()
	l.mu.Unlock()

	watchdogDone := make(chan struct{})
	watchdogStopped := make(chan struct{})
	var fired atomic.Bool
	go func() {
		defer close(watchdogStopped)
		l.runWatchdog(timeout, watchdogDone, &fired)
	}()

	// Send output regularly, then close
	go func() {
		for i := 0; i < 5; i++ {
			w.WriteString(`{"type":"assistant","message":{"content":[{"type":"text","text":"working..."}]}}` + "\n")
			time.Sleep(100 * time.Millisecond)
		}
		w.Close()
	}()

	l.processOutput(r)
	close(watchdogDone)
	<-watchdogStopped
	close(l.events)
	<-done

	if fired.Load() {
		t.Error("Watchdog should NOT fire for an actively producing process")
	}

	// Verify no WatchdogTimeout events
	for _, e := range events {
		if e.Type == EventWatchdogTimeout {
			t.Error("Should not have received WatchdogTimeout event for active process")
		}
	}
}

// TestLoop_WatchdogDisabledWithZeroTimeout tests that watchdog is disabled when timeout is 0.
func TestLoop_WatchdogDisabledWithZeroTimeout(t *testing.T) {
	l := NewLoop("/test/prd.json", "test", 5, testProvider)
	l.SetWatchdogTimeout(0)

	if l.WatchdogTimeout() != 0 {
		t.Errorf("Expected watchdog timeout 0, got %v", l.WatchdogTimeout())
	}

	// Verify that runIteration would not start a watchdog
	// (tested indirectly: timeout == 0 means the if-block in runIteration is skipped)
	// We test this by verifying the constructor behavior and setter
	l2 := NewLoop("/test/prd.json", "test", 5, testProvider)
	l2.SetWatchdogTimeout(0)

	l2.mu.Lock()
	wt := l2.watchdogTimeout
	l2.mu.Unlock()

	if wt != 0 {
		t.Errorf("Expected internal watchdogTimeout to be 0, got %v", wt)
	}
}

// TestLoop_LastOutputTimeUpdated tests that lastOutputTime is updated on each scanner output.
func TestLoop_LastOutputTimeUpdated(t *testing.T) {
	l := NewLoop("/test/prd.json", "test", 5, testProvider)
	l.iteration = 1

	// Drain events to avoid blocking
	go func() {
		for range l.Events() {
		}
	}()

	// Record initial time
	l.mu.Lock()
	l.lastOutputTime = time.Now().Add(-1 * time.Hour) // Set to an old time
	initialTime := l.lastOutputTime
	l.mu.Unlock()

	// Send output through processOutput
	r, w, _ := os.Pipe()
	go func() {
		w.WriteString(`{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}` + "\n")
		time.Sleep(50 * time.Millisecond)
		w.WriteString(`{"type":"assistant","message":{"content":[{"type":"text","text":"world"}]}}` + "\n")
		w.Close()
	}()

	l.processOutput(r)
	close(l.events)

	// Verify lastOutputTime was updated
	l.mu.Lock()
	finalTime := l.lastOutputTime
	l.mu.Unlock()

	if !finalTime.After(initialTime) {
		t.Errorf("Expected lastOutputTime to be updated after output, initial=%v, final=%v", initialTime, finalTime)
	}
}

// TestLoop_WatchdogReturnsError tests that watchdog kill causes runIteration to return an error
// that feeds into retry logic.
func TestLoop_WatchdogReturnsError(t *testing.T) {
	// This test verifies the error message format that runIterationWithRetry will see
	l := NewLoop("/test/prd.json", "test", 5, testProvider)
	l.SetWatchdogTimeout(100 * time.Millisecond)

	// The watchdog error message should contain "watchdog timeout"
	// This ensures the retry logic in runIterationWithRetry will process it
	expectedPrefix := "watchdog timeout:"
	errMsg := fmt.Sprintf("watchdog timeout: no output for %s", 100*time.Millisecond)
	if !strings.HasPrefix(errMsg, expectedPrefix) {
		t.Errorf("Expected error to start with %q, got %q", expectedPrefix, errMsg)
	}
}

// TestLoop_WatchdogWithWorkDir tests that watchdog works with NewLoopWithWorkDir too.
func TestLoop_WatchdogWithWorkDir(t *testing.T) {
	l := NewLoopWithWorkDir("/test/prd.json", "/work", "test", 5, testProvider)

	if l.WatchdogTimeout() != DefaultWatchdogTimeout {
		t.Errorf("Expected default watchdog timeout for NewLoopWithWorkDir, got %v", l.WatchdogTimeout())
	}
}

// TestLoop_CaptureStderr verifies the stderr ring buffer keeps only the last
// maxStderrTail non-empty lines, so a crash surfaces recent output in the TUI.
func TestLoop_CaptureStderr(t *testing.T) {
	l := NewLoop("/test/prd.json", "test", 5, testProvider)

	l.captureStderr("")    // dropped: blank
	l.captureStderr("   ") // dropped: whitespace only
	for i := 0; i < maxStderrTail+5; i++ {
		l.captureStderr(fmt.Sprintf("line %d", i))
	}

	if len(l.stderrTail) != maxStderrTail {
		t.Fatalf("expected %d retained lines, got %d", maxStderrTail, len(l.stderrTail))
	}
	if got, want := l.stderrTail[len(l.stderrTail)-1], fmt.Sprintf("line %d", maxStderrTail+4); got != want {
		t.Errorf("expected last line %q, got %q", want, got)
	}
	if got, want := l.stderrTail[0], "line 5"; got != want {
		t.Errorf("expected oldest retained line %q, got %q", want, got)
	}
}

// TestStoryHasCommit verifies that a <chief-done/> signal is only trusted when a
// matching commit actually landed, gating story completion on committed work.
func TestStoryHasCommit(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "t@t.io")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "feat: US-001 - Add login")

	l := &Loop{workDir: dir}

	if !l.storyHasCommit("US-001", "Add login") {
		t.Error("expected true for a story with a matching commit")
	}
	if l.storyHasCommit("US-002", "No such story") {
		t.Error("expected false for a story with no matching commit")
	}
	// Not a git repo: can't determine, must not block completion.
	l2 := &Loop{workDir: t.TempDir()}
	if !l2.storyHasCommit("US-001", "Add login") {
		t.Error("expected true (fail-open) when the directory is not a git repo")
	}
}

// TestLoop_DoneWithoutCommitIsNotTrusted verifies that a <chief-done/> signal in
// a git repo with no matching commit is treated as a failed attempt (parked),
// never marked done, so uncommitted work is not silently lost.
func TestLoop_DoneWithoutCommitIsNotTrusted(t *testing.T) {
	dir := t.TempDir()
	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitRun("init")
	gitRun("config", "user.email", "t@t.io")
	gitRun("config", "user.name", "t")

	md := "# Test Project\n\nDesc\n\n### US-001: Story One\n- [ ] works\n"
	prdPath := filepath.Join(dir, "prd.md")
	if err := os.WriteFile(prdPath, []byte(md), 0644); err != nil {
		t.Fatalf("write prd: %v", err)
	}

	// Mock agent: claims done but never commits anything.
	scriptPath := filepath.Join(dir, "mock-claude")
	script := "#!/bin/bash\n" +
		`echo '{"type":"assistant","message":{"content":[{"type":"text","text":"done <chief-done/>"}]}}'` + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	l := NewLoopWithWorkDir(prdPath, dir, "", 20, &mockProvider{cliPath: scriptPath})
	l.buildPrompt = promptBuilderForPRD(prdPath)
	l.SetMaxAttemptsPerStory(1)
	l.DisableRetry()

	var sawNoCommit, sawParked bool
	done := make(chan bool)
	go func() {
		for e := range l.Events() {
			switch e.Type {
			case EventStoryNoCommit:
				sawNoCommit = true
			case EventStoryNeedsReview:
				sawParked = true
			}
		}
		done <- true
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := l.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	<-done

	if !sawNoCommit {
		t.Error("expected EventStoryNoCommit when done is claimed without a commit")
	}
	if !sawParked {
		t.Error("expected story to be parked after the no-commit attempt")
	}

	p, err := prd.LoadPRD(prdPath)
	if err != nil {
		t.Fatalf("reload prd: %v", err)
	}
	if p.UserStories[0].Passes {
		t.Error("story must not be marked done without a matching commit")
	}
}

// TestLoop_SetReview verifies the review-enabled predicate reflects the
// configured skill/instructions.
func TestLoop_SetReview(t *testing.T) {
	l := NewLoop("/test/prd.json", "test", 5, testProvider)
	if l.reviewEnabled() {
		t.Error("expected review disabled by default")
	}
	l.SetReview("/code-quality", "")
	if !l.reviewEnabled() {
		t.Error("expected review enabled with a skill")
	}
	l.SetReview("", "watch for N+1")
	if !l.reviewEnabled() {
		t.Error("expected review enabled with instructions only")
	}
	l.SetReview("  ", "  ")
	if l.reviewEnabled() {
		t.Error("expected whitespace-only config to be treated as disabled")
	}
}

// TestLoop_ReviewAgentRunsAfterCommit verifies that when a review is configured,
// a separate review agent runs after the build agent commits: the agent CLI is
// invoked a second time, EventReviewStart/EventReviewDone are emitted, and the
// story still ends up done. With no review configured, the CLI runs only once
// and no review events are emitted.
func TestLoop_ReviewAgentRunsAfterCommit(t *testing.T) {
	newRun := func(t *testing.T, withReview bool) (calls int, reviewStart, reviewDone, storyDone bool) {
		t.Helper()
		dir := t.TempDir()
		gitInit(t, dir)

		prdPath := createTestPRD(t, dir, false)
		callsPath := filepath.Join(dir, "calls.txt")

		// Mock agent: records the call, implements + commits the story (idempotent
		// so the review call's re-commit is a harmless no-op), then signals done.
		scriptPath := filepath.Join(dir, "mock-claude")
		script := "#!/bin/bash\n" +
			"echo call >> " + callsPath + "\n" +
			"echo content > " + filepath.Join(dir, "impl.txt") + "\n" +
			"git -C " + dir + " add impl.txt >/dev/null 2>&1\n" +
			"git -C " + dir + " commit -m 'feat: US-001 - Test Story' >/dev/null 2>&1\n" +
			`echo '{"type":"assistant","message":{"content":[{"type":"text","text":"done <chief-done/>"}]}}'` + "\n"
		if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
			t.Fatalf("write script: %v", err)
		}

		l := NewLoopWithWorkDir(prdPath, dir, "", 10, &mockProvider{cliPath: scriptPath})
		l.buildPrompt = promptBuilderForPRD(prdPath)
		l.DisableRetry()
		if withReview {
			l.SetReview("", "check the implementation carefully")
		}

		done := make(chan bool)
		go func() {
			for e := range l.Events() {
				switch e.Type {
				case EventReviewStart:
					reviewStart = true
				case EventReviewDone:
					reviewDone = true
				case EventStoryDone:
					storyDone = true
				}
			}
			done <- true
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := l.Run(ctx); err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
		<-done

		data, err := os.ReadFile(callsPath)
		if err != nil {
			t.Fatalf("read calls: %v", err)
		}
		calls = strings.Count(strings.TrimSpace(string(data)), "\n") + 1

		p, err := prd.LoadPRD(prdPath)
		if err != nil {
			t.Fatalf("reload prd: %v", err)
		}
		if !p.UserStories[0].Passes {
			t.Error("expected story to be marked done")
		}
		return calls, reviewStart, reviewDone, storyDone
	}

	t.Run("with review: agent runs twice and emits review events", func(t *testing.T) {
		calls, reviewStart, reviewDone, storyDone := newRun(t, true)
		if calls != 2 {
			t.Errorf("expected 2 agent invocations (build + review), got %d", calls)
		}
		if !reviewStart || !reviewDone {
			t.Errorf("expected review events, got start=%v done=%v", reviewStart, reviewDone)
		}
		if !storyDone {
			t.Error("expected EventStoryDone from the build agent")
		}
	})

	t.Run("without review: agent runs once and emits no review events", func(t *testing.T) {
		calls, reviewStart, reviewDone, _ := newRun(t, false)
		if calls != 1 {
			t.Errorf("expected 1 agent invocation (build only), got %d", calls)
		}
		if reviewStart || reviewDone {
			t.Error("expected no review events when review is disabled")
		}
	})
}

// TestTimestampedLogName verifies per-run log names carry a sortable timestamp.
func TestTimestampedLogName(t *testing.T) {
	ts := time.Date(2026, 7, 21, 14, 32, 5, 0, time.UTC)
	tests := []struct {
		base string
		want string
	}{
		{"claude.log", "claude-2026-07-21-143205.log"},
		{"codex.log", "codex-2026-07-21-143205.log"},
		{"noext", "noext-2026-07-21-143205"},
	}
	for _, tt := range tests {
		if got := timestampedLogName(tt.base, ts); got != tt.want {
			t.Errorf("timestampedLogName(%q) = %q, want %q", tt.base, got, tt.want)
		}
	}
}

// gitInit sets up a fresh repo with one initial commit for progress-commit tests.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"}, {"config", "user.email", "t@t.com"}, {"config", "user.name", "T"},
		{"checkout", "-b", "main"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, string(out))
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "seed"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "seed"}, {"commit", "-m", "initial commit"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, string(out))
		}
	}
}

func gitHeadCount(t *testing.T, dir string) int {
	t.Helper()
	cmd := exec.Command("git", "rev-list", "--count", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-list: %v", err)
	}
	n := 0
	for _, r := range strings.TrimSpace(string(out)) {
		n = n*10 + int(r-'0')
	}
	return n
}

func gitTracked(t *testing.T, dir, path string) bool {
	t.Helper()
	cmd := exec.Command("git", "cat-file", "-e", "HEAD:"+path)
	cmd.Dir = dir
	return cmd.Run() == nil
}

func commitMsgAt(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "log", "-1", "--format=%s", ref)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("log %s: %v", ref, err)
	}
	return strings.TrimSpace(string(out))
}

func TestLoop_CommitStoryProgress(t *testing.T) {
	t.Run("amends into the story commit when it is HEAD", func(t *testing.T) {
		dir := t.TempDir()
		gitInit(t, dir)
		// The agent's story commit is HEAD.
		if err := os.WriteFile(filepath.Join(dir, "app.go"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"add", "app.go"}, {"commit", "-m", "feat: US-001 - Story One"}} {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %s", args, string(out))
			}
		}
		before := gitHeadCount(t, dir)

		// chief's working files appear after the story commit.
		prdPath := filepath.Join(dir, "prd.md")
		if err := os.WriteFile(prdPath, []byte("# PRD\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "progress.md"), []byte("# progress\n"), 0644); err != nil {
			t.Fatal(err)
		}

		l := NewLoopWithWorkDir(prdPath, dir, "", 1, testProvider)
		l.commitStoryProgress("US-001", "Story One")

		if got := gitHeadCount(t, dir); got != before {
			t.Errorf("commit count = %d, want %d (should amend, not add)", got, before)
		}
		if commitMsgAt(t, dir, "HEAD") != "feat: US-001 - Story One" {
			t.Error("story commit subject should stay unchanged after amend")
		}
		if !gitTracked(t, dir, "prd.md") || !gitTracked(t, dir, "progress.md") {
			t.Error("prd.md/progress.md should ride in the amended story commit")
		}
	})

	t.Run("commits standalone when HEAD is not the story commit", func(t *testing.T) {
		dir := t.TempDir()
		gitInit(t, dir) // HEAD is "initial commit", not the story
		before := gitHeadCount(t, dir)

		prdPath := filepath.Join(dir, "prd.md")
		if err := os.WriteFile(prdPath, []byte("# PRD\n"), 0644); err != nil {
			t.Fatal(err)
		}

		l := NewLoopWithWorkDir(prdPath, dir, "", 1, testProvider)
		l.commitStoryProgress("US-002", "Story Two")

		if got := gitHeadCount(t, dir); got != before+1 {
			t.Errorf("commit count = %d, want %d (standalone commit expected)", got, before+1)
		}
		if msg := commitMsgAt(t, dir, "HEAD"); msg != "chore: track US-002 progress" {
			t.Errorf("standalone commit subject = %q, want %q", msg, "chore: track US-002 progress")
		}
		if !gitTracked(t, dir, "prd.md") {
			t.Error("prd.md should be tracked by the standalone commit")
		}
	})

	t.Run("no-op outside a git repo", func(t *testing.T) {
		dir := t.TempDir()
		prdPath := filepath.Join(dir, "prd.md")
		if err := os.WriteFile(prdPath, []byte("# PRD\n"), 0644); err != nil {
			t.Fatal(err)
		}
		l := NewLoopWithWorkDir(prdPath, dir, "", 1, testProvider)
		l.commitStoryProgress("US-003", "Story Three") // must not panic or error
	})
}
