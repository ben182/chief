package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initTestRepoWithRemote returns a working repo whose origin is a local bare repo,
// plus the bare repo's path. Both start out sharing one commit on main.
func initTestRepoWithRemote(t *testing.T) (dir, remote string) {
	t.Helper()
	dir = initTestRepo(t)
	remote = filepath.Join(t.TempDir(), "origin.git")

	runInDir(t, "", "git", "init", "--bare", remote)
	runInDir(t, dir, "git", "remote", "add", "origin", remote)
	runInDir(t, dir, "git", "push", "-u", "origin", "main")
	return dir, remote
}

// runInDir runs a command in dir (or the process's cwd when dir is empty) and
// fails the test if it doesn't succeed.
func runInDir(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v failed: %s", args, string(out))
	}
}

// pushBranchFromClone makes commits on branch via a second clone and pushes them,
// simulating another machine advancing the same chief/<prd> branch.
func pushBranchFromClone(t *testing.T, remote, branch string, files ...string) {
	t.Helper()
	clone := filepath.Join(t.TempDir(), "clone")
	runInDir(t, "", "git", "clone", remote, clone)
	runInDir(t, clone, "git", "config", "user.email", "other@test.com")
	runInDir(t, clone, "git", "config", "user.name", "Other")
	runInDir(t, clone, "git", "checkout", "-B", branch)
	for _, f := range files {
		commitFile(t, clone, f, "from clone\n", "add "+f)
	}
	runInDir(t, clone, "git", "push", "-u", "origin", branch)
}

func TestBranchSyncPredicates(t *testing.T) {
	tests := []struct {
		name            string
		sync            BranchSync
		diverged        bool
		fastForwardable bool
	}{
		{"no remote branch", BranchSync{}, false, false},
		{"in sync", BranchSync{RemoteExists: true}, false, false},
		{"only ahead", BranchSync{RemoteExists: true, Ahead: 3}, false, false},
		{"strictly behind", BranchSync{RemoteExists: true, Behind: 2}, true, true},
		{"diverged both ways", BranchSync{RemoteExists: true, Behind: 2, Ahead: 3}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sync.Diverged(); got != tt.diverged {
				t.Errorf("Diverged() = %v, want %v", got, tt.diverged)
			}
			if got := tt.sync.FastForwardable(); got != tt.fastForwardable {
				t.Errorf("FastForwardable() = %v, want %v", got, tt.fastForwardable)
			}
		})
	}
}

func TestCheckBranchSync(t *testing.T) {
	t.Run("reports nothing without a remote", func(t *testing.T) {
		dir := initTestRepo(t)
		runInDir(t, dir, "git", "checkout", "-b", "chief/default")

		if got := CheckBranchSync(dir, "chief/default"); got.Diverged() {
			t.Errorf("CheckBranchSync() = %+v, want no divergence without origin", got)
		}
	})

	t.Run("reports nothing for a branch the remote has never seen", func(t *testing.T) {
		dir, _ := initTestRepoWithRemote(t)
		runInDir(t, dir, "git", "checkout", "-b", "chief/default")
		commitFile(t, dir, "local.txt", "local\n", "add local.txt")

		got := CheckBranchSync(dir, "chief/default")
		if got.RemoteExists || got.Diverged() {
			t.Errorf("CheckBranchSync() = %+v, want zero value for unpushed branch", got)
		}
	})

	t.Run("skips protected branches", func(t *testing.T) {
		dir, remote := initTestRepoWithRemote(t)
		pushBranchFromClone(t, remote, "main", "remote.txt")

		// main really is behind now, but we never auto-push it, so there is
		// nothing to warn about.
		if got := CheckBranchSync(dir, "main"); got.Diverged() {
			t.Errorf("CheckBranchSync() = %+v, want protected branch skipped", got)
		}
	})

	t.Run("reports nothing for a branch missing locally", func(t *testing.T) {
		dir, remote := initTestRepoWithRemote(t)
		pushBranchFromClone(t, remote, "chief/default", "remote.txt")

		if got := CheckBranchSync(dir, "chief/default"); got.RemoteExists {
			t.Errorf("CheckBranchSync() = %+v, want zero value when branch is local-only absent", got)
		}
	})

	t.Run("detects a branch strictly behind the remote", func(t *testing.T) {
		dir, remote := initTestRepoWithRemote(t)
		pushBranchFromClone(t, remote, "chief/default", "remote1.txt", "remote2.txt")

		// Recreate the branch locally from main, the state chief lands in after a
		// `clean` removed the local branch that had already been pushed.
		runInDir(t, dir, "git", "checkout", "-b", "chief/default", "main")

		got := CheckBranchSync(dir, "chief/default")
		if !got.Diverged() {
			t.Fatalf("CheckBranchSync() = %+v, want divergence", got)
		}
		if got.Behind != 2 || got.Ahead != 0 {
			t.Errorf("CheckBranchSync() = %+v, want behind 2 / ahead 0", got)
		}
		if !got.FastForwardable() {
			t.Error("expected a strictly-behind branch to be fast-forwardable")
		}
	})

	t.Run("detects a branch diverged in both directions", func(t *testing.T) {
		dir, remote := initTestRepoWithRemote(t)
		pushBranchFromClone(t, remote, "chief/default", "remote1.txt")

		runInDir(t, dir, "git", "checkout", "-b", "chief/default", "main")
		commitFile(t, dir, "local1.txt", "local\n", "add local1.txt")
		commitFile(t, dir, "local2.txt", "local\n", "add local2.txt")

		got := CheckBranchSync(dir, "chief/default")
		if got.Behind != 1 || got.Ahead != 2 {
			t.Errorf("CheckBranchSync() = %+v, want behind 1 / ahead 2", got)
		}
		if got.FastForwardable() {
			t.Error("expected a two-way divergence not to be fast-forwardable")
		}
	})

	t.Run("reports no divergence once the remote is merged in", func(t *testing.T) {
		dir, remote := initTestRepoWithRemote(t)
		pushBranchFromClone(t, remote, "chief/default", "remote1.txt")
		runInDir(t, dir, "git", "checkout", "-b", "chief/default", "main")
		runInDir(t, dir, "git", "fetch", "origin", "chief/default")
		runInDir(t, dir, "git", "merge", "--ff-only", "FETCH_HEAD")

		if got := CheckBranchSync(dir, "chief/default"); got.Diverged() {
			t.Errorf("CheckBranchSync() = %+v, want no divergence after merging", got)
		}
	})
}

