package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/git"
)

// initWorktreeTestRepo creates a git repo with one commit on main, the base a
// worktree can be cut from.
func initWorktreeTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "checkout", "-b", "main"},
	} {
		runGit(t, dir, args...)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n"), 0644); err != nil {
		t.Fatalf("failed to write README: %v", err)
	}
	runGit(t, dir, "git", "add", ".")
	runGit(t, dir, "git", "commit", "-m", "initial commit")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command(args[0], args[1:]...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("%v failed: %s", args, string(out))
	}
	return string(out)
}

// worktreeFixture creates a repo plus the worktree chief would create for
// prdName, and returns the repo directory and the worktree path.
func worktreeFixture(t *testing.T, prdName string) (string, string) {
	t.Helper()
	repoDir := initWorktreeTestRepo(t)
	branch := git.BranchForPRD(prdName)
	wtPath, err := git.WorktreePathForPRD(repoDir, "", prdName, branch)
	if err != nil {
		t.Fatalf("WorktreePathForPRD() error = %v", err)
	}
	opts := git.CreateWorktreeOptions{
		RepoDir:      repoDir,
		WorktreePath: wtPath,
		Branch:       branch,
		PRDName:      prdName,
	}
	if _, err := git.CreateWorktree(opts); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	return repoDir, wtPath
}

func TestRunWorktreeSetupFailsWhenWorktreeMissing(t *testing.T) {
	repoDir := initWorktreeTestRepo(t)

	var out strings.Builder
	opts := WorktreeOptions{
		Name:    "auth",
		BaseDir: repoDir,
		Config:  &config.Config{Worktree: config.WorktreeConfig{Setup: "echo hello"}},
		Stdout:  &out,
	}

	err := RunWorktreeSetup(opts)
	if err == nil {
		t.Fatal("expected an error for a PRD without a worktree")
	}
	// The message has to name the PRD and the place chief looked, otherwise
	// there is nothing to act on.
	if !strings.Contains(err.Error(), "auth") {
		t.Errorf("error does not name the PRD: %v", err)
	}
	expectedPath := filepath.Join(repoDir, ".chief", "worktrees", "auth")
	if !strings.Contains(err.Error(), expectedPath) {
		t.Errorf("error does not name the expected worktree path %q: %v", expectedPath, err)
	}
	if out.String() != "" {
		t.Errorf("nothing should have run, got output: %q", out.String())
	}
}

// envProbe prints every variable a setup or teardown command is promised, in a
// fixed order, so one run answers the whole contract at once. It also prints
// the worktree's own README, which only exists inside the worktree — that is
// the proof of the working directory.
const envProbe = `echo "$CHIEF_PRD_NAME|$CHIEF_BRANCH|$CHIEF_BASE_BRANCH|$CHIEF_WORKTREE_PATH|$CHIEF_REPO_DIR"; ls README.md`

func TestRunWorktreeSetupRunsInTheWorktreeWithChiefEnvironment(t *testing.T) {
	repoDir, wtPath := worktreeFixture(t, "auth")

	var out strings.Builder
	opts := WorktreeOptions{
		Name:    "auth",
		BaseDir: repoDir,
		Config:  &config.Config{Worktree: config.WorktreeConfig{Setup: envProbe}},
		Stdout:  &out,
	}

	if err := RunWorktreeSetup(opts); err != nil {
		t.Fatalf("RunWorktreeSetup() error = %v\noutput:\n%s", err, out.String())
	}

	// The expected line is spelled out rather than assembled from the same
	// pieces the code uses, so it can disagree with the implementation.
	want := "auth|chief/auth|main|" + wtPath + "|" + repoDir
	if !strings.Contains(out.String(), want) {
		t.Errorf("output does not contain the environment line\nwant: %s\ngot:\n%s", want, out.String())
	}
	if !strings.Contains(out.String(), "README.md") {
		t.Errorf("setup did not run inside the worktree, got:\n%s", out.String())
	}
}

