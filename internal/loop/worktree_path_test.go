package loop

import (
	"os"
	"path/filepath"
	"testing"
)

// The manager decides which copy of a PRD a run works from. Everything
// downstream reads that one path — the agent's prompt, the in-progress status,
// progress.md, the run log, the per-story commit — so this is the seam that
// keeps a worktree run out of the project's working tree.
func TestInstanceFollowsItsWorktree(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, ".chief", "prds", "auth", "prd.md")
	worktree := filepath.Join(base, ".chief", "worktrees", "auth")
	mapped := filepath.Join(worktree, ".chief", "prds", "auth", "prd.md")

	if err := os.MkdirAll(filepath.Dir(mapped), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mapped, []byte("# PRD\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewManager(10, nil)
	m.SetBaseDir(base)
	if err := m.RegisterWithWorktree("auth", home, worktree, "chief/auth"); err != nil {
		t.Fatalf("RegisterWithWorktree() error = %v", err)
	}

	inst := m.GetInstance("auth")
	if inst.PRDPath != mapped {
		t.Errorf("PRDPath = %q, want the worktree's copy %q", inst.PRDPath, mapped)
	}
	if inst.HomePRDPath != home {
		t.Errorf("HomePRDPath = %q, want the project's copy %q", inst.HomePRDPath, home)
	}

	// Removing the worktree sends the PRD home again — the copy there is the one
	// the clean flow just wrote the run's state back to.
	if err := m.ClearWorktreeInfo("auth", false); err != nil {
		t.Fatalf("ClearWorktreeInfo() error = %v", err)
	}
	inst = m.GetInstance("auth")
	if inst.PRDPath != home {
		t.Errorf("PRDPath after clearing = %q, want %q", inst.PRDPath, home)
	}
	if inst.HomePRDPath != "" {
		t.Errorf("HomePRDPath after clearing = %q, want it empty", inst.HomePRDPath)
	}
}

// A worktree that holds no copy of the PRD is not followed: writing the run's
// status into thin air would lose it, and the project's copy still works.
func TestInstanceStaysHomeWhenTheWorktreeHasNoCopy(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, ".chief", "prds", "auth", "prd.md")
	worktree := filepath.Join(base, ".chief", "worktrees", "auth")

	m := NewManager(10, nil)
	m.SetBaseDir(base)
	if err := m.RegisterWithWorktree("auth", home, worktree, "chief/auth"); err != nil {
		t.Fatalf("RegisterWithWorktree() error = %v", err)
	}

	if inst := m.GetInstance("auth"); inst.PRDPath != home || inst.HomePRDPath != "" {
		t.Errorf("PRDPath = %q / HomePRDPath = %q, want the project's copy and no home recorded",
			inst.PRDPath, inst.HomePRDPath)
	}
}

// A PRD kept outside the project has no counterpart in a worktree and is used
// where it is.
func TestInstanceOutsideTheProjectIsLeftWhereItIs(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(t.TempDir(), "prd.md")

	m := NewManager(10, nil)
	m.SetBaseDir(base)
	if err := m.RegisterWithWorktree("auth", outside, filepath.Join(base, "wt"), "chief/auth"); err != nil {
		t.Fatalf("RegisterWithWorktree() error = %v", err)
	}
	if inst := m.GetInstance("auth"); inst.PRDPath != outside {
		t.Errorf("PRDPath = %q, want %q", inst.PRDPath, outside)
	}
}
