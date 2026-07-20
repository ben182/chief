//go:build !windows

package loop

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcessGroup makes the agent the leader of its own process group so the
// whole tree (the agent CLI plus every tool/MCP subprocess it spawns) can be
// killed together. Without this, killing only the direct child orphans its
// grandchildren, which pile up across iterations.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessGroup kills the agent and its entire process group. A negative pid
// targets the group (see kill(2)). Falls back to killing just the process if
// the group signal fails (e.g. the child never got its own group).
func killProcessGroup(p *os.Process) {
	if p == nil {
		return
	}
	if err := syscall.Kill(-p.Pid, syscall.SIGKILL); err != nil {
		_ = p.Kill()
	}
}
