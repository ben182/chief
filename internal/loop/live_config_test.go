package loop

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ben182/chief/internal/config"
)

// liveConfigSource stands in for the manager's config: a pointer that is
// replaced wholesale on every change, never written through, which is what the
// loop is allowed to assume.
type liveConfigSource struct {
	mu  sync.RWMutex
	cfg *config.Config
}

func (s *liveConfigSource) get() *config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *liveConfigSource) set(cfg *config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
}

// A run outlives the decisions made when it started. Changing the review
// settings has to reach the story after next, not the run after this one.
func TestLoop_ReviewSettingsFollowTheLiveConfig(t *testing.T) {
	src := &liveConfigSource{cfg: &config.Config{
		Review: config.ReviewConfig{Enabled: config.Bool(true), Model: "haiku", Skill: "/first"},
	}}

	l := NewLoop("/tmp/prd.md", "", 5, &mockProvider{})
	l.SetConfigFn(src.get)

	if !l.reviewEnabled() {
		t.Fatal("expected the review to be on")
	}
	if got := l.modelForMode(modeReview); got != "haiku" {
		t.Errorf("review model = %q, want haiku", got)
	}
	if got := l.currentReviewer().skill; got != "/first" {
		t.Errorf("review skill = %q, want /first", got)
	}

	// Mid-run: drop to a different model and a different skill.
	src.set(&config.Config{
		Review: config.ReviewConfig{Enabled: config.Bool(true), Model: "opus", Skill: "/second"},
	})

	if got := l.modelForMode(modeReview); got != "opus" {
		t.Errorf("review model = %q, want opus after the config changed", got)
	}
	if got := l.currentReviewer().skill; got != "/second" {
		t.Errorf("review skill = %q, want /second after the config changed", got)
	}

	// And switching it off entirely stops the next story being reviewed.
	src.set(&config.Config{Review: config.ReviewConfig{Enabled: config.Bool(false)}})
	if l.reviewEnabled() {
		t.Error("expected the review to be off after the config changed")
	}
}

// An empty model still means "the phase default", not "the build model", when it
// comes from the live config.
func TestLoop_LiveReviewModelFallsBackToPhaseDefault(t *testing.T) {
	src := &liveConfigSource{cfg: &config.Config{
		Review:      config.ReviewConfig{Enabled: config.Bool(true)},
		Consolidate: config.ConsolidateConfig{Enabled: config.Bool(true)},
	}}

	l := NewLoop("/tmp/prd.md", "", 5, &mockProvider{})
	l.SetConfigFn(src.get)

	if got := l.modelForMode(modeReview); got != defaultPhaseModel {
		t.Errorf("review model = %q, want %q", got, defaultPhaseModel)
	}
	if got := l.modelForMode(modeConsolidate); got != defaultPhaseModel {
		t.Errorf("consolidate model = %q, want %q", got, defaultPhaseModel)
	}
	if got := l.modelForMode(modeBuild); got != "" {
		t.Errorf("the build agent must keep the provider's model, got %q", got)
	}
}

func TestLoop_ConsolidateSettingsFollowTheLiveConfig(t *testing.T) {
	src := &liveConfigSource{cfg: &config.Config{}}

	l := NewLoop("/tmp/prd.md", "", 5, &mockProvider{})
	l.SetConfigFn(src.get)

	if l.consolidateEnabled() {
		t.Fatal("expected the consolidation pass to be off")
	}

	// A skill alone turns the pass on, exactly as it does at startup — the live
	// path has to go through ConsolidateConfig.Active(), not just read `enabled`.
	src.set(&config.Config{Consolidate: config.ConsolidateConfig{Skill: "/code-quality"}})
	if !l.consolidateEnabled() {
		t.Error("expected a configured skill to enable the pass")
	}
	if got := l.currentConsolidator().skill; got != "/code-quality" {
		t.Errorf("consolidate skill = %q, want /code-quality", got)
	}

	// An explicit `enabled: false` still wins over the skill.
	src.set(&config.Config{Consolidate: config.ConsolidateConfig{
		Enabled: config.Bool(false), Skill: "/code-quality",
	}})
	if l.consolidateEnabled() {
		t.Error("expected an explicit enabled:false to win over the skill")
	}
}

