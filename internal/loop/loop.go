// Package loop provides the core agent loop that orchestrates Claude Code
// to implement user stories. It includes the main Loop struct for single
// PRD execution, Manager for parallel PRD execution, and Parser for
// processing Claude's stream-json output.
package loop

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ben182/chief/embed"
	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/prd"
)

// RetryConfig configures automatic retry behavior on Claude crashes.
type RetryConfig struct {
	MaxRetries  int             // Maximum number of retry attempts (default: 3)
	RetryDelays []time.Duration // Delays between retries (default: 0s, 5s, 15s)
	Enabled     bool            // Whether retry is enabled (default: true)
}

// DefaultWatchdogTimeout is the default duration of silence before the watchdog kills a hung process.
const DefaultWatchdogTimeout = 5 * time.Minute

// DefaultMaxAttemptsPerStory is how many times a single story may be attempted
// before it is parked for human review and the loop moves on to other stories.
const DefaultMaxAttemptsPerStory = 5

// errAllStoriesComplete is the sentinel a prompt builder returns when the PRD
// has no actionable stories left. It is the only builder error that means "the
// run is done" — anything else (a PRD that momentarily failed to load, say) is
// a real error and must not be mistaken for completion, or the loop would
// consolidate, announce success and push with stories unimplemented.
var errAllStoriesComplete = errors.New("all stories are complete")

// maxReviewAttempts caps how many times the review agent is re-run for a single
// story when it ends its turn without signalling <chief-done/>. Unlike the build
// agent (whose completion is gated on <chief-done/> + a commit and which retries
// across the outer Run loop), the reviewer runs inside finalizeStory, so it needs
// its own bounded retry: a reviewer that stops early gets another fresh-context
// pass to actually finish. The cap keeps a reviewer that never signals done from
// blocking the loop, since review is best-effort and must never stall progress.
const maxReviewAttempts = 3

// maxConsolidateAttempts caps how many times the end-of-run consolidation pass is
// re-run when it ends its turn without signalling <chief-done/>. It is lower than
// maxReviewAttempts because the pass reads the whole run's diff, making each
// attempt expensive, and because it is the most optional thing chief does: the
// stories are already built, reviewed and committed, so giving up early costs
// nothing but a bit of tidiness.
const maxConsolidateAttempts = 2

// DefaultRetryConfig returns the default retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:  3,
		RetryDelays: []time.Duration{0, 5 * time.Second, 15 * time.Second},
		Enabled:     true,
	}
}

// logTimestampLayout is the sortable, filesystem-safe timestamp embedded in each
// per-run log file name. It matches the layout the summary package uses.
const logTimestampLayout = "2006-01-02-150405"

// timestampedLogName turns a provider's static log file name (e.g. "claude.log")
// into a per-run name carrying t's timestamp (e.g. "claude-2006-01-02-150405.log").
// A name without a ".log" suffix still gets the timestamp appended before any
// extension, falling back gracefully for unexpected names.
func timestampedLogName(base string, t time.Time) string {
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	if name == "" {
		name = base
		ext = ""
	}
	return name + "-" + t.Format(logTimestampLayout) + ext
}

// Loop manages the core agent loop that invokes the configured agent repeatedly until all stories are complete.
type Loop struct {
	prdPath           string
	workDir           string
	prompt            string
	buildPrompt       func() (string, string, string, error) // optional: rebuild prompt each iteration; returns (prompt, storyID, storyTitle, error)
	maxIter           int
	iteration         int
	events            chan Event
	provider          Provider
	agentCmd          *exec.Cmd
	logFile           *os.File
	logPath           string
	logMu             sync.Mutex // serializes logFile writes across the stdout/stderr goroutines
	mu                sync.Mutex
	stopped           bool
	paused            bool
	retryConfig       RetryConfig
	lastOutputTime    atomic.Int64 // last stdout activity as Unix nanos; read/written lock-free by the watchdog and per-line hot path
	watchdogTimeout   time.Duration
	sawStoryDone      bool
	currentStoryID    string
	currentStoryTitle string
	stderrTail        []string       // last few stderr lines from the current iteration, for crash diagnostics
	attempts          map[string]int // per-story attempt count, keyed by story ID
	maxAttempts       int            // attempts allowed per story before parking it for review
	warnedNoGit       bool           // whether the "not a git repo" warning was already emitted

	// review configures the optional post-commit review agent. When enabled, a
	// separate agent reviews (and fixes) each story's committed changes before the
	// story is marked done. Which agent an iteration is running is carried by the
	// explicit iterationMode parameter rather than a shared flag; sawReviewDone
	// records that the reviewer signalled <chief-done/> for the current iteration.
	review        reviewer
	sawReviewDone bool

	// consolidate configures the optional end-of-run consolidation pass, which
	// refactors away the seams between stories that no per-story review can see.
	// sawConsolidateDone records that it signalled <chief-done/>.
	consolidate        consolidator
	sawConsolidateDone bool

	// configFn, when set, supplies the project config every time one of these
	// settings is needed, rather than the run using whatever they were when it
	// started. A run is long enough that the answer can legitimately change
	// mid-flight — turning the review off after watching it fix nothing for three
	// stories, or dropping it to a cheaper model — and a setting that only takes
	// effect on the next run is a setting you have to stop the run to use. The
	// captured review/consolidate/watchdog values stay as the fallback for a Loop
	// driven directly, without a manager to read a project config from.
	//
	// It must be safe to call from any goroutine and must not hand back a config
	// that is mutated in place afterwards: readers here copy fields out of it
	// without holding any lock the writer knows about. Manager.Config satisfies
	// both — the TUI replaces the whole config on every edit rather than writing
	// through the pointer.
	configFn func() *config.Config
	// startRef is the branch HEAD captured before this run made any commits. It
	// scopes the consolidation pass to StartRef..HEAD — this run's commits only —
	// so a followup run never refactors work an earlier run already shipped. The
	// Manager sets it from the same value it hands the run summary, so both see an
	// identical window; Run() falls back to capturing it itself for a Loop used
	// directly. Empty means "no scoping possible" and the pass is skipped rather
	// than let loose on the whole branch.
	startRef string
}

