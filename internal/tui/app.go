package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/loop"
	"github.com/ben182/chief/internal/prd"
	tea "github.com/charmbracelet/bubbletea"
)

// App is the main Bubble Tea model for the Chief TUI.
type App struct {
	prd                 *prd.PRD
	prdPath             string
	prdName             string
	state               AppState
	iteration           int
	startTime           time.Time
	// runBaselineDone holds the IDs of stories that were already passing when
	// the current run started. The dashboard progress bar counts only stories
	// outside this set, so follow-up stories appended to an otherwise finished
	// PRD show progress for this run rather than for the whole PRD. Nil before
	// any run has started this session, in which case the whole PRD is counted.
	runBaselineDone map[string]bool
	selectedIndex       int
	storiesScrollOffset int
	width               int
	height              int
	err                 error

	// Loop manager for parallel PRD execution
	manager  *loop.Manager
	provider loop.Provider
	maxIter  int

	// Activity tracking
	lastActivity string

	// File watching
	watcher         *prd.Watcher
	progressWatcher *prd.ProgressWatcher
	progress        map[string][]prd.ProgressEntry

	// View mode
	viewMode  ViewMode
	logViewer *LogViewer

	// PRD tab bar (always visible)
	tabBar *TabBar

	// PRD picker (for creating new PRDs)
	picker  *PRDPicker
	baseDir string // Base directory for .chief/prds/

	// Project config
	config *config.Config

	// Diff viewer
	diffViewer *DiffViewer

	// Help overlay
	helpOverlay      *HelpOverlay
	previousViewMode ViewMode // View to return to when closing help

	// Branch warning dialog
	branchWarning       *BranchWarning
	pendingStartPRD     string // PRD name waiting to start after branch decision
	pendingWorktreePath string // Absolute worktree path for pending PRD
	pendingSyncBranch   string // Branch awaiting reconciliation with origin, for DialogBranchBehindRemote

	// Worktree setup spinner
	worktreeSpinner *WorktreeSpinner

	// Completion screen
	completionScreen *CompletionScreen

	// Story timing tracking, keyed by PRD name. Keeping this per-PRD (rather than
	// a single set of fields for the viewed PRD) means the ETA survives tab
	// switches and is tracked for background PRDs too, not just the one on screen.
	storyTimings       map[string][]StoryTiming
	currentStoryID     map[string]string
	currentStoryStart  map[string]time.Time
	currentStoryCost   map[string]float64    // cost accrued for the in-progress story (across retries), per PRD
	currentStoryTokens map[string]TokenUsage // token usage accrued for the in-progress story, per PRD
	// reviewingStoryID names the story whose committed work the separate review
	// agent is currently inspecting, per PRD. Set on EventReviewStart and cleared
	// on EventReviewDone. While set, the story stays selected and is shown with a
	// "Reviewing" tag instead of advancing to the next story.
	reviewingStoryID map[string]string
	totalCost        float64 // cumulative cost across all stories this run

	// branchSyncChecked records which PRDs have had their branch compared against
	// origin, so the check (which fetches, and may raise a dialog) runs once per
	// start rather than re-firing when the interrupted start resumes.
	branchSyncChecked map[string]bool

	// Settings overlay
	settingsOverlay *SettingsOverlay

	// Quit confirmation dialog
	quitConfirm *QuitConfirmation

	// Completion notification callback
	onCompletion func(prdName string)

	// Verbose mode - show raw Claude output
	verbose bool

	// autoStart triggers the loop automatically on launch (chief start)
	autoStart bool

	// Post-exit action - what to do after TUI exits
	PostExitAction PostExitAction
	PostExitPRD    string // PRD name for post-exit action
}

// PostExitAction represents an action to take after the TUI exits.
// NewApp creates a new App with the given PRD.
func NewApp(prdPath string, provider loop.Provider) (*App, error) {
	return NewAppWithOptions(prdPath, 10, provider)
}