func TestRunWorktreeSetupWritesTheOutputToTheStoryLog(t *testing.T) {
	repoDir, wtPath := worktreeFixture(t, "auth")

	var out strings.Builder
	opts := WorktreeOptions{
		Name:    "auth",
		BaseDir: repoDir,
		Config:  &config.Config{Worktree: config.WorktreeConfig{Setup: "echo installing-deps"}},
		Stdout:  &out,
	}
	if err := RunWorktreeSetup(opts); err != nil {
		t.Fatalf("RunWorktreeSetup() error = %v", err)
	}

	// The log belongs to the worktree the command ran against, which is where the
	// run keeps the PRD's working files.
	logDir := filepath.Join(wtPath, ".chief", "prds", "auth")
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("no log directory %s: %v", logDir, err)
	}
	var logs []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "setup-") && strings.HasSuffix(e.Name(), ".log") {
			logs = append(logs, e.Name())
		}
	}
	if len(logs) != 1 {
		t.Fatalf("want exactly one setup log in %s, got %v", logDir, logs)
	}
	logPath := filepath.Join(logDir, logs[0])
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}
	if !strings.Contains(string(data), "installing-deps") {
		t.Errorf("log does not contain the command's output, got:\n%s", string(data))
	}
	// The CLI has to say where the log is, otherwise it is a file nobody finds.
	if !strings.Contains(out.String(), logPath) {
		t.Errorf("output does not name the log file %q, got:\n%s", logPath, out.String())
	}
}

// gateWriter releases the setup command by creating gatePath the moment the
// line it is waiting for arrives. A CLI that collects the output and prints it
// at the end would open the gate only after the command is over — and the
// command never gets there, so the setup times out instead.
type gateWriter struct {
	mu       sync.Mutex
	gatePath string
	buf      strings.Builder
}

func (g *gateWriter) Write(p []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.buf.Write(p)
	if strings.Contains(g.buf.String(), "READY") {
		_ = os.WriteFile(g.gatePath, nil, 0644)
	}
	return len(p), nil
}

func (g *gateWriter) String() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.buf.String()
}

func TestRunWorktreeSetupStreamsOutputWhileTheCommandRuns(t *testing.T) {
	repoDir, _ := worktreeFixture(t, "auth")
	gate := filepath.Join(t.TempDir(), "gate")

	// The marker is assembled by printf so it appears in the command's output
	// but not in the command line the CLI echoes before starting — otherwise
	// the header alone would open the gate.
	setup := `printf 'READ%s\n' Y; while [ ! -f "` + gate + `" ]; do sleep 0.05; done; echo released`

	out := &gateWriter{gatePath: gate}
	opts := WorktreeOptions{
		Name:    "auth",
		BaseDir: repoDir,
		Config: &config.Config{Worktree: config.WorktreeConfig{
			Setup: setup,
			// The command hangs forever without the gate; the timeout turns
			// "not streamed" into a failure instead of a hung test.
			SetupTimeoutSeconds: 10,
		}},
		Stdout: out,
	}

	start := time.Now()
	if err := RunWorktreeSetup(opts); err != nil {
		t.Fatalf("RunWorktreeSetup() error = %v\noutput:\n%s", err, out.String())
	}
	elapsed := time.Since(start)

	if !strings.Contains(out.String(), "released") {
		t.Errorf("command did not get past the gate, output:\n%s", out.String())
	}
	// Without streaming the run can only end at the timeout, so a fast run is
	// the assertion that the gate was opened from live output.
	if elapsed > 5*time.Second {
		t.Errorf("run took %s, output was not streamed while the command ran", elapsed)
	}
}

