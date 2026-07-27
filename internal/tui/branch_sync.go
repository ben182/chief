package tui

import (
	"fmt"

	"github.com/ben182/chief/internal/git"
	tea "github.com/charmbracelet/bubbletea"
)

// branchSyncPreflight returns a command that compares the branch this run will
// commit to against origin, or nil when the run may start immediately.
//
// It exists because the cost of finding out late is so lopsided: a branch that is
// behind origin still accepts every commit a run makes, and only the push at the
// very end is rejected — after hours of agent time. The same question costs one
// fetch up front.
//
// The check runs once per PRD start (see App.branchSyncChecked) and is skipped for
// worktree runs, whose branch is created fresh from the default branch in a
// directory of its own, and for protected branches, which are never auto-pushed.
func (a *App) branchSyncPreflight(prdName, prdDir string) tea.Cmd {
	if a.branchSyncChecked == nil {
		a.branchSyncChecked = make(map[string]bool)
	}
	if a.branchSyncChecked[prdName] {
		return nil
	}
	if !git.IsGitRepo(a.baseDir) {
		return nil
	}
	if a.manager != nil {
		if inst := a.manager.GetInstance(prdName); inst != nil && inst.WorktreeDir != "" {
			return nil
		}
	}
	branch, err := git.GetCurrentBranch(a.baseDir)
	if err != nil || branch == "" || git.IsProtectedBranch(branch) {
		return nil
	}

	// Mark it done now: the answer arrives asynchronously, and the resumed start
	// must not queue the check a second time.
	a.branchSyncChecked[prdName] = true
	a.lastActivity = fmt.Sprintf("Checking %s against origin...", branch)

	dir := a.baseDir
	return func() tea.Msg {
		return branchSyncCheckMsg{
			prdName: prdName,
			prdDir:  prdDir,
			branch:  branch,
			sync:    git.CheckBranchSync(dir, branch),
		}
	}
}

// handleBranchSyncCheck acts on the preflight result: a branch in sync (or one we
// couldn't compare) starts the run as if nothing had interrupted it, while a
// diverged branch raises the dialog so the user decides before any work is done.
func (a App) handleBranchSyncCheck(msg branchSyncCheckMsg) (tea.Model, tea.Cmd) {
	if !msg.sync.Diverged() {
		a.lastActivity = ""
		return a.doStartLoop(msg.prdName, msg.prdDir)
	}

	a.branchWarning.SetSize(a.width, a.height)
	a.branchWarning.SetContext(msg.branch, msg.prdName, git.WorktreePathForPRD(a.baseDir, msg.prdName))
	a.branchWarning.SetSyncState(msg.branch, msg.sync)
	a.branchWarning.SetDialogContext(DialogBranchBehindRemote)
	a.branchWarning.Reset()
	a.pendingStartPRD = msg.prdName
	a.pendingSyncBranch = msg.branch
	a.viewMode = ViewBranchWarning
	a.lastActivity = fmt.Sprintf("%s is behind origin", msg.branch)
	return a, nil
}

// runBranchSync returns a command that reconciles branch with origin before the
// interrupted start resumes.
func (a *App) runBranchSync(prdName, prdDir, branch string) tea.Cmd {
	dir := a.baseDir
	return func() tea.Msg {
		return branchSyncResultMsg{
			prdName: prdName,
			prdDir:  prdDir,
			branch:  branch,
			err:     git.SyncBranchToRemote(dir, branch),
		}
	}
}

// handleBranchSyncResult resumes the start once the branch is reconciled. A failed
// reconcile (typically a rebase that conflicts, which SyncBranchToRemote aborts)
// stops the start: the divergence is still there, so starting would walk straight
// back into the rejected push this check exists to prevent.
func (a App) handleBranchSyncResult(msg branchSyncResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		a.lastActivity = "Could not sync with origin: " + msg.err.Error()
		return a, nil
	}
	a.lastActivity = fmt.Sprintf("Synced %s with origin", msg.branch)
	return a.doStartLoop(msg.prdName, msg.prdDir)
}