// maxStderrTail is how many trailing stderr lines are kept to surface on a crash.
const maxStderrTail = 10

// NewLoop creates a new Loop instance.
func NewLoop(prdPath, prompt string, maxIter int, provider Provider) *Loop {
	return &Loop{
		prdPath:         prdPath,
		prompt:          prompt,
		maxIter:         maxIter,
		provider:        provider,
		events:          make(chan Event, 100),
		retryConfig:     DefaultRetryConfig(),
		watchdogTimeout: DefaultWatchdogTimeout,
		attempts:        make(map[string]int),
		maxAttempts:     DefaultMaxAttemptsPerStory,
	}
}

// NewLoopWithWorkDir creates a new Loop instance with a configurable working directory.
// When workDir is empty, defaults to the project root for backward compatibility.
func NewLoopWithWorkDir(prdPath, workDir string, prompt string, maxIter int, provider Provider) *Loop {
	return &Loop{
		prdPath:         prdPath,
		workDir:         workDir,
		prompt:          prompt,
		maxIter:         maxIter,
		provider:        provider,
		events:          make(chan Event, 100),
		retryConfig:     DefaultRetryConfig(),
		watchdogTimeout: DefaultWatchdogTimeout,
		attempts:        make(map[string]int),
		maxAttempts:     DefaultMaxAttemptsPerStory,
	}
}

// NewLoopWithEmbeddedPrompt creates a new Loop instance using the embedded agent prompt.
// The prompt is rebuilt on each iteration to inline the current story context.
func NewLoopWithEmbeddedPrompt(prdPath string, maxIter int, provider Provider) *Loop {
	l := NewLoop(prdPath, "", maxIter, provider)
	l.buildPrompt = promptBuilderForPRD(prdPath, provider.SupportsInteractiveQuestions())
	return l
}

// prdNameFromPath returns the PRD's name — the unique directory that holds its
// prd.md (".chief/prds/<name>/prd.md" → "<name>"), which is also its branch
// suffix "chief/<name>". It namespaces story commits ("feat: <name>/<ID> - …")
// so same-numbered stories from different PRDs never collide.
func prdNameFromPath(prdPath string) string {
	return filepath.Base(filepath.Dir(prdPath))
}

// promptBuilderForPRD returns a function that loads the PRD and builds a prompt
// with the next story inlined. This is called before each iteration so that
// newly completed stories are skipped. The returned storyID is stored on the Loop.
//
// subagents says whether the provider running the build agent can delegate to
// subagents — today only Claude. It gates the prompt's research-delegation block
// the same way the interactive PRD prompts gate their explore block, so a provider
// without subagents is never told to use them. The provider is read once here
// rather than per iteration because it never changes for the life of a loop.
func promptBuilderForPRD(prdPath string, subagents bool) func() (string, string, string, error) {
	return func() (string, string, string, error) {
		p, err := prd.LoadPRD(prdPath)
		if err != nil {
			return "", "", "", fmt.Errorf("failed to load PRD for prompt: %w", err)
		}

		story := p.NextStory()
		if story == nil {
			return "", "", "", errAllStoriesComplete
		}

		// Mark the story as in-progress in the markdown file. Best-effort: this is
		// a purely cosmetic status shown in the UI (unlike done/needs-review, which
		// are the source of truth for what's left to do), and there's no loop event
		// channel here to surface a failure on.
		_ = prd.SetStoryStatus(prdPath, story.ID, "in-progress")

		storyCtx := p.NextStoryContext()

		prompt := embed.GetPrompt(prd.ProgressPath(prdPath), *storyCtx, prdNameFromPath(prdPath), story.ID, story.Title, subagents)
		return prompt, story.ID, story.Title, nil
	}
}

// SetReview configures the separate review agent. When enabled is true, a review
// agent runs after each story's build agent commits: it reviews the committed
// changes with a fresh context and fixes and re-commits anything it finds. enabled
// decides on its own — skill and instructions only shape the review it runs, they
// never switch it on. Callers coming from a project config pass
// config.ReviewConfig.Active(), which is where "a skill alone enables it" lives.
func (l *Loop) SetReview(enabled bool, skill, instructions string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.review = reviewer{enabled: enabled, skill: skill, instructions: instructions}
}

// SetConfigFn makes the loop read the review, consolidation and watchdog
// settings from fn every time it needs them, instead of using the values it was
// configured with when the run started. Pass nil to go back to the captured
// values. See the configFn field for what fn has to guarantee.
func (l *Loop) SetConfigFn(fn func() *config.Config) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.configFn = fn
}

// liveConfig returns the current project config, or nil when the loop has no
// live source (or the source has none to give).
func (l *Loop) liveConfig() *config.Config {
	l.mu.Lock()
	fn := l.configFn
	l.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn()
}

