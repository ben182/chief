package tui

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/loop"
	"github.com/ben182/chief/internal/prd"
)

func TestParkedStoryLabelsOnlyIncludesReviewStories(t *testing.T) {
	p := &prd.PRD{UserStories: []prd.UserStory{
		{ID: "US-001", Title: "Done story"},
		{ID: "US-002", Title: "Needs a look", NeedsReview: true},
		{ID: "US-003", Title: "Also parked", NeedsReview: true},
	}}

	got := parkedStoryLabels(p)

	want := []string{"US-002 - Needs a look", "US-003 - Also parked"}
	if len(got) != len(want) {
		t.Fatalf("expected %d parked labels, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("label %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParkedStoryLabelsEmptyWhenNothingParked(t *testing.T) {
	p := &prd.PRD{UserStories: []prd.UserStory{
		{ID: "US-001", Title: "Done"},
	}}

	if got := parkedStoryLabels(p); got != nil {
		t.Errorf("expected no labels when nothing is parked, got %v", got)
	}
}

func TestParkedStoryLabelsNilPRD(t *testing.T) {
	// The completion screen can be reached with the PRD reload having failed.
	if got := parkedStoryLabels(nil); got != nil {
		t.Errorf("expected nil for a nil PRD, got %v", got)
	}
}

func TestStoryRefsPreservesPRDOrderAndNamespace(t *testing.T) {
	p := &prd.PRD{UserStories: []prd.UserStory{
		{ID: "US-002", Title: "Second"},
		{ID: "US-001", Title: "First"},
	}}

	got := storyRefs("auth", p)

	if len(got) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(got))
	}
	// PRD order, not sorted: the summary walks these in the order the PRD lists.
	if got[0].ID != "US-002" || got[1].ID != "US-001" {
		t.Errorf("expected PRD order US-002,US-001, got %s,%s", got[0].ID, got[1].ID)
	}
	// The PRD name namespaces the commit lookup so a same-numbered story from
	// another PRD on this branch cannot match.
	for _, ref := range got {
		if ref.PRDName != "auth" {
			t.Errorf("expected PRDName 'auth' on every ref, got %q", ref.PRDName)
		}
	}
}

func TestStoryRefsNilPRD(t *testing.T) {
	if got := storyRefs("auth", nil); got != nil {
		t.Errorf("expected nil for a nil PRD, got %v", got)
	}
}

func TestBranchForReturnsRegisteredBranch(t *testing.T) {
	m := loop.NewManager(10, nil)
	if err := m.RegisterWithWorktree("auth", "/proj/.chief/prds/auth/prd.md", "", "chief/auth"); err != nil {
		t.Fatalf("register: %v", err)
	}
	a := &App{manager: m, baseDir: "/proj"}

	if got := a.branchFor("auth"); got != "chief/auth" {
		t.Errorf("expected branch 'chief/auth', got %q", got)
	}
}

func TestBranchForUnknownPRD(t *testing.T) {
	m := loop.NewManager(10, nil)
	a := &App{manager: m, baseDir: "/proj"}

	// An unregistered PRD has no branch; push/PR must not guess one.
	if got := a.branchFor("nope"); got != "" {
		t.Errorf("expected empty branch for an unknown PRD, got %q", got)
	}
}

func TestCompletionGitDirPrefersWorktree(t *testing.T) {
	m := loop.NewManager(10, nil)
	worktree := filepath.Join("/proj", ".chief", "worktrees", "auth")
	if err := m.RegisterWithWorktree("auth", "/proj/.chief/prds/auth/prd.md", worktree, "chief/auth"); err != nil {
		t.Fatalf("register: %v", err)
	}
	a := &App{manager: m, baseDir: "/proj"}

	// Post-completion actions must act on the worktree's branch, not the root's.
	if got := a.completionGitDir("auth"); got != worktree {
		t.Errorf("expected the worktree dir %q, got %q", worktree, got)
	}
}

func TestCompletionGitDirFallsBackToBaseDir(t *testing.T) {
	m := loop.NewManager(10, nil)
	if err := m.Register("auth", "/proj/.chief/prds/auth/prd.md"); err != nil {
		t.Fatalf("register: %v", err)
	}
	a := &App{manager: m, baseDir: "/proj"}

	if got := a.completionGitDir("auth"); got != "/proj" {
		t.Errorf("expected the project root '/proj', got %q", got)
	}
}

func TestCompletionGitDirUnknownPRDFallsBackToBaseDir(t *testing.T) {
	m := loop.NewManager(10, nil)
	a := &App{manager: m, baseDir: "/proj"}

	if got := a.completionGitDir("nope"); got != "/proj" {
		t.Errorf("expected the project root '/proj', got %q", got)
	}
}

func TestHandleAutoActionResultPushErrorShowsOnScreen(t *testing.T) {
	a := &App{completionScreen: NewCompletionScreen()}

	model, cmd := a.handleAutoActionResult(autoActionResultMsg{
		action: "push",
		err:    errors.New("remote rejected"),
	})

	got := model.(App)
	if got.completionScreen.pushState != AutoActionError {
		t.Errorf("expected push state Error, got %v", got.completionScreen.pushState)
	}
	// A failed push must not chain into PR creation.
	if cmd != nil {
		t.Error("expected no follow-up command after a failed push")
	}
}

func TestHandleAutoActionResultPushSuccessChainsPRWhenConfigured(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 3, 3, "chief/auth", 3, true, 0, 0, nil, 0)
	a := &App{
		completionScreen: cs,
		config: &config.Config{
			OnComplete: config.OnCompleteConfig{CreatePR: true},
		},
	}

	model, cmd := a.handleAutoActionResult(autoActionResultMsg{action: "push"})

	got := model.(App)
	if got.completionScreen.pushState != AutoActionSuccess {
		t.Errorf("expected push state Success, got %v", got.completionScreen.pushState)
	}
	if got.completionScreen.prState != AutoActionInProgress {
		t.Errorf("expected PR state InProgress after a successful push, got %v", got.completionScreen.prState)
	}
	if cmd == nil {
		t.Error("expected a follow-up command to create the PR")
	}
}