// NewAppWithOptions creates a new App with the given PRD and options.
// If maxIter <= 0, it will be calculated dynamically based on remaining stories.
func NewAppWithOptions(prdPath string, maxIter int, provider loop.Provider) (*App, error) {
	p, err := prd.LoadPRD(prdPath)
	if err != nil {
		return nil, err
	}

	// Calculate dynamic default if maxIter <= 0. Each story may be attempted up
	// to DefaultMaxAttemptsPerStory times before being parked, so the global cap
	// is only a runaway backstop and must sit above that per-story budget.
	if maxIter <= 0 {
		remaining := 0
		for _, story := range p.UserStories {
			if !story.Passes && !story.NeedsReview {
				remaining++
			}
		}
		maxIter = remaining*loop.DefaultMaxAttemptsPerStory + 5
		if maxIter < 5 {
			maxIter = 5
		}
	}

	// Extract PRD name from path (directory name or filename without extension)
	prdName := filepath.Base(filepath.Dir(prdPath))
	if prdName == "." || prdName == "/" {
		prdName = filepath.Base(prdPath)
	}

	// Create file watcher
	watcher, err := prd.NewWatcher(prdPath)
	if err != nil {
		return nil, err
	}

	// Determine base directory for PRD picker
	// If path contains .chief/prds/, go up to the project root (4 levels up from prd.md)
	// .chief/prds/<name>/prd.md -> .chief/prds/<name> -> .chief/prds -> .chief -> project root
	baseDir := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(prdPath))))
	if !strings.Contains(prdPath, ".chief/prds/") {
		// Fallback to current working directory
		baseDir, _ = os.Getwd()
	}

	// Load project config
	cfg, err := config.Load(baseDir)
	if err != nil {
		cfg = config.Default()
	}

	// Prune stale worktrees on startup (clean git's internal tracking)
	if git.IsGitRepo(baseDir) {
		_ = git.PruneWorktrees(baseDir)
	}

	// Create progress watcher and load initial progress
	progressWatcher, _ := prd.NewProgressWatcher(prdPath)
	progress, _ := prd.ParseProgress(prd.ProgressPath(prdPath))

	// Restore persisted story timings so the ETA is available immediately after
	// a restart, without waiting for two stories to finish again.
	storyTimings := make(map[string][]StoryTiming)
	storyTimings[prdName] = loadPersistedTimings(prdPath, p.UserStories)

	// Create loop manager for parallel PRD execution
	manager := loop.NewManager(maxIter, provider)
	manager.SetBaseDir(baseDir)
	manager.SetConfig(cfg)

	// Register the initial PRD with the manager
	manager.Register(prdName, prdPath)

	// Create tab bar for always-visible PRD tabs
	tabBar := NewTabBar(baseDir, prdName, manager)

	// Create picker with manager reference (for creating new PRDs)
	picker := NewPRDPicker(baseDir, prdName, manager)

	app := &App{
		prd:              p,
		prdPath:          prdPath,
		prdName:          prdName,
		state:            StateReady,
		iteration:        0,
		selectedIndex:    0,
		maxIter:          maxIter,
		manager:          manager,
		provider:         provider,
		watcher:          watcher,
		progressWatcher:  progressWatcher,
		progress:         progress,
		viewMode:         ViewDashboard,
		logViewer:        NewLogViewer(),
		diffViewer:       NewDiffViewer(baseDir),
		tabBar:           tabBar,
		picker:           picker,
		baseDir:          baseDir,
		config:           cfg,
		helpOverlay:      NewHelpOverlay(),
		branchWarning:    NewBranchWarning(),
		worktreeSpinner:  NewWorktreeSpinner(),
		completionScreen: NewCompletionScreen(),
		settingsOverlay:  NewSettingsOverlay(),
		quitConfirm:      NewQuitConfirmation(),

		storyTimings:       storyTimings,
		currentStoryID:     make(map[string]string),
		currentStoryStart:  make(map[string]time.Time),
		currentStoryCost:   make(map[string]float64),
		currentStoryTokens: make(map[string]TokenUsage),
		reviewingStoryID:   make(map[string]string),
		branchSyncChecked:  make(map[string]bool),
	}
	// Signal in the story-done marker that a review still follows, so the reader
	// knows the build agent's <chief-done/> isn't the final word on the story.
	app.logViewer.SetReviewPending(cfg != nil && cfg.Review.Active())
	return app, nil
}

// SetCompletionCallback sets a callback that is called when any PRD completes.
func (a *App) SetCompletionCallback(fn func(prdName string)) {
	a.onCompletion = fn
	if a.manager != nil {
		a.manager.SetCompletionCallback(fn)
	}
}

// SetVerbose enables or disables verbose mode (raw Claude output in log).
func (a *App) SetVerbose(v bool) {
	a.verbose = v
}

// SetAutoStart makes the loop start automatically on launch.
func (a *App) SetAutoStart(v bool) {
	a.autoStart = v
}

// DisableRetry disables automatic retry on Claude crashes.
func (a *App) DisableRetry() {
	if a.manager != nil {
		a.manager.DisableRetry()
	}
}

// Init initializes the App.
func (a App) Init() tea.Cmd {
	// Start the file watcher
	if a.watcher != nil {
		if err := a.watcher.Start(); err != nil {
			// Log error but don't fail - watcher is not critical
			a.lastActivity = "Warning: file watcher failed to start"
		}
	}

	// Start the progress watcher
	if a.progressWatcher != nil {
		_ = a.progressWatcher.Start()
	}

	cmds := []tea.Cmd{
		tea.EnterAltScreen,
		a.listenForPRDChanges(),
		a.listenForManagerEvents(),
		a.listenForProgressChanges(),
	}
	if a.autoStart {
		cmds = append(cmds, func() tea.Msg { return autoStartMsg{} })
	}
	return tea.Batch(cmds...)
}

// listenForManagerEvents listens for events from all managed loops.
func (a *App) listenForManagerEvents() tea.Cmd {
	if a.manager == nil {
		return nil
	}
	return func() tea.Msg {
		event, ok := <-a.manager.Events()
		if !ok {
			return nil
		}
		return LoopEventMsg{PRDName: event.PRDName, Event: event.Event}
	}
}

