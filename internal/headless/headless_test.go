package headless

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/loop"
)

// testProvider drives the run from a shell script instead of a real agent CLI,
// the way the loop's own tests do.
type testProvider struct{ script string }

func (p *testProvider) Name() string                             { return "Test" }
func (p *testProvider) CLIPath() string                          { return p.script }
func (p *testProvider) InteractiveCommand(_, _ string) *exec.Cmd { return exec.Command("true") }
func (p *testProvider) SupportsInteractiveQuestions() bool       { return false }
func (p *testProvider) ParseLine(line string) *loop.Event        { return loop.ParseLine(line) }
func (p *testProvider) LogFileName() string                      { return "claude.log" }
func (p *testProvider) CleanOutput(out string) string            { return out }

func (p *testProvider) LoopCommand(ctx context.Context, _, workDir string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, p.script)
	cmd.Dir = workDir
	return cmd
}

// project builds a git repo holding one PRD with a single story, and returns
// the repo root and the PRD's path.
func project(t *testing.T, storyID, title string) (dir, prdPath string) {
	t.Helper()
	dir = t.TempDir()
	gitInit(t, dir)

	prdDir := filepath.Join(dir, ".chief", "prds", "demo")
	if err := os.MkdirAll(prdDir, 0755); err != nil {
		t.Fatal(err)
	}
	md := fmt.Sprintf("# Demo\n\nA demo project\n\n### %s: %s\n\n- [ ] It works\n", storyID, title)
	prdPath = filepath.Join(prdDir, "prd.md")
	if err := os.WriteFile(prdPath, []byte(md), 0644); err != nil {
		t.Fatal(err)
	}
	return dir, prdPath
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"}, {"config", "user.email", "t@t.com"}, {"config", "user.name", "T"},
		{"checkout", "-b", "main"},
	} {
		runGit(t, dir, args...)
	}
	if err := os.WriteFile(filepath.Join(dir, "seed"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "seed")
	runGit(t, dir, "commit", "-m", "initial commit")
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// agentScript writes an executable standing in for the agent CLI. It implements
// the story by writing a file and committing it, then signals <chief-done/>, so
// the loop's commit check is satisfied and the story is marked done.
func agentScript(t *testing.T, dir, commitSubject string, extra ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mock-agent")
	body := "#!/bin/bash\n" +
		strings.Join(extra, "\n") + "\n" +
		"echo impl >> impl.txt\n" +
		"git add impl.txt >/dev/null 2>&1\n" +
		"git commit -m " + shellQuote(commitSubject) + " >/dev/null 2>&1\n" +
		`echo '{"type":"assistant","message":{"content":[{"type":"text","text":"built it <chief-done/>"}]}}'` + "\n"
	if err := os.WriteFile(path, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	_ = dir
	return path
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// run executes a headless run against the project and returns the result and
// the log it wrote.
func run(t *testing.T, opts Options) (Result, string) {
	t.Helper()
	var log bytes.Buffer
	opts.Out = &log
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := Run(ctx, opts)
	if err != nil {
		t.Fatalf("Run: %v\nlog:\n%s", err, log.String())
	}
	return res, log.String()
}

func TestRunBuildsTheStoryAndReportsItDone(t *testing.T) {
	dir, prdPath := project(t, "US-001", "Test Story")
	runGit(t, dir, "checkout", "-b", "work")

	res, log := run(t, Options{
		PRDPath:  prdPath,
		BaseDir:  dir,
		Provider: &testProvider{script: agentScript(t, dir, "feat: demo/US-001 - Test Story")},
		Config:   &config.Config{},
	})

	if !res.Completed {
		t.Errorf("expected the run to complete, got %+v\nlog:\n%s", res, log)
	}
	if res.Passing != 1 || res.Stories != 1 {
		t.Errorf("expected 1/1 stories passing, got %d/%d", res.Passing, res.Stories)
	}
	if res.PRDName != "demo" {
		t.Errorf("PRDName = %q, want %q", res.PRDName, "demo")
	}
	if res.Branch != "work" {
		t.Errorf("Branch = %q, want the checked-out branch %q", res.Branch, "work")
	}
	if len(res.Parked) != 0 {
		t.Errorf("expected no parked stories, got %v", res.Parked)
	}
	for _, want := range []string{"US-001 started", "US-001 done", "all stories resolved", "complete after"} {
		if !strings.Contains(log, want) {
			t.Errorf("log is missing %q:\n%s", want, log)
		}
	}
}

func TestRunOnAProtectedBranchCutsThePRDsOwnBranch(t *testing.T) {
	dir, prdPath := project(t, "US-001", "Test Story")
	// The project stays on main, which is what a freshly cloned checkout on a
	// server is standing on.

	res, log := run(t, Options{
		PRDPath:  prdPath,
		BaseDir:  dir,
		Provider: &testProvider{script: agentScript(t, dir, "feat: demo/US-001 - Test Story")},
		Config:   &config.Config{},
	})

	if res.Branch != "chief/demo" {
		t.Errorf("Branch = %q, want chief/demo\nlog:\n%s", res.Branch, log)
	}
	if got := runGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "chief/demo" {
		t.Errorf("checkout is on %q, want chief/demo", got)
	}
	// main must be exactly where it was: an unattended run committing onto it is
	// the thing this behaviour exists to prevent.
	if got := runGit(t, dir, "log", "--oneline", "main"); strings.Count(got, "\n") != 0 {
		t.Errorf("main gained commits:\n%s", got)
	}
}

func TestRunInAWorktreeRunsSetupAndWorksThere(t *testing.T) {
	dir, prdPath := project(t, "US-001", "Test Story")
	marker := filepath.Join(dir, "setup-ran")

	cfg := &config.Config{}
	cfg.Worktree.Setup = "echo hello-from-setup; echo $CHIEF_BRANCH > " + marker

	res, log := run(t, Options{
		PRDPath:  prdPath,
		BaseDir:  dir,
		Provider: &testProvider{script: agentScript(t, dir, "feat: demo/US-001 - Test Story")},
		Config:   cfg,
		Worktree: true,
	})

	if res.Branch != "chief/demo" {
		t.Errorf("Branch = %q, want chief/demo", res.Branch)
	}
	if res.WorkDir == dir {
		t.Errorf("WorkDir = %q, want a worktree outside the project checkout", res.WorkDir)
	}
	if !git.IsWorktree(res.WorkDir) {
		t.Errorf("WorkDir %q is not a worktree", res.WorkDir)
	}
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("setup did not run: %v\nlog:\n%s", err, log)
	}
	if strings.TrimSpace(string(got)) != "chief/demo" {
		t.Errorf("setup saw CHIEF_BRANCH=%q, want chief/demo", strings.TrimSpace(string(got)))
	}
	if !strings.Contains(log, "running: echo hello-from-setup") {
		t.Errorf("log does not mention the setup command:\n%s", log)
	}
	// The story's commit belongs to the worktree's branch, not to main.
	if !res.Completed {
		t.Errorf("expected the run to complete\nlog:\n%s", log)
	}
}

func TestRunAbortsWhenWorktreeSetupFails(t *testing.T) {
	dir, prdPath := project(t, "US-001", "Test Story")

	cfg := &config.Config{}
	cfg.Worktree.Setup = "echo nope >&2; exit 3"

	var log bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := Run(ctx, Options{
		PRDPath:  prdPath,
		BaseDir:  dir,
		Provider: &testProvider{script: agentScript(t, dir, "feat: demo/US-001 - Test Story")},
		Config:   cfg,
		Worktree: true,
		Out:      &log,
	})
	if err == nil {
		t.Fatalf("expected the run to abort on a failing setup\nlog:\n%s", log.String())
	}
	if !strings.Contains(err.Error(), "setup failed") {
		t.Errorf("error = %v, want it to name the setup failure", err)
	}
}

func TestRunPushesAndSummarisesOnlyWhenConfigured(t *testing.T) {
	// A config with every on-complete action off must leave the branch alone:
	// no summary commit, no push attempt against a remote that isn't there.
	dir, prdPath := project(t, "US-001", "Test Story")
	runGit(t, dir, "checkout", "-b", "work")

	before := runGit(t, dir, "rev-parse", "HEAD")
	res, log := run(t, Options{
		PRDPath:  prdPath,
		BaseDir:  dir,
		Provider: &testProvider{script: agentScript(t, dir, "feat: demo/US-001 - Test Story")},
		Config:   &config.Config{},
	})

	if len(res.Actions) != 0 {
		t.Errorf("expected no post-completion actions, got %v", res.Actions)
	}
	if strings.Contains(log, "push") {
		t.Errorf("log mentions a push that was never configured:\n%s", log)
	}
	after := runGit(t, dir, "rev-parse", "HEAD")
	if before == after {
		t.Error("expected the story's commit on the branch")
	}
}

func TestRunReportsAFailedPushWithoutFailingTheRun(t *testing.T) {
	dir, prdPath := project(t, "US-001", "Test Story")
	runGit(t, dir, "checkout", "-b", "work")

	cfg := &config.Config{}
	cfg.OnComplete.Push = true // there is no remote, so this cannot succeed

	res, log := run(t, Options{
		PRDPath:  prdPath,
		BaseDir:  dir,
		Provider: &testProvider{script: agentScript(t, dir, "feat: demo/US-001 - Test Story")},
		Config:   cfg,
	})

	if !res.Completed {
		t.Errorf("a failed push must not make the run incomplete: %+v", res)
	}
	if res.Actions["push"] == nil {
		t.Errorf("expected the push failure to be reported, got %v", res.Actions)
	}
	if !strings.Contains(log, "push") {
		t.Errorf("log does not mention the push:\n%s", log)
	}
}

func TestRunWithoutCommitsSkipsPostCompletionActions(t *testing.T) {
	dir, prdPath := project(t, "US-001", "Test Story")
	runGit(t, dir, "checkout", "-b", "work")

	// An agent that claims done but commits nothing: the loop keeps retrying it
	// and the run ends with no work to push.
	script := filepath.Join(t.TempDir(), "mock-agent")
	body := "#!/bin/bash\n" +
		`echo '{"type":"assistant","message":{"content":[{"type":"text","text":"all done <chief-done/>"}]}}'` + "\n"
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{}
	cfg.OnComplete.Push = true

	res, log := run(t, Options{
		PRDPath:       prdPath,
		BaseDir:       dir,
		Provider:      &testProvider{script: script},
		Config:        cfg,
		MaxIterations: 2,
		NoRetry:       true,
	})

	if res.Completed {
		t.Errorf("a run that built nothing must not report completion: %+v", res)
	}
	if _, tried := res.Actions["push"]; tried {
		t.Errorf("nothing was committed, so nothing should have been pushed: %v", res.Actions)
	}
	if !strings.Contains(log, "nothing to push") {
		t.Errorf("log does not say why it stopped:\n%s", log)
	}
}

func TestRunRejectsAMissingProviderAndPRD(t *testing.T) {
	if _, err := Run(context.Background(), Options{PRDPath: "x/prd.md"}); err == nil {
		t.Error("expected an error without a provider")
	}
	if _, err := Run(context.Background(), Options{Provider: &testProvider{}}); err == nil {
		t.Error("expected an error without a PRD")
	}
}

func TestRunFailsOnAPRDThatIsNotThere(t *testing.T) {
	dir := t.TempDir()
	_, err := Run(context.Background(), Options{
		PRDPath:  filepath.Join(dir, ".chief", "prds", "ghost", "prd.md"),
		Provider: &testProvider{},
	})
	if err == nil {
		t.Fatal("expected an error for a PRD that does not exist")
	}
	if !strings.Contains(err.Error(), "reading") {
		t.Errorf("error = %v, want it to say what it could not read", err)
	}
}
