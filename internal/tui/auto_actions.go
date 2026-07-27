package tui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/prd"
	"github.com/ben182/chief/internal/summary"
	tea "github.com/charmbracelet/bubbletea"
)

// showCompletionScreen configures and shows the completion screen for a PRD.
// Returns a tea.Cmd if auto-actions need to be started, nil otherwise.
func (a *App) showCompletionScreen(prdName string) tea.Cmd {
	// Count completed stories
	completed := 0
	total := len(a.prd.UserStories)
	for _, story := range a.prd.UserStories {
		if story.Passes {
			completed++
		}
	}

	// Get branch from manager
	branch := ""
	if instance := a.manager.GetInstance(prdName); instance != nil {
		branch = instance.Branch
	}

	// Count commits on the branch
	commitCount := 0
	if branch != "" {
		commitCount = git.CommitCount(a.baseDir, branch)
	}

	// Check if auto-actions are configured
	hasAutoActions := a.config != nil && (a.config.OnComplete.Push || a.config.OnComplete.CreatePR)

	totalDuration := a.GetElapsedTime()
	a.completionScreen.Configure(prdName, completed, total, branch, commitCount, hasAutoActions, totalDuration, a.sleptDuringRun(), a.storyTimings[prdName], a.totalCost)
	a.completionScreen.SetSize(a.width, a.height)
	a.viewMode = ViewCompletion

	// Always start confetti tick
	cmds := []tea.Cmd{tickConfetti()}

	// Post-completion actions only make sense when there is committed work: a run
	// can finish with zero commits (every story parked, or the agent never
	// committed), and there is then nothing to summarize, push, or PR.
	summaryEnabled := a.config != nil && a.config.OnComplete.Summary
	pushEnabled := a.config != nil && a.config.OnComplete.Push
	canAct := branch != "" && commitCount > 0

	switch {
	case summaryEnabled && canAct:
		// Summarize first, then chain into push (if enabled) so the summary commit
		// rides along in the pushed branch / PR.
		a.completionScreen.SetSummaryInProgress()
		cmds = append(cmds, tickCompletionSpinner(),
			a.runAutoSummary(prdName, true, pushEnabled))
	case pushEnabled && canAct:
		a.completionScreen.SetPushInProgress()
		cmds = append(cmds, tickCompletionSpinner(), a.runAutoPush())
	}

	// If only PR is configured (no push), we can't create a PR without pushing first
	// So PR-only without push is a no-op (push is required for PR)
	return tea.Batch(cmds...)
}