// Update handles messages and updates the model.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		// Log viewer size is set authoritatively in renderLogView (with correct -4 width).
		// Only update height here for scroll calculations; width will match on next render.
		a.logViewer.SetSize(a.width-4, a.height-headerHeight-footerHeight-2)
		return a, nil

	case autoStartMsg:
		if a.state == StateReady {
			return a.startLoop()
		}
		return a, nil

	case LoopEventMsg:
		return a.handleLoopEvent(msg.PRDName, msg.Event)

	case LoopFinishedMsg:
		return a.handleLoopFinished(msg.PRDName, msg.Err)

	case PRDCompletedMsg:
		// A PRD completed - trigger completion notification
		if a.onCompletion != nil {
			a.onCompletion(msg.PRDName)
		}
		// Refresh tab bar and picker to show updated status
		if a.tabBar != nil {
			a.tabBar.Refresh()
		}
		a.picker.Refresh()
		return a, nil

	case mergeResultMsg:
		return a.handleMergeResult(msg)

	case cleanResultMsg:
		return a.handleCleanResult(msg)

	case autoActionResultMsg:
		return a.handleAutoActionResult(msg)

	case summaryResultMsg:
		return a.handleSummaryResult(msg)

	case branchSyncCheckMsg:
		return a.handleBranchSyncCheck(msg)

	case branchSyncResultMsg:
		return a.handleBranchSyncResult(msg)

	case backgroundAutoActionResultMsg:
		return a.handleBackgroundAutoAction(msg)

	case completionSpinnerTickMsg:
		if a.viewMode == ViewCompletion && a.completionScreen.IsAutoActionRunning() {
			a.completionScreen.Tick()
			return a, tickCompletionSpinner()
		}
		return a, nil

	case confettiTickMsg:
		if a.viewMode == ViewCompletion && a.completionScreen.HasConfetti() {
			a.completionScreen.TickConfetti()
			return a, tickConfetti()
		}
		return a, nil

	case worktreeStepResultMsg:
		return a.handleWorktreeStepResult(msg)

	case elapsedTickMsg:
		if a.state == StateRunning {
			return a, tickElapsed()
		}
		return a, nil

	case worktreeSpinnerTickMsg:
		if a.viewMode == ViewWorktreeSpinner {
			a.worktreeSpinner.Tick()
			return a, tickWorktreeSpinner()
		}
		return a, nil

	case settingsGHCheckResultMsg:
		return a.handleSettingsGHCheck(msg)

	case ProgressUpdateMsg:
		a.progress = msg.Entries
		return a, a.listenForProgressChanges()

	case PRDUpdateMsg:
		return a.handlePRDUpdate(msg)

	case LaunchInitMsg:
		a.PostExitAction = PostExitInit
		a.PostExitPRD = msg.Name
		return a, tea.Quit

	case LaunchEditMsg:
		a.PostExitAction = PostExitEdit
		a.PostExitPRD = msg.Name
		return a, tea.Quit

	case tea.KeyMsg:
		// Handle help overlay first (can be opened/closed from any view)
		if msg.String() == "?" {
			if a.viewMode == ViewHelp {
				// Close help, return to previous view
				a.viewMode = a.previousViewMode
			} else {
				// Open help, remember current view
				a.previousViewMode = a.viewMode
				a.viewMode = ViewHelp
				a.helpOverlay.SetSize(a.width, a.height)
				a.helpOverlay.SetViewMode(a.previousViewMode)
			}
			return a, nil
		}

		// Handle settings overlay (can be opened/closed from any view)
		if msg.String() == "," {
			if a.viewMode == ViewSettings {
				// Close settings
				a.viewMode = a.previousViewMode
				return a, nil
			}
			if a.viewMode == ViewDashboard || a.viewMode == ViewLog || a.viewMode == ViewPicker || a.viewMode == ViewCompletion {
				a.previousViewMode = a.viewMode
				a.settingsOverlay.SetSize(a.width, a.height)
				a.settingsOverlay.LoadFromConfig(a.config)
				a.viewMode = ViewSettings
				return a, nil
			}
		}

		// Handle help view (only Esc closes it besides ?)
		if a.viewMode == ViewHelp {
			if msg.String() == "esc" {
				a.viewMode = a.previousViewMode
			}
			// Ignore other keys in help view
			return a, nil
		}

		// Handle settings view
		if a.viewMode == ViewSettings {
			return a.handleSettingsKeys(msg)
		}

		// Handle picker view separately (it has its own input mode)
		if a.viewMode == ViewPicker {
			return a.handlePickerKeys(msg)
		}

		// Handle branch warning view
		if a.viewMode == ViewBranchWarning {
			return a.handleBranchWarningKeys(msg)
		}

		// Handle worktree spinner view - only Esc is active
		if a.viewMode == ViewWorktreeSpinner {
			return a.handleWorktreeSpinnerKeys(msg)
		}

		// Handle completion screen view
		if a.viewMode == ViewCompletion {
			return a.handleCompletionKeys(msg)
		}

		// Handle quit confirmation dialog
		if a.viewMode == ViewQuitConfirm {
			return a.handleQuitConfirmKeys(msg)
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return a.tryQuit()

		// View switching
		case "t":
			if a.viewMode == ViewDashboard || a.viewMode == ViewDiff {
				a.viewMode = ViewLog
				// SetSize is handled by renderLogView with correct dimensions
			} else {
				a.viewMode = ViewDashboard
			}
			return a, nil

		// Diff view
		case "d":
			if a.viewMode == ViewDashboard || a.viewMode == ViewLog {
				// Use the current PRD's worktree directory if available, otherwise base dir
				diffDir := a.baseDir
				if instance := a.manager.GetInstance(a.prdName); instance != nil && instance.WorktreeDir != "" {
					diffDir = instance.WorktreeDir
				}
				a.diffViewer.SetBaseDir(diffDir)
				a.diffViewer.SetSize(a.width-4, a.height-headerHeight-footerHeight-2)
				// Load diff for the selected story's commit
				if story := a.GetSelectedStory(); story != nil {
					a.diffViewer.LoadForStory(a.prdName, story.ID, story.Title)
				} else {
					a.diffViewer.Load()
				}
				a.viewMode = ViewDiff
			} else if a.viewMode == ViewDiff {
				a.viewMode = ViewDashboard
			}
			return a, nil

		// New PRD (opens picker in input mode)
		case "n":
			if a.viewMode == ViewDashboard || a.viewMode == ViewLog || a.viewMode == ViewDiff {
				a.picker.Refresh()
				a.picker.SetSize(a.width, a.height)
				a.picker.StartInputMode()
				a.viewMode = ViewPicker
			}
			return a, nil

		// List PRDs (opens picker in selection mode)
		case "l":
			if a.viewMode == ViewDashboard || a.viewMode == ViewLog || a.viewMode == ViewDiff {
				a.picker.Refresh()
				a.picker.SetSize(a.width, a.height)
				a.viewMode = ViewPicker
			}
			return a, nil

		// Edit current PRD
		case "e":
			if a.viewMode == ViewDashboard || a.viewMode == ViewLog || a.viewMode == ViewDiff {
				a.stopAllLoops()
				a.stopWatcher()
				return a, func() tea.Msg {
					return LaunchEditMsg{Name: a.prdName}
				}
			}
			return a, nil

		// Number keys 1-9 to switch PRDs
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			if a.viewMode == ViewDashboard || a.viewMode == ViewLog || a.viewMode == ViewDiff {
				index := int(msg.String()[0] - '1') // Convert "1" to 0, "2" to 1, etc.
				if entry := a.tabBar.GetEntry(index); entry != nil {
					return a.switchToPRD(entry.Name, entry.Path)
				}
			}
			return a, nil

		// Left/right arrows cycle through PRDs (works past 9, unlike number keys)
		case "left", "right":
			if a.viewMode == ViewDashboard || a.viewMode == ViewLog || a.viewMode == ViewDiff {
				count := a.tabBar.Count()
				if count > 1 {
					index := a.tabBar.ActiveIndex()
					if msg.String() == "right" {
						index = (index + 1) % count
					} else {
						index = (index - 1 + count) % count
					}
					if entry := a.tabBar.GetEntry(index); entry != nil {
						return a.switchToPRD(entry.Name, entry.Path)
					}
				}
			}
			return a, nil

		// Loop controls (work in both views)
		case "s":
			if a.state == StateReady || a.state == StatePaused || a.state == StateError || a.state == StateStopped {
				return a.startLoop()
			}
		case "p":
			if a.state == StateRunning {
				return a.pauseLoop()
			}
		case "x":
			if a.state == StateRunning || a.state == StatePaused {
				return a.stopLoopAndUpdate()
			}

		// Navigation - different behavior based on view
		case "up", "k":
			if s := a.activeScrollable(); s != nil {
				s.ScrollUp()
			} else {
				if a.selectedIndex > 0 {
					a.selectedIndex--
					if a.selectedIndex < a.storiesScrollOffset {
						a.storiesScrollOffset = a.selectedIndex
					}
				}
			}
		case "down", "j":
			if s := a.activeScrollable(); s != nil {
				s.ScrollDown()
			} else {
				if a.selectedIndex < len(a.prd.UserStories)-1 {
					a.selectedIndex++
					a.adjustStoriesScroll()
				}
			}

		// Log/diff scrolling
		case "ctrl+d", "pgdown":
			if s := a.activeScrollable(); s != nil {
				s.PageDown()
			}
		case "ctrl+u", "pgup":
			if s := a.activeScrollable(); s != nil {
				s.PageUp()
			}
		case "g":
			if s := a.activeScrollable(); s != nil {
				s.ScrollToTop()
			}
		case "G":
			if s := a.activeScrollable(); s != nil {
				s.ScrollToBottom()
			}

		// Max iterations control
		case "+", "=":
			a.adjustMaxIterations(5)
		case "-", "_":
			a.adjustMaxIterations(-5)
		}
	}

	return a, nil
}