// Without a live source the loop keeps using what it was configured with, so a
// Loop driven directly — every test here, and any caller without a manager —
// behaves exactly as before.
func TestLoop_FallsBackToCapturedSettings(t *testing.T) {
	l := NewLoop("/tmp/prd.md", "", 5, &mockProvider{})
	l.SetReview(true, "/captured", "look here")
	l.SetReviewModel("haiku")
	l.SetConsolidate(true, "", "")
	l.SetWatchdogTimeout(90 * time.Second)

	if !l.reviewEnabled() || !l.consolidateEnabled() {
		t.Fatal("expected the captured settings to be in force")
	}
	if got := l.modelForMode(modeReview); got != "haiku" {
		t.Errorf("review model = %q, want haiku", got)
	}
	if got := l.currentReviewer().skill; got != "/captured" {
		t.Errorf("review skill = %q, want /captured", got)
	}
	if got := l.currentWatchdogTimeout(); got != 90*time.Second {
		t.Errorf("watchdog = %v, want 90s", got)
	}

	// A live source that has nothing to give falls back the same way.
	l.SetConfigFn(func() *config.Config { return nil })
	if !l.reviewEnabled() {
		t.Error("a nil live config must fall back to the captured settings")
	}
	if got := l.currentWatchdogTimeout(); got != 90*time.Second {
		t.Errorf("watchdog = %v, want 90s from the captured value", got)
	}
}

func TestLoop_WatchdogTimeoutFollowsTheLiveConfig(t *testing.T) {
	src := &liveConfigSource{cfg: &config.Config{}}

	l := NewLoop("/tmp/prd.md", "", 5, &mockProvider{})
	l.SetWatchdogTimeout(90 * time.Second)
	l.SetConfigFn(src.get)

	// 0 in the config means "not set", which is the built-in default — not a
	// disabled watchdog, which is what SetWatchdogTimeout(0) means.
	if got := l.currentWatchdogTimeout(); got != 90*time.Second {
		t.Errorf("watchdog = %v, want the captured 90s while the config is unset", got)
	}

	src.set(&config.Config{Loop: config.LoopConfig{WatchdogTimeoutSeconds: 900}})
	if got := l.currentWatchdogTimeout(); got != 900*time.Second {
		t.Errorf("watchdog = %v, want 900s", got)
	}

	src.set(&config.Config{Loop: config.LoopConfig{WatchdogTimeoutSeconds: 0}})
	if got := l.currentWatchdogTimeout(); got != 90*time.Second {
		t.Errorf("watchdog = %v, want the captured 90s again", got)
	}
}

// The loop reads the config from its own goroutines while the UI replaces it.
// Under -race this is the test that would catch a reader holding onto fields of
// a config someone else is editing in place.
func TestLoop_LiveConfigIsSafeUnderConcurrentReplacement(t *testing.T) {
	src := &liveConfigSource{cfg: &config.Config{}}

	l := NewLoop("/tmp/prd.md", "", 5, &mockProvider{})
	l.SetConfigFn(src.get)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			src.set(&config.Config{
				Review:      config.ReviewConfig{Enabled: config.Bool(i%2 == 0), Model: "haiku"},
				Consolidate: config.ConsolidateConfig{Skill: "/code-quality"},
				Loop:        config.LoopConfig{WatchdogTimeoutSeconds: i%600 + 1},
			})
		}
	}()

	for i := 0; i < 2000; i++ {
		_ = l.reviewEnabled()
		_ = l.consolidateEnabled()
		_ = l.modelForMode(modeReview)
		_ = l.currentWatchdogTimeout()
		_ = l.currentReviewer().skill
	}

	close(stop)
	wg.Wait()
}

// countingGuard records the reference-counted acquires and releases a run makes,
// without spawning the real sleep helper.
type countingGuard struct {
	mu       sync.Mutex
	refs     int
	acquires int
	releases int
}

func (g *countingGuard) Acquire() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.refs++
	g.acquires++
}

func (g *countingGuard) Release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.refs--
	g.releases++
}

func (g *countingGuard) held() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.refs > 0
}

func (g *countingGuard) counts() (int, int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.acquires, g.releases
}

