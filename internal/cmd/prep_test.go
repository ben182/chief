package cmd

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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
// stamped, and the caller is told how to get past it.
func TestRunPrepRefusesWithoutATerminal(t *testing.T) {
	withTerminal(t, false)
	dir, prdPath, _ := prepProject(t)
	started := filepath.Join(t.TempDir(), "started")

	err := RunPrep(PrepOptions{
		Name: "app", BaseDir: dir, Out: &bytes.Buffer{},
		Provider: sessionProvider{script: "touch " + started},
	})
	if err == nil || !strings.Contains(err.Error(), "--skip-prep") {
		t.Fatalf("err = %v, want the terminal error", err)
	}
	if _, statErr := os.Stat(started); statErr == nil {
		t.Error("a session was started with nobody to answer it")
	}
	if fresh, _ := prd.CheckPrep(prdPath, runtime.GOOS); fresh {
		t.Error("stamped without a prep")
	}
}
