package git

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// envProbe prints every variable a setup or teardown command is promised, in a
// fixed order, so one run of the command answers the whole contract at once.
const envProbe = `echo "$CHIEF_PRD_NAME|$CHIEF_BRANCH|$CHIEF_BASE_BRANCH|$CHIEF_WORKTREE_PATH|$CHIEF_REPO_DIR"`

// worktreeFixture creates a worktree for a PRD the way chief does and returns
// the repo directory, the worktree path and the context a command would get.
func worktreeFixture(t *testing.T, prdName, branch string) (string, string, WorktreeContext) {
	t.Helper()
	dir := initTestRepo(t)
	wtPath := filepath.Join(dir, "worktrees", prdName)
	opts := CreateWorktreeOptions{
		RepoDir:      dir,
		WorktreePath: wtPath,
		Branch:       branch,
		PRDName:      prdName,
	}
	if err := CreateWorktree(opts); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	return dir, wtPath, WorktreeContext{
		PRDName:      prdName,
		Branch:       branch,
		BaseBranch:   RecordedBaseBranch(dir, branch),
		WorktreePath: wtPath,
		RepoDir:      dir,
	}
}

func TestRunSetupPassesTheContextAsEnvironment(t *testing.T) {
	dir, wtPath, wt := worktreeFixture(t, "auth", "chief/auth")

	res, err := RunSetup(wt, envProbe, RunOptions{})
	if err != nil {
		t.Fatalf("RunSetup() error = %v (%s)", err, res.Output)
	}

	// initTestRepo commits on main, so main is the branch chief cut chief/auth
	// from and recorded as its base.
	want := strings.Join([]string{"auth", "chief/auth", "main", wtPath, dir}, "|")
	if res.Output != want {
		t.Errorf("setup saw %q, want %q", res.Output, want)
	}
}

func TestRunTeardownPassesTheContextAsEnvironment(t *testing.T) {
	dir, wtPath, wt := worktreeFixture(t, "auth", "chief/auth")

	res, err := RunTeardown(wt, envProbe, RunOptions{})
	if err != nil {
		t.Fatalf("RunTeardown() error = %v (%s)", err, res.Output)
	}

	want := strings.Join([]string{"auth", "chief/auth", "main", wtPath, dir}, "|")
	if res.Output != want {
		t.Errorf("teardown saw %q, want %q", res.Output, want)
	}
}

// A branch chief did not create has no recorded base. The teardown then has to
// see an empty CHIEF_BASE_BRANCH rather than a guessed one, so a script can tell
// "unknown" apart from a real branch name.
func TestRunTeardownLeavesAnUnknownBaseBranchEmpty(t *testing.T) {
	dir := initTestRepo(t)
	if err := runGitChecked(dir, "failed to create branch", "branch", "chief/auth", "main"); err != nil {
		t.Fatalf("branch setup failed: %v", err)
	}
	wtPath := filepath.Join(dir, "worktrees", "auth")
	if err := CreateWorktree(CreateWorktreeOptions{
		RepoDir:      dir,
		WorktreePath: wtPath,
		Branch:       "chief/auth",
		PRDName:      "auth",
	}); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}

	wt := WorktreeContext{
		PRDName:      "auth",
		Branch:       "chief/auth",
		BaseBranch:   RecordedBaseBranch(dir, "chief/auth"),
		WorktreePath: wtPath,
		RepoDir:      dir,
	}
	res, err := RunTeardown(wt, `echo "[$CHIEF_BASE_BRANCH]"`, RunOptions{})
	if err != nil {
		t.Fatalf("RunTeardown() error = %v (%s)", err, res.Output)
	}
	if res.Output != "[]" {
		t.Errorf("CHIEF_BASE_BRANCH = %s, want an empty value", res.Output)
	}
}

// The paths are promised absolute, so a script can hand them to a tool with a
// working directory of its own.
func TestWorktreeCommandPathsAreAbsolute(t *testing.T) {
	dir, wtPath, _ := worktreeFixture(t, "auth", "chief/auth")

	wt := WorktreeContext{
		WorktreePath: dir + "/worktrees/../worktrees/auth",
		RepoDir:      dir + "/.",
	}
	res, err := RunSetup(wt, `echo "$CHIEF_WORKTREE_PATH|$CHIEF_REPO_DIR"`, RunOptions{})
	if err != nil {
		t.Fatalf("RunSetup() error = %v (%s)", err, res.Output)
	}
	if want := wtPath + "|" + dir; res.Output != want {
		t.Errorf("paths = %q, want %q", res.Output, want)
	}
}

func TestRunSetupRunsInsideTheWorktree(t *testing.T) {
	_, _, wt := worktreeFixture(t, "auth", "chief/auth")

	res, err := RunSetup(wt, "ls", RunOptions{})
	if err != nil {
		t.Fatalf("RunSetup() error = %v (%s)", err, res.Output)
	}
	if !strings.Contains(res.Output, "README.md") {
		t.Errorf("setup ran outside the worktree, it saw: %q", res.Output)
	}
}

func TestRunSetupWithoutACommandIsANoOp(t *testing.T) {
	res, err := RunSetup(WorktreeContext{WorktreePath: "/nonexistent"}, "   ", RunOptions{})
	if err != nil {
		t.Fatalf("RunSetup() error = %v", err)
	}
	if res.Output != "" {
		t.Errorf("RunSetup() output = %q, want empty", res.Output)
	}
}

