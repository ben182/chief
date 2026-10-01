package loop

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ben182/chief/internal/prd"
)

func commitStoryFile(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", name}, {"commit", "-m", name}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out[:len(out)-1])
}

// The next morning's session continues last night's run: same start commit,
// last night's figures carried along.
func TestResumeOrBeginRunContinuesOpenRun(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	prdPath := filepath.Join(dir, "prd.md")

	first, prior := resumeOrBeginRun(prdPath, dir)
	if first == "" || prior != (prd.RunSession{}) {
		t.Fatalf("first start = %q, %+v; want HEAD and nothing prior", first, prior)
	}
	if err := prd.RecordRunSession(prd.ProgressPath(prdPath), prd.RunSession{ID: "night", Duration: 3 * time.Hour, Cost: 40}); err != nil {
		t.Fatal(err)
	}
	commitStoryFile(t, dir, "story-1")

	ref, prior := resumeOrBeginRun(prdPath, dir)
	if ref != first {
		t.Errorf("resumed start ref = %s, want the run's %s", ref, first)
	}
	if prior.Duration != 3*time.Hour || prior.Cost != 40 {
		t.Errorf("prior = %+v, want last night's session", prior)
	}
}

// A finished run is not continued: the next start begins at HEAD.
func TestResumeOrBeginRunAfterFinishedRun(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	prdPath := filepath.Join(dir, "prd.md")

	resumeOrBeginRun(prdPath, dir)
	head := commitStoryFile(t, dir, "story-1")
	if err := prd.EndRun(prd.ProgressPath(prdPath)); err != nil {
		t.Fatal(err)
	}

	ref, prior := resumeOrBeginRun(prdPath, dir)
	if ref != head || prior != (prd.RunSession{}) {
		t.Errorf("start after a finished run = %s, %+v; want HEAD %s and a fresh run", ref, prior, head)
	}
}

// When history was rewritten under an open run, its start commit says nothing
// about the branch any more, and a new run begins.
func TestResumeOrBeginRunStartRefGone(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	prdPath := filepath.Join(dir, "prd.md")
	if err := prd.BeginRun(prd.ProgressPath(prdPath), "0123456789abcdef0123456789abcdef01234567"); err != nil {
		t.Fatal(err)
	}
	head := commitStoryFile(t, dir, "story-1")

	if ref, _ := resumeOrBeginRun(prdPath, dir); ref != head {
		t.Errorf("start ref = %s, want HEAD %s", ref, head)
	}
}

// waitForState waits until the PRD's loop has left the running state.
func waitForState(t *testing.T, m *Manager, name string) LoopState {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if state, _, _ := m.GetState(name); state != LoopStateRunning {
			m.wg.Wait()
			return state
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("loop did not stop in time")
	return 0
}

// Two chief sessions, one run: the evening session stops short, the morning one
// finishes it, and the totals it reports carry the evening's share.
func TestRunSpansSessions(t *testing.T) {
	tmpDir := t.TempDir()
	gitInit(t, tmpDir)
	prdDir := filepath.Join(tmpDir, "nightly")
	if err := os.MkdirAll(prdDir, 0755); err != nil {
		t.Fatal(err)
	}
	prdPath := filepath.Join(prdDir, "prd.md")
	write := func(status string) {
		md := "# Test Project\n\nDesc\n\n### US-001: Story One\n**Status:** " + status + "\n- [ ] works\n"
		if err := os.WriteFile(prdPath, []byte(md), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("todo")

	// An agent that spends something and gets nowhere: the run ends on its
	// iteration budget with the story still open.
	script := filepath.Join(tmpDir, "mock-claude")
	body := "#!/bin/bash\nsleep 0.2\n" +
		`echo '{"type":"result","subtype":"success","total_cost_usd":2.5}'` + "\n"
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}

	evening := NewManager(1, &mockProvider{cliPath: script})
	evening.SetBaseDir(tmpDir)
	go func() {
		for range evening.Events() {
		}
	}()
	if err := evening.Register("nightly", prdPath); err != nil {
		t.Fatal(err)
	}
	if err := evening.Start("nightly"); err != nil {
		t.Fatal(err)
	}
	if state := waitForState(t, evening, "nightly"); state == LoopStateComplete {
		t.Fatal("the evening session should not finish the run")
	}
	eveningRef := evening.GetInstance("nightly").StartRef
	eveningTotals := evening.RunTotals("nightly")
	if eveningTotals.Duration <= 0 || eveningTotals.Cost != 2.5 {
		t.Fatalf("evening session spent no time: %+v", eveningTotals)
	}

	// Overnight the story got done and committed; the morning is a new process.
	commitStoryFile(t, tmpDir, "story-1")
	write("done")

	morning := NewManager(1, &mockProvider{cliPath: script})
	morning.SetBaseDir(tmpDir)
	go func() {
		for range morning.Events() {
		}
	}()
	if err := morning.Register("nightly", prdPath); err != nil {
		t.Fatal(err)
	}
	if err := morning.Start("nightly"); err != nil {
		t.Fatal(err)
	}
	inst := morning.GetInstance("nightly")
	if inst.StartRef != eveningRef {
		t.Errorf("morning start ref = %s, want the run's %s", inst.StartRef, eveningRef)
	}
	if state := waitForState(t, morning, "nightly"); state != LoopStateComplete {
		t.Fatalf("morning session ended %v, want complete", state)
	}
	total := morning.RunTotals("nightly")
	// progress.md keeps milliseconds, so that is the precision the evening returns in.
	if total.Duration < eveningTotals.Duration.Truncate(time.Millisecond) {
		t.Errorf("run duration %v is less than the evening session's %v", total.Duration, eveningTotals.Duration)
	}
	if total.Cost != eveningTotals.Cost {
		t.Errorf("run cost = %v, want the evening's %v (the morning spent nothing)", total.Cost, eveningTotals.Cost)
	}

	// The run is over: whatever starts next is a new one.
	if _, open, _ := prd.OpenRun(prd.ProgressPath(prdPath)); open {
		t.Error("a finished run is still open in progress.md")
	}
}
