package awake

import (
	"os/exec"
	"runtime"
	"testing"
)

// testGuard returns a Guard whose helper is a long-lived stand-in process, so
// the reference counting can be exercised without depending on caffeinate (and
// without actually pinning the developer's machine awake). Each started command
// is recorded so the test can assert on how many were spawned.
func testGuard(t *testing.T) (*Guard, *[]*exec.Cmd) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stand-in helper process is POSIX-only")
	}

	var started []*exec.Cmd
	g := &Guard{newCmd: func() *exec.Cmd {
		cmd := exec.Command("sleep", "60")
		started = append(started, cmd)
		return cmd
	}}
	t.Cleanup(func() {
		for _, cmd := range started {
			if cmd.ProcessState == nil && cmd.Process != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		}
	})
	return g, &started
}

func TestGuardAcquireStartsHelperOnce(t *testing.T) {
	g, started := testGuard(t)

	g.Acquire()
	if !g.Held() {
		t.Fatal("guard should be held after Acquire")
	}
	g.Acquire()

	if len(*started) != 1 {
		t.Fatalf("want 1 helper process for 2 Acquires, got %d", len(*started))
	}
}

func TestGuardReleasesOnlyAfterLastHolder(t *testing.T) {
	g, started := testGuard(t)

	g.Acquire()
	g.Acquire()
	helper := (*started)[0]

	g.Release()
	if !g.Held() {
		t.Fatal("guard should still be held while a second holder remains")
	}
	if helper.ProcessState != nil {
		t.Fatal("helper should still be running while a second holder remains")
	}

	g.Release()
	if g.Held() {
		t.Fatal("guard should be released once the last holder is gone")
	}
	// A non-nil ProcessState means Release waited on the killed helper, so it is
	// reaped rather than left as a zombie.
	if helper.ProcessState == nil {
		t.Fatal("helper should have been killed and reaped by Release")
	}
}

func TestGuardReacquireAfterRelease(t *testing.T) {
	g, started := testGuard(t)

	g.Acquire()
	g.Release()
	g.Acquire()

	if !g.Held() {
		t.Fatal("guard should be held again after re-acquiring")
	}
	if len(*started) != 2 {
		t.Fatalf("want a fresh helper for the second Acquire, got %d total", len(*started))
	}
}

func TestGuardUnbalancedReleaseIsNoOp(t *testing.T) {
	g, started := testGuard(t)

	g.Release() // never acquired
	g.Acquire()
	g.Release()
	g.Release() // one too many

	if g.Held() {
		t.Fatal("guard should not be held after extra Releases")
	}
	if len(*started) != 1 {
		t.Fatalf("extra Releases should not spawn helpers, got %d", len(*started))
	}
}

func TestNewGuardIsNotHeld(t *testing.T) {
	if NewGuard().Held() {
		t.Fatal("a fresh guard should not be holding the machine awake")
	}
}