func TestRunSetupWritesTheFullOutputToATimestampedLog(t *testing.T) {
	_, _, wt := worktreeFixture(t, "auth", "chief/auth")
	logDir := t.TempDir()

	res, err := RunSetup(wt, "echo first; echo second >&2; echo third", RunOptions{LogDir: logDir})
	if err != nil {
		t.Fatalf("RunSetup() error = %v", err)
	}

	if got := filepath.Dir(res.LogPath); got != logDir {
		t.Fatalf("log written to %q, want a file in %q", res.LogPath, logDir)
	}
	name := filepath.Base(res.LogPath)
	if !regexp.MustCompile(`^setup-\d{4}-\d{2}-\d{2}-\d{6}\.log$`).MatchString(name) {
		t.Errorf("log file name = %q, want setup-<timestamp>.log", name)
	}

	data, err := os.ReadFile(res.LogPath)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	for _, want := range []string{"first", "second", "third"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("log = %q, missing %q", string(data), want)
		}
	}
}

func TestRunTeardownLogIsNamedAfterTheTeardown(t *testing.T) {
	_, _, wt := worktreeFixture(t, "auth", "chief/auth")
	logDir := t.TempDir()

	res, err := RunTeardown(wt, "echo bye", RunOptions{LogDir: logDir})
	if err != nil {
		t.Fatalf("RunTeardown() error = %v", err)
	}
	if !strings.HasPrefix(filepath.Base(res.LogPath), "teardown-") {
		t.Errorf("log file name = %q, want a teardown- prefix", filepath.Base(res.LogPath))
	}
}

func TestRunSetupIgnoresItsOwnLogsInTheLogDir(t *testing.T) {
	_, _, wt := worktreeFixture(t, "auth", "chief/auth")
	logDir := t.TempDir()

	if _, err := RunSetup(wt, "echo hi", RunOptions{LogDir: logDir}); err != nil {
		t.Fatalf("RunSetup() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(logDir, ".gitignore"))
	if err != nil {
		t.Fatalf("reading the .gitignore: %v", err)
	}
	if !strings.Contains(string(data), "*.log") {
		t.Errorf(".gitignore = %q, want a *.log pattern", string(data))
	}
}

func TestRunSetupReportsLinesWhileTheCommandIsStillRunning(t *testing.T) {
	_, _, wt := worktreeFixture(t, "auth", "chief/auth")
	gate := filepath.Join(t.TempDir(), "gate")

	// The command waits for a file the callback writes, so it can only finish
	// once a line has been reported. A run that handed its output over at the
	// end would never get past the wait.
	command := fmt.Sprintf(`echo ready; for i in $(seq 1 100); do if [ -f %q ]; then break; fi; sleep 0.05; done; echo finished`, gate)

	var mu sync.Mutex
	var seen []string
	start := time.Now()
	res, err := RunSetup(wt, command, RunOptions{OnLine: func(line string) {
		mu.Lock()
		seen = append(seen, line)
		mu.Unlock()
		if line == "ready" {
			_ = os.WriteFile(gate, nil, 0644)
		}
	}})
	if err != nil {
		t.Fatalf("RunSetup() error = %v (%s)", err, res.Output)
	}

	// The wait gives up after five seconds on its own, so only a run that got
	// its first line early can be this quick.
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("RunSetup() took %s — the output was not reported while the command ran", elapsed)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0] != "ready" || seen[1] != "finished" {
		t.Errorf("reported lines = %q, want [ready finished]", seen)
	}
}

func TestRunSetupReportsStdoutAndStderrInTheOrderTheyWereWritten(t *testing.T) {
	_, _, wt := worktreeFixture(t, "auth", "chief/auth")

	var seen []string
	if _, err := RunSetup(wt, "echo one; echo two >&2; echo three", RunOptions{
		OnLine: func(line string) { seen = append(seen, line) },
	}); err != nil {
		t.Fatalf("RunSetup() error = %v", err)
	}

	if strings.Join(seen, ",") != "one,two,three" {
		t.Errorf("reported lines = %q, want [one two three]", seen)
	}
}

func TestRunSetupTimeoutKillsTheCommandAndItsChildren(t *testing.T) {
	_, _, wt := worktreeFixture(t, "auth", "chief/auth")
	marker := filepath.Join(t.TempDir(), "child-survived")

	command := fmt.Sprintf(`(sleep 1; touch %q) & echo working; sleep 30`, marker)
	start := time.Now()
	res, err := RunSetup(wt, command, RunOptions{Timeout: 300 * time.Millisecond})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("RunSetup() error = nil, want a timeout error")
	}
	if !res.TimedOut {
		t.Errorf("RunResult.TimedOut = false, want true (err = %v)", err)
	}
	if elapsed > 10*time.Second {
		t.Errorf("RunSetup() took %s, want it to give up near the timeout", elapsed)
	}

	// The backgrounded child would have created the marker a second in; killing
	// only the shell would leave it running.
	time.Sleep(1500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Error("a child of the timed-out setup survived and kept working")
	}
}

func TestRunSetupKeepsTheWholeOutputInTheLogAndOnlyTheTailInMemory(t *testing.T) {
	_, _, wt := worktreeFixture(t, "auth", "chief/auth")
	logDir := t.TempDir()

	res, err := RunSetup(wt, "seq 1 500", RunOptions{LogDir: logDir})
	if err != nil {
		t.Fatalf("RunSetup() error = %v", err)
	}

	if got := len(strings.Split(res.Output, "\n")); got != retainedOutputLines {
		t.Errorf("RunResult.Output has %d lines, want %d", got, retainedOutputLines)
	}
	if !strings.HasPrefix(res.Output, "301\n") {
		t.Errorf("RunResult.Output starts with %.10q, want the tail starting at 301", res.Output)
	}

	data, err := os.ReadFile(res.LogPath)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	for _, want := range []string{"\n1\n", "\n250\n", "\n500\n"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("log is missing %q — it should hold the whole output", want)
		}
	}
}
