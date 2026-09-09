package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/loop"
)

// initRepoOnMain makes dir a git repo with one commit, sitting on main —
// a protected branch, so starting a PRD raises the worktree dialog.
func initRepoOnMain(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %s", args, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	run("checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n"), 0644); err != nil {
		t.Fatalf("failed to write README: %v", err)
	}
	run("add", ".")
	run("commit", "-m", "initial commit")
}

// worktreeDirApp is an App on repo dir with worktree.dir set to template.
func worktreeDirApp(t *testing.T, dir, template string) *App {
	t.Helper()
	a := newTestApp(nil, 100, 30)
	a.baseDir = dir
	a.config = &config.Config{Worktree: config.WorktreeConfig{Dir: template}}
	a.branchWarning = NewBranchWarning()
	return a
}

// The dialog is where the user learns where the worktree is about to go, so it
// has to name the configured location rather than the one chief used to use.
func TestStartLoopShowsConfiguredWorktreePath(t *testing.T) {
	dir := t.TempDir()
	initRepoOnMain(t, dir)
	a := worktreeDirApp(t, dir, "../{repo}-worktrees/{branch}")

	model, _ := a.startLoopForPRD("auth")
	got, ok := model.(App)
	if !ok {
		t.Fatalf("unexpected model type %T", model)
	}
	if got.viewMode != ViewBranchWarning {
		t.Fatalf("viewMode = %v, want ViewBranchWarning", got.viewMode)
	}

	want := filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-worktrees", "chief-auth")
	if got.pendingWorktreePath != want {
		t.Errorf("pendingWorktreePath = %q, want %q", got.pendingWorktreePath, want)
	}
}

// The hint beside the "Create worktree" option names the same directory, shown
// relative to the checkout while the template keeps it inside.
func TestBranchWarningHintShowsConfiguredWorktreePath(t *testing.T) {
	dir := t.TempDir()
	initRepoOnMain(t, dir)
	a := worktreeDirApp(t, dir, "wt/{prd}")

	model, _ := a.startLoopForPRD("auth")
	got := model.(App)
	got.branchWarning.SetSize(120, 40)

	rendered := stripANSI(got.branchWarning.Render())
	if !strings.Contains(rendered, "wt/auth/") {
		t.Errorf("expected the dialog to show %q, got:\n%s", "wt/auth/", rendered)
	}
	if strings.Contains(rendered, ".chief/worktrees/auth") {
		t.Error("expected the dialog to drop the old fixed path")
	}
}

// A template that cannot hold a worktree has to say so before anything is
// created, not leave git to fail halfway through.
func TestStartLoopReportsUnusableWorktreeTemplate(t *testing.T) {
	dir := t.TempDir()
	initRepoOnMain(t, dir)
	a := worktreeDirApp(t, dir, ".")

	model, _ := a.startLoopForPRD("auth")
	got, ok := model.(App)
	if !ok {
		t.Fatalf("unexpected model type %T", model)
	}
	if got.viewMode == ViewBranchWarning {
		t.Error("expected no worktree dialog for an unusable template")
	}
	if !strings.Contains(got.lastActivity, "main checkout") {
		t.Errorf("lastActivity = %q, want it to explain the rejected template", got.lastActivity)
	}
}

// The picker is the only place an orphaned worktree turns up, so it has to find
// them wherever the template puts them.
func TestPickerFindsOrphanedWorktreeAtConfiguredPath(t *testing.T) {
	dir := t.TempDir()
	initRepoOnMain(t, dir)

	template := "../{repo}-worktrees/{prd}"
	worktreePath := filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-worktrees", "auth")
	if _, err := git.CreateWorktree(git.CreateWorktreeOptions{
		RepoDir:      dir,
		WorktreePath: worktreePath,
		Branch:       "chief/auth",
	}); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}

	p := NewPRDPicker(dir, "", nil, template)

	var entry *PRDEntry
	for i := range p.entries {
		if p.entries[i].Name == "auth" {
			entry = &p.entries[i]
		}
	}
	if entry == nil {
		t.Fatalf("expected an entry for the orphaned worktree, got %d entries", len(p.entries))
	}
	if !entry.Orphaned {
		t.Error("expected the entry to be marked orphaned")
	}
	if entry.WorktreeDir != worktreePath {
		t.Errorf("WorktreeDir = %q, want %q", entry.WorktreeDir, worktreePath)
	}
	if display := p.worktreeDisplayPath(*entry); display != worktreePath+"/" {
		t.Errorf("display path = %q, want %q", display, worktreePath+"/")
	}
}

// The clean flow removes the directory the picker found, so it must not
// re-derive a path of its own — an orphan has no branch to feed {branch}.
func TestCleanConfirmationCarriesTheResolvedWorktreePath(t *testing.T) {
	p := NewPRDPicker(t.TempDir(), "", nil, "")
	p.entries = []PRDEntry{{
		Name:        "auth",
		WorktreeDir: "/tmp/project-worktrees/auth",
		LoopState:   loop.LoopStateReady,
	}}
	p.selectedIndex = 0
	p.StartCleanConfirmation()

	cc := p.GetCleanConfirmation()
	if cc == nil {
		t.Fatal("expected a clean confirmation")
	}
	if cc.WorktreePath != "/tmp/project-worktrees/auth" {
		t.Errorf("WorktreePath = %q, want %q", cc.WorktreePath, "/tmp/project-worktrees/auth")
	}
}

// The clean flow removes the directory that is actually there. Deriving it from
// the template again would miss a worktree the template no longer describes —
// and an orphan has no branch to feed a {branch} placeholder with.
func TestCleanRemovesTheWorktreeThePickerFound(t *testing.T) {
	base := t.TempDir()
	writePRDFixture(t, base, "auth", "todo")
	a := pickerApp(t, base, "auth")
	a.config = &config.Config{Worktree: config.WorktreeConfig{Dir: "../{repo}-worktrees/{prd}"}}

	worktreePath := filepath.Join(filepath.Dir(base), filepath.Base(base)+"-worktrees", "auth")
	a.picker.entries = []PRDEntry{{
		Name:        "auth",
		Path:        filepath.Join(base, ".chief", "prds", "auth", "prd.md"),
		WorktreeDir: worktreePath,
		LoopState:   loop.LoopStateReady,
	}}
	a.picker.selectedIndex = 0
	a.picker.StartCleanConfirmation()

	removed := ""
	a.removeWorktree = func(repoDir, path string) error {
		removed = path
		return nil
	}

	_, cmd := a.handlePickerKeys(key("enter"))
	if cmd == nil {
		t.Fatal("expected a clean command")
	}
	cmd()

	if removed != worktreePath {
		t.Errorf("removed %q, want %q", removed, worktreePath)
	}
}
