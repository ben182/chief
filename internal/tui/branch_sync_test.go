package tui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/loop"
)

// initSyncTestRepo creates a git repo checked out on branch, with a bare origin
// that already has that branch, so tests can make the two diverge.
func initSyncTestRepo(t *testing.T, branch string) (dir, remote string) {
	t.Helper()
	dir = t.TempDir()
	remote = filepath.Join(t.TempDir(), "origin.git")
	mustRun(t, "", "git", "init", "--bare", remote)

	mustRun(t, dir, "git", "init", "-b", "main")
	mustRun(t, dir, "git", "config", "user.email", "test@test.com")
	mustRun(t, dir, "git", "config", "user.name", "Test")
	writeAndCommit(t, dir, "README.md", "# Test\n")
	mustRun(t, dir, "git", "remote", "add", "origin", remote)
	mustRun(t, dir, "git", "push", "-u", "origin", "main")

	if branch != "main" {
		mustRun(t, dir, "git", "checkout", "-b", branch)
		mustRun(t, dir, "git", "push", "-u", "origin", branch)
	}
	return dir, remote
}

// advanceRemoteBranch pushes a commit to branch on remote via a separate clone,
// leaving the original repo behind — as another machine would.
func advanceRemoteBranch(t *testing.T, remote, branch string) {
	t.Helper()
	clone := filepath.Join(t.TempDir(), "clone")
	mustRun(t, "", "git", "clone", remote, clone)
	mustRun(t, clone, "git", "config", "user.email", "other@test.com")
	mustRun(t, clone, "git", "config", "user.name", "Other")
	mustRun(t, clone, "git", "checkout", branch)
	writeAndCommit(t, clone, "remote.txt", "from elsewhere\n")
	mustRun(t, clone, "git", "push", "origin", branch)
}

func writeAndCommit(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	mustRun(t, dir, "git", "add", name)
	mustRun(t, dir, "git", "commit", "-m", "add "+name)
}

func mustRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v failed: %s", args, string(out))
	}
}

// behindRemoteDialog builds the divergence dialog the way the preflight does, so
// the tests exercise the real call order (SetSyncState before SetDialogContext,
// then Reset).
func behindRemoteDialog(branch string, sync git.BranchSync) *BranchWarning {
	bw := NewBranchWarning()
	bw.SetSize(80, 24)
	bw.SetContext(branch, "default", ".chief/worktrees/default/")
	bw.SetSyncState(branch, sync)
	bw.SetDialogContext(DialogBranchBehindRemote)
	bw.Reset()
	return bw
}

func TestBranchWarningBehindRemoteOptions(t *testing.T) {
	bw := behindRemoteDialog("chief/default", git.BranchSync{RemoteExists: true, Behind: 3})

	if len(bw.options) != 3 {
		t.Fatalf("expected 3 options when behind remote, got %d", len(bw.options))
	}
	if bw.options[0].option != BranchOptionSyncWithRemote {
		t.Errorf("expected first option to be SyncWithRemote, got %v", bw.options[0].option)
	}
	if !bw.options[0].recommended {
		t.Error("expected syncing to be the recommended option")
	}
	if bw.GetSelectedOption() != BranchOptionSyncWithRemote {
		t.Errorf("expected default selection to be SyncWithRemote, got %v", bw.GetSelectedOption())
	}
	if bw.options[1].option != BranchOptionContinue {
		t.Errorf("expected second option to be Continue, got %v", bw.options[1].option)
	}
	if bw.options[2].option != BranchOptionCancel {
		t.Errorf("expected third option to be Cancel, got %v", bw.options[2].option)
	}
}

func TestBranchWarningBehindRemoteNamesTheOperation(t *testing.T) {
	t.Run("fast-forward when nothing local to replay", func(t *testing.T) {
		bw := behindRemoteDialog("chief/default", git.BranchSync{RemoteExists: true, Behind: 2})
		label := bw.options[0].label
		if !strings.Contains(label, "Fast-forward") {
			t.Errorf("label = %q, want it to name a fast-forward", label)
		}
		if !strings.Contains(label, "origin/chief/default") {
			t.Errorf("label = %q, want the remote branch named", label)
		}
	})

	t.Run("rebase when local commits exist", func(t *testing.T) {
		bw := behindRemoteDialog("chief/default", git.BranchSync{RemoteExists: true, Behind: 2, Ahead: 4})
		label := bw.options[0].label
		if !strings.Contains(label, "Rebase") {
			t.Errorf("label = %q, want it to name a rebase", label)
		}
	})
}

