package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/ben182/chief/internal/git"
)

func TestCompletionScreen_Configure(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 8, 10, "chief/auth", 5, true, 0, 0, nil, 0)

	if cs.PRDName() != "auth" {
		t.Errorf("expected prdName 'auth', got '%s'", cs.PRDName())
	}
	if cs.Branch() != "chief/auth" {
		t.Errorf("expected branch 'chief/auth', got '%s'", cs.Branch())
	}
	if !cs.HasBranch() {
		t.Error("expected HasBranch() to be true")
	}
}

func TestCompletionScreen_NoBranch(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 8, 8, "", 0, false, 0, 0, nil, 0)

	if cs.HasBranch() {
		t.Error("expected HasBranch() to be false when branch is empty")
	}
}

func TestCompletionScreen_RenderHeader(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 8, 10, "chief/auth", 5, true, 0, 0, nil, 0)
	cs.SetSize(80, 40)

	rendered := cs.Render()
	if !strings.Contains(rendered, "PRD Complete!") {
		t.Error("expected 'PRD Complete!' in render output")
	}
	if !strings.Contains(rendered, "auth") {
		t.Error("expected PRD name 'auth' in render output")
	}
	if !strings.Contains(rendered, "8/10") {
		t.Error("expected '8/10' stories count in render output")
	}
}

func TestCompletionScreen_RenderBranchInfo(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 8, 8, "chief/auth", 5, true, 0, 0, nil, 0)
	cs.SetSize(80, 40)

	rendered := cs.Render()
	if !strings.Contains(rendered, "chief/auth") {
		t.Error("expected branch 'chief/auth' in render output")
	}
	if !strings.Contains(rendered, "5 commits") {
		t.Error("expected '5 commits' in render output")
	}
}

func TestCompletionScreen_RenderSingleCommit(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 1, 1, "chief/auth", 1, false, 0, 0, nil, 0)
	cs.SetSize(80, 40)

	rendered := cs.Render()
	if !strings.Contains(rendered, "1 commit") {
		t.Error("expected '1 commit' (singular) in render output")
	}
}

func TestCompletionScreen_RenderNoBranch(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 8, 8, "", 0, false, 0, 0, nil, 0)
	cs.SetSize(80, 40)

	rendered := cs.Render()
	if strings.Contains(rendered, "Branch:") {
		t.Error("expected no 'Branch:' when no branch is set")
	}
	if strings.Contains(rendered, "commit") {
		t.Error("expected no commit info when no branch is set")
	}
}

func TestCompletionScreen_RenderNoAutoActions(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 8, 8, "chief/auth", 5, false, 0, 0, nil, 0)
	cs.SetSize(80, 40)

	rendered := cs.Render()
	if !strings.Contains(rendered, "Configure auto-push and PR in settings") {
		t.Error("expected auto-actions hint when hasAutoActions is false")
	}
}

func TestCompletionScreen_RenderWithAutoActions(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 8, 8, "chief/auth", 5, true, 0, 0, nil, 0)
	cs.SetSize(80, 40)

	rendered := cs.Render()
	if strings.Contains(rendered, "Configure auto-push and PR in settings") {
		t.Error("expected no auto-actions hint when hasAutoActions is true")
	}
}

func TestCompletionScreen_RenderFooterWithBranch(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 8, 8, "chief/auth", 5, true, 0, 0, nil, 0)
	cs.SetSize(80, 40)

	rendered := cs.Render()
	if !strings.Contains(rendered, "m: merge") {
		t.Error("expected 'm: merge' in footer when branch is set")
	}
	if !strings.Contains(rendered, "c: clean") {
		t.Error("expected 'c: clean' in footer when branch is set")
	}
	if !strings.Contains(rendered, "l: switch PRD") {
		t.Error("expected 'l: switch PRD' in footer")
	}
	if !strings.Contains(rendered, "q: quit") {
		t.Error("expected 'q: quit' in footer")
	}
}