func TestRunWorktreeTeardownRunsTheCommandWithChiefEnvironment(t *testing.T) {
	repoDir, wtPath := worktreeFixture(t, "auth")

	var out strings.Builder
	opts := WorktreeOptions{
		Name:    "auth",
		BaseDir: repoDir,
		Config:  &config.Config{Worktree: config.WorktreeConfig{Teardown: envProbe}},
		Stdout:  &out,
	}
	if err := RunWorktreeTeardown(opts); err != nil {
		t.Fatalf("RunWorktreeTeardown() error = %v\noutput:\n%s", err, out.String())
	}

	want := "auth|chief/auth|main|" + wtPath + "|" + repoDir
	if !strings.Contains(out.String(), want) {
		t.Errorf("output does not contain the environment line\nwant: %s\ngot:\n%s", want, out.String())
	}

	// The log belongs to the worktree the command ran against, which is where the
	// run keeps the PRD's working files.
	logDir := filepath.Join(wtPath, ".chief", "prds", "auth")
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("no log directory %s: %v", logDir, err)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "teardown-") && strings.HasSuffix(e.Name(), ".log") {
			found = true
		}
	}
	if !found {
		t.Errorf("no teardown log in %s, got %v", logDir, entries)
	}
}

func TestRunWorktreeTeardownKeepsTheWorktree(t *testing.T) {
	repoDir, wtPath := worktreeFixture(t, "auth")

	var out strings.Builder
	opts := WorktreeOptions{
		Name:    "auth",
		BaseDir: repoDir,
		Config:  &config.Config{Worktree: config.WorktreeConfig{Teardown: "echo dropping-database"}},
		Stdout:  &out,
	}
	if err := RunWorktreeTeardown(opts); err != nil {
		t.Fatalf("RunWorktreeTeardown() error = %v", err)
	}

	// The manual teardown only runs the command; removing the worktree stays
	// the job of the clean flow, so both the directory and git's registration
	// have to survive.
	if _, err := os.Stat(filepath.Join(wtPath, "README.md")); err != nil {
		t.Errorf("worktree at %s was removed: %v", wtPath, err)
	}
	if list := runGit(t, repoDir, "git", "worktree", "list"); !strings.Contains(list, "chief/auth") {
		t.Errorf("git no longer knows the worktree:\n%s", list)
	}
	if branch := runGit(t, repoDir, "git", "branch", "--list", "chief/auth"); !strings.Contains(branch, "chief/auth") {
		t.Errorf("branch chief/auth was deleted:\n%s", branch)
	}
}

func TestRunWorktreeSetupFailsWithoutAConfiguredCommand(t *testing.T) {
	repoDir, _ := worktreeFixture(t, "auth")

	var out strings.Builder
	err := RunWorktreeSetup(WorktreeOptions{
		Name:    "auth",
		BaseDir: repoDir,
		Config:  &config.Config{},
		Stdout:  &out,
	})
	if err == nil {
		t.Fatal("expected an error when no worktree.setup is configured")
	}
	if !strings.Contains(err.Error(), "worktree.setup") {
		t.Errorf("error should name the missing config key, got: %v", err)
	}
}

func TestRunWorktreeTeardownFailsWithoutAConfiguredCommand(t *testing.T) {
	repoDir, _ := worktreeFixture(t, "auth")

	var out strings.Builder
	err := RunWorktreeTeardown(WorktreeOptions{
		Name:    "auth",
		BaseDir: repoDir,
		Config:  &config.Config{},
		Stdout:  &out,
	})
	if err == nil {
		t.Fatal("expected an error when no worktree.teardown is configured")
	}
	if !strings.Contains(err.Error(), "worktree.teardown") {
		t.Errorf("error should name the missing config key, got: %v", err)
	}
}

func TestRunWorktreeSetupReportsAFailingCommand(t *testing.T) {
	repoDir, _ := worktreeFixture(t, "auth")

	var out strings.Builder
	err := RunWorktreeSetup(WorktreeOptions{
		Name:    "auth",
		BaseDir: repoDir,
		Config:  &config.Config{Worktree: config.WorktreeConfig{Setup: "echo nope >&2; exit 3"}},
		Stdout:  &out,
	})
	if err == nil {
		t.Fatal("expected an error for a setup command exiting non-zero")
	}
	if !strings.Contains(err.Error(), "setup") {
		t.Errorf("error should say which step failed, got: %v", err)
	}
	if !strings.Contains(out.String(), "nope") {
		t.Errorf("stderr should be streamed too, got:\n%s", out.String())
	}
}
