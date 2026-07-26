// Package awake keeps the machine from falling asleep while a run is in flight.
// A Ralph loop is a walk-away workflow — you start it and leave — but an
// untouched keyboard counts as idle, so the OS suspends the machine mid-story
// and the agent sits frozen until someone wiggles the mouse.
package awake

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
)

// Guard holds a sleep-prevention assertion for as long as at least one caller
// needs it. Acquire/Release are reference counted, so N loops running in
// parallel share a single helper process and the machine is only let go once the
// last of them has finished.
//
// Everything here is best-effort: staying awake is a convenience, so a missing
// or failing helper is swallowed rather than surfaced — it must never break a run.
type Guard struct {
	mu   sync.Mutex
	refs int
	cmd  *exec.Cmd
	// newCmd builds the helper process. A field rather than a direct call to
	// inhibitCmd so tests can drive the reference counting with a stand-in
	// process on any platform.
	newCmd func() *exec.Cmd
}

// NewGuard returns a Guard that is not yet holding the machine awake.
func NewGuard() *Guard {
	return &Guard{newCmd: inhibitCmd}
}

// Acquire asks the OS to stay awake, starting the helper on the first call.
func (g *Guard) Acquire() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.refs++
	if g.refs > 1 || g.cmd != nil || g.newCmd == nil {
		return
	}

	cmd := g.newCmd()
	if cmd == nil {
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}
	g.cmd = cmd
}

// Release drops one reference and lets the machine sleep again once the last
// holder is gone. Releasing an unheld Guard is a no-op.
func (g *Guard) Release() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.refs == 0 {
		return
	}
	g.refs--
	if g.refs > 0 || g.cmd == nil {
		return
	}

	// Kill can fail because the helper already exited on its own; Wait has to run
	// either way, or the finished helper stays a zombie for as long as chief is up.
	_ = g.cmd.Process.Kill()
	_ = g.cmd.Wait()
	g.cmd = nil
}

// Held reports whether a helper process is currently keeping the machine awake.
func (g *Guard) Held() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.cmd != nil
}

// inhibitCmd builds the platform's sleep-inhibiting helper, or nil on platforms
// where we don't know how to ask.
func inhibitCmd() *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		// -i blocks the idle sleep that catches an unattended machine, -s the
		// on-AC system sleep (macOS ignores -s on battery). -w ties the helper's
		// lifetime to ours, so a chief that is SIGKILLed — past any chance to
		// Release — can't leave the machine pinned awake for good.
		return exec.Command("caffeinate", "-i", "-s", "-w", strconv.Itoa(os.Getpid()))
	default:
		return nil
	}
}