func TestSyncBranchToRemote(t *testing.T) {
	t.Run("fast-forwards a strictly behind branch", func(t *testing.T) {
		dir, remote := initTestRepoWithRemote(t)
		pushBranchFromClone(t, remote, "chief/default", "remote1.txt", "remote2.txt")
		runInDir(t, dir, "git", "checkout", "-b", "chief/default", "main")

		if err := SyncBranchToRemote(dir, "chief/default"); err != nil {
			t.Fatalf("SyncBranchToRemote() error = %v", err)
		}

		if got := CheckBranchSync(dir, "chief/default"); got.Diverged() {
			t.Errorf("after sync: %+v, want no divergence", got)
		}
		if _, err := os.Stat(filepath.Join(dir, "remote2.txt")); err != nil {
			t.Errorf("expected remote commits present after fast-forward: %v", err)
		}
	})

	t.Run("rebases local commits onto the remote tip", func(t *testing.T) {
		dir, remote := initTestRepoWithRemote(t)
		pushBranchFromClone(t, remote, "chief/default", "remote1.txt")
		runInDir(t, dir, "git", "checkout", "-b", "chief/default", "main")
		commitFile(t, dir, "local1.txt", "local\n", "add local1.txt")

		if err := SyncBranchToRemote(dir, "chief/default"); err != nil {
			t.Fatalf("SyncBranchToRemote() error = %v", err)
		}

		got := CheckBranchSync(dir, "chief/default")
		if got.Behind != 0 {
			t.Errorf("after rebase: %+v, want behind 0", got)
		}
		if got.Ahead != 1 {
			t.Errorf("after rebase: %+v, want the local commit replayed on top (ahead 1)", got)
		}
		// Both histories must be present in the working tree.
		for _, f := range []string{"remote1.txt", "local1.txt"} {
			if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
				t.Errorf("expected %s after rebase: %v", f, err)
			}
		}
	})

	t.Run("aborts a conflicting rebase and leaves the branch intact", func(t *testing.T) {
		dir, remote := initTestRepoWithRemote(t)
		// Both sides touch the same file with different content.
		pushBranchFromClone(t, remote, "chief/default", "shared.txt")
		runInDir(t, dir, "git", "checkout", "-b", "chief/default", "main")
		commitFile(t, dir, "shared.txt", "local version\n", "add shared.txt")

		headBefore, err := runGit(dir, "rev-parse", "HEAD")
		if err != nil {
			t.Fatalf("rev-parse: %v", err)
		}

		if err := SyncBranchToRemote(dir, "chief/default"); err == nil {
			t.Fatal("SyncBranchToRemote() expected an error on conflicting rebase, got nil")
		}

		// The abort must have restored the original tip and left no rebase in progress.
		headAfter, err := runGit(dir, "rev-parse", "HEAD")
		if err != nil {
			t.Fatalf("rev-parse: %v", err)
		}
		if headAfter != headBefore {
			t.Errorf("HEAD = %s, want unchanged %s after aborted rebase", headAfter, headBefore)
		}
		if _, err := os.Stat(filepath.Join(dir, ".git", "rebase-merge")); !os.IsNotExist(err) {
			t.Error("expected no rebase in progress after abort")
		}
	})

	t.Run("is a no-op for a branch already in sync", func(t *testing.T) {
		dir, _ := initTestRepoWithRemote(t)
		runInDir(t, dir, "git", "checkout", "-b", "chief/default")
		commitFile(t, dir, "local.txt", "local\n", "add local.txt")
		runInDir(t, dir, "git", "push", "-u", "origin", "chief/default")

		if err := SyncBranchToRemote(dir, "chief/default"); err != nil {
			t.Errorf("SyncBranchToRemote() error = %v, want nil for in-sync branch", err)
		}
	})
}

func TestParseRevListCounts(t *testing.T) {
	tests := []struct {
		in          string
		left, right int
		ok          bool
	}{
		{"2\t3", 2, 3, true},
		{"0\t0", 0, 0, true},
		{"  4\t5  ", 4, 5, true},
		{"2", 0, 0, false},
		{"", 0, 0, false},
		{"a\tb", 0, 0, false},
		{"1\t2\t3", 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			left, right, ok := parseRevListCounts(tt.in)
			if ok != tt.ok || left != tt.left || right != tt.right {
				t.Errorf("parseRevListCounts(%q) = (%d, %d, %v), want (%d, %d, %v)",
					tt.in, left, right, ok, tt.left, tt.right, tt.ok)
			}
		})
	}
}
