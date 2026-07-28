package loop

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ben182/chief/internal/awake"
	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/prd"
)

// LoopState represents the state of a loop instance.
type LoopState int

const (
	LoopStateReady LoopState = iota
	LoopStateRunning
	LoopStatePaused
	LoopStateStopped
	LoopStateComplete
	LoopStateError
)

func (s LoopState) String() string {
	switch s {
	case LoopStateReady:
		return "Ready"
	case LoopStateRunning:
		return "Running"
	case LoopStatePaused:
		return "Paused"
	case LoopStateStopped:
		return "Stopped"
	case LoopStateComplete:
		return "Complete"
	case LoopStateError:
		return "Error"
	default:
		return "Unknown"
	}
}

// LoopInstance represents a single loop with its metadata.
type LoopInstance struct {
	Name        string
	PRDPath     string
	WorktreeDir string // Working directory for this PRD (empty = project root)
	Branch      string // Git branch for this PRD (empty = current branch)
	// StartRef is the branch HEAD hash captured when this run started, so the run
	// summary can be scoped to this run's commits (StartRef..HEAD). Empty when HEAD
	// couldn't be read (e.g. a repo with no commits yet).
	StartRef  string
	Loop      *Loop
	State     LoopState
	Iteration int
	StartTime time.Time
	Error     error
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
}

// ManagerEvent represents an event from any managed loop.
type ManagerEvent struct {
	PRDName   string
	Event     Event
	Completed bool // True if this PRD just completed all stories
}

// Manager manages multiple Loop instances for parallel PRD execution.
type Manager struct {
	instances      map[string]*LoopInstance
	events         chan ManagerEvent
	maxIter        int
	retryConfig    RetryConfig
	provider       Provider
	baseDir        string         // Project root directory (for CLAUDE.md etc.)
	config         *config.Config // Project config for post-completion actions
	awake          sleepGuard     // Keeps the machine from sleeping while loops run
	mu             sync.RWMutex
	wg             sync.WaitGroup
	onComplete     func(prdName string)                  // Callback when a PRD completes
	onPostComplete func(prdName, branch, workDir string) // Callback for post-completion actions (push, PR)
}

// sleepGuard is the part of awake.Guard the manager uses: a reference-counted
// request to keep the machine awake. An interface so a test can count the
// acquires and releases a run makes without spawning the real helper process.
type sleepGuard interface {
	Acquire()
	Release()
}

// NewManager creates a new loop manager.
func NewManager(maxIter int, provider Provider) *Manager {
	return &Manager{
		instances:   make(map[string]*LoopInstance),
		events:      make(chan ManagerEvent, 100),
		maxIter:     maxIter,
		retryConfig: DefaultRetryConfig(),
		provider:    provider,
		awake:       awake.NewGuard(),
	}
}

// SetRetryConfig sets the retry configuration for new loops.
func (m *Manager) SetRetryConfig(config RetryConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retryConfig = config
}

// DisableRetry disables automatic retry for new loops.
func (m *Manager) DisableRetry() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retryConfig.Enabled = false
}

