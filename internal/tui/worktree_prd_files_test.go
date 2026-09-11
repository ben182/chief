package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/loop"
)

// worktreeRunApp stages an App about to finish worktree setup for a PRD that
// lives in the project, with a real manager behind it.
func worktreeRunApp(t *testing.T, base string) *App {
	t.Helper()
	m := loop.NewManager(10, nil)
	m.SetBaseDir(base)

	a := newTestApp(nil, 100, 40)
	a.baseDir = base
	a.manager = m
	a.config = &config.Config{}
	a.prdName = "auth"
	a.prdPath = filepath.Join(base, ".chief", "prds", "auth", "prd.md")
	a.viewMode = ViewWorktreeSpinner
	a.pendingStartPRD = "auth"
	a.pendingWorktreePath = filepath.Join(base, ".chief", "worktrees", "auth")
	a.worktreeSpinner = NewWorktreeSpinner()
	a.worktreeSpinner.Configure("auth", "chief/auth", "main", ".chief/worktrees/auth/", "")
	a.picker = NewPRDPicker(base, "auth", m, "")
	return a
}

// writePRD writes a prd.md into dir, creating the directory.
func writePRD(t *testing.T, dir, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "prd.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// A run given a worktree works from the PRD copy inside it. That is what keeps
// the status it records, its progress and its log out of the project the user is
// still working in — and what lets the per-story commit carry them on the branch.
func TestWorktreeRunWorksFromTheWorktreeCopy(t *testing.T) {
	base := t.TempDir()
	initRepoWithRecordedBase(t, base, "chief/auth", "main")
	home := writePRD(t, filepath.Join(base, ".chief", "prds", "auth"), "# PRD\n")
	a := worktreeRunApp(t, base)

	a.finishWorktreeSetup()

	mapped := filepath.Join(a.pendingWorktreePath, ".chief", "prds", "auth", "prd.md")
	inst := a.manager.GetInstance("auth")
	if inst == nil {
		t.Fatal("the PRD should be registered after the worktree setup")
	}
	if inst.PRDPath != mapped {
		t.Errorf("run works from %q, want the worktree's copy %q", inst.PRDPath, mapped)
	}
	if inst.HomePRDPath != home {
		t.Errorf("home path = %q, want %q", inst.HomePRDPath, home)
	}
	if data, err := os.ReadFile(mapped); err != nil || string(data) != "# PRD\n" {
		t.Errorf("worktree copy = %q (err %v), want the project's content", string(data), err)
	}
}

// A resumed run finds its own copy already there, and that copy — not the
// project's, which the run left behind stories ago — is the current one.
func TestWorktreeRunKeepsTheCopyItAlreadyHas(t *testing.T) {
	base := t.TempDir()
	initRepoWithRecordedBase(t, base, "chief/auth", "main")
	writePRD(t, filepath.Join(base, ".chief", "prds", "auth"), "# PRD\n")
	a := worktreeRunApp(t, base)
	a.pendingWorktreeReused = true
	mapped := writePRD(t, filepath.Join(a.pendingWorktreePath, ".chief", "prds", "auth"), "# PRD\n**Status:** in-progress\n")

	a.finishWorktreeSetup()

	data, err := os.ReadFile(mapped)
	if err != nil || string(data) != "# PRD\n**Status:** in-progress\n" {
		t.Errorf("worktree copy = %q (err %v), want the run's own state kept", string(data), err)
	}
}

// A worktree created for this run holds only what its branch carries. In a
// project that tracks .chief/ that is the PRD as it was last committed — without
// the story the user added five minutes ago, which is exactly the story they
// started the run for.
func TestFreshWorktreeGetsThePRDAsItStandsInTheProject(t *testing.T) {
	base := t.TempDir()
	initRepoWithRecordedBase(t, base, "chief/auth", "main")
	writePRD(t, filepath.Join(base, ".chief", "prds", "auth"), "# PRD\n### US-002: added since the last commit\n")
	a := worktreeRunApp(t, base)
	mapped := writePRD(t, filepath.Join(a.pendingWorktreePath, ".chief", "prds", "auth"), "# PRD\n")

	a.finishWorktreeSetup()

	data, err := os.ReadFile(mapped)
	if err != nil || string(data) != "# PRD\n### US-002: added since the last commit\n" {
		t.Errorf("worktree copy = %q (err %v), want the project's current PRD", string(data), err)
	}
}

// Removing a worktree would take the run's record with it. In a project that
// gitignores .chief/ the branch never carried it, so this copy is the only one
// there is.
func TestCleaningAWorktreeBringsThePRDStateHome(t *testing.T) {
	base := t.TempDir()
	initRepoWithRecordedBase(t, base, "chief/auth", "main")
	home := writePRD(t, filepath.Join(base, ".chief", "prds", "auth"), "# PRD\n")
	a := worktreeRunApp(t, base)
	worktree := a.pendingWorktreePath
	writePRD(t, filepath.Join(worktree, ".chief", "prds", "auth"), "# PRD\n**Status:** done\n")

	var removed string
	a.removeWorktree = func(repoDir, worktreePath string) error {
		removed = worktreePath
		return nil
	}

	msg := a.cleanWorktreeCmd("auth", "chief/auth", worktree, "", false)()
	res, ok := msg.(cleanResultMsg)
	if !ok {
		t.Fatalf("unexpected message %T", msg)
	}
	if !res.success {
		t.Fatalf("clean failed: %s", res.message)
	}
	if removed != worktree {
		t.Errorf("removed %q, want %q", removed, worktree)
	}
	data, err := os.ReadFile(home)
	if err != nil || string(data) != "# PRD\n**Status:** done\n" {
		t.Errorf("the project's prd.md = %q (err %v), want the run's state written back", string(data), err)
	}
}