func TestCompletionScreen_RenderFooterNoBranch(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 8, 8, "", 0, false, 0, 0, nil, 0)
	cs.SetSize(80, 40)

	rendered := cs.Render()
	if strings.Contains(rendered, "m: merge") {
		t.Error("expected no 'm: merge' in footer when no branch is set")
	}
	if strings.Contains(rendered, "c: clean") {
		t.Error("expected no 'c: clean' in footer when no branch is set")
	}
	if !strings.Contains(rendered, "l: switch PRD") {
		t.Error("expected 'l: switch PRD' in footer")
	}
	if !strings.Contains(rendered, "q: quit") {
		t.Error("expected 'q: quit' in footer")
	}
}

func TestCompletionScreen_PushInProgress(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 8, 8, "chief/auth", 5, true, 0, 0, nil, 0)
	cs.SetPushInProgress()
	cs.SetSize(80, 40)

	rendered := cs.Render()
	if !strings.Contains(rendered, "Pushing branch to remote") {
		t.Error("expected 'Pushing branch to remote' when push is in progress")
	}
	if cs.pushState != AutoActionInProgress {
		t.Errorf("expected push state to be AutoActionInProgress, got %d", cs.pushState)
	}
	if !cs.IsAutoActionRunning() {
		t.Error("expected IsAutoActionRunning() to be true when push is in progress")
	}
}

func TestCompletionScreen_PushSuccess(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 8, 8, "chief/auth", 5, true, 0, 0, nil, 0)
	cs.SetPushSuccess()
	cs.SetSize(80, 40)

	rendered := cs.Render()
	if !strings.Contains(rendered, "Pushed branch to remote") {
		t.Error("expected 'Pushed branch to remote' when push succeeded")
	}
	// Should not show the "configure" hint when auto-actions are active
	if strings.Contains(rendered, "Configure auto-push") {
		t.Error("expected no auto-push hint when push is active")
	}
}

func TestCompletionScreen_PushError(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 8, 8, "chief/auth", 5, true, 0, 0, nil, 0)
	cs.SetPushError("authentication failed")
	cs.SetSize(80, 40)

	rendered := cs.Render()
	if !strings.Contains(rendered, "Push failed") {
		t.Error("expected 'Push failed' when push errored")
	}
	if !strings.Contains(rendered, "authentication failed") {
		t.Error("expected error message in render output")
	}
}

func TestCompletionScreen_PRInProgress(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 8, 8, "chief/auth", 5, true, 0, 0, nil, 0)
	cs.SetPushSuccess()
	cs.SetPRInProgress()
	cs.SetSize(80, 40)

	rendered := cs.Render()
	if !strings.Contains(rendered, "Creating pull request") {
		t.Error("expected 'Creating pull request' when PR is in progress")
	}
	if !cs.IsAutoActionRunning() {
		t.Error("expected IsAutoActionRunning() to be true when PR is in progress")
	}
}

func TestCompletionScreen_PRSuccess(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 8, 8, "chief/auth", 5, true, 0, 0, nil, 0)
	cs.SetPushSuccess()
	cs.SetPRSuccess(git.PR{
		URL:   "https://github.com/org/repo/pull/42",
		Title: "feat(auth): Authentication",
		Base:  "develop",
	})
	cs.SetSize(80, 40)

	rendered := cs.Render()
	if !strings.Contains(rendered, "Created PR") {
		t.Error("expected 'Created PR' when PR succeeded")
	}
	if !strings.Contains(rendered, "feat(auth): Authentication") {
		t.Error("expected PR title in render output")
	}
	if !strings.Contains(rendered, "develop") {
		t.Error("expected the PR's base branch in render output")
	}
	if !strings.Contains(rendered, "https://github.com/org/repo/pull/42") {
		t.Error("expected PR URL in render output")
	}
	if cs.IsAutoActionRunning() {
		t.Error("expected IsAutoActionRunning() to be false when all actions complete")
	}
}