// waitFor polls cond until it holds or the deadline passes, so the ticker-driven
// reconcile doesn't need a fixed sleep to be observed.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestManagerHoldAwake_FollowsTheSettingMidRun(t *testing.T) {
	// Re-check often enough that the test doesn't have to wait seconds for it.
	restore := awakeRecheckInterval
	awakeRecheckInterval = time.Millisecond
	defer func() { awakeRecheckInterval = restore }()

	guard := &countingGuard{}
	m := NewManager(10, &mockProvider{})
	m.awake = guard
	m.SetConfig(&config.Config{Loop: config.LoopConfig{KeepAwake: true}})

	release := m.holdAwakeWhileEnabled(context.Background())

	if !guard.held() {
		t.Fatal("expected the machine to be held awake at the start")
	}

	// Switched off during the run: the assertion has to be given back now, not
	// at the end of the run.
	m.SetConfig(&config.Config{Loop: config.LoopConfig{KeepAwake: false}})
	waitFor(t, "the assertion to be released", func() bool { return !guard.held() })

	// And switched back on: this is the one people actually reach for mid-run.
	m.SetConfig(&config.Config{Loop: config.LoopConfig{KeepAwake: true}})
	waitFor(t, "the assertion to be re-taken", func() bool { return guard.held() })

	release()

	if guard.held() {
		t.Error("expected the assertion to be released when the run ends")
	}
	acquires, releases := guard.counts()
	if acquires != releases {
		t.Errorf("unbalanced reference counting: %d acquires, %d releases", acquires, releases)
	}
}

// Starting with the setting off must not take an assertion at all, and stopping
// must not release one that was never taken — the guard is shared with every
// other running PRD.
func TestManagerHoldAwake_DisabledTakesNothing(t *testing.T) {
	guard := &countingGuard{}
	m := NewManager(10, &mockProvider{})
	m.awake = guard
	m.SetConfig(&config.Config{Loop: config.LoopConfig{KeepAwake: false}})

	release := m.holdAwakeWhileEnabled(context.Background())
	release()

	acquires, releases := guard.counts()
	if acquires != 0 || releases != 0 {
		t.Errorf("expected the guard to be untouched, got %d acquires and %d releases", acquires, releases)
	}
}

// A cancelled run stops the ticker and still hands the assertion back.
func TestManagerHoldAwake_ReleasesAfterContextCancel(t *testing.T) {
	guard := &countingGuard{}
	m := NewManager(10, &mockProvider{})
	m.awake = guard
	m.SetConfig(&config.Config{Loop: config.LoopConfig{KeepAwake: true}})

	ctx, cancel := context.WithCancel(context.Background())
	release := m.holdAwakeWhileEnabled(ctx)
	cancel()
	release()

	if guard.held() {
		t.Error("expected the assertion to be released")
	}
}

// A loop started by the manager reads its review settings from the manager, so
// an edit made while it runs reaches the next story rather than the next run.
func TestManagerStart_LoopFollowsConfigChanges(t *testing.T) {
	tmpDir := t.TempDir()
	prdPath := createTestPRDWithName(t, tmpDir, "test-prd")

	m := NewManager(10, &mockProvider{cliPath: "/usr/bin/true"})
	m.SetConfig(&config.Config{
		Review: config.ReviewConfig{Enabled: config.Bool(true), Model: "haiku"},
	})
	if err := m.Register("test-prd", prdPath); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if err := m.Start("test-prd"); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	// StopAll rather than Stop: it waits for the loop goroutine to finish, so the
	// run's log file is closed before t.TempDir tries to remove the directory.
	t.Cleanup(m.StopAll)

	instance, err := m.lookup("test-prd")
	if err != nil {
		t.Fatalf("lookup failed: %v", err)
	}
	instance.mu.Lock()
	l := instance.Loop
	instance.mu.Unlock()

	if got := l.modelForMode(modeReview); got != "haiku" {
		t.Fatalf("review model = %q, want haiku", got)
	}

	// The edit the user makes while watching the run.
	m.SetConfig(&config.Config{
		Review: config.ReviewConfig{Enabled: config.Bool(false)},
		Loop:   config.LoopConfig{WatchdogTimeoutSeconds: 1200},
	})

	if l.reviewEnabled() {
		t.Error("expected the running loop to see the review switched off")
	}
	if got := l.currentWatchdogTimeout(); got != 1200*time.Second {
		t.Errorf("watchdog = %v, want 1200s from the edited config", got)
	}
}