// SetCompletionCallback sets a callback that is called when any PRD completes.
func (m *Manager) SetCompletionCallback(fn func(prdName string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onComplete = fn
}

// SetPostCompleteCallback sets a callback for post-completion actions (push, PR creation).
// The callback receives the PRD name, branch name, and working directory.
func (m *Manager) SetPostCompleteCallback(fn func(prdName, branch, workDir string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onPostComplete = fn
}

// SetBaseDir sets the project root directory so Claude runs from there and picks up CLAUDE.md.
func (m *Manager) SetBaseDir(baseDir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.baseDir = baseDir
}

// SetConfig sets the project config for post-completion actions.
func (m *Manager) SetConfig(cfg *config.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = cfg
}

// Config returns the current project config.
func (m *Manager) Config() *config.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// Events returns the channel for receiving events from all loops.
func (m *Manager) Events() <-chan ManagerEvent {
	return m.events
}

// lookup returns the instance registered under name, or an error if none is.
// It holds m.mu only for the map read and returns before the caller takes
// instance.mu, preserving the m.mu -> instance.mu lock order used throughout.
func (m *Manager) lookup(name string) (*LoopInstance, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	instance, exists := m.instances[name]
	if !exists {
		return nil, fmt.Errorf("PRD %s not found", name)
	}
	return instance, nil
}

// Register registers a PRD with the manager (does not start it).
func (m *Manager) Register(name, prdPath string) error {
	return m.RegisterWithWorktree(name, prdPath, "", "")
}

// RegisterWithWorktree registers a PRD with worktree metadata (does not start it).
func (m *Manager) RegisterWithWorktree(name, prdPath, worktreeDir, branch string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already registered
	if _, exists := m.instances[name]; exists {
		return fmt.Errorf("PRD %s is already registered", name)
	}

	m.instances[name] = &LoopInstance{
		Name:        name,
		PRDPath:     prdPath,
		WorktreeDir: worktreeDir,
		Branch:      branch,
		State:       LoopStateReady,
	}

	return nil
}

// Unregister removes a PRD from the manager (stops it first if running).
func (m *Manager) Unregister(name string) error {
	instance, err := m.lookup(name)
	if err != nil {
		return err
	}

	// Stop if running
	if instance.State == LoopStateRunning {
		m.Stop(name)
	}

	m.mu.Lock()
	delete(m.instances, name)
	m.mu.Unlock()

	return nil
}

// Start starts the loop for a specific PRD.
func (m *Manager) Start(name string) error {
	if m.provider == nil {
		return fmt.Errorf("manager provider is not configured")
	}

	// Snapshot the manager fields this Start needs under m.mu, then release it
	// before taking instance.mu. GetAllInstances/GetRunningPRDs lock in the order
	// m.mu -> instance.mu; taking them the other way round here (holding
	// instance.mu while grabbing m.mu) would be an AB-BA deadlock under load.
	m.mu.RLock()
	instance, exists := m.instances[name]
	baseDir := m.baseDir
	cfg := m.config
	retryConfig := m.retryConfig
	maxIter := m.maxIter
	provider := m.provider
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("PRD %s not found", name)
	}

	instance.mu.Lock()
	if instance.State == LoopStateRunning {
		instance.mu.Unlock()
		return fmt.Errorf("PRD %s is already running", name)
	}

	// Create a new loop instance, using worktree-aware constructor if WorktreeDir is set.
	// When no worktree is configured, run from the project root (baseDir) so that
	// CLAUDE.md and other project-level files are visible to Claude.
	workDir := instance.WorktreeDir
	if workDir == "" {
		workDir = baseDir
	}
	instance.Loop = NewLoopWithWorkDir(instance.PRDPath, workDir, "", maxIter, provider)
	instance.Loop.buildPrompt = promptBuilderForPRD(instance.PRDPath, provider.SupportsInteractiveQuestions())
	if cfg != nil {
		instance.Loop.SetReview(cfg.Review.Active(), cfg.Review.Skill, cfg.Review.Instructions)
		instance.Loop.SetReviewModel(cfg.Review.Model)
		instance.Loop.SetConsolidate(cfg.Consolidate.Active(), cfg.Consolidate.Skill, cfg.Consolidate.Instructions)
		instance.Loop.SetConsolidateModel(cfg.Consolidate.Model)
	}
	instance.Loop.SetRetryConfig(retryConfig)
	if cfg != nil && cfg.Loop.WatchdogTimeoutSeconds > 0 {
		instance.Loop.SetWatchdogTimeout(time.Duration(cfg.Loop.WatchdogTimeoutSeconds) * time.Second)
	}
	// The values above are the starting point; from here the loop re-reads them
	// from the manager whenever it needs them, so editing the review model or the
	// watchdog timeout during a run applies to the rest of that run.
	instance.Loop.SetConfigFn(m.Config)
	// Capture the branch HEAD before the loop makes any commits, so the run
	// summary can be scoped to exactly this run's work (StartRef..HEAD). On a
	// followup run this is the tip left by the previous run, so its already-landed
	// stories are excluded. Best-effort: an unborn branch leaves it empty, which
	// falls back to summarizing every matching story commit on the branch.
	instance.StartRef, _ = git.HeadHash(workDir)
	// Hand the loop the same ref, so the end-of-run consolidation pass refactors
	// exactly the window the summary describes — this run's commits, never an
	// earlier run's already-shipped work.
	instance.Loop.SetStartRef(instance.StartRef)
	instance.ctx, instance.cancel = context.WithCancel(context.Background())
	instance.State = LoopStateRunning
	instance.StartTime = time.Now()
	instance.Error = nil
	instance.mu.Unlock()

	// Start the loop in a goroutine
	m.wg.Add(1)
	go m.runLoop(instance)

	return nil
}

// awakeRecheckInterval is how often a running loop re-reads loop.keepAwake.
// Sleep is an OS idle timer measured in minutes, so noticing the switch within a
// few seconds is as good as noticing it instantly, and the check is a mutex read.
// A var rather than a const so tests don't have to wait it out.
var awakeRecheckInterval = 5 * time.Second

// holdAwakeWhileEnabled keeps the machine awake for as long as loop.keepAwake is
// on, and returns a function that gives the assertion back.
//
// It re-checks on a ticker rather than reading the setting once at the start.
// The whole point of the setting is to survive a walk-away run, so noticing
// halfway through that it is off — or plugging in and wanting it on — is exactly
// when someone reaches for it, and a run is long enough that waiting for the next
// one is not an answer.
//
// The returned function is not safe to call twice, and must not run concurrently
// with the ticker: it waits for the goroutine to stop before touching the
// reference it holds.
func (m *Manager) holdAwakeWhileEnabled(ctx context.Context) func() {
	held := false
	reconcile := func() {
		cfg := m.Config()
		want := cfg != nil && cfg.Loop.KeepAwake
		if want == held {
			return
		}
		if want {
			m.awake.Acquire()
		} else {
			m.awake.Release()
		}
		held = want
	}
	reconcile()

	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(awakeRecheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				reconcile()
			}
		}
	}()

	return func() {
		close(stop)
		<-stopped
		if held {
			m.awake.Release()
			held = false
		}
	}
}