// A followup run pushes onto a branch whose PR is already open: the screen has
// to say so rather than claim it opened a second one.
func TestCompletionScreen_PRAlreadyOpen(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 8, 8, "chief/auth", 5, true, 0, 0, nil, 0)
	cs.SetPushSuccess()
	cs.SetPRSuccess(git.PR{
		URL:            "https://github.com/org/repo/pull/42",
		Title:          "feat(auth): Authentication",
		Base:           "develop",
		AlreadyExisted: true,
	})
	cs.SetSize(80, 40)

	rendered := cs.Render()
	if !strings.Contains(rendered, "PR already open") {
		t.Error("expected 'PR already open' for a pre-existing pull request")
	}
	if strings.Contains(rendered, "Created PR") {
		t.Error("expected no 'Created PR' claim for a pre-existing pull request")
	}
	if !strings.Contains(rendered, "https://github.com/org/repo/pull/42") {
		t.Error("expected the existing PR's URL in render output")
	}
}

func TestCompletionScreen_PRError(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 8, 8, "chief/auth", 5, true, 0, 0, nil, 0)
	cs.SetPushSuccess()
	cs.SetPRError("gh not found, Install: https://cli.github.com")
	cs.SetSize(80, 40)

	rendered := cs.Render()
	if !strings.Contains(rendered, "PR creation failed") {
		t.Error("expected 'PR creation failed' when PR errored")
	}
	if !strings.Contains(rendered, "gh not found") {
		t.Error("expected error message in render output")
	}
}

func TestCompletionScreen_ConfigureResetsAutoActions(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 8, 8, "chief/auth", 5, true, 0, 0, nil, 0)
	cs.SetPushSuccess()
	cs.SetPRSuccess(git.PR{URL: "https://example.com", Title: "title"})

	// Reconfigure should reset
	cs.Configure("payments", 3, 5, "chief/payments", 2, false, 0, 0, nil, 0)

	if cs.pushState != AutoActionIdle {
		t.Error("expected push state to be reset after Configure")
	}
	if cs.prState != AutoActionIdle {
		t.Error("expected PR state to be reset after Configure")
	}
	if cs.pr.URL != "" {
		t.Error("expected the PR URL to be empty after Configure")
	}
}

func TestCompletionScreen_Tick(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 8, 8, "chief/auth", 5, true, 0, 0, nil, 0)
	cs.SetPushInProgress()

	initial := cs.spinnerFrame
	cs.Tick()
	if cs.spinnerFrame != initial+1 {
		t.Error("expected spinner frame to advance on Tick()")
	}
}

func TestCompletionScreen_PushErrorNonBlocking(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 8, 8, "chief/auth", 5, true, 0, 0, nil, 0)
	cs.SetPushError("network error")
	cs.SetSize(80, 40)

	rendered := cs.Render()
	// Footer should still be present (keybindings remain usable)
	if !strings.Contains(rendered, "m: merge") {
		t.Error("expected footer keybindings to remain usable after push error")
	}
	if !strings.Contains(rendered, "q: quit") {
		t.Error("expected 'q: quit' in footer after error")
	}
}

// A run whose machine dozed off has to say so, otherwise the working time on
// screen silently contradicts the clock on the wall.
func TestCompletionScreen_RendersSleptTime(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 8, 8, "chief/auth", 5, true, 2*time.Hour+53*time.Minute, 56*time.Minute, nil, 0)
	cs.SetSize(80, 40)

	rendered := cs.Render()
	if !strings.Contains(rendered, "Mac slept 56m00s during the run") {
		t.Errorf("expected the slept-time line in render output, got:\n%s", rendered)
	}
	// The run's own duration stays working time — the nap is reported beside it,
	// not folded into it.
	if !strings.Contains(rendered, "Completed in 2h53m00s") {
		t.Errorf("expected the working time to be unchanged by sleep, got:\n%s", rendered)
	}
}

func TestCompletionScreen_NoSleptTimeLineWithoutSleep(t *testing.T) {
	cs := NewCompletionScreen()
	cs.Configure("auth", 8, 8, "chief/auth", 5, true, 2*time.Hour, 0, nil, 0)
	cs.SetSize(80, 40)

	rendered := cs.Render()
	if strings.Contains(rendered, "slept") {
		t.Errorf("expected no slept-time line when nothing was detected, got:\n%s", rendered)
	}
}

