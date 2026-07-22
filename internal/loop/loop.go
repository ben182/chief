// Package loop provides the core agent loop that orchestrates Claude Code
// to implement user stories. It includes the main Loop struct for single
// PRD execution, Manager for parallel PRD execution, and Parser for
// processing Claude's stream-json output.
package loop

import (
	"bufio"
	"context"
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
	mu                sync.Mutex
	stopped           bool
	paused            bool
	retryConfig       RetryConfig
	lastOutputTime    time.Time
	watchdogTimeout   time.Duration
	sawStoryDone      bool
	currentStoryID    string
	currentStoryTitle string
	stderrTail        []string       // last few stderr lines from the current iteration, for crash diagnostics
	attempts          map[string]int // per-story attempt count, keyed by story ID
	maxAttempts       int            // attempts allowed per story before parking it for review
	warnedNoGit       bool           // whether the "not a git repo" warning was already emitted

	// Review agent: when reviewSkill or reviewInstructions is set, a separate
	// agent reviews (and fixes) each story's committed changes before it is marked
	// done. reviewMode is true only while that review agent is running, so
	// processOutput/runIteration know a <chief-done/> came from the reviewer, not
	// the build agent.
	reviewSkill        string
	reviewInstructions string
	reviewMode         bool
	sawReviewDone      bool
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
	l.buildPrompt = promptBuilderForPRD(prdPath)
	return l
}

// promptBuilderForPRD returns a function that loads the PRD and builds a prompt
// with the next story inlined. This is called before each iteration so that
// newly completed stories are skipped. The returned storyID is stored on the Loop.
func promptBuilderForPRD(prdPath string) func() (string, string, string, error) {
	return func() (string, string, string, error) {
		p, err := prd.LoadPRD(prdPath)
		if err != nil {
			return "", "", "", fmt.Errorf("failed to load PRD for prompt: %w", err)
		}

		story := p.NextStory()
		if story == nil {
			return "", "", "", fmt.Errorf("all stories are complete")
		}

		// Mark the story as in-progress in the markdown file
		_ = prd.SetStoryStatus(prdPath, story.ID, "in-progress")

		storyCtx := p.NextStoryContext()

		prompt := embed.GetPrompt(prd.ProgressPath(prdPath), *storyCtx, story.ID, story.Title)
		return prompt, story.ID, story.Title, nil
	}
}

// SetReview configures the separate review agent. When either skill or
// instructions is non-empty, a review agent runs after each story's build agent
// commits: it reviews the committed changes with a fresh context and fixes and
// re-commits anything it finds. Both empty disables the review.
func (l *Loop) SetReview(skill, instructions string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reviewSkill = skill
	l.reviewInstructions = instructions
}