// tryQuit attempts to quit the app. If any loop is running, it shows the quit
// confirmation dialog instead of quitting immediately.
func (a App) tryQuit() (tea.Model, tea.Cmd) {
	loopRunning := a.manager != nil && a.manager.IsAnyRunning()
	// A finished loop leaves IsAnyRunning() false, but a post-completion action
	// (summary/push/PR) may still be running on the completion screen; quitting
	// would kill its process and leave e.g. a half-written summary behind.
	actionRunning := a.viewMode == ViewCompletion && a.completionScreen.IsAutoActionRunning()
	if loopRunning || actionRunning {
		a.previousViewMode = a.viewMode
		// A running loop is the more consequential warning, so it wins the copy.
		if loopRunning {
			a.quitConfirm.ForLoop()
		} else {
			a.quitConfirm.ForAutoAction()
		}
		a.viewMode = ViewQuitConfirm
		a.quitConfirm.Reset()
		a.quitConfirm.SetSize(a.width, a.height)
		return a, nil
	}
	a.stopAllLoops()
	a.stopWatcher()
	return a, tea.Quit
}

// handleQuitConfirmKeys handles keyboard input for the quit confirmation dialog.
func (a App) handleQuitConfirmKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.viewMode = a.previousViewMode
		return a, nil
	case "up", "k":
		a.quitConfirm.MoveUp()
		return a, nil
	case "down", "j":
		a.quitConfirm.MoveDown()
		return a, nil
	case "enter":
		if a.quitConfirm.GetSelected() == QuitOptionQuit {
			a.stopAllLoops()
			a.stopWatcher()
			return a, tea.Quit
		}
		// Cancel
		a.viewMode = a.previousViewMode
		return a, nil
	}
	return a, nil
}

// renderQuitConfirmView renders the quit confirmation dialog.
func (a *App) renderQuitConfirmView() string {
	a.quitConfirm.SetSize(a.width, a.height)
	return a.quitConfirm.Render()
}

// View renders the TUI.
func (a App) View() string {
	switch a.viewMode {
	case ViewLog:
		return a.renderLogView()
	case ViewDiff:
		return a.renderDiffView()
	case ViewPicker:
		return a.renderPickerView()
	case ViewHelp:
		return a.renderHelpView()
	case ViewBranchWarning:
		return a.renderBranchWarningView()
	case ViewWorktreeSpinner:
		return a.renderWorktreeSpinnerView()
	case ViewCompletion:
		return a.renderCompletionView()
	case ViewSettings:
		return a.renderSettingsView()
	case ViewQuitConfirm:
		return a.renderQuitConfirmView()
	default:
		return a.renderDashboard()
	}
}

// renderBranchWarningView renders the branch warning dialog.
func (a *App) renderBranchWarningView() string {
	a.branchWarning.SetSize(a.width, a.height)
	return a.branchWarning.Render()
}

