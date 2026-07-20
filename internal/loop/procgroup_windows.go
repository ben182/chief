//go:build windows

package loop

import (
	"os"
	"os/exec"
)

// setProcessGroup is a no-op on Windows: there is no Setpgid. ponytail: Windows
// leaves grandchild processes unkilled; wire up a Job Object if that ever bites.
func setProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup kills just the process on Windows.
func killProcessGroup(p *os.Process) {
	if p != nil {
		_ = p.Kill()
	}
}