// currentReviewer returns the review settings in force right now: from the live
// config when there is one, otherwise the values captured at the start.
func (l *Loop) currentReviewer() reviewer {
	if cfg := l.liveConfig(); cfg != nil {
		return reviewer{
			enabled:      cfg.Review.Active(),
			skill:        cfg.Review.Skill,
			instructions: cfg.Review.Instructions,
			model:        cfg.Review.Model,
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.review
}

// currentConsolidator returns the consolidation settings in force right now.
func (l *Loop) currentConsolidator() consolidator {
	if cfg := l.liveConfig(); cfg != nil {
		return consolidator{
			enabled:      cfg.Consolidate.Active(),
			skill:        cfg.Consolidate.Skill,
			instructions: cfg.Consolidate.Instructions,
			model:        cfg.Consolidate.Model,
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.consolidate
}

// currentWatchdogTimeout returns the silence timeout the next iteration should
// run under. A config value of 0 means "unset", which falls back to the captured
// timeout — not to a disabled watchdog, which is what SetWatchdogTimeout(0)
// means for a caller driving the loop directly.
func (l *Loop) currentWatchdogTimeout() time.Duration {
	if cfg := l.liveConfig(); cfg != nil && cfg.Loop.WatchdogTimeoutSeconds > 0 {
		return time.Duration(cfg.Loop.WatchdogTimeoutSeconds) * time.Second
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.watchdogTimeout
}

// SetReviewModel sets the model the review agent runs on, e.g. "haiku". An empty
// string — the default — runs it on defaultPhaseModel instead. It never affects
// the build agent, and it is inert for providers whose CLI takes no model.
func (l *Loop) SetReviewModel(model string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.review.model = model
}

// SetConsolidateModel sets the model the consolidation agent runs on, e.g.
// "haiku". An empty string — the default — runs it on defaultPhaseModel instead.
// It never affects the build agent, and it is inert for providers whose CLI takes
// no model.
func (l *Loop) SetConsolidateModel(model string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.consolidate.model = model
}

// providerForMode returns the provider the given phase's agent should be spawned
// with. The build agent always gets the configured provider untouched; the review
// and consolidation agents get a copy running on their own model, so the phases
// that only read already-committed code don't cost what building it did. A
// provider that can't switch models is returned unchanged, which is what makes the
// phase models inert for every non-Claude CLI.
func (l *Loop) providerForMode(mode iterationMode) Provider {
	model := l.modelForMode(mode)
	if model == "" {
		return l.provider
	}
	switcher, ok := l.provider.(ModelSwitcher)
	if !ok {
		return l.provider
	}
	return switcher.WithModel(model)
}

// modelForMode returns the model a phase's agent runs on, or "" for the build
// agent, which keeps the provider's own model.
func (l *Loop) modelForMode(mode iterationMode) string {
	switch mode {
	case modeReview:
		return l.currentReviewer().effectiveModel()
	case modeConsolidate:
		return l.currentConsolidator().effectiveModel()
	default:
		return ""
	}
}

// reviewEnabled reports whether a review agent should run after a story commits.
func (l *Loop) reviewEnabled() bool {
	return l.currentReviewer().active()
}

// SetConsolidate configures the end-of-run consolidation pass. When enabled is
// true, a single agent runs once the run finishes: it looks across every commit
// the run landed for the seams no per-story review can see (parallel
// implementations of the same thing, competing patterns, leftovers from abandoned
// approaches) and refactors them away in one separate commit. enabled decides on
// its own — skill and instructions only shape the pass it runs, they never switch
// it on. Callers coming from a project config pass
// config.ConsolidateConfig.Active(), which is where "a skill alone enables it" lives.
func (l *Loop) SetConsolidate(enabled bool, skill, instructions string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.consolidate = consolidator{enabled: enabled, skill: skill, instructions: instructions}
}

// consolidateEnabled reports whether the consolidation pass should run at the end
// of the run.
func (l *Loop) consolidateEnabled() bool {
	return l.currentConsolidator().active()
}

// SetStartRef records the branch HEAD captured before this run made any commits,
// scoping the consolidation pass to this run's commits (startRef..HEAD). Callers
// that already capture this for the run summary should pass the same value so
// both features describe an identical window. When it is never set, Run()
// captures it itself at startup.
func (l *Loop) SetStartRef(ref string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.startRef = ref
}

// Events returns the channel for receiving events from the loop.
func (l *Loop) Events() <-chan Event {
	return l.events
}

// Iteration returns the current iteration number.
func (l *Loop) Iteration() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.iteration
}

// LogPath returns the absolute path of the current run's log file, or "" if the
// run has not opened one yet.
func (l *Loop) LogPath() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.logPath
}

// Run executes the agent loop until completion or max iterations.
func (l *Loop) Run(ctx context.Context) error {
	// Close the events channel on every exit path, including the early error
	// returns: the Manager's forwarder drains l.events until it closes, and
	// runLoop waits for that forwarder — an unclosed channel would leave both
	// waiting forever. Run is called exactly once per Loop (every start builds a
	// fresh Loop), so the close cannot double-fire.
	defer close(l.events)

	if l.provider == nil {
		return fmt.Errorf("loop provider is not configured")
	}

	if err := l.openRunLog(); err != nil {
		return err
	}
	defer l.logFile.Close()

	// Capture the branch HEAD before the first iteration commits anything, so the
	// end-of-run consolidation pass can be scoped to this run's commits. A caller
	// that already captured it (the Manager, which hands the same value to the run
	// summary) wins, so both features see an identical window.
	l.mu.Lock()
	needStartRef := l.startRef == ""
	l.mu.Unlock()
	if needStartRef {
		ref, _ := git.HeadHash(l.effectiveWorkDir())
		l.mu.Lock()
		l.startRef = ref
		l.mu.Unlock()
	}

	for {
		l.mu.Lock()
		if l.stopped {
			l.mu.Unlock()
			return nil
		}
		if l.paused {
			l.mu.Unlock()
			return nil
		}
		l.iteration++
		currentIter := l.iteration
		l.mu.Unlock()

		// Check if max iterations reached
		if currentIter > l.maxIter {
			// The run is over, even though stories remain. Consolidate what it did
			// land before handing off to the summary/push, exactly as on a full
			// completion — a run cut short by the iteration cap still leaves seams
			// between the stories it finished.
			l.runConsolidation(ctx, currentIter-1)
			l.events <- Event{
				Type:      EventMaxIterationsReached,
				Iteration: currentIter - 1,
			}
			return nil
		}

		// Rebuild prompt if builder is set (inlines the current story each iteration)
		if l.buildPrompt != nil {
			prompt, storyID, storyTitle, err := l.buildPrompt()
			if errors.Is(err, errAllStoriesComplete) {
				// No actionable stories left: the run is done. Consolidate before
				// announcing completion, so the refactor lands *before* the TUI's
				// post-completion actions run — the summary then describes the
				// consolidated result and the push/PR carries it.
				l.runConsolidation(ctx, currentIter)
				l.events <- Event{
					Type:      EventComplete,
					Iteration: currentIter,
				}
				return nil
			}
			if err != nil {
				l.events <- Event{
					Type: EventError,
					Err:  err,
				}
				return err
			}
			l.mu.Lock()
			l.prompt = prompt
			l.currentStoryID = storyID
			l.currentStoryTitle = storyTitle
			l.sawStoryDone = false
			l.mu.Unlock()
		}

		// Send iteration start event with current story ID
		l.mu.Lock()
		iterStoryID := l.currentStoryID
		l.mu.Unlock()
		l.events <- Event{
			Type:      EventIterationStart,
			Iteration: currentIter,
			StoryID:   iterStoryID,
		}

		// Run a single iteration with retry logic
		if err := l.runIterationWithRetry(ctx, modeBuild); err != nil {
			l.events <- Event{
				Type: EventError,
				Err:  err,
			}
			return err
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Resolve the story outcome for this iteration: mark it done (gated on a
		// real commit and an optional review pass) or count a failed attempt and
		// eventually park it. A source-of-truth write failure returns an error that
		// stops the run. buildPrompt on the next iteration returns
		// errAllStoriesComplete when no actionable stories remain, which causes
		// EventComplete to be emitted above.
		if err := l.finalizeStory(ctx, currentIter); err != nil {
			return err
		}

		// Check pause flag after the story is fully finalized (including its
		// review pass), so a pause stops between stories, never inside one.
		l.mu.Lock()
		if l.paused {
			l.mu.Unlock()
			return nil
		}
		l.mu.Unlock()
	}
}

// runIterationWithRetry wraps runIteration with retry logic for crash recovery.
// mode selects which agent (build or review) the iteration runs.
func (l *Loop) runIterationWithRetry(ctx context.Context, mode iterationMode) error {
	l.mu.Lock()
	config := l.retryConfig
	l.mu.Unlock()

	var lastErr error
	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		// Check if retry is enabled (except for first attempt)
		if attempt > 0 {
			if !config.Enabled {
				return lastErr
			}

			// Get delay for this retry
			delayIdx := attempt - 1
			if delayIdx >= len(config.RetryDelays) {
				delayIdx = len(config.RetryDelays) - 1
			}
			delay := config.RetryDelays[delayIdx]

			// Emit retry event
			l.mu.Lock()
			iter := l.iteration
			crashLog := append([]string(nil), l.stderrTail...)
			l.mu.Unlock()
			l.events <- Event{
				Type:       EventRetrying,
				Iteration:  iter,
				RetryCount: attempt,
				RetryMax:   config.MaxRetries,
				Text:       fmt.Sprintf("%s crashed, retrying (%d/%d)...", l.provider.Name(), attempt, config.MaxRetries),
				CrashLog:   crashLog,
			}

			// Wait before retry
			if delay > 0 {
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}

		// Check if stopped during delay
		l.mu.Lock()
		if l.stopped {
			l.mu.Unlock()
			return nil
		}
		l.mu.Unlock()

		// Run the iteration
		err := l.runIteration(ctx, mode)
		if err == nil {
			return nil // Success
		}

		// Check if this is a context cancellation (don't retry)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Check if stopped intentionally
		l.mu.Lock()
		stopped := l.stopped
		l.mu.Unlock()
		if stopped {
			return nil
		}

		lastErr = err
	}

	return fmt.Errorf("max retries (%d) exceeded: %w", config.MaxRetries, lastErr)
}

// runIteration spawns the agent and processes its output. mode selects which
// agent is running so a <chief-done/> is attributed to the build or review agent.
func (l *Loop) runIteration(ctx context.Context, mode iterationMode) error {
	workDir := l.effectiveWorkDir()
	cmd := l.providerForMode(mode).LoopCommand(ctx, l.prompt, workDir)
	setProcessGroup(cmd) // kill the whole subprocess tree, not just the direct child
	// Resolved per iteration, and before l.mu is taken: the live config source
	// locks l.mu itself, and a raised timeout is worth picking up on the next
	// agent rather than only on the next run.
	watchdogTimeout := l.currentWatchdogTimeout()
	l.mu.Lock()
	l.agentCmd = cmd
	// Clear the command on every return path (success or error) so IsRunning()
	// doesn't keep reporting a finished process as running during retry delays.
	defer func() {
		l.mu.Lock()
		l.agentCmd = nil
		l.mu.Unlock()
	}()
	l.stderrTail = nil // reset crash diagnostics for this iteration
	// Initialize watchdog state
	l.lastOutputTime.Store(time.Now().UnixNano())
	l.mu.Unlock()

	// Create pipes for stdout and stderr
	stdout, err := l.agentCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := l.agentCmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the command
	if err := l.agentCmd.Start(); err != nil {
		return fmt.Errorf("failed to start %s: %w", l.provider.Name(), err)
	}

	// Start watchdog goroutine to detect hung processes. watchdogStopped is
	// closed when the goroutine has fully returned, so runIteration can join it
	// below before returning — otherwise the watchdog could still be mid-send on
	// l.events when Run() closes that channel, panicking with "send on closed
	// channel".
	watchdogDone := make(chan struct{})
	watchdogStopped := make(chan struct{})
	var watchdogFired atomic.Bool
	if watchdogTimeout > 0 {
		go func() {
			defer close(watchdogStopped)
			l.runWatchdog(watchdogTimeout, watchdogDone, &watchdogFired)
		}()
	} else {
		close(watchdogStopped)
	}

	// Process stdout in a separate goroutine
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		l.processOutput(stdout, mode)
	}()

	// Log stderr to the log file
	go func() {
		defer wg.Done()
		l.logStream(stderr, "[stderr] ")
	}()

	// Wait for output processing to complete
	wg.Wait()

	// Stop the watchdog and wait for it to fully exit before returning, so it
	// can never send on l.events after Run() has closed the channel.
	close(watchdogDone)
	<-watchdogStopped

	// Wait for the command to finish
	if err := l.agentCmd.Wait(); err != nil {
		// If the context was cancelled, don't treat it as an error
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Check if we were stopped intentionally
		l.mu.Lock()
		stopped := l.stopped
		l.mu.Unlock()
		if stopped {
			return nil
		}
		// Check if we killed the process ourselves after <chief-done/>.
		// That's a graceful end of the iteration, not a crash, so don't retry.
		// Covers the build agent (sawStoryDone), the review agent (sawReviewDone)
		// and the end-of-run consolidation pass (sawConsolidateDone).
		l.mu.Lock()
		saw := l.sawStoryDone || l.sawReviewDone || l.sawConsolidateDone
		l.mu.Unlock()
		if saw {
			return nil
		}
		// A real crash (or watchdog kill): map it to the loop-level error to surface.
		return classifyExit(err, watchdogFired.Load(), watchdogTimeout, l.provider.Name())
	}

	return nil
}

// classifyExit turns a non-graceful agent Wait() error into the loop-level error
// to surface. A watchdog kill is reported as a timeout; anything else is a crash
// attributed to the provider. It is pure so the mapping can be unit-tested
// without spawning a process.
func classifyExit(err error, watchdogFired bool, watchdogTimeout time.Duration, providerName string) error {
	if watchdogFired {
		return fmt.Errorf("watchdog timeout: no output for %s", watchdogTimeout)
	}
	return fmt.Errorf("%s exited with error: %w", providerName, err)
}

// runWatchdog monitors lastOutputTime and kills the process if no output is received
// within the timeout duration. It stops when watchdogDone is closed.
func (l *Loop) runWatchdog(timeout time.Duration, done <-chan struct{}, fired *atomic.Bool) {
	// Check interval scales with timeout: 1/5 of timeout, clamped to [10ms, 10s]
	checkInterval := timeout / 5
	if checkInterval < 10*time.Millisecond {
		checkInterval = 10 * time.Millisecond
	}
	if checkInterval > 10*time.Second {
		checkInterval = 10 * time.Second
	}
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			lastOutput := time.Unix(0, l.lastOutputTime.Load())
			l.mu.Lock()
			stopped := l.stopped
			l.mu.Unlock()

			if stopped {
				return
			}

			if time.Since(lastOutput) > timeout {
				fired.Store(true)

				// Emit watchdog timeout event
				l.mu.Lock()
				iter := l.iteration
				l.mu.Unlock()
				l.events <- Event{
					Type:      EventWatchdogTimeout,
					Iteration: iter,
					Text:      fmt.Sprintf("No output for %s, killing hung process", timeout),
				}

				// Kill the process group
				l.mu.Lock()
				if l.agentCmd != nil {
					killProcessGroup(l.agentCmd.Process)
				}
				l.mu.Unlock()
				return
			}
		case <-done:
			return
		}
	}
}

// processOutput reads stdout line by line, logs it, and parses events. mode
// tells it whether the build or review agent produced the output, so a
// <chief-done/> sets the matching done flag and the reviewer's done is swallowed.
func (l *Loop) processOutput(r io.Reader, mode iterationMode) {
	scanner := bufio.NewScanner(r)
	// Increase buffer size for long lines (Claude can output large JSON)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		// Update last output time for watchdog. This is on the per-line hot path,
		// so it uses an atomic store instead of taking l.mu for every line.
		l.lastOutputTime.Store(time.Now().UnixNano())

		// Log raw output
		l.logLine(line)

		// Parse the line and emit event if valid
		if event := l.provider.ParseLine(line); event != nil {
			l.mu.Lock()
			event.Iteration = l.iteration
			if event.Type == EventStoryDone {
				// Claude doesn't always exit after <chief-done/>, which leaves the
				// scanner blocked until the watchdog kills it. Terminate the process
				// now so the iteration ends immediately. runIteration treats a Wait
				// error as success when the corresponding done flag is set, so this
				// is not a crash.
				switch mode {
				case modeReview:
					l.sawReviewDone = true
				case modeConsolidate:
					l.sawConsolidateDone = true
				default:
					l.sawStoryDone = true
				}
				if l.agentCmd != nil {
					killProcessGroup(l.agentCmd.Process)
				}
				l.mu.Unlock()
				// Swallow the review and consolidation agents' <chief-done/> so the
				// TUI doesn't render a spurious "story done" for them: only the build
				// agent's done signals story completion, and it was already forwarded.
				// Their own completion is reported via EventReviewDone /
				// EventConsolidateDone instead.
				if !mode.isStoryAgent() {
					continue
				}
				l.events <- *event
				continue
			}
			l.mu.Unlock()
			l.events <- *event
		}
	}
}

// logStream logs a stream with a prefix.
func (l *Loop) logStream(r io.Reader, prefix string) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		l.logLine(prefix + line)
		l.captureStderr(line)
	}
}