// runBackgroundAutoActions triggers summary/push/PR for a background PRD that
// just completed (no completion screen is shown for background PRDs). It writes
// and commits the summary first, best-effort, so it rides along in the push.
func (a *App) runBackgroundAutoActions(prdName string) tea.Cmd {
	summaryEnabled := a.config != nil && a.config.OnComplete.Summary
	pushEnabled := a.config != nil && a.config.OnComplete.Push
	if !summaryEnabled && !pushEnabled {
		return nil
	}

	instance := a.manager.GetInstance(prdName)
	if instance == nil || instance.Branch == "" {
		return nil
	}

	branch := instance.Branch
	dir := a.baseDir
	if instance.WorktreeDir != "" {
		dir = instance.WorktreeDir
	}

	// Don't act on a branch with no committed work (see showCompletionScreen).
	if git.CommitCount(dir, branch) == 0 {
		return nil
	}

	provider := a.provider
	prdPath := instance.PRDPath
	prdDir := a.summaryDir(prdName, dir)
	sinceRef := instance.StartRef

	return func() tea.Msg {
		if summaryEnabled {
			var stories []git.StoryRef
			var parked []string
			if p, err := prd.LoadPRD(prdPath); err == nil {
				stories = storyRefs(prdName, p)
				parked = parkedStoryLabels(p)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			_, _ = summary.Generate(ctx, provider, dir, prdDir, stories, parked, sinceRef) // best-effort
			cancel()
		}
		if !pushEnabled {
			return nil
		}
		if err := git.PushBranch(dir, branch); err != nil {
			return backgroundAutoActionResultMsg{prdName: prdName, action: "push", err: err}
		}
		return backgroundAutoActionResultMsg{prdName: prdName, action: "push"}
	}
}

// handleAutoActionResult handles the result of an auto-action (push or PR creation).
func (a App) handleAutoActionResult(msg autoActionResultMsg) (tea.Model, tea.Cmd) {
	switch msg.action {
	case "push":
		if msg.err != nil {
			a.completionScreen.SetPushError(msg.err.Error())
			return a, nil
		}
		a.completionScreen.SetPushSuccess()

		// If PR creation is configured, start it now
		if a.config != nil && a.config.OnComplete.CreatePR && a.completionScreen.HasBranch() {
			a.completionScreen.SetPRInProgress()
			return a, tea.Batch(
				tickCompletionSpinner(),
				a.runAutoCreatePR(),
			)
		}
		return a, nil

	case "pr":
		if msg.err != nil {
			a.completionScreen.SetPRError(msg.err.Error())
			return a, nil
		}
		a.completionScreen.SetPRSuccess(msg.pr)
		return a, nil
	}
	return a, nil
}

// handleBackgroundAutoAction handles auto-action results for background PRDs.
func (a App) handleBackgroundAutoAction(msg backgroundAutoActionResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		// Log error but don't block - background action failed silently
		return a, nil
	}

	if msg.action == "push" && a.config != nil && a.config.OnComplete.CreatePR {
		// Chain PR creation after successful push
		instance := a.manager.GetInstance(msg.prdName)
		if instance != nil && instance.Branch != "" {
			prdName := msg.prdName
			branch := instance.Branch
			dir := a.baseDir
			prdPath := instance.PRDPath
			if prdPath == "" {
				prdPath = prd.PRDPath(a.baseDir, prdName)
			}
			baseOverride := a.config.OnComplete.PRBaseBranch
			return a, func() tea.Msg {
				p, err := prd.LoadPRD(prdPath)
				if err != nil {
					return backgroundAutoActionResultMsg{prdName: prdName, action: "pr", err: err}
				}
				title := git.PRTitleFromPRD(prdName, p)
				body := git.PRBodyFromPRD(p)
				_, err = git.EnsurePR(dir, branch, title, body, baseOverride)
				return backgroundAutoActionResultMsg{prdName: prdName, action: "pr", err: err}
			}
		}
	}

	return a, nil
}

// branchFor returns the branch a PRD's loop is running on, or "" if unknown.
func (a *App) branchFor(prdName string) string {
	if inst := a.manager.GetInstance(prdName); inst != nil {
		return inst.Branch
	}
	return ""
}

// completionGitDir returns the directory whose branch post-completion actions
// (summary, push) operate on: the PRD's worktree when configured, else the
// project root.
func (a *App) completionGitDir(prdName string) string {
	if inst := a.manager.GetInstance(prdName); inst != nil && inst.WorktreeDir != "" {
		return inst.WorktreeDir
	}
	return a.baseDir
}

// summaryDir returns the directory the run summary for prdName should be written
// to, inside gitDir. It derives the PRD's own directory from its registered path
// (so the legacy .chief/prd.md and direct-path layouts land beside the PRD rather
// than in a phantom .chief/prds/<name>) and maps that location into gitDir, which
// is the worktree for worktree runs and the project root otherwise.
func (a *App) summaryDir(prdName, gitDir string) string {
	prdPath := a.prdPathForPRD(prdName)
	if prdPath == "" {
		return prd.PRDDir(gitDir, prdName)
	}
	rel, err := filepath.Rel(a.baseDir, filepath.Dir(prdPath))
	if err != nil || strings.HasPrefix(rel, "..") {
		return prd.PRDDir(gitDir, prdName)
	}
	return filepath.Join(gitDir, rel)
}

// parkedStoryLabels returns "ID - Title" for every story parked for human
// review, so the summary can call them out under its open-points section.
func parkedStoryLabels(p *prd.PRD) []string {
	if p == nil {
		return nil
	}
	var out []string
	for _, s := range p.UserStories {
		if s.NeedsReview {
			out = append(out, s.ID+" - "+s.Title)
		}
	}
	return out
}

// storyRefs maps a PRD's stories to git.StoryRef, in PRD order, so the summary
// can be scoped to the commits chief authored for exactly these stories. prdName
// namespaces the commit lookup so same-numbered stories from other PRDs on the
// branch are excluded.
func storyRefs(prdName string, p *prd.PRD) []git.StoryRef {
	if p == nil {
		return nil
	}
	refs := make([]git.StoryRef, 0, len(p.UserStories))
	for _, s := range p.UserStories {
		refs = append(refs, git.StoryRef{PRDName: prdName, ID: s.ID, Title: s.Title})
	}
	return refs
}

