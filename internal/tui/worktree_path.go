package tui

import (
	"path/filepath"
	"strings"

	"github.com/ben182/chief/internal/git"
)

// worktreeBranchFor names the branch chief gives a PRD's worktree. It is the
// same name BranchWarning suggests, so a {branch} placeholder in the path
// template points at the same directory before and after the dialog.
func worktreeBranchFor(prdName string) string {
	return "chief/" + prdName
}

// worktreeDirSetting returns the configured worktree.dir template, trimmed.
// Empty — no config, or nothing configured — leaves the default location in
// place.
func (a App) worktreeDirSetting() string {
	if a.config == nil {
		return ""
	}
	return strings.TrimSpace(a.config.Worktree.Dir)
}

// worktreePathFor resolves the configured template into the absolute path of a
// PRD's worktree. It fails when the template points somewhere a worktree cannot
// live; the caller reports that instead of starting a run that git would refuse.
func (a App) worktreePathFor(prdName, branch string) (string, error) {
	return git.WorktreePathForPRD(a.baseDir, a.worktreeDirSetting(), prdName, branch)
}

// displayWorktreePath renders a worktree path for the UI: relative to the main
// checkout when it sits inside it, absolute when the template puts it
// elsewhere, and with a trailing separator either way so it reads as a
// directory.
func displayWorktreePath(baseDir, worktreePath string) string {
	if worktreePath == "" {
		return ""
	}
	rel, err := filepath.Rel(baseDir, worktreePath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return worktreePath + string(filepath.Separator)
	}
	return rel + string(filepath.Separator)
}