// captureStderr keeps the last maxStderrTail non-empty stderr lines so a crash
// can surface them in the TUI instead of hiding them in the log file.
func (l *Loop) captureStderr(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	l.mu.Lock()
	l.stderrTail = append(l.stderrTail, line)
	if len(l.stderrTail) > maxStderrTail {
		l.stderrTail = l.stderrTail[len(l.stderrTail)-maxStderrTail:]
	}
	l.mu.Unlock()
}

// logLine writes a line to the log file. processOutput (stdout) and logStream
// (stderr) call this from separate goroutines, so a mutex keeps their lines from
// interleaving mid-line and corrupting the stream-json log.
func (l *Loop) logLine(line string) {
	l.logMu.Lock()
	defer l.logMu.Unlock()
	if l.logFile != nil {
		l.logFile.WriteString(line + "\n")
	}
}

// Stop terminates the current agent process and stops the loop.
func (l *Loop) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.stopped = true

	if l.agentCmd != nil {
		killProcessGroup(l.agentCmd.Process)
	}
}

// Pause sets the pause flag. The loop will stop after the current story fully
// completes: the build iteration finishes, and when a review is configured its
// review pass still runs before the loop returns. Only Stop() abandons a story
// mid-flight.
func (l *Loop) Pause() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.paused = true
}

