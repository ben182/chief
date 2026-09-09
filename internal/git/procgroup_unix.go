//go:build !windows

package git

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcessGroup makes a worktree command the leader of its own process group
// so the whole tree can be killed together. A setup script is a shell that
// spawns installers; killing only the shell would leave them running, and a
// timeout that leaves the hung process behind has not achieved anything.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessGroup kills the command and its entire process group. A negative
// pid targets the group (see kill(2)). Falls back to killing just the process
// if the group signal fails (e.g. the child never got its own group).
func killProcessGroup(p *os.Process) {
	if p == nil {
		return
	}
	if err := syscall.Kill(-p.Pid, syscall.SIGKILL); err != nil {
		_ = p.Kill()
	}
}