// handleBranchWarningKeys handles keyboard input for the branch warning dialog.
func (a App) handleBranchWarningKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle edit mode input
	if a.branchWarning.IsEditMode() {
		switch msg.String() {
		case "esc":
			// Cancel edit mode
			a.branchWarning.CancelEditMode()
			return a, nil
		case "enter":
			// Confirm edit
			a.branchWarning.CancelEditMode()
			return a, nil
		case "backspace":
			a.branchWarning.DeleteInputChar()
			return a, nil
		default:
			// Add character to branch name
			if len(msg.String()) == 1 {
				a.branchWarning.AddInputChar(rune(msg.String()[0]))
			}
			return a, nil
		}
	}

	switch msg.String() {
	case "esc":
		a.viewMode = ViewDashboard
		a.pendingStartPRD = ""
		a.pendingWorktreePath = ""
		a.pendingSyncBranch = ""
		a.lastActivity = "Cancelled"
		return a, nil

	case "up", "k":
		a.branchWarning.MoveUp()
		return a, nil

	case "down", "j":
		a.branchWarning.MoveDown()
		return a, nil

	case "e":
		// Start editing branch name if on an option that involves a branch
		opt := a.branchWarning.GetSelectedOption()
		if opt == BranchOptionCreateWorktree || opt == BranchOptionCreateBranch {
			a.branchWarning.StartEditMode()
		}
		return a, nil

	case "enter":
		prdName := a.pendingStartPRD
		prdDir := prd.PRDDir(a.baseDir, prdName)
		syncBranch := a.pendingSyncBranch
		a.pendingStartPRD = ""
		a.pendingWorktreePath = ""
		a.pendingSyncBranch = ""
		a.viewMode = ViewDashboard

		switch a.branchWarning.GetSelectedOption() {
		case BranchOptionSyncWithRemote:
			a.lastActivity = "Syncing " + syncBranch + " with origin..."
			return a, a.runBranchSync(prdName, prdDir, syncBranch)

		case BranchOptionCreateWorktree:
			branchName := a.branchWarning.GetSuggestedBranch()
			worktreePath := git.WorktreePathForPRD(a.baseDir, prdName)
			relWorktreePath := fmt.Sprintf(".chief/worktrees/%s/", prdName)

			// Detect default branch for display
			defaultBranch := "main"
			if db, err := git.GetDefaultBranch(a.baseDir); err == nil {
				defaultBranch = db
			}

			// Configure and show the spinner
			a.worktreeSpinner.Configure(prdName, branchName, defaultBranch, relWorktreePath, a.config.Worktree.Setup)
			a.worktreeSpinner.SetSize(a.width, a.height)
			a.pendingStartPRD = prdName
			a.pendingWorktreePath = worktreePath
			a.viewMode = ViewWorktreeSpinner

			// Start the first async step (create worktree which includes branch creation)
			return a, tea.Batch(
				tickWorktreeSpinner(),
				a.runWorktreeStep(SpinnerStepCreateBranch, a.baseDir, worktreePath, branchName),
			)

		case BranchOptionCreateBranch:
			// Create the branch with (possibly edited) name
			branchName := a.branchWarning.GetSuggestedBranch()
			if err := git.CreateBranch(a.baseDir, branchName); err != nil {
				a.lastActivity = "Error creating branch: " + err.Error()
				return a, nil
			}
			// Track the branch on the manager instance
			if instance := a.manager.GetInstance(prdName); instance != nil {
				a.manager.UpdateWorktreeInfo(prdName, "", branchName)
			}
			a.lastActivity = "Created branch: " + branchName
			// Now start the loop
			return a.doStartLoop(prdName, prdDir)

		case BranchOptionContinue:
			// Continue on current branch / run in same directory
			return a.doStartLoop(prdName, prdDir)

		case BranchOptionCancel:
			a.lastActivity = "Cancelled"
			return a, nil
		}
	}

	return a, nil
}

// renderWorktreeSpinnerView renders the worktree setup spinner.
func (a *App) renderWorktreeSpinnerView() string {
	a.worktreeSpinner.SetSize(a.width, a.height)
	return a.worktreeSpinner.Render()
}

// handleWorktreeSpinnerKeys handles keyboard input for the worktree spinner.
func (a App) handleWorktreeSpinnerKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Cancel setup and clean up
		a.worktreeSpinner.Cancel()
		a.cleanupWorktreeSetup()
		a.viewMode = ViewDashboard
		a.lastActivity = "Worktree setup cancelled"
		a.pendingStartPRD = ""
		a.pendingWorktreePath = ""
		return a, nil
	}
	// Ignore all other keys during spinner
	return a, nil
}

// cleanupWorktreeSetup cleans up a partially created worktree and branch.
func (a *App) cleanupWorktreeSetup() {
	if a.pendingWorktreePath != "" {
		// Try to remove the worktree if it was created
		if git.IsWorktree(a.pendingWorktreePath) {
			_ = git.RemoveWorktree(a.baseDir, a.pendingWorktreePath)
		}
	}
}

// prdPathForPRD returns the prd.md path for a PRD by name, or "" if unknown.
func (a *App) prdPathForPRD(prdName string) string {
	if prdName == a.prdName {
		return a.prdPath
	}
	if a.manager == nil {
		return ""
	}
	if inst := a.manager.GetInstance(prdName); inst != nil {
		return inst.PRDPath
	}
	return ""
}

// renderCompletionView renders the completion screen.
func (a *App) renderCompletionView() string {
	a.completionScreen.SetSize(a.width, a.height)
	return a.completionScreen.Render()
}

// renderSettingsView renders the settings overlay.
func (a *App) renderSettingsView() string {
	a.settingsOverlay.SetSize(a.width, a.height)
	return a.settingsOverlay.Render()
}