// Resume clears the pause flag.
func (l *Loop) Resume() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.paused = false
}

// IsPaused returns whether the loop is paused.
func (l *Loop) IsPaused() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.paused
}

// IsStopped returns whether the loop is stopped.
func (l *Loop) IsStopped() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stopped
}

// effectiveWorkDir returns the working directory to use for the agent.
// If workDir is set, it is used directly. Otherwise, defaults to the PRD directory.
func (l *Loop) effectiveWorkDir() string {
	if l.workDir != "" {
		return l.workDir
	}
	return filepath.Dir(l.prdPath)
}

// storyHasCommit reports whether a commit matching the story landed, so a
// <chief-done/> signal can be trusted. Outside a git repo the commit status
// can't be determined, so it returns true (fail-open) rather than blocking
// completion in non-git setups. Inside a repo — even a brand-new one with no
// commits yet — the absence of a matching commit means the story is not done.
func (l *Loop) storyHasCommit(storyID, title string) bool {
	dir := l.effectiveWorkDir()
	if !git.IsGitRepo(dir) {
		l.mu.Lock()
		warn := !l.warnedNoGit
		l.warnedNoGit = true
		l.mu.Unlock()
		if warn && l.events != nil {
			l.events <- Event{
				Type: EventNoGitRepo,
				Text: fmt.Sprintf("⚠ %s is not a git repo: story completion can't be commit-verified and work is not persisted between iterations", dir),
			}
		}
		return true
	}
	// Whole-branch search (no since-ref): a story committed by an earlier run on
	// this branch is still done, so a followup run must recognize it as complete.
	hash, err := git.FindCommitForStory(dir, prdNameFromPath(l.prdPath), storyID, title, "")
	if err != nil {
		// git log itself failed, which is not the same as "no matching commit".
		// One legitimate case must stay fail-closed: a repo with no commits yet,
		// where git log errors and "not done" is the right answer. Probe HEAD to
		// tell it apart. If HEAD resolves, the repo has commits and the check
		// itself is what broke (transient git failure, corrupt pack): fail open
		// and trust <chief-done/> rather than counting a failed attempt against
		// a story whose commit may be sitting in history.
		if _, headErr := git.HeadHash(dir); headErr == nil {
			l.logLine(fmt.Sprintf("[chief] could not verify commit for story %s (%v); trusting <chief-done/>", storyID, err))
			return true
		}
		return false
	}
	return hash != ""
}