// reviewEnabled reports whether a review agent should run after a story commits.
func (l *Loop) reviewEnabled() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.TrimSpace(l.reviewSkill) != "" || strings.TrimSpace(l.reviewInstructions) != ""
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
	if l.provider == nil {
		return fmt.Errorf("loop provider is not configured")
	}

	// Open a per-run log file in the PRD directory. The name carries a timestamp
	// (e.g. claude-2006-01-02-150405.log) so each run keeps its own log next to
	// its own summary-<time>.md, instead of every run appending to one file that
	// grows without bound and mixes runs together.
	prdDir := filepath.Dir(l.prdPath)
	git.IgnoreLogsIn(prdDir)
	logPath := filepath.Join(prdDir, timestampedLogName(l.provider.LogFileName(), time.Now()))
	var err error
	l.logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	l.mu.Lock()
	l.logPath = logPath
	l.mu.Unlock()
	defer l.logFile.Close()
	defer close(l.events)

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
			l.events <- Event{
				Type:      EventMaxIterationsReached,
				Iteration: currentIter - 1,
			}
			return nil
		}

		// Rebuild prompt if builder is set (inlines the current story each iteration)
		if l.buildPrompt != nil {
			prompt, storyID, storyTitle, err := l.buildPrompt()
			if err != nil {
				l.events <- Event{
					Type:      EventComplete,
					Iteration: currentIter,
				}
				return nil
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
		if err := l.runIterationWithRetry(ctx); err != nil {
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

		// If the agent emitted <chief-done/>, mark the story as done in prd.md.
		// Otherwise the story did not complete this iteration: count the attempt
		// and, once the per-story limit is hit, park it for human review so the
		// loop can move on to other unblocked stories instead of retrying forever.
		l.mu.Lock()
		saw := l.sawStoryDone
		storyID := l.currentStoryID
		storyTitle := l.currentStoryTitle
		l.sawStoryDone = false
		l.mu.Unlock()
		// A <chief-done/> signal is only trusted if a matching commit actually
		// landed. Otherwise the agent claimed done but produced no committed work
		// (forgot to commit, hook rejected, crash before commit): the next
		// fresh-context iteration would move on and the change could be lost. Treat
		// that as a failed attempt so the story is retried or eventually parked.
		if saw && storyID != "" && !l.storyHasCommit(storyID, storyTitle) {
			saw = false
			l.events <- Event{
				Type:      EventStoryNoCommit,
				Iteration: currentIter,
				StoryID:   storyID,
				Text:      fmt.Sprintf("Story %s signalled done but no commit was found; treating as incomplete", storyID),
			}
		}
		if saw && storyID != "" {
			// The build agent finished and committed. Before marking the story
			// done, run a separate review agent (fresh context) that reviews and
			// fixes the committed changes. It is a best-effort quality gate: a
			// review crash is logged but does not un-complete the story.
			if l.reviewEnabled() {
				l.runReview(ctx, currentIter, storyID, storyTitle)
			}
			_ = prd.SetStoryStatus(l.prdPath, storyID, "done")
			l.commitStoryProgress(storyID, storyTitle)
		} else if storyID != "" {
			l.mu.Lock()
			l.attempts[storyID]++
			attempts := l.attempts[storyID]
			maxAttempts := l.maxAttempts
			l.mu.Unlock()
			if maxAttempts > 0 && attempts >= maxAttempts {
				_ = prd.SetStoryStatus(l.prdPath, storyID, "needs-review")
				l.events <- Event{
					Type:      EventStoryNeedsReview,
					Iteration: currentIter,
					StoryID:   storyID,
					Text:      fmt.Sprintf("Story %s failed %d times, parked for human review", storyID, attempts),
				}
			}
		}
		// buildPrompt on the next iteration will return error when no actionable
		// stories remain, which causes EventComplete to be emitted above.

		// Check pause flag after iteration (loop stops after current iteration completes)
		l.mu.Lock()
		if l.paused {
			l.mu.Unlock()
			return nil
		}
		l.mu.Unlock()
	}
}

// runIterationWithRetry wraps runIteration with retry logic for crash recovery.
func (l *Loop) runIterationWithRetry(ctx context.Context) error {
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
		err := l.runIteration(ctx)
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

// runIteration spawns the agent and processes its output.
func (l *Loop) runIteration(ctx context.Context) error {
	workDir := l.effectiveWorkDir()
	cmd := l.provider.LoopCommand(ctx, l.prompt, workDir)
	setProcessGroup(cmd) // kill the whole subprocess tree, not just the direct child
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
	l.lastOutputTime = time.Now()
	watchdogTimeout := l.watchdogTimeout
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
		l.processOutput(stdout)
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
		// Covers both the build agent (sawStoryDone) and the review agent
		// (sawReviewDone).
		l.mu.Lock()
		saw := l.sawStoryDone || l.sawReviewDone
		l.mu.Unlock()
		if saw {
			return nil
		}
		// Check if the watchdog killed the process
		if watchdogFired.Load() {
			return fmt.Errorf("watchdog timeout: no output for %s", watchdogTimeout)
		}
		return fmt.Errorf("%s exited with error: %w", l.provider.Name(), err)
	}

	return nil
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
			l.mu.Lock()
			lastOutput := l.lastOutputTime
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

// processOutput reads stdout line by line, logs it, and parses events.
func (l *Loop) processOutput(r io.Reader) {
	scanner := bufio.NewScanner(r)
	// Increase buffer size for long lines (Claude can output large JSON)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		// Update last output time for watchdog
		l.mu.Lock()
		l.lastOutputTime = time.Now()
		l.mu.Unlock()

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
				reviewMode := l.reviewMode
				if reviewMode {
					l.sawReviewDone = true
				} else {
					l.sawStoryDone = true
				}
				if l.agentCmd != nil {
					killProcessGroup(l.agentCmd.Process)
				}
				l.mu.Unlock()
				// Swallow the reviewer's <chief-done/> so the TUI doesn't render a
				// second "story done" for the same story; the build agent's done
				// was already forwarded.
				if reviewMode {
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

// logLine writes a line to the log file.
func (l *Loop) logLine(line string) {
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

// Pause sets the pause flag. The loop will stop after the current iteration completes.
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
	hash, _ := git.FindCommitForStory(dir, storyID, title)
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
	// pathspec, and progress.md / .gitignore may not have been written yet.
	var paths []string
	for _, p := range []string{l.prdPath, prd.ProgressPath(l.prdPath), filepath.Join(prdDir, ".gitignore")} {
		if _, err := os.Stat(p); err == nil {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return
	}
	expected := fmt.Sprintf("feat: %s - %s", storyID, storyTitle)
	if subj, err := git.HeadSubject(dir); err == nil && subj == expected {
		_ = git.AmendPaths(dir, paths...)
		return
	}
	_ = git.CommitPaths(dir, fmt.Sprintf("chore: track %s progress", storyID), paths...)
}

// runReview spawns a separate agent that reviews (and fixes) the changes the
// build agent just committed for the story. It reuses runIteration for the
// process plumbing (watchdog, output parsing, <chief-done/> handling) via the
// reviewMode flag, but swaps in the review prompt. It is best-effort: a review
// crash is surfaced as an event but never un-completes the story, so a flaky
// reviewer can't block progress. The reviewer commits its own fixes; chief does
// not gate the story on a pass/fail signal.
func (l *Loop) runReview(ctx context.Context, iteration int, storyID, storyTitle string) {
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

	// Swap in the review prompt and enter review mode for the duration of this
	// process. buildPrompt rebuilds l.prompt on the next iteration, so restoring
	// the old prompt is unnecessary; but reviewMode/sawReviewDone must be reset.
	l.mu.Lock()
	l.prompt = prompt
	l.reviewMode = true
	l.sawReviewDone = false
	l.mu.Unlock()

	// A crashed reviewer is retried like any other iteration, but a persistent
	// failure must not stall the loop, so errors are only logged.
	runErr := l.runIterationWithRetry(ctx)

	l.mu.Lock()
	l.reviewMode = false
	l.sawReviewDone = false
	l.mu.Unlock()

	text := fmt.Sprintf("Review complete for %s", storyID)
	if runErr != nil && ctx.Err() == nil {
		text = fmt.Sprintf("Review of %s did not finish cleanly: %v", storyID, runErr)
	}
	l.events <- Event{
		Type:      EventReviewDone,
		Iteration: iteration,
		StoryID:   storyID,
		Text:      text,
	}
}

// buildReviewPrompt loads the PRD and builds the review-agent prompt for the
// story currently being reviewed, inlining that story's context.
func (l *Loop) buildReviewPrompt() (string, error) {
	l.mu.Lock()
	storyID := l.currentStoryID
	storyTitle := l.currentStoryTitle
	skill := l.reviewSkill
	instructions := l.reviewInstructions
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