// The line needs a row of its own, or it pushes the footer out of the box.
func TestCompletionScreen_SleptTimeGrowsModal(t *testing.T) {
	timings := []StoryTiming{{StoryID: "AUTH-1", Title: "Login", Duration: time.Minute}}

	awake := NewCompletionScreen()
	awake.Configure("auth", 8, 8, "chief/auth", 5, true, 2*time.Hour, 0, timings, 0)
	awake.SetSize(80, 40)

	slept := NewCompletionScreen()
	slept.Configure("auth", 8, 8, "chief/auth", 5, true, 2*time.Hour, 56*time.Minute, timings, 0)
	slept.SetSize(80, 40)

	if got, want := slept.calculateModalHeight(), awake.calculateModalHeight()+1; got != want {
		t.Errorf("modal height with slept line = %d, want %d", got, want)
	}
}

func TestCenterModal(t *testing.T) {
	modal := "test modal content"
	result := centerModal(modal, 80, 40)

	// Should have top padding and left padding
	lines := strings.Split(result, "\n")
	if len(lines) < 2 {
		t.Fatal("expected centered modal to have multiple lines")
	}

	// First lines should be empty (top padding)
	hasTopPadding := false
	for _, line := range lines {
		if line == "" {
			hasTopPadding = true
			break
		}
	}
	if !hasTopPadding {
		t.Error("expected top padding in centered modal")
	}
}

// TestCompletionScreen_CodeStats covers the block that reports what the run did
// to the code: the numbers it draws, and the cases where it stays out of the way.
func TestCompletionScreen_CodeStats(t *testing.T) {
	fullStat := git.DiffStat{
		Insertions: 4812, Deletions: 387,
		FilesAdded: 31, FilesModified: 14, FilesDeleted: 2,
		Languages: []git.LanguageStat{
			{Name: "PHP", Insertions: 3204, Files: 28},
			{Name: "Blade", Insertions: 1120, Files: 9},
			{Name: "YAML", Insertions: 488, Files: 3},
			{Name: "JSON", Insertions: 12, Files: 1},
		},
		TestInsertions: 1840, TestFiles: 12,
	}

	newScreen := func(stat git.DiffStat) *CompletionScreen {
		cs := NewCompletionScreen()
		cs.SetSize(100, 40)
		cs.Configure("auth", 3, 3, "chief/auth", 5, false, time.Hour, 0, nil, 0)
		cs.SetCodeStats(stat)
		return cs
	}

	t.Run("reports lines, files and tests", func(t *testing.T) {
		out := newScreen(fullStat).Render()
		for _, want := range []string{
			"4,812",              // insertions, with thousands separators
			"387",                // deletions
			"47 files",           // 31 + 14 + 2
			"31 new",             // added files called out separately
			"PHP 3,204",          // language breakdown, biggest first
			"Tests: 1,840 lines", // the test share
			"12 files",
			"(38%)",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("Render() missing %q", want)
			}
		}
	})

	t.Run("names at most three languages", func(t *testing.T) {
		out := newScreen(fullStat).Render()
		if !strings.Contains(out, "YAML 488") {
			t.Error("Render() should name the third language")
		}
		// The fourth is dropped rather than wrapped onto another line.
		if strings.Contains(out, "JSON") {
			t.Error("Render() should stop after three languages")
		}
	})

	t.Run("stays silent without stats", func(t *testing.T) {
		// A run that committed nothing, or whose start ref was never captured.
		out := newScreen(git.DiffStat{}).Render()
		if strings.Contains(out, "lines in") || strings.Contains(out, "Tests:") {
			t.Errorf("Render() drew code stats for an empty stat:\n%s", out)
		}
	})

	t.Run("omits the test line when nothing was tested", func(t *testing.T) {
		out := newScreen(git.DiffStat{
			Insertions: 120, FilesAdded: 2,
			Languages: []git.LanguageStat{{Name: "Markdown", Insertions: 120, Files: 2}},
		}).Render()
		if !strings.Contains(out, "+120") {
			t.Error("Render() should still report the lines")
		}
		if strings.Contains(out, "Tests:") {
			t.Error("Render() should omit the test line when no tests were written")
		}
	})

	t.Run("omits the file count when only lines are known", func(t *testing.T) {
		out := newScreen(git.DiffStat{Insertions: 10, Deletions: 2}).Render()
		if strings.Contains(out, "in 0 files") {
			t.Error("Render() should not report a zero file count")
		}
	})

	t.Run("Configure clears stats from the previous PRD", func(t *testing.T) {
		cs := newScreen(fullStat)
		// A second PRD finishes; its stats have not arrived yet.
		cs.Configure("billing", 2, 2, "chief/billing", 3, false, time.Hour, 0, nil, 0)
		if strings.Contains(cs.Render(), "4,812") {
			t.Error("Configure() should clear the previous run's code stats")
		}
	})
}