// commitStoryProgress attaches chief's own working files (prd.md, progress.md
// and the scoped .gitignore) to the story that just finished, so they stop
// piling up as uncommitted changes and a completed story's tracked progress
// survives an interrupted run. When the agent's story commit is HEAD it folds
// them in via amend, keeping one commit per story; otherwise it makes a small
// standalone commit. Best-effort: any failure is left for the end-of-run summary
// sweep to pick up. The TUI writes this story's progress.md timing just after it
// finishes and may not have landed yet — whatever is missed here is captured by
// the next story's commit or the final sweep.
func (l *Loop) commitStoryProgress(storyID, storyTitle string) {
	dir := l.effectiveWorkDir()
	if !git.IsGitRepo(dir) {
		return
	}
	prdDir := filepath.Dir(l.prdPath)
	// Only stage files that exist: `git add` fails the whole command on a missing
	// pathspec, and progress.md / .gitignore may not have been written yet. The
	// follow-up inbox (todos.md) is swept in too so a `chief followup` run's
	// checked-off items don't linger as an uncommitted change after the loop lands
	// the stories it ingested; FollowupInboxPath returns "" when no inbox exists.
	var paths []string
	candidates := []string{l.prdPath, prd.ProgressPath(l.prdPath), filepath.Join(prdDir, ".gitignore")}
	if inbox := prd.FollowupInboxPath(prdDir); inbox != "" {
		candidates = append(candidates, inbox)
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			paths = append(paths, p)
		}
	}
	// A user who gitignored .chief/ chose to keep these files local; committing
	// them anyway would override that choice. Files that are already tracked stay
	// in — gitignore doesn't apply to them, and skipping them would let their
	// changes pile up uncommitted.
	paths = git.CommittablePaths(dir, paths...)
	if len(paths) == 0 {
		return
	}
	expected := fmt.Sprintf("feat: %s/%s - %s", prdNameFromPath(l.prdPath), storyID, storyTitle)
	if subj, err := git.HeadSubject(dir); err == nil && subj == expected {
		_ = git.AmendPaths(dir, paths...)
		return
	}
	_ = git.CommitPaths(dir, fmt.Sprintf("chore: track %s progress", storyID), paths...)
}

