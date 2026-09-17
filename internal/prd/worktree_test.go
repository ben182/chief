package prd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathIn(t *testing.T) {
	base := filepath.Join("/proj")
	wt := filepath.Join("/proj", ".chief", "worktrees", "auth")

	tests := []struct {
		name string
		path string
		want string
		ok   bool
	}{
		{
			name: "standard PRD",
			path: filepath.Join(base, ".chief", "prds", "auth", "prd.md"),
			want: filepath.Join(wt, ".chief", "prds", "auth", "prd.md"),
			ok:   true,
		},
		{
			name: "legacy .chief/prd.md keeps its layout",
			path: filepath.Join(base, ".chief", "prd.md"),
			want: filepath.Join(wt, ".chief", "prd.md"),
			ok:   true,
		},
		{
			name: "a PRD outside the project has no counterpart",
			path: filepath.Join("/elsewhere", "prd.md"),
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := PathIn(base, tt.path, wt)
			if ok != tt.ok {
				t.Fatalf("PathIn(%q) ok = %v, want %v", tt.path, ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("PathIn(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsUnder(t *testing.T) {
	wt := filepath.Join("/proj", ".chief", "worktrees", "auth")
	if !IsUnder(wt, filepath.Join(wt, ".chief", "prds", "auth", "prd.md")) {
		t.Error("a path inside the worktree should count as under it")
	}
	if IsUnder(wt, filepath.Join("/proj", ".chief", "prds", "auth", "prd.md")) {
		t.Error("the project's copy is not under the worktree")
	}
}

// Mirror is how a PRD's working files reach the worktree a run works in, and how
// the run's record comes home when that worktree is removed.
func TestMirror(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "prds", "auth")

	write := func(dir, name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(src, "prd.md", "# PRD\n")
	write(src, "progress.md", "# progress\n")
	write(src, "todos.md", "- [ ] one\n")
	write(src, "claude-2026-01-01-000000.log", "megabytes")
	if err := os.MkdirAll(filepath.Join(src, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Mirror(src, dst); err != nil {
		t.Fatalf("Mirror() error = %v", err)
	}

	for _, name := range []string{"prd.md", "progress.md", "todos.md"} {
		if _, err := os.Stat(filepath.Join(dst, name)); err != nil {
			t.Errorf("%s was not mirrored: %v", name, err)
		}
	}
	// Run logs are per-checkout and huge; copying them is the one thing nobody
	// wants twice.
	if _, err := os.Stat(filepath.Join(dst, "claude-2026-01-01-000000.log")); err == nil {
		t.Error("run logs should not be mirrored")
	}
	if _, err := os.Stat(filepath.Join(dst, "nested")); err == nil {
		t.Error("subdirectories should not be mirrored")
	}

	// Mirroring again overwrites: the caller decides which side is authoritative.
	write(src, "prd.md", "# PRD v2\n")
	if err := Mirror(src, dst); err != nil {
		t.Fatalf("second Mirror() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dst, "prd.md"))
	if err != nil || string(data) != "# PRD v2\n" {
		t.Errorf("mirrored prd.md = %q (err %v), want the newer content", string(data), err)
	}
}

// A PRD directory that isn't there yet is not an error: there is nothing to
// mirror, and the caller carries on.
func TestMirrorMissingSourceIsNoError(t *testing.T) {
	if err := Mirror(filepath.Join(t.TempDir(), "gone"), t.TempDir()); err != nil {
		t.Errorf("Mirror() error = %v, want nil for a missing source", err)
	}
}

func TestSeedWorktreeGivesAFreshWorktreeTheProjectsCopy(t *testing.T) {
	base := t.TempDir()
	wt := t.TempDir()

	prdDir := filepath.Join(base, ".chief", "prds", "auth")
	if err := os.MkdirAll(prdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	homePRD := filepath.Join(prdDir, "prd.md")
	if err := os.WriteFile(homePRD, []byte("the project's copy"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A worktree that already carries an older copy — what a project tracking
	// .chief gets from the branch, missing every edit since it was committed.
	wtPRDDir := filepath.Join(wt, ".chief", "prds", "auth")
	if err := os.MkdirAll(wtPRDDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wtPRD := filepath.Join(wtPRDDir, "prd.md")
	if err := os.WriteFile(wtPRD, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SeedWorktree(base, homePRD, wt, false); err != nil {
		t.Fatalf("SeedWorktree: %v", err)
	}
	got, err := os.ReadFile(wtPRD)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "the project's copy" {
		t.Errorf("a fresh worktree kept %q, want the project's copy", got)
	}
}

func TestSeedWorktreeLeavesAReusedWorktreesOwnProgressAlone(t *testing.T) {
	base := t.TempDir()
	wt := t.TempDir()

	prdDir := filepath.Join(base, ".chief", "prds", "auth")
	if err := os.MkdirAll(prdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	homePRD := filepath.Join(prdDir, "prd.md")
	if err := os.WriteFile(homePRD, []byte("the project's copy"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A reused worktree's copy is where an earlier run recorded its progress.
	// Overwriting it would throw that record away.
	wtPRDDir := filepath.Join(wt, ".chief", "prds", "auth")
	if err := os.MkdirAll(wtPRDDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wtPRD := filepath.Join(wtPRDDir, "prd.md")
	if err := os.WriteFile(wtPRD, []byte("two stories already done"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SeedWorktree(base, homePRD, wt, true); err != nil {
		t.Fatalf("SeedWorktree: %v", err)
	}
	got, err := os.ReadFile(wtPRD)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "two stories already done" {
		t.Errorf("a reused worktree's copy was overwritten with %q", got)
	}
}

func TestSeedWorktreeSeedsAReusedWorktreeThatHasNoCopyYet(t *testing.T) {
	base := t.TempDir()
	wt := t.TempDir()

	prdDir := filepath.Join(base, ".chief", "prds", "auth")
	if err := os.MkdirAll(prdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	homePRD := filepath.Join(prdDir, "prd.md")
	if err := os.WriteFile(homePRD, []byte("the project's copy"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Reused, but nothing to preserve: the run would otherwise start with no PRD.
	if err := SeedWorktree(base, homePRD, wt, true); err != nil {
		t.Fatalf("SeedWorktree: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(wt, ".chief", "prds", "auth", "prd.md"))
	if err != nil {
		t.Fatalf("the worktree was left without a PRD: %v", err)
	}
	if string(got) != "the project's copy" {
		t.Errorf("got %q, want the project's copy", got)
	}
}

func TestSeedWorktreeLeavesAPRDOutsideTheProjectWhereItIs(t *testing.T) {
	base := t.TempDir()
	wt := t.TempDir()
	outside := t.TempDir()

	homePRD := filepath.Join(outside, "prd.md")
	if err := os.WriteFile(homePRD, []byte("kept elsewhere"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A PRD that does not live under the project has no counterpart in a
	// worktree, and inventing one would put the run's state somewhere nobody
	// asked for.
	if err := SeedWorktree(base, homePRD, wt, false); err != nil {
		t.Fatalf("SeedWorktree: %v", err)
	}
	if entries, err := os.ReadDir(wt); err != nil || len(entries) != 0 {
		t.Errorf("the worktree gained %v, want nothing", entries)
	}
}