// runLoop runs a loop instance and forwards events.
func (m *Manager) runLoop(instance *LoopInstance) {
	defer m.wg.Done()

	// Hold the machine awake for as long as this loop runs. Nobody is at the
	// keyboard during a run, so without this the OS idle-sleeps mid-story and the
	// agent is frozen until someone comes back. The guard is reference counted, so
	// parallel PRDs share one assertion and the machine is released when the last
	// loop ends.
	releaseAwake := m.holdAwakeWhileEnabled(instance.ctx)
	defer releaseAwake()

	// Start event forwarding goroutine. It drains the loop's channel until the
	// loop closes it (Run's deferred close), never exiting early: Loop.Run's own
	// sends block when nobody reads, so a forwarder that stops reading while Run
	// still emits would keep Run — and with it wg.Wait — from ever returning.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for event := range instance.Loop.Events() {
			instance.mu.Lock()
			instance.Iteration = event.Iteration
			instance.mu.Unlock()

			// Check if this is a completion event
			completed := event.Type == EventComplete

			// Forward event to manager channel. On the quit path the TUI stops
			// draining m.events while it blocks in StopAll — a bare send here
			// deadlocks the whole shutdown once the buffer fills. When the
			// instance is cancelled, drop the event instead and keep consuming.
			forwarded := false
			select {
			case m.events <- ManagerEvent{
				PRDName:   instance.Name,
				Event:     event,
				Completed: completed,
			}:
				forwarded = true
			case <-instance.ctx.Done():
			}

			// If completed, trigger callbacks — but not for an event dropped on
			// cancellation: a quit must not kick off post-completion actions.
			if completed && forwarded {
				m.mu.RLock()
				callback := m.onComplete
				postCallback := m.onPostComplete
				m.mu.RUnlock()
				if callback != nil {
					callback(instance.Name)
				}
				if postCallback != nil {
					instance.mu.Lock()
					branch := instance.Branch
					workDir := instance.WorktreeDir
					instance.mu.Unlock()
					postCallback(instance.Name, branch, workDir)
				}
			}
		}
	}()

	// Run the loop
	err := instance.Loop.Run(instance.ctx)

	// Update state based on result
	instance.mu.Lock()
	if err != nil && err != context.Canceled {
		instance.State = LoopStateError
		instance.Error = err
	} else if instance.Loop.IsPaused() {
		instance.State = LoopStatePaused
	} else if instance.Loop.IsStopped() {
		instance.State = LoopStateStopped
	} else {
		// Check if PRD is complete. AllResolved treats stories parked for human
		// review as terminal, so a run that parked its last stuck story ends as
		// Complete rather than being misreported as Paused.
		p, loadErr := prd.LoadPRD(instance.PRDPath)
		if loadErr == nil && p.AllResolved() {
			instance.State = LoopStateComplete
		} else if instance.State == LoopStateRunning {
			// Loop ended but not explicitly stopped/paused/completed
			instance.State = LoopStatePaused
		}
	}
	instance.mu.Unlock()

	<-done
}

// Pause pauses the loop for a specific PRD: it stops after the current story
// fully completes, including its review pass when one is configured.
func (m *Manager) Pause(name string) error {
	instance, err := m.lookup(name)
	if err != nil {
		return err
	}

	instance.mu.Lock()
	defer instance.mu.Unlock()

	if instance.State != LoopStateRunning {
		return fmt.Errorf("PRD %s is not running", name)
	}

	if instance.Loop != nil {
		instance.Loop.Pause()
	}

	return nil
}