// runReview spawns a separate agent that reviews (and fixes) the changes the
// build agent just committed for the story. It reuses runIteration for the
// process plumbing (watchdog, output parsing, <chief-done/> handling) by running
// it in modeReview, but swaps in the review prompt. It is best-effort: a review
// crash is surfaced as an event but never un-completes the story, so a flaky
// reviewer can't block progress. The reviewer commits its own fixes; chief does
// not gate the story on a pass/fail signal.
func (l *Loop) runReview(ctx context.Context, iteration int, storyID, storyTitle string) {
	// Skip cleanly if we're already shutting down. A pause deliberately does NOT
	// skip the review: pausing means "stop after the current story", and a story
	// with review enabled isn't finished until its review ran — bailing here would
	// mark it done unreviewed, which is exactly what the user paused to avoid.
	// Only a hard stop (or context cancellation) abandons the review.
	select {
	case <-ctx.Done():
		return
	default:
	}
	l.mu.Lock()
	if l.stopped {
		l.mu.Unlock()
		return
	}
	l.mu.Unlock()

	prompt, err := l.buildReviewPrompt()
	if err != nil {
		l.events <- Event{
			Type:      EventReviewDone,
			Iteration: iteration,
			StoryID:   storyID,
			Text:      fmt.Sprintf("Review skipped for %s: %v", storyID, err),
		}
		return
	}

	l.events <- Event{
		Type:      EventReviewStart,
		Iteration: iteration,
		StoryID:   storyID,
		Text:      fmt.Sprintf("Reviewing story %s", storyID),
	}

	// Swap in the review prompt. buildPrompt rebuilds l.prompt on the next
	// iteration, so restoring the old prompt is unnecessary. The review agent is
	// selected by passing modeReview to runIterationWithRetry rather than by a
	// shared flag.
	l.mu.Lock()
	l.prompt = prompt
	l.mu.Unlock()

	// Re-run the reviewer until it actually signals <chief-done/>, bounded by
	// maxReviewAttempts. runIterationWithRetry only retries on a crash: a clean
	// process exit returns nil even when the reviewer ended its turn WITHOUT
	// signalling done. Reporting "complete" on that clean-but-unfinished exit is
	// exactly the premature-completion bug — the story gets marked done while the
	// review never finished. So each pass is gated on sawReviewDone; an unfinished
	// pass earns a fresh-context re-run, mirroring how the build agent is gated on
	// its own done signal across the outer Run loop.
	var runErr error
	var reviewDone bool
	for attempt := 0; attempt < maxReviewAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return
		default:
		}
		l.mu.Lock()
		if l.stopped {
			l.mu.Unlock()
			return
		}
		l.sawReviewDone = false
		l.mu.Unlock()

		// A crashed reviewer is retried like any other iteration, but a persistent
		// failure must not stall the loop, so errors are only surfaced, not fatal.
		runErr = l.runIterationWithRetry(ctx, modeReview)

		l.mu.Lock()
		reviewDone = l.sawReviewDone
		l.sawReviewDone = false
		l.mu.Unlock()

		// A crash or an actual completion signal both end the review; only a
		// clean-but-unfinished exit (no error, no done signal) warrants another pass.
		if runErr != nil || reviewDone || ctx.Err() != nil {
			break
		}
	}

	text := fmt.Sprintf("Review complete for %s", storyID)
	switch {
	case runErr != nil && ctx.Err() == nil:
		text = fmt.Sprintf("Review of %s did not finish cleanly: %v", storyID, runErr)
	case !reviewDone && ctx.Err() == nil:
		text = fmt.Sprintf("Review of %s stopped without signalling completion after %d attempts", storyID, maxReviewAttempts)
	}
	l.events <- Event{
		Type:      EventReviewDone,
		Iteration: iteration,
		StoryID:   storyID,
		Text:      text,
	}
}

// runConsolidation spawns a single agent, once, at the end of the run: it looks
// across every commit the run landed and refactors away the seams that no
// per-story review can see — two stories that each grew their own helper for the
// same job, competing patterns for one concern, leftovers from an abandoned
// approach. Each story was built with a fresh context, so those only become
// visible when someone finally looks at the whole run.
//
// It reuses runIteration for the process plumbing by running in modeConsolidate,
// swapping in the consolidation prompt. Like the reviewer it is best-effort: a
// crash or a pass that never signals done is surfaced as an event but never fails
// the run — the stories are already built, committed and reviewed, and an
// unconsolidated run is a far better outcome than a blocked one.
//
// It is a no-op when the pass is disabled, when the run is being stopped or
// paused, or when the run landed no story commits to consolidate.
func (l *Loop) runConsolidation(ctx context.Context, iteration int) {
	if !l.consolidateEnabled() {
		return
	}
	// Skip cleanly if we're already shutting down.
	select {
	case <-ctx.Done():
		return
	default:
	}
	l.mu.Lock()
	if l.stopped || l.paused {
		l.mu.Unlock()
		return
	}
	l.mu.Unlock()

	prompt, err := l.buildConsolidatePrompt()
	if err != nil {
		l.events <- Event{
			Type:      EventConsolidateDone,
			Iteration: iteration,
			Text:      fmt.Sprintf("Consolidation skipped: %v", err),
		}
		return
	}

	l.events <- Event{
		Type:      EventConsolidateStart,
		Iteration: iteration,
		Text:      "Consolidating this run's commits",
	}

	l.mu.Lock()
	l.prompt = prompt
	l.mu.Unlock()

	// Re-run until the pass actually signals <chief-done/>, bounded by
	// maxConsolidateAttempts, for the same reason the reviewer is: a clean process
	// exit returns nil even when the agent ended its turn *without* finishing, and
	// reporting that as success would claim a consolidation that never happened.
	var runErr error
	var done bool
	for attempt := 0; attempt < maxConsolidateAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return
		default:
		}
		l.mu.Lock()
		if l.stopped || l.paused {
			l.mu.Unlock()
			return
		}
		l.sawConsolidateDone = false
		l.mu.Unlock()

		runErr = l.runIterationWithRetry(ctx, modeConsolidate)

		l.mu.Lock()
		done = l.sawConsolidateDone
		l.sawConsolidateDone = false
		l.mu.Unlock()

		// A crash or an actual completion signal both end the pass; only a
		// clean-but-unfinished exit warrants another fresh-context attempt.
		if runErr != nil || done || ctx.Err() != nil {
			break
		}
	}

	text := "Consolidation complete"
	switch {
	case runErr != nil && ctx.Err() == nil:
		text = fmt.Sprintf("Consolidation did not finish cleanly: %v", runErr)
	case !done && ctx.Err() == nil:
		text = fmt.Sprintf("Consolidation stopped without signalling completion after %d attempts", maxConsolidateAttempts)
	}
	l.events <- Event{
		Type:      EventConsolidateDone,
		Iteration: iteration,
		Text:      text,
	}
}