// handleSettingsKeys handles keyboard input for the settings overlay.
func (a App) handleSettingsKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Dismiss GH error on any key
	if a.settingsOverlay.HasGHError() {
		a.settingsOverlay.DismissGHError()
		return a, nil
	}

	// Handle inline text editing
	if a.settingsOverlay.IsEditing() {
		switch msg.String() {
		case "enter":
			a.settingsOverlay.ConfirmEdit()
			a.settingsOverlay.ApplyToConfig(a.config)
			_ = config.Save(a.baseDir, a.config)
			return a, nil
		case "esc":
			a.settingsOverlay.CancelEdit()
			return a, nil
		case "backspace":
			a.settingsOverlay.DeleteEditChar()
			return a, nil
		default:
			if len(msg.String()) == 1 {
				a.settingsOverlay.AddEditChar(rune(msg.String()[0]))
			}
			return a, nil
		}
	}

	switch msg.String() {
	case "esc":
		a.viewMode = a.previousViewMode
		return a, nil
	case "q", "ctrl+c":
		return a.tryQuit()
	case "up", "k":
		a.settingsOverlay.MoveUp()
		return a, nil
	case "down", "j":
		a.settingsOverlay.MoveDown()
		return a, nil
	case "enter":
		item := a.settingsOverlay.GetSelectedItem()
		if item == nil {
			return a, nil
		}
		switch item.Type {
		case SettingsItemBool:
			key, newVal := a.settingsOverlay.ToggleBool()
			if key == "onComplete.createPR" && newVal {
				// Validate GH CLI asynchronously
				return a, func() tea.Msg {
					installed, authenticated, err := git.CheckGHCLI()
					return settingsGHCheckResultMsg{installed: installed, authenticated: authenticated, err: err}
				}
			}
			a.settingsOverlay.ApplyToConfig(a.config)
			_ = config.Save(a.baseDir, a.config)
			return a, nil
		case SettingsItemString:
			a.settingsOverlay.StartEditing()
			return a, nil
		}
	}

	return a, nil
}

// handleSettingsGHCheck handles the GH CLI check result from settings.
func (a App) handleSettingsGHCheck(msg settingsGHCheckResultMsg) (tea.Model, tea.Cmd) {
	if a.viewMode != ViewSettings {
		return a, nil
	}

	if msg.err != nil || !msg.installed || !msg.authenticated {
		// Validation failed - revert toggle and show error
		a.settingsOverlay.RevertToggle()
		errMsg := "GitHub CLI (gh) is not installed"
		if msg.installed && !msg.authenticated {
			errMsg = "GitHub CLI (gh) is not authenticated. Run: gh auth login"
		}
		if msg.err != nil {
			errMsg = msg.err.Error()
		}
		a.settingsOverlay.SetGHError(errMsg)
		return a, nil
	}

	// Validation passed - save the config
	a.settingsOverlay.ApplyToConfig(a.config)
	_ = config.Save(a.baseDir, a.config)
	return a, nil
}

// handleCompletionKeys handles keyboard input for the completion screen.
func (a App) handleCompletionKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return a.tryQuit()

	case "l":
		// Switch to the picker
		a.picker.Refresh()
		a.picker.SetSize(a.width, a.height)
		a.viewMode = ViewPicker
		return a, nil

	case "m":
		// Merge the completed PRD's branch
		if a.completionScreen.HasBranch() {
			branch := a.completionScreen.Branch()
			baseDir := a.baseDir
			a.viewMode = ViewDashboard
			return a, func() tea.Msg {
				conflicts, err := git.MergeBranch(baseDir, branch)
				if err != nil {
					return mergeResultMsg{branch: branch, conflicts: conflicts, err: err}
				}
				output := parseMergeSuccessMessage(baseDir, branch)
				return mergeResultMsg{branch: branch, output: output}
			}
		}
		return a, nil

	case "c":
		// Clean the PRD's worktree - switch to picker with clean dialog
		if a.completionScreen.HasBranch() {
			prdName := a.completionScreen.PRDName()
			a.picker.Refresh()
			a.picker.SetSize(a.width, a.height)
			// Select the completed PRD in the picker
			for i, entry := range a.picker.entries {
				if entry.Name == prdName {
					a.picker.selectedIndex = i
					break
				}
			}
			if a.picker.CanClean() {
				a.picker.StartCleanConfirmation()
			}
			a.viewMode = ViewPicker
		}
		return a, nil

	case "esc":
		a.viewMode = ViewDashboard
		return a, nil
	}

	return a, nil
}

// runWorktreeStep runs a worktree setup step asynchronously.
func (a *App) runWorktreeStep(step WorktreeSpinnerStep, baseDir, worktreePath, branchName string) tea.Cmd {
	switch step {
	case SpinnerStepCreateBranch:
		return func() tea.Msg {
			// CreateWorktree handles both branch creation and worktree addition
			if err := git.CreateWorktree(baseDir, worktreePath, branchName); err != nil {
				return worktreeStepResultMsg{step: SpinnerStepCreateBranch, err: err}
			}
			return worktreeStepResultMsg{step: SpinnerStepCreateBranch}
		}

	case SpinnerStepRunSetup:
		setupCmd := a.config.Worktree.Setup
		return func() tea.Msg {
			cmd := exec.Command("sh", "-c", setupCmd)
			cmd.Dir = worktreePath
			if out, err := cmd.CombinedOutput(); err != nil {
				return worktreeStepResultMsg{
					step: SpinnerStepRunSetup,
					err:  fmt.Errorf("%s\n%s", err.Error(), strings.TrimSpace(string(out))),
				}
			}
			return worktreeStepResultMsg{step: SpinnerStepRunSetup}
		}
	}
	return nil
}

// handleWorktreeStepResult handles the result of a worktree setup step.
func (a App) handleWorktreeStepResult(msg worktreeStepResultMsg) (tea.Model, tea.Cmd) {
	// Ignore results if we've already cancelled or left the spinner view
	if a.viewMode != ViewWorktreeSpinner || a.worktreeSpinner.IsCancelled() {
		return a, nil
	}

	if msg.err != nil {
		a.worktreeSpinner.SetError(msg.err.Error())
		return a, nil
	}

	switch msg.step {
	case SpinnerStepCreateBranch:
		// Branch creation completed - advance through both branch and worktree steps
		// (CreateWorktree does both in one call)
		a.worktreeSpinner.AdvanceStep() // Complete "Creating branch"
		a.worktreeSpinner.AdvanceStep() // Complete "Creating worktree"

		// Check if we need to run setup
		if a.worktreeSpinner.HasSetupCommand() {
			return a, a.runWorktreeStep(SpinnerStepRunSetup, a.baseDir, a.pendingWorktreePath, "")
		}

		// No setup - we're done, transition to loop
		return a.finishWorktreeSetup()

	case SpinnerStepRunSetup:
		a.worktreeSpinner.AdvanceStep() // Complete "Running setup"
		return a.finishWorktreeSetup()
	}

	return a, nil
}

