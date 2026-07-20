// Package notify sends best-effort desktop notifications. It is used to ping
// the user when a long-running loop finishes and they've walked away.
package notify

import (
	"fmt"
	"os/exec"
	"runtime"
)

// Send fires a best-effort desktop notification. Any failure (missing notifier,
// no display server) is ignored: a notification must never break the loop.
func Send(title, body string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		// osascript takes AppleScript string literals; %q yields a valid one.
		script := fmt.Sprintf("display notification %q with title %q sound name \"Glass\"", body, title)
		cmd = exec.Command("osascript", "-e", script)
	case "linux":
		cmd = exec.Command("notify-send", title, body)
	default:
		return
	}
	_ = cmd.Run()
}