// buildConsolidatePrompt builds the consolidation-agent prompt, inlining the log
// of the commits *this run* landed.
//
// The run scoping is the point: the agent is handed startRef..HEAD, so a followup
// run consolidates only its own stories and never reopens work an earlier run
// already summarized, pushed or shipped. It returns an error — which the caller
// turns into a clean skip — when the run has nothing to consolidate:
//
//   - not a git repo, so commits can't be identified at all;
//   - no start ref (unborn branch), which would leave the pass unscoped and let it
//     loose on the entire branch history;
//   - no story commits in this run's window (every story was already done, or the
//     run landed nothing).
func (l *Loop) buildConsolidatePrompt() (string, error) {
	c := l.currentConsolidator()
	skill := c.skill
	instructions := c.instructions

	l.mu.Lock()
	startRef := l.startRef
	l.mu.Unlock()

	dir := l.effectiveWorkDir()
	if !git.IsGitRepo(dir) {
		return "", fmt.Errorf("%s is not a git repo", dir)
	}
	if startRef == "" {
		return "", fmt.Errorf("no run start ref to scope this run's commits to")
	}

	p, err := prd.LoadPRD(l.prdPath)
	if err != nil {
		return "", fmt.Errorf("failed to load PRD for consolidation: %w", err)
	}
	prdName := prdNameFromPath(l.prdPath)

	refs := make([]git.StoryRef, 0, len(p.UserStories))
	for _, s := range p.UserStories {
		refs = append(refs, git.StoryRef{PRDName: prdName, ID: s.ID, Title: s.Title})
	}
	commits, err := git.CommitLogForStories(dir, refs, startRef)
	if err != nil {
		return "", fmt.Errorf("failed to collect this run's commits: %w", err)
	}
	if strings.TrimSpace(commits) == "" {
		return "", fmt.Errorf("this run landed no story commits")
	}

	sinceSpec := startRef + "..HEAD"
	return embed.GetConsolidatePrompt(prd.ProgressPath(l.prdPath), commits, sinceSpec, prdName, skill, instructions), nil
}

// buildReviewPrompt loads the PRD and builds the review-agent prompt for the
// story currently being reviewed, inlining that story's context.
func (l *Loop) buildReviewPrompt() (string, error) {
	r := l.currentReviewer()
	skill := r.skill
	instructions := r.instructions

	l.mu.Lock()
	storyID := l.currentStoryID
	storyTitle := l.currentStoryTitle
	l.mu.Unlock()

	p, err := prd.LoadPRD(l.prdPath)
	if err != nil {
		return "", fmt.Errorf("failed to load PRD for review: %w", err)
	}
	storyCtx := p.StoryContextByID(storyID)
	if storyCtx == nil {
		return "", fmt.Errorf("story %s not found for review", storyID)
	}
	return embed.GetReviewPrompt(prd.ProgressPath(l.prdPath), *storyCtx, storyID, storyTitle, skill, instructions), nil
}

// IsRunning returns whether an agent process is currently running.
func (l *Loop) IsRunning() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.agentCmd != nil && l.agentCmd.Process != nil
}

// SetMaxIterations updates the maximum iterations limit.
func (l *Loop) SetMaxIterations(maxIter int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.maxIter = maxIter
}

// MaxIterations returns the current max iterations limit.
func (l *Loop) MaxIterations() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.maxIter
}

// SetMaxAttemptsPerStory sets how many times a story is attempted before it is
// parked for human review. A value <= 0 disables parking (stories retry until
// the global max-iterations backstop fires).
func (l *Loop) SetMaxAttemptsPerStory(n int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.maxAttempts = n
}

// SetRetryConfig updates the retry configuration.
func (l *Loop) SetRetryConfig(config RetryConfig) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.retryConfig = config
}

// DisableRetry disables automatic retry on crash.
func (l *Loop) DisableRetry() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.retryConfig.Enabled = false
}

// SetWatchdogTimeout sets the watchdog timeout duration.
// Setting timeout to 0 disables the watchdog.
func (l *Loop) SetWatchdogTimeout(timeout time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.watchdogTimeout = timeout
}

// WatchdogTimeout returns the current watchdog timeout duration.
func (l *Loop) WatchdogTimeout() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.watchdogTimeout
}