func TestBranchWarningBehindRemoteRender(t *testing.T) {
	t.Run("reports both commit counts", func(t *testing.T) {
		bw := behindRemoteDialog("chief/default", git.BranchSync{RemoteExists: true, Behind: 3, Ahead: 7})
		out := bw.Render()

		for _, want := range []string{"Behind Remote", "3 commits", "7 commits", "rejected"} {
			if !strings.Contains(out, want) {
				t.Errorf("rendered dialog missing %q", want)
			}
		}
	})

	t.Run("omits the local count when there is nothing ahead", func(t *testing.T) {
		bw := behindRemoteDialog("chief/default", git.BranchSync{RemoteExists: true, Behind: 1})
		out := bw.Render()

		if !strings.Contains(out, "1 commit") {
			t.Error("rendered dialog should report the single remote commit")
		}
		if strings.Contains(out, "You have") {
			t.Error("rendered dialog should not claim local commits when Ahead is 0")
		}
	})

	t.Run("does not offer branch-name editing", func(t *testing.T) {
		bw := behindRemoteDialog("chief/default", git.BranchSync{RemoteExists: true, Behind: 1})
		if out := bw.Render(); strings.Contains(out, "Edit branch") {
			t.Error("no option here creates a branch, so the footer must not offer to edit one")
		}
	})
}

// Reset re-derives the branch name from the PRD for contexts where it's an
// editable suggestion. Here it names a branch that already exists, and clobbering
// it would point the sync at chief/<prd> instead of the real branch.
func TestBranchWarningBehindRemoteKeepsBranchAcrossReset(t *testing.T) {
	bw := behindRemoteDialog("feature/legacy-name", git.BranchSync{RemoteExists: true, Behind: 1})

	if got := bw.GetSuggestedBranch(); got != "feature/legacy-name" {
		t.Errorf("GetSuggestedBranch() = %q, want the actual branch preserved", got)
	}
}

func TestBranchWarningResetStillDerivesBranchForOtherContexts(t *testing.T) {
	bw := NewBranchWarning()
	bw.SetContext("main", "auth", ".chief/worktrees/auth/")
	bw.SetDialogContext(DialogProtectedBranch)
	bw.branchName = "edited/by/user"
	bw.Reset()

	if got := bw.GetSuggestedBranch(); got != "chief/auth" {
		t.Errorf("GetSuggestedBranch() = %q, want reset to chief/auth", got)
	}
}

