//go:build !windows

package loop

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// An agent that ends its turn while something it started still runs — recap's
// build agent backgrounded `swift build` and ended the turn to wait for it —
// used to leave that process behind. It held SwiftPM's lock, and the next
// eleven iterations waited on it. The iteration now takes its process group
// down with it however the agent ended.
func TestLoop_IterationEndReapsWhatTheAgentLeftRunning(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "bg.pid")
	scriptPath := filepath.Join(dir, "mock-claude")
	script := "#!/bin/bash\n" +
		"sleep 30 >/dev/null 2>&1 &\n" +
		"echo $! > " + pidFile + "\n" +
		`echo '{"type":"assistant","message":{"content":[{"type":"text","text":"The build is still running, I will wait for it."}]}}'` + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil { //nolint:gosec // a test script must be executable
		t.Fatal(err)
	}

	l := NewLoopWithWorkDir("/test/prd.json", dir, "test", 5, &mockProvider{cliPath: scriptPath})
	l.iteration = 1
	go func() {
		for range l.Events() {
		}
	}()
	_ = l.runIteration(context.Background(), modeBuild)

	data, err := os.ReadFile(pidFile) //nolint:gosec // the test's own file
	if err != nil {
		t.Fatalf("the mock agent did not start its background process: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

	deadline := time.Now().Add(2 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatal("the process the agent left running survived the iteration")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