// finishWorktreeSetup completes the worktree setup and starts the loop.
func (a App) finishWorktreeSetup() (tea.Model, tea.Cmd) {
	prdName := a.pendingStartPRD
	worktreePath := a.pendingWorktreePath
	branchName := a.worktreeSpinner.branchName
	prdDir := prd.PRDDir(a.baseDir, prdName)

	// Register or update with worktree info. Prefer the already-known PRD path
	// (handles the legacy .chief/prd.md and direct-path layouts) over convention.
	prdPath := a.prdPathForPRD(prdName)
	if prdPath == "" {
		prdPath = filepath.Join(prdDir, "prd.md")
	}
	if instance := a.manager.GetInstance(prdName); instance == nil {
		a.manager.RegisterWithWorktree(prdName, prdPath, worktreePath, branchName)
	} else {
		a.manager.UpdateWorktreeInfo(prdName, worktreePath, branchName)
	}

	a.lastActivity = fmt.Sprintf("Created worktree at %s on branch %s", worktreePath, branchName)
	a.viewMode = ViewDashboard
	a.pendingStartPRD = ""
	a.pendingWorktreePath = ""

	return a.doStartLoop(prdName, prdDir)
}

// handleMergeResult handles the result of an async merge operation.
func (a App) handleMergeResult(msg mergeResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		a.picker.SetMergeResult(&MergeResult{
			Success:   false,
			Message:   fmt.Sprintf("Failed to merge %s into current branch", msg.branch),
			Conflicts: msg.conflicts,
			Branch:    msg.branch,
		})
	} else {
		a.picker.SetMergeResult(&MergeResult{
			Success: true,
			Message: msg.output,
			Branch:  msg.branch,
		})
		a.lastActivity = fmt.Sprintf("Merged %s", msg.branch)
	}
	// Switch to picker to show the merge result if not already there
	if a.viewMode != ViewPicker {
		a.picker.Refresh()
		a.picker.SetSize(a.width, a.height)
		a.viewMode = ViewPicker
	}
	return a, nil
}

// handleCleanConfirmationKeys handles keyboard input for the clean confirmation dialog.
func (a App) handleCleanConfirmationKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.picker.CancelCleanConfirmation()
		return a, nil
	case "up", "k":
		a.picker.CleanConfirmMoveUp()
		return a, nil
	case "down", "j":
		a.picker.CleanConfirmMoveDown()
		return a, nil
	case "enter":
		cc := a.picker.GetCleanConfirmation()
		if cc == nil {
			return a, nil
		}

		option := a.picker.GetCleanOption()
		if option == CleanOptionCancel {
			a.picker.CancelCleanConfirmation()
			return a, nil
		}

		prdName := cc.EntryName
		branch := cc.Branch
		clearBranch := option == CleanOptionRemoveAll
		baseDir := a.baseDir
		worktreePath := git.WorktreePathForPRD(baseDir, prdName)

		return a, func() tea.Msg {
			// Remove the worktree
			if err := git.RemoveWorktree(baseDir, worktreePath); err != nil {
				return cleanResultMsg{
					prdName: prdName,
					success: false,
					message: fmt.Sprintf("Failed to remove worktree: %s", err.Error()),
				}
			}

			// Delete branch if requested
			if clearBranch && branch != "" {
				if err := git.DeleteBranch(baseDir, branch); err != nil {
					return cleanResultMsg{
						prdName:     prdName,
						success:     true,
						message:     fmt.Sprintf("Removed worktree but failed to delete branch: %s", err.Error()),
						clearBranch: false,
					}
				}
			}

			msg := fmt.Sprintf("Removed worktree for %s", prdName)
			if clearBranch && branch != "" {
				msg = fmt.Sprintf("Removed worktree and deleted branch %s", branch)
			}
			return cleanResultMsg{
				prdName:     prdName,
				success:     true,
				message:     msg,
				clearBranch: clearBranch,
			}
		}
	}

	return a, nil
}

// handleCleanResult handles the result of an async clean operation.
func (a App) handleCleanResult(msg cleanResultMsg) (tea.Model, tea.Cmd) {
	a.picker.CancelCleanConfirmation()
	a.picker.SetCleanResult(&CleanResult{
		Success: msg.success,
		Message: msg.message,
	})

	if msg.success {
		// Clear worktree info from manager
		if a.manager != nil {
			a.manager.ClearWorktreeInfo(msg.prdName, msg.clearBranch)
		}
		a.picker.Refresh()
		a.lastActivity = fmt.Sprintf("Cleaned worktree for %s", msg.prdName)
	}

	return a, nil
}

// renderHelpView renders the help overlay.
func (a *App) renderHelpView() string {
	a.helpOverlay.SetSize(a.width, a.height)
	return a.helpOverlay.Render()
}

// renderPickerView renders the PRD picker modal overlaid on the dashboard.
func (a *App) renderPickerView() string {
	// Render the dashboard in the background
	background := a.renderDashboard()

	// Overlay the picker
	a.picker.SetSize(a.width, a.height)
	picker := a.picker.Render()

	// For now, just return the picker (it handles centering)
	// In a more sophisticated implementation, we could overlay with transparency
	_ = background
	return picker
}

// GetPRD returns the current PRD.
func (a *App) GetPRD() *prd.PRD {
	return a.prd
}

