package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/loop"
	"github.com/ben182/chief/internal/prd"
)

// sessionProvider stands in for the agent CLI of an interactive session: its
// "session" is a shell script run in the project, and the prompt it was given
// is kept for the test to read.
type sessionProvider struct {
	script string
	prompt *string
}

func (p sessionProvider) Name() string    { return "session" }
func (p sessionProvider) CLIPath() string { return "sh" }
func (p sessionProvider) LoopCommand(_ context.Context, _, _ string) *exec.Cmd {
	return exec.Command("true")
}
func (p sessionProvider) InteractiveCommand(workDir, prompt string) *exec.Cmd {
	if p.prompt != nil {
		*p.prompt = prompt
	}
	cmd := exec.Command("sh", "-c", p.script)
	cmd.Dir = workDir
	return cmd
}
func (sessionProvider) SupportsInteractiveQuestions() bool { return false }
func (sessionProvider) CleanOutput(output string) string   { return output }
func (sessionProvider) ParseLine(_ string) *loop.Event     { return nil }
func (sessionProvider) LogFileName() string                { return "session.log" }

// withTerminal pretends there is a person at a terminal for the test's length.
func withTerminal(t *testing.T, yes bool) {
	t.Helper()
	was := canAsk
	canAsk = func() bool { return yes }
	t.Cleanup(func() { canAsk = was })
}