// TestCompletionScreen_NetLines covers the net balance beside the +/− pair: the
// number that tells a run which grew the code from one that shrank it.
func TestCompletionScreen_NetLines(t *testing.T) {
	render := func(stat git.DiffStat) string {
		cs := NewCompletionScreen()
		cs.SetSize(100, 40)
		cs.Configure("auth", 3, 3, "chief/auth", 5, false, time.Hour, 0, nil, 0)
		cs.SetCodeStats(stat)
		return cs.Render()
	}

	t.Run("reports a positive balance", func(t *testing.T) {
		out := render(git.DiffStat{Insertions: 4812, Deletions: 387, FilesModified: 47})
		if !strings.Contains(out, "net +4,425") {
			t.Errorf("Render() missing the net balance:\n%s", out)
		}
	})

	t.Run("reports a negative balance for a run that shrank the code", func(t *testing.T) {
		out := render(git.DiffStat{Insertions: 640, Deletions: 1843, FilesModified: 29})
		if !strings.Contains(out, "net "+glyph("−", "-")+"1,203") {
			t.Errorf("Render() missing the negative net balance:\n%s", out)
		}
	})

	t.Run("omits the balance when nothing was deleted", func(t *testing.T) {
		// Without deletions the net is just the insertions again.
		out := render(git.DiffStat{Insertions: 300, FilesAdded: 4})
		if strings.Contains(out, "net ") {
			t.Errorf("Render() should omit a net equal to the insertions:\n%s", out)
		}
	})
}

// TestCompletionScreen_RateLimitWaited covers the line that explains a long run
// holding a short amount of work.
func TestCompletionScreen_RateLimitWaited(t *testing.T) {
	newScreen := func(waited time.Duration) *CompletionScreen {
		cs := NewCompletionScreen()
		cs.SetSize(100, 40)
		cs.Configure("auth", 3, 3, "chief/auth", 5, false, 4*time.Hour+13*time.Minute, 0, nil, 0)
		cs.SetRateLimitWaited(waited)
		return cs
	}

	t.Run("reports the wait as a share of the total", func(t *testing.T) {
		out := newScreen(94 * time.Minute).Render()
		if !strings.Contains(out, "1h34m00s of that waiting for the usage window") {
			t.Errorf("Render() missing the wait line:\n%s", out)
		}
	})

	t.Run("stays silent when the run never waited", func(t *testing.T) {
		out := newScreen(0).Render()
		if strings.Contains(out, "usage window") {
			t.Errorf("Render() drew a wait line for a run that never waited:\n%s", out)
		}
	})

	t.Run("Configure clears the wait from the previous PRD", func(t *testing.T) {
		cs := newScreen(94 * time.Minute)
		cs.Configure("billing", 2, 2, "chief/billing", 3, false, time.Hour, 0, nil, 0)
		if strings.Contains(cs.Render(), "usage window") {
			t.Error("Configure() should clear the previous run's wait")
		}
	})
}