func TestPluralCommits(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0 commits"},
		{1, "1 commit"},
		{2, "2 commits"},
	}
	for _, tt := range tests {
		if got := pluralCommits(tt.n); got != tt.want {
			t.Errorf("pluralCommits(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

// preflightRepo builds a git repo on branch with a bare origin, and an App rooted
// at it, mirroring the shape the preflight sees at start time.
func preflightRepo(t *testing.T, branch string) (*App, string, string) {
	t.Helper()
	dir, remote := initSyncTestRepo(t, branch)
	app := &App{
		baseDir:           dir,
		manager:           loop.NewManager(10, nil),
		branchSyncChecked: make(map[string]bool),
	}
	return app, dir, remote
}

// runPreflight invokes the preflight and returns the resulting message, or nil
// when the preflight decided the run may start immediately.
func runPreflight(app *App, prdName string) *branchSyncCheckMsg {
	cmd := app.branchSyncPreflight(prdName, "/tmp/prd")
	if cmd == nil {
		return nil
	}
	msg, ok := cmd().(branchSyncCheckMsg)
	if !ok {
		return nil
	}
	return &msg
}

func TestBranchSyncPreflight(t *testing.T) {
	t.Run("flags a branch behind origin", func(t *testing.T) {
		app, _, remote := preflightRepo(t, "chief/default")
		advanceRemoteBranch(t, remote, "chief/default")

		msg := runPreflight(app, "default")
		if msg == nil {
			t.Fatal("expected the preflight to run for a chief branch")
		}
		if !msg.sync.Diverged() {
			t.Errorf("sync = %+v, want divergence detected", msg.sync)
		}
		if msg.branch != "chief/default" {
			t.Errorf("branch = %q, want chief/default", msg.branch)
		}
	})

	t.Run("clears a branch in sync with origin", func(t *testing.T) {
		app, _, _ := preflightRepo(t, "chief/default")

		msg := runPreflight(app, "default")
		if msg == nil {
			t.Fatal("expected the preflight to run")
		}
		if msg.sync.Diverged() {
			t.Errorf("sync = %+v, want no divergence", msg.sync)
		}
	})

	t.Run("skips a protected branch", func(t *testing.T) {
		app, _, _ := preflightRepo(t, "main")

		if msg := runPreflight(app, "default"); msg != nil {
			t.Errorf("expected no check on a protected branch, got %+v", msg)
		}
	})

	t.Run("skips a non-git directory", func(t *testing.T) {
		app := &App{baseDir: t.TempDir(), branchSyncChecked: make(map[string]bool)}

		if msg := runPreflight(app, "default"); msg != nil {
			t.Errorf("expected no check outside a git repo, got %+v", msg)
		}
	})

	t.Run("skips a worktree run", func(t *testing.T) {
		app, dir, remote := preflightRepo(t, "chief/default")
		advanceRemoteBranch(t, remote, "chief/default")
		// A worktree run commits in its own directory on a freshly created branch,
		// so the root's branch says nothing about it.
		if err := app.manager.RegisterWithWorktree("default", "/tmp/prd.md", dir+"/.chief/worktrees/default", "chief/default"); err != nil {
			t.Fatalf("RegisterWithWorktree: %v", err)
		}

		if msg := runPreflight(app, "default"); msg != nil {
			t.Errorf("expected no check for a worktree run, got %+v", msg)
		}
	})

	t.Run("runs only once per PRD", func(t *testing.T) {
		app, _, remote := preflightRepo(t, "chief/default")
		advanceRemoteBranch(t, remote, "chief/default")

		if msg := runPreflight(app, "default"); msg == nil {
			t.Fatal("expected the first preflight to run")
		}
		// The resumed start must not fetch and re-raise the dialog in a loop.
		if msg := runPreflight(app, "default"); msg != nil {
			t.Errorf("expected the second preflight to be skipped, got %+v", msg)
		}
	})

	t.Run("tracks PRDs independently", func(t *testing.T) {
		app, _, _ := preflightRepo(t, "chief/default")

		if msg := runPreflight(app, "default"); msg == nil {
			t.Fatal("expected the preflight to run for the first PRD")
		}
		if msg := runPreflight(app, "other"); msg == nil {
			t.Error("expected a different PRD to get its own check")
		}
	})
}

func TestHandleBranchSyncCheckRaisesDialog(t *testing.T) {
	app, _, _ := preflightRepo(t, "chief/default")
	app.branchWarning = NewBranchWarning()
	app.width, app.height = 80, 24

	m, cmd := app.handleBranchSyncCheck(branchSyncCheckMsg{
		prdName: "default",
		prdDir:  "/tmp/prd",
		branch:  "chief/default",
		sync:    git.BranchSync{RemoteExists: true, Behind: 2},
	})
	got := m.(App)

	if got.viewMode != ViewBranchWarning {
		t.Errorf("viewMode = %v, want ViewBranchWarning", got.viewMode)
	}
	if got.branchWarning.GetDialogContext() != DialogBranchBehindRemote {
		t.Error("expected the divergence dialog context")
	}
	if got.pendingSyncBranch != "chief/default" {
		t.Errorf("pendingSyncBranch = %q, want chief/default", got.pendingSyncBranch)
	}
	if got.pendingStartPRD != "default" {
		t.Errorf("pendingStartPRD = %q, want default", got.pendingStartPRD)
	}
	// The loop must not have been started behind the dialog's back.
	if cmd != nil {
		t.Error("expected no command to run while awaiting the user's decision")
	}
}

func TestHandleBranchSyncResultReportsFailure(t *testing.T) {
	app, _, _ := preflightRepo(t, "chief/default")
	// Mark the check done so the resumed start wouldn't re-run it either way.
	app.branchSyncChecked["default"] = true

	m, cmd := app.handleBranchSyncResult(branchSyncResultMsg{
		prdName: "default",
		prdDir:  "/tmp/prd",
		branch:  "chief/default",
		err:     errors.New("conflict during rebase"),
	})
	got := m.(App)

	if cmd != nil {
		t.Error("expected the start to stop when the branch could not be synced")
	}
	if !strings.Contains(got.lastActivity, "conflict during rebase") {
		t.Errorf("lastActivity = %q, want it to surface the sync failure", got.lastActivity)
	}
}