// prepProject is a git repo with an origin, holding one PRD with a blocked
// story, committed and pushed.
func prepProject(t *testing.T) (dir, prdPath, origin string) {
	t.Helper()
	dir = t.TempDir()
	initGitRepoOnBranch(t, dir, "chief/app")
	prdPath = filepath.Join(dir, ".chief", "prds", "app", "prd.md")
	if err := os.MkdirAll(filepath.Dir(prdPath), 0755); err != nil {
		t.Fatal(err)
	}
	md := "# App\n\n### US-001: Sign\n**Status:** blocked\n**Blockiert (Ben):** 1Password is locked\n- [ ] Signed\n"
	if err := os.WriteFile(prdPath, []byte(md), 0644); err != nil {
		t.Fatal(err)
	}
	origin = filepath.Join(t.TempDir(), "origin.git")
	for _, args := range [][]string{
		{"add", "."}, {"commit", "-qm", "prd"},
		{"init", "--bare", "-q", origin}, {"remote", "add", "origin", origin},
		{"push", "-q", "-u", "origin", "chief/app"},
	} {
		gitIn(t, dir, args...)
	}
	return dir, prdPath, origin
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// A box prep's session edits the PRD and the box config; chief then commits and
// pushes both, and stamps the PRD as prepared for the box — and only the box.
func TestRunPrepCommitsPushesAndStampsTheTarget(t *testing.T) {
	withTerminal(t, true)
	dir, prdPath, origin := prepProject(t)

	// The session releases the blocked story, marks a story for macOS, and adds
	// an apt package — what a real one would do after its questions.
	script := `set -e
p=.chief/prds/app/prd.md
sed -i.bak -e 's/^\*\*Status:\*\* blocked$/**Status:** todo/' -e '/^\*\*Blockiert (Ben):\*\*/d' "$p" && rm "$p.bak"
printf '\n### US-002: Xcode build\n**Braucht:** macOS (Xcode)\n- [ ] Builds\n' >> "$p"
printf 'box:\n  packages:\n    - ffmpeg\n' > .chief/config.yaml
`
	var prompt string
	var out bytes.Buffer
	err := RunPrep(PrepOptions{
		Name: "app", BaseDir: dir, Box: true, Out: &out,
		Provider: sessionProvider{script: script, prompt: &prompt},
	})
	if err != nil {
		t.Fatalf("RunPrep: %v\n%s", err, out.String())
	}

	if !strings.Contains(prompt, "box.packages") || !strings.Contains(prompt, "compared with `linux`") {
		t.Errorf("the box prep was not told what the box has:\n%s", prompt)
	}

	if got := gitIn(t, dir, "log", "-1", "--format=%s"); got != "chore: prep app for the box" {
		t.Errorf("last commit = %q", got)
	}
	pushed := gitIn(t, dir, "--git-dir", origin, "show", "chief/app:.chief/prds/app/prd.md")
	if strings.Contains(pushed, "Blockiert") || !strings.Contains(pushed, "**Braucht:** macOS (Xcode)") {
		t.Errorf("origin does not have the prepared PRD:\n%s", pushed)
	}
	if cfg := gitIn(t, dir, "--git-dir", origin, "show", "chief/app:.chief/config.yaml"); !strings.Contains(cfg, "ffmpeg") {
		t.Errorf("origin does not have the box config:\n%s", cfg)
	}

	if fresh, reason := prd.CheckPrep(prdPath, prd.PrepTargetBox); !fresh {
		t.Errorf("not prepared for the box after the prep: %s", reason)
	}
	if fresh, _ := prd.CheckPrep(prdPath, runtime.GOOS); fresh {
		t.Error("a box prep must not count as a prep of this machine")
	}
	// The stamp is this checkout's own and stays out of the branch.
	if status := gitIn(t, dir, "status", "--porcelain"); status != "" {
		t.Errorf("the prep left the checkout dirty:\n%s", status)
	}
}

// Without a terminal there is nobody to talk to: no session starts, nothing is
// stamped, and the caller is told why.
func TestRunPrepRefusesWithoutATerminal(t *testing.T) {
	withTerminal(t, false)
	dir, prdPath, _ := prepProject(t)
	started := filepath.Join(t.TempDir(), "started")

	err := RunPrep(PrepOptions{
		Name: "app", BaseDir: dir, Out: &bytes.Buffer{},
		Provider: sessionProvider{script: "touch " + started},
	})
	if err == nil || !strings.Contains(err.Error(), "needs a terminal") {
		t.Fatalf("err = %v, want the terminal error", err)
	}
	if _, statErr := os.Stat(started); statErr == nil {
		t.Error("a session was started with nobody to answer it")
	}
	if fresh, _ := prd.CheckPrep(prdPath, runtime.GOOS); fresh {
		t.Error("stamped without a prep")
	}
}

// writeAppPRD puts a PRD at .chief/prds/app/prd.md under a fresh project.
func writeAppPRD(t *testing.T, md string) (dir, prdPath string) {
	t.Helper()
	dir = t.TempDir()
	prdPath = filepath.Join(dir, ".chief", "prds", "app", "prd.md")
	if err := os.MkdirAll(filepath.Dir(prdPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prdPath, []byte(md), 0644); err != nil {
		t.Fatal(err)
	}
	return dir, prdPath
}

// A start whose PRD was never prepared runs the prep first and then goes ahead;
// the next start finds the stamp and asks nothing.
func TestEnsurePreparedRunsThePrepOnceAndThenLetsTheRunStart(t *testing.T) {
	withTerminal(t, true)
	dir, prdPath := writeAppPRD(t, "# App\n\n### US-001: Page\n- [ ] Shown\n")
	sessions := 0
	provider := func() (loop.Provider, error) {
		sessions++
		return sessionProvider{script: "true"}, nil
	}

	for i := 0; i < 2; i++ {
		// Only the start that ran the prep asks; Enter says yes.
		err := EnsurePrepared(GateOptions{PRDPath: prdPath, BaseDir: dir, Provider: provider, Out: &bytes.Buffer{}, In: strings.NewReader("\n")})
		if err != nil {
			t.Fatalf("start %d: %v", i+1, err)
		}
	}
	if sessions != 1 {
		t.Errorf("%d prep sessions for two starts of an unchanged PRD, want 1", sessions)
	}
	if fresh, reason := prd.CheckPrep(prdPath, runtime.GOOS); !fresh {
		t.Errorf("not prepared after the gate: %s", reason)
	}
}

// With nobody at a terminal the gate never starts a session: it fails at once,
// naming the command that fixes it — a box, a script or a service must not hang.
func TestEnsurePreparedFailsWithoutATerminalInsteadOfWaiting(t *testing.T) {
	withTerminal(t, false)
	dir, prdPath := writeAppPRD(t, "# App\n\n### US-001: Page\n- [ ] Shown\n")

	err := EnsurePrepared(GateOptions{
		PRDPath: prdPath, BaseDir: dir, Box: true, Out: &bytes.Buffer{},
		Provider: func() (loop.Provider, error) {
			t.Fatal("no provider should be needed without a terminal")
			return nil, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "chief box prep app") || !strings.Contains(err.Error(), "--skip-prep") {
		t.Fatalf("err = %v", err)
	}
}

// A PRD prepared for the box that has nothing the box can do stops `box up`
// before a machine is paid for, and says why.
func TestEnsurePreparedRefusesWhenNothingCanRunThere(t *testing.T) {
	withTerminal(t, false)
	dir, prdPath := writeAppPRD(t, "# App\n\n"+
		"### US-001: Xcode build\n**Braucht:** macOS (Xcode)\n- [ ] Builds\n\n"+
		"### US-002: Notarize\n**Blocked by:** US-001\n- [ ] Notarized\n\n"+
		"### US-003: Sign\n**Status:** blocked\n**Blockiert (Ben):** 1Password is locked\n- [ ] Signed\n")
	if err := prd.RecordPrep(prdPath, prd.PrepTargetBox, time.Now()); err != nil {
		t.Fatal(err)
	}

	err := EnsurePrepared(GateOptions{PRDPath: prdPath, BaseDir: dir, Box: true, Out: &bytes.Buffer{}})
	if err == nil {
		t.Fatal("expected the start to be refused")
	}
	if want := "nothing in PRD app can run on the box — 1 blocked, 1 need another system, 1 wait on those"; !strings.Contains(err.Error(), want) {
		t.Errorf("err = %v\nwant it to contain %q", err, want)
	}

	// On a Mac the same PRD has a story to run.
	if runtime.GOOS == "darwin" {
		if err := prd.RecordPrep(prdPath, runtime.GOOS, time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := EnsurePrepared(GateOptions{PRDPath: prdPath, BaseDir: dir, Out: &bytes.Buffer{}}); err != nil {
			t.Errorf("locally on macOS: %v", err)
		}
	}
}

// A **Braucht:** line chief cannot read lets its story run here; the start
// says so instead of running it without a word.
func TestEnsurePreparedWarnsAboutAnUnreadableNeedsLine(t *testing.T) {
	withTerminal(t, false)
	dir, prdPath := writeAppPRD(t, "# App\n\n### US-001: Build\n**Braucht:** Xcode\n- [ ] Builds\n")
	if err := prd.RecordPrep(prdPath, runtime.GOOS, time.Now()); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := EnsurePrepared(GateOptions{PRDPath: prdPath, BaseDir: dir, Out: &out}); err != nil {
		t.Fatalf("EnsurePrepared: %v", err)
	}
	if !strings.Contains(out.String(), `Warning: US-001: **Braucht:** "Xcode" names no operating system`) {
		t.Errorf("no warning:\n%s", out.String())
	}
}

// A .chief/ in a subfolder of the repository, and the paths relative to where
// chief runs: the prep's commit has to find them all the same. git took them
// relative to the repository root and committed nothing.
func TestCommitAndPushPrepFindsFilesInASubfolder(t *testing.T) {
	repo := t.TempDir()
	initGitRepoOnBranch(t, repo, "main")
	project := filepath.Join(repo, "apps", "web")
	prdPath := filepath.Join(project, ".chief", "prds", "app", "prd.md")
	if err := os.MkdirAll(filepath.Dir(prdPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prdPath, []byte("# App\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)

	var out bytes.Buffer
	commitAndPushPrep(&out, []string{filepath.Join(".chief", "prds", "app", "prd.md")}, "chore: prep app for macOS")
	if got := gitIn(t, repo, "log", "-1", "--format=%s"); got != "chore: prep app for macOS" {
		t.Fatalf("last commit = %q\n%s", got, out.String())
	}
	if files := gitIn(t, repo, "show", "--name-only", "--format=", "HEAD"); files != "apps/web/.chief/prds/app/prd.md" {
		t.Errorf("committed %q", files)
	}
}

// Leaving the prep session with /exit looks exactly like finishing it. So after
// a prep the gate started, the start asks first, and a "no" — or no answer —
// starts nothing. The prep itself stands: its stamp stays.
func TestEnsurePreparedAsksBeforeStartingAfterAPrep(t *testing.T) {
	withTerminal(t, true)
	for _, answer := range []string{"n\n", "nein\n", ""} {
		dir, prdPath := writeAppPRD(t, "# App\n\n### US-001: Page\n- [ ] Shown\n")
		var out bytes.Buffer
		err := EnsurePrepared(GateOptions{
			PRDPath: prdPath, BaseDir: dir, Out: &out, In: strings.NewReader(answer),
			Provider: func() (loop.Provider, error) { return sessionProvider{script: "true"}, nil },
		})
		if !errors.Is(err, ErrStartDeclined) {
			t.Errorf("answer %q: err = %v, want ErrStartDeclined", answer, err)
		}
		if !strings.Contains(out.String(), "Start the run now? [Y/n]") {
			t.Errorf("answer %q: not asked:\n%s", answer, out.String())
		}
		if fresh, reason := prd.CheckPrep(prdPath, runtime.GOOS); !fresh {
			t.Errorf("answer %q: the prep's stamp is gone: %s", answer, reason)
		}
	}
	dir, prdPath := writeAppPRD(t, "# App\n\n### US-001: Page\n- [ ] Shown\n")
	if err := EnsurePrepared(GateOptions{
		PRDPath: prdPath, BaseDir: dir, Box: true, Out: &bytes.Buffer{}, In: strings.NewReader("y\n"),
		Provider: func() (loop.Provider, error) { return sessionProvider{script: "true"}, nil },
	}); err != nil {
		t.Errorf("a yes did not start: %v", err)
	}
}

// The morning after: the stamp is fresh, but every story left is blocked. At a
// terminal the gate starts the prep to release them instead of refusing, and
// checks again afterwards.
func TestEnsurePreparedStartsThePrepWhenEverythingIsBlocked(t *testing.T) {
	withTerminal(t, true)
	dir, prdPath := writeAppPRD(t, "# App\n\n### US-001: Sign\n**Status:** blocked\n**Blockiert (Ben):** 1Password is locked\n- [ ] Signed\n")
	if err := prd.RecordPrep(prdPath, runtime.GOOS, time.Now()); err != nil {
		t.Fatal(err)
	}
	release := `p=.chief/prds/app/prd.md
sed -i.bak -e 's/^\*\*Status:\*\* blocked$/**Status:** todo/' -e '/^\*\*Blockiert (Ben):\*\*/d' "$p" && rm "$p.bak"
`
	sessions := 0
	var out bytes.Buffer
	err := EnsurePrepared(GateOptions{
		PRDPath: prdPath, BaseDir: dir, Out: &out, In: strings.NewReader("\n"),
		Provider: func() (loop.Provider, error) { sessions++; return sessionProvider{script: release}, nil },
	})
	if err != nil {
		t.Fatalf("EnsurePrepared: %v\n%s", err, out.String())
	}
	if sessions != 1 || !strings.Contains(out.String(), "to release the blocked stories") {
		t.Errorf("sessions = %d, want one prep to release the story\n%s", sessions, out.String())
	}

	// A prep that releases nothing ends the start as before, after one try.
	dir, prdPath = writeAppPRD(t, "# App\n\n### US-001: Sign\n**Status:** blocked\n**Blockiert (Ben):** 1Password is locked\n- [ ] Signed\n")
	if err := prd.RecordPrep(prdPath, runtime.GOOS, time.Now()); err != nil {
		t.Fatal(err)
	}
	sessions = 0
	err = EnsurePrepared(GateOptions{
		PRDPath: prdPath, BaseDir: dir, Out: &bytes.Buffer{}, In: strings.NewReader("\n"),
		Provider: func() (loop.Provider, error) { sessions++; return sessionProvider{script: "true"}, nil },
	})
	if err == nil || !strings.Contains(err.Error(), "1 blocked") || sessions != 1 {
		t.Errorf("err = %v, sessions = %d; want one prep and then the refusal", err, sessions)
	}
}

// A local run in a PRD's worktree reads the worktree's copy of the PRD. The
// gate has to check and prepare that copy, not the project's: stamped in the
// project, the worktree's copy is still unprepared; prepared through the gate,
// the stamp and the commit land in the worktree.
func TestEnsurePreparedChecksTheWorktreesCopy(t *testing.T) {
	repo := initWorktreeTestRepo(t)
	home := filepath.Join(repo, ".chief", "prds", "app", "prd.md")
	if err := os.MkdirAll(filepath.Dir(home), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(home, []byte("# App\n\n### US-001: Page\n- [ ] Shown\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "git", "add", ".")
	runGit(t, repo, "git", "commit", "-m", "prd")
	wtPath, err := git.WorktreePathForPRD(repo, "", "app", git.BranchForPRD("app"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := git.CreateWorktree(git.CreateWorktreeOptions{RepoDir: repo, WorktreePath: wtPath, Branch: git.BranchForPRD("app"), PRDName: "app"}); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	live := filepath.Join(wtPath, ".chief", "prds", "app", "prd.md")
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("the worktree has no copy of the PRD: %v", err)
	}

	withTerminal(t, false)
	if err := prd.RecordPrep(home, runtime.GOOS, time.Now()); err != nil {
		t.Fatal(err)
	}
	err = EnsurePrepared(GateOptions{PRDPath: filepath.Join(".chief", "prds", "app", "prd.md"), BaseDir: repo, Out: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "not prepared") {
		t.Fatalf("err = %v; the project's stamp must not count for the worktree's copy", err)
	}

	withTerminal(t, true)
	edit := "printf '\\n### US-002: More\\n- [ ] More\\n' >> " + live
	if err := EnsurePrepared(GateOptions{
		PRDPath: home, BaseDir: repo, Out: &bytes.Buffer{}, In: strings.NewReader("\n"),
		Provider: func() (loop.Provider, error) { return sessionProvider{script: edit}, nil },
	}); err != nil {
		t.Fatalf("EnsurePrepared: %v", err)
	}
	if fresh, reason := prd.CheckPrep(live, runtime.GOOS); !fresh {
		t.Errorf("the worktree's copy is not stamped: %s", reason)
	}
	if got := strings.TrimSpace(runGit(t, wtPath, "git", "log", "-1", "--format=%s")); !strings.HasPrefix(got, "chore: prep app for") {
		t.Errorf("the worktree's last commit = %q, want the prep's", got)
	}
}

// `chief prep -h` used to start a real prep on the default PRD: every argument
// starting with "-" was skipped. Help is help, and an unknown flag is an error.
func TestParsePrepArgs(t *testing.T) {
	for _, tc := range []struct {
		args       []string
		name       string
		help, fail bool
	}{
		{nil, "", false, false},
		{[]string{"auth"}, "auth", false, false},
		{[]string{"-h"}, "", true, false},
		{[]string{"auth", "--help"}, "", true, false},
		{[]string{"--skip-prep"}, "", false, true},
		{[]string{"auth", "-x"}, "", false, true},
		{[]string{"auth", "billing"}, "", false, true},
	} {
		name, help, err := ParsePrepArgs(tc.args)
		if name != tc.name || help != tc.help || (err != nil) != tc.fail {
			t.Errorf("ParsePrepArgs(%q) = %q, %v, %v", tc.args, name, help, err)
		}
	}
	if u := PrepUsage(true); !strings.Contains(u, "chief box prep [name]") || !strings.Contains(u, "box: section") {
		t.Errorf("box usage:\n%s", u)
	}
	if u := PrepUsage(false); !strings.Contains(u, "chief prep [name]") || strings.Contains(u, "config.yaml") {
		t.Errorf("usage:\n%s", u)
	}
}
