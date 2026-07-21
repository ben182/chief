package summary

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minicodemonkey/chief/internal/loop"
)

// fakeProvider is a loop.Provider whose loop command writes a fixed file (to
// stand in for the agent writing SUMMARY.md), or fails without writing.
type fakeProvider struct {
	writePath string // where the "agent" writes the summary
	content   string
	fail      bool // exit non-zero and write nothing
}

func (f *fakeProvider) Name() string    { return "Fake" }
func (f *fakeProvider) CLIPath() string { return "fake" }
func (f *fakeProvider) LoopCommand(ctx context.Context, prompt, workDir string) *exec.Cmd {
	if f.fail {
		return exec.CommandContext(ctx, "sh", "-c", "exit 1")
	}
	script := fmt.Sprintf("mkdir -p %q && printf '%%s' %q > %q",
		filepath.Dir(f.writePath), f.content, f.writePath)
	return exec.CommandContext(ctx, "sh", "-c", script)
}
func (f *fakeProvider) InteractiveCommand(workDir, prompt string) *exec.Cmd { return nil }
func (f *fakeProvider) SupportsInteractiveQuestions() bool                  { return false }
func (f *fakeProvider) CleanOutput(output string) string                    { return output }
func (f *fakeProvider) ParseLine(line string) *loop.Event                   { return nil }
func (f *fakeProvider) LogFileName() string                                 { return "fake.log" }

func initRepoWithBranch(t *testing.T) (dir, branch string) {
	t.Helper()
	dir = t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, string(out))
		}
	}
	run("init")
	run("config", "user.email", "t@t.com")
	run("config", "user.name", "T")
	run("checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# t\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")

	branch = "chief/feature"
	run("checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "feat: S1 - add feature")
	return dir, branch
}

func TestGenerate_WritesAndCommits(t *testing.T) {
	dir, branch := initRepoWithBranch(t)
	prdDir := filepath.Join(dir, ".chief", "prds", "default")
	now := time.Date(2026, 7, 21, 14, 32, 5, 0, time.UTC)
	summaryPath := filepath.Join(prdDir, FileNameFor(now))

	if base := filepath.Base(summaryPath); base != "SUMMARY-2026-07-21-143205.md" {
		t.Fatalf("unexpected timestamped name %q", base)
	}

	provider := &fakeProvider{writePath: summaryPath, content: "# Run Summary"}

	res, err := generateAt(context.Background(), provider, dir, prdDir, branch, []string{"S2 - parked"}, now)
	if err != nil {
		t.Fatalf("generateAt: %v", err)
	}
	if res.Path != summaryPath {
		t.Errorf("Path = %q, want %q", res.Path, summaryPath)
	}
	if !res.Committed {
		t.Error("expected Committed = true")
	}
	if _, err := os.Stat(summaryPath); err != nil {
		t.Fatalf("summary file missing: %v", err)
	}

	// The summary commit must exist on the branch.
	cmd := exec.Command("git", "log", "-1", "--format=%s")
	cmd.Dir = dir
	out, _ := cmd.Output()
	if !strings.Contains(string(out), "run summary") {
		t.Errorf("last commit subject = %q, want a run-summary commit", strings.TrimSpace(string(out)))
	}
}

func TestGenerate_NothingToSummarize(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, string(out))
		}
	}
	run("init")
	run("config", "user.email", "t@t.com")
	run("config", "user.name", "T")
	run("checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# t\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
	// Branch with no commits ahead of main.
	run("checkout", "-b", "chief/empty")

	prdDir := filepath.Join(dir, ".chief", "prds", "default")
	now := time.Date(2026, 7, 21, 14, 32, 5, 0, time.UTC)
	provider := &fakeProvider{writePath: filepath.Join(prdDir, FileNameFor(now)), content: "x"}

	_, err := generateAt(context.Background(), provider, dir, prdDir, "chief/empty", nil, now)
	if !errors.Is(err, ErrNothingToSummarize) {
		t.Fatalf("expected ErrNothingToSummarize, got %v", err)
	}
}

func TestGenerate_AgentFailsWithoutFile(t *testing.T) {
	dir, branch := initRepoWithBranch(t)
	prdDir := filepath.Join(dir, ".chief", "prds", "default")
	now := time.Date(2026, 7, 21, 14, 32, 5, 0, time.UTC)
	provider := &fakeProvider{writePath: filepath.Join(prdDir, FileNameFor(now)), content: "x", fail: true}

	_, err := generateAt(context.Background(), provider, dir, prdDir, branch, nil, now)
	if err == nil {
		t.Fatal("expected an error when the agent fails and writes no file")
	}
	if errors.Is(err, ErrNothingToSummarize) {
		t.Fatalf("wrong error class: %v", err)
	}
}