func TestHandleAutoActionResultPushSuccessWithoutPRConfig(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 3, 3, "chief/auth", 3, true, 0, 0, nil, 0)
	a := &App{
		completionScreen: cs,
		config:           &config.Config{}, // CreatePR off
	}

	model, cmd := a.handleAutoActionResult(autoActionResultMsg{action: "push"})

	got := model.(App)
	if got.completionScreen.prState != AutoActionIdle {
		t.Errorf("expected PR state to stay Idle, got %v", got.completionScreen.prState)
	}
	if cmd != nil {
		t.Error("expected no PR command when createPR is off")
	}
}

func TestHandleAutoActionResultPushSuccessWithoutBranchSkipsPR(t *testing.T) {
	cs := NewCompletionScreen()
	// No branch: nothing to open a PR from.
	cs.Configure("auth", 3, 3, "", 3, true, 0, 0, nil, 0)
	a := &App{
		completionScreen: cs,
		config: &config.Config{
			OnComplete: config.OnCompleteConfig{CreatePR: true},
		},
	}

	_, cmd := a.handleAutoActionResult(autoActionResultMsg{action: "push"})

	if cmd != nil {
		t.Error("expected no PR command without a branch")
	}
}

func TestHandleAutoActionResultPRError(t *testing.T) {
	a := &App{completionScreen: NewCompletionScreen()}

	model, _ := a.handleAutoActionResult(autoActionResultMsg{
		action: "pr",
		err:    errors.New("gh not authenticated"),
	})

	got := model.(App)
	if got.completionScreen.prState != AutoActionError {
		t.Errorf("expected PR state Error, got %v", got.completionScreen.prState)
	}
}

func TestHandleAutoActionResultUnknownActionIsInert(t *testing.T) {
	a := &App{completionScreen: NewCompletionScreen()}

	model, cmd := a.handleAutoActionResult(autoActionResultMsg{action: "something-else"})

	got := model.(App)
	if got.completionScreen.pushState != AutoActionIdle || got.completionScreen.prState != AutoActionIdle {
		t.Error("expected an unknown action to leave both states Idle")
	}
	if cmd != nil {
		t.Error("expected no command for an unknown action")
	}
}

func TestHandleBackgroundAutoActionErrorDoesNotChain(t *testing.T) {
	m := loop.NewManager(10, nil)
	if err := m.RegisterWithWorktree("auth", "/proj/prd.md", "", "chief/auth"); err != nil {
		t.Fatalf("register: %v", err)
	}
	a := &App{
		manager: m,
		baseDir: "/proj",
		config: &config.Config{
			OnComplete: config.OnCompleteConfig{CreatePR: true},
		},
	}

	// A background PRD's failed push is swallowed by design (no screen to show
	// it on), but it must not go on to open a PR for unpushed commits.
	_, cmd := a.handleBackgroundAutoAction(backgroundAutoActionResultMsg{
		prdName: "auth",
		action:  "push",
		err:     errors.New("push failed"),
	})

	if cmd != nil {
		t.Error("expected no PR command after a failed background push")
	}
}

func TestHandleBackgroundAutoActionPushChainsPR(t *testing.T) {
	m := loop.NewManager(10, nil)
	if err := m.RegisterWithWorktree("auth", "/proj/prd.md", "", "chief/auth"); err != nil {
		t.Fatalf("register: %v", err)
	}
	a := &App{
		manager: m,
		baseDir: "/proj",
		config: &config.Config{
			OnComplete: config.OnCompleteConfig{CreatePR: true},
		},
	}

	_, cmd := a.handleBackgroundAutoAction(backgroundAutoActionResultMsg{
		prdName: "auth",
		action:  "push",
	})

	if cmd == nil {
		t.Error("expected a PR command after a successful background push")
	}
}

func TestHandleBackgroundAutoActionWithoutBranchSkipsPR(t *testing.T) {
	m := loop.NewManager(10, nil)
	// Registered without a branch: a root-directory run on a protected branch.
	if err := m.Register("auth", "/proj/prd.md"); err != nil {
		t.Fatalf("register: %v", err)
	}
	a := &App{
		manager: m,
		baseDir: "/proj",
		config: &config.Config{
			OnComplete: config.OnCompleteConfig{CreatePR: true},
		},
	}

	_, cmd := a.handleBackgroundAutoAction(backgroundAutoActionResultMsg{
		prdName: "auth",
		action:  "push",
	})

	if cmd != nil {
		t.Error("expected no PR command without a branch")
	}
}

func TestHandleBackgroundAutoActionPRResultIsTerminal(t *testing.T) {
	m := loop.NewManager(10, nil)
	if err := m.RegisterWithWorktree("auth", "/proj/prd.md", "", "chief/auth"); err != nil {
		t.Fatalf("register: %v", err)
	}
	a := &App{
		manager: m,
		baseDir: "/proj",
		config: &config.Config{
			OnComplete: config.OnCompleteConfig{CreatePR: true},
		},
	}

	// A "pr" result must not chain further, or a PR success would loop forever.
	_, cmd := a.handleBackgroundAutoAction(backgroundAutoActionResultMsg{
		prdName: "auth",
		action:  "pr",
	})

	if cmd != nil {
		t.Error("expected no follow-up command after a PR result")
	}
}