// GetSelectedStory returns the currently selected story.
func (a *App) GetSelectedStory() *prd.UserStory {
	if a.selectedIndex >= 0 && a.selectedIndex < len(a.prd.UserStories) {
		return &a.prd.UserStories[a.selectedIndex]
	}
	return nil
}

// storiesListHeight calculates how many story lines fit in the panel.
// Must match the calculation in renderStoriesPanel.
func (a *App) storiesListHeight() int {
	fh := footerHeight
	if a.height < 12 {
		fh = 0
	}
	contentHeight := a.height - a.effectiveHeaderHeight() - fh - 2
	if a.isNarrowMode() {
		storiesHeight := max((contentHeight*40)/100, 5)
		return storiesHeight - 5
	}
	return contentHeight - 5
}

// adjustStoriesScroll ensures the selected index is visible in the scroll window.
func (a *App) adjustStoriesScroll() {
	listHeight := a.storiesListHeight()
	if listHeight <= 0 {
		return
	}
	if a.selectedIndex < a.storiesScrollOffset {
		a.storiesScrollOffset = a.selectedIndex
	}
	if a.selectedIndex >= a.storiesScrollOffset+listHeight {
		a.storiesScrollOffset = a.selectedIndex - listHeight + 1
	}
	// Clamp
	maxOffset := len(a.prd.UserStories) - listHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	if a.storiesScrollOffset > maxOffset {
		a.storiesScrollOffset = maxOffset
	}
	if a.storiesScrollOffset < 0 {
		a.storiesScrollOffset = 0
	}
}

// clearInProgress clears all in-progress flags by setting each in-progress
// story's status to "todo" in the markdown file, then reloads.
func (a *App) clearInProgress() {
	dirty := false
	for _, story := range a.prd.UserStories {
		if story.InProgress {
			_ = prd.SetStoryStatus(a.prdPath, story.ID, "todo")
			dirty = true
		}
	}
	if dirty {
		if p, err := prd.LoadPRD(a.prdPath); err == nil {
			a.prd = p
		}
	}
}

// selectInProgressStory sets the selected index to the first in-progress story,
// or, when a story is under review, to that story — so the selection stays on
// the story being reviewed rather than jumping ahead while the review runs.
func (a *App) selectInProgressStory() {
	if id := a.reviewingStoryID[a.prdName]; id != "" && a.selectStoryByID(id) {
		return
	}
	for i, story := range a.prd.UserStories {
		if story.InProgress {
			a.selectedIndex = i
			a.adjustStoriesScroll()
			return
		}
	}
}

// selectStoryByID selects the story with the given ID, returning true if found.
func (a *App) selectStoryByID(id string) bool {
	for i, story := range a.prd.UserStories {
		if story.ID == id {
			a.selectedIndex = i
			a.adjustStoriesScroll()
			return true
		}
	}
	return false
}

// reviewActive reports whether a separate review agent runs after each story's
// build commit. When it does, a story's timing is only finalized once the review
// finishes (see handleLoopEvent) so the review pass counts toward the ETA.
func (a *App) reviewActive() bool {
	return a.config != nil && a.config.Review.Active()
}

// isReviewing reports whether the given story is currently being inspected by
// the review agent for the viewed PRD.
func (a *App) isReviewing(storyID string) bool {
	return storyID != "" && a.reviewingStoryID[a.prdName] == storyID
}

// GetState returns the current app state.
func (a *App) GetState() AppState {
	return a.state
}

// GetIteration returns the current iteration count.
func (a *App) GetIteration() int {
	return a.iteration
}

// GetLastActivity returns the last activity message.
func (a *App) GetLastActivity() string {
	return a.lastActivity
}

// adjustMaxIterations adjusts the max iterations by delta.
func (a *App) adjustMaxIterations(delta int) {
	newMax := a.maxIter + delta
	if newMax < 1 {
		newMax = 1
	}
	a.maxIter = newMax

	// Update the manager's default
	if a.manager != nil {
		a.manager.SetMaxIterations(newMax)
		// Also update any running loop for the current PRD
		a.manager.SetMaxIterationsForInstance(a.prdName, newMax)
	}

	a.lastActivity = fmt.Sprintf("Max iterations: %d", newMax)
}

// listenForProgressChanges listens for progress.md file changes and returns them as messages.
func (a *App) listenForProgressChanges() tea.Cmd {
	if a.progressWatcher == nil {
		return nil
	}
	return func() tea.Msg {
		entries, ok := <-a.progressWatcher.Events()
		if !ok {
			return nil
		}
		return ProgressUpdateMsg{Entries: entries}
	}
}

// listenForPRDChanges listens for PRD file changes and returns them as messages.
func (a *App) listenForPRDChanges() tea.Cmd {
	if a.watcher == nil {
		return nil
	}
	return func() tea.Msg {
		event, ok := <-a.watcher.Events()
		if !ok {
			return nil
		}
		return PRDUpdateMsg{PRD: event.PRD, Error: event.Error}
	}
}

// handlePRDUpdate handles PRD file change events.
func (a App) handlePRDUpdate(msg PRDUpdateMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil {
		// File error - could be temporary, keep watching
		a.lastActivity = "PRD file error: " + msg.Error.Error()
	} else if msg.PRD != nil {
		// Update the PRD
		a.prd = msg.PRD

		// Adjust selected index if it's now out of bounds
		if a.selectedIndex >= len(a.prd.UserStories) {
			a.selectedIndex = len(a.prd.UserStories) - 1
			if a.selectedIndex < 0 {
				a.selectedIndex = 0
			}
		}

		// Auto-select the in-progress story so the user sees its details
		a.selectInProgressStory()
		a.adjustStoriesScroll()
	}

	// Continue listening for changes
	return a, a.listenForPRDChanges()
}

// stopWatcher stops the file watchers.
func (a *App) stopWatcher() {
	if a.watcher != nil {
		a.watcher.Stop()
	}
	if a.progressWatcher != nil {
		a.progressWatcher.Stop()
	}
}