// runAutoSummary returns a tea.Cmd that generates and commits the run summary in
// the background. showOnScreen reflects progress on the completion screen;
// pushAfter asks the handler to start auto-push once the summary lands.
func (a *App) runAutoSummary(prdName string, showOnScreen, pushAfter bool) tea.Cmd {
	provider := a.provider
	gitDir := a.completionGitDir(prdName)
	prdDir := a.summaryDir(prdName, gitDir)
	stories := storyRefs(prdName, a.prd)
	parked := parkedStoryLabels(a.prd)
	sinceRef := ""
	if inst := a.manager.GetInstance(prdName); inst != nil {
		sinceRef = inst.StartRef
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		res, err := summary.Generate(ctx, provider, gitDir, prdDir, stories, parked, sinceRef)
		if errors.Is(err, summary.ErrNothingToSummarize) {
			err = nil // nothing to describe; treat as a clean skip
		}
		fileName := ""
		if res.Path != "" {
			fileName = filepath.Base(res.Path)
		}
		return summaryResultMsg{prdName: prdName, fileName: fileName, err: err, showOnScreen: showOnScreen, pushAfter: pushAfter}
	}
}

// handleSummaryResult reflects summary completion on the UI and, when requested,
// chains into auto-push so the summary commit is included in the pushed branch.
func (a App) handleSummaryResult(msg summaryResultMsg) (tea.Model, tea.Cmd) {
	if msg.showOnScreen {
		if msg.err != nil {
			a.completionScreen.SetSummaryError(msg.err.Error())
		} else {
			a.completionScreen.SetSummarySuccess(msg.fileName)
		}
		// Push even if the summary failed: the per-story commits are still worth
		// pushing, and the failure is already surfaced on the completion screen.
		if msg.pushAfter && a.completionScreen.HasBranch() {
			a.completionScreen.SetPushInProgress()
			return a, tea.Batch(tickCompletionSpinner(), a.runAutoPush())
		}
		return a, nil
	}
	// No completion screen (e.g. a max-iterations partial run): note the outcome.
	if msg.prdName == a.prdName {
		if msg.err != nil {
			a.lastActivity = "Summary failed: " + msg.err.Error()
		} else if msg.fileName != "" {
			a.lastActivity = "Run summary written (" + msg.fileName + ")"
		} else {
			a.lastActivity = "Run summary written"
		}
	}
	return a, nil
}

// runAutoPush returns a tea.Cmd that pushes the branch in the background.
func (a *App) runAutoPush() tea.Cmd {
	branch := a.completionScreen.Branch()
	// Use worktree dir if available, otherwise base dir
	dir := a.baseDir
	if instance := a.manager.GetInstance(a.completionScreen.PRDName()); instance != nil && instance.WorktreeDir != "" {
		dir = instance.WorktreeDir
	}
	return func() tea.Msg {
		err := git.PushBranch(dir, branch)
		return autoActionResultMsg{action: "push", err: err}
	}
}

// runAutoCreatePR returns a tea.Cmd that creates a PR in the background.
func (a *App) runAutoCreatePR() tea.Cmd {
	prdName := a.completionScreen.PRDName()
	branch := a.completionScreen.Branch()
	dir := a.baseDir

	// Load the PRD to generate PR content. Use the registered path (prd.md) so
	// parsing succeeds - the old prd.json layout was migrated away.
	prdPath := a.prdPathForPRD(prdName)
	if prdPath == "" {
		prdPath = prd.PRDPath(a.baseDir, prdName)
	}
	baseOverride := ""
	if a.config != nil {
		baseOverride = a.config.OnComplete.PRBaseBranch
	}
	return func() tea.Msg {
		p, err := prd.LoadPRD(prdPath)
		if err != nil {
			return autoActionResultMsg{action: "pr", err: fmt.Errorf("failed to load PRD: %s", err.Error())}
		}
		title := git.PRTitleFromPRD(prdName, p)
		body := git.PRBodyFromPRD(p)
		pr, err := git.EnsurePR(dir, branch, title, body, baseOverride)
		if err != nil {
			return autoActionResultMsg{action: "pr", err: err}
		}
		return autoActionResultMsg{action: "pr", pr: pr}
	}
}