// Stop stops the loop for a specific PRD immediately.
func (m *Manager) Stop(name string) error {
	instance, err := m.lookup(name)
	if err != nil {
		return err
	}

	instance.mu.Lock()
	defer instance.mu.Unlock()

	if instance.State != LoopStateRunning && instance.State != LoopStatePaused {
		return nil // Already stopped
	}

	if instance.Loop != nil {
		instance.Loop.Stop()
	}
	if instance.cancel != nil {
		instance.cancel()
	}

	instance.State = LoopStateStopped

	return nil
}

// UpdateWorktreeInfo updates the worktree directory and branch for an existing PRD instance.
func (m *Manager) UpdateWorktreeInfo(name, worktreeDir, branch string) error {
	instance, err := m.lookup(name)
	if err != nil {
		return err
	}

	instance.mu.Lock()
	defer instance.mu.Unlock()

	instance.WorktreeDir = worktreeDir
	instance.Branch = branch

	return nil
}

// ClearWorktreeInfo clears the worktree directory and optionally the branch for a PRD instance.
func (m *Manager) ClearWorktreeInfo(name string, clearBranch bool) error {
	instance, err := m.lookup(name)
	if err != nil {
		return err
	}

	instance.mu.Lock()
	defer instance.mu.Unlock()

	instance.WorktreeDir = ""
	if clearBranch {
		instance.Branch = ""
	}

	return nil
}

// GetState returns the state of a specific PRD loop.
func (m *Manager) GetState(name string) (LoopState, int, error) {
	instance, err := m.lookup(name)
	if err != nil {
		return LoopStateReady, 0, err
	}

	instance.mu.Lock()
	defer instance.mu.Unlock()

	return instance.State, instance.Iteration, instance.Error
}

// snapshot returns a copy of the instance's metadata taken under instance.mu, so
// callers get a consistent, race-free view without holding a reference to the
// live struct. Only the plain data fields are copied; the Loop, context and mutex
// are intentionally left zero.
func (i *LoopInstance) snapshot() *LoopInstance {
	i.mu.Lock()
	defer i.mu.Unlock()
	return &LoopInstance{
		Name:        i.Name,
		PRDPath:     i.PRDPath,
		WorktreeDir: i.WorktreeDir,
		Branch:      i.Branch,
		StartRef:    i.StartRef,
		State:       i.State,
		Iteration:   i.Iteration,
		StartTime:   i.StartTime,
		Error:       i.Error,
	}
}

// GetInstance returns a copy of the loop instance data for a specific PRD.
func (m *Manager) GetInstance(name string) *LoopInstance {
	instance, err := m.lookup(name)
	if err != nil {
		return nil
	}
	return instance.snapshot()
}

// GetAllInstances returns a snapshot of all loop instances.
func (m *Manager) GetAllInstances() []*LoopInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*LoopInstance, 0, len(m.instances))
	for _, instance := range m.instances {
		result = append(result, instance.snapshot())
	}

	return result
}

// GetRunningPRDs returns the names of all currently running PRDs.
func (m *Manager) GetRunningPRDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]string, 0)
	for name, instance := range m.instances {
		instance.mu.Lock()
		if instance.State == LoopStateRunning {
			result = append(result, name)
		}
		instance.mu.Unlock()
	}

	return result
}

// GetRunningCount returns the number of currently running loops.
func (m *Manager) GetRunningCount() int {
	return len(m.GetRunningPRDs())
}

// StopAll stops all running loops.
func (m *Manager) StopAll() {
	m.mu.RLock()
	names := make([]string, 0, len(m.instances))
	for name := range m.instances {
		names = append(names, name)
	}
	m.mu.RUnlock()

	for _, name := range names {
		m.Stop(name)
	}

	// Wait for all loops to finish
	m.wg.Wait()
}

// IsAnyRunning returns true if any loop is currently running.
func (m *Manager) IsAnyRunning() bool {
	return m.GetRunningCount() > 0
}

// SetMaxIterations updates the default max iterations for new loops.
func (m *Manager) SetMaxIterations(maxIter int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxIter = maxIter
}

// MaxIterations returns the current default max iterations.
func (m *Manager) MaxIterations() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.maxIter
}

// SetMaxIterationsForInstance updates max iterations for a specific running loop.
func (m *Manager) SetMaxIterationsForInstance(name string, maxIter int) error {
	instance, err := m.lookup(name)
	if err != nil {
		return err
	}

	instance.mu.Lock()
	defer instance.mu.Unlock()

	if instance.Loop != nil {
		instance.Loop.SetMaxIterations(maxIter)
	}

	return nil
}
