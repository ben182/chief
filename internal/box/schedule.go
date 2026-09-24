package box

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// startTimer is the transient timer a run waits behind when it was asked to
// start later. Transient rather than written into the cloud-config, because the
// time is known only when `up` or `retry` is run, and a retry has to be able to
// move it.
const startTimer = "chief-start"

// NextAt is the next time the clock reads hh:mm in now's zone: later today, or
// tomorrow when that has already passed. It is how "--at 23:00" is meant — the
// night ahead, not a date to type.
func NextAt(now time.Time, clock string) (time.Time, error) {
	h, m, ok := parseClock(clock)
	if !ok {
		return time.Time{}, fmt.Errorf("--at needs a time of day like 23:00, got %q", clock)
	}
	y, mo, d := now.Date()
	at := time.Date(y, mo, d, h, m, 0, 0, now.Location())
	if !at.After(now) {
		at = time.Date(y, mo, d+1, h, m, 0, 0, now.Location())
	}
	return at, nil
}

func parseClock(s string) (hour, minute int, ok bool) {
	hs, ms, found := strings.Cut(strings.TrimSpace(s), ":")
	if !found || len(ms) != 2 || hs == "" || len(hs) > 2 {
		return 0, 0, false
	}
	h, err1 := strconv.Atoi(hs)
	m, err2 := strconv.Atoi(ms)
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

// calendarUTC is t as a systemd calendar spec. The box's clock runs on UTC, and
// saying so in the spec means nothing depends on that staying true.
func calendarUTC(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05") + " UTC"
}

// cancelStartScript takes back a start an earlier `up` or `retry` scheduled, so
// a retry never leaves two starts armed — one of them for a run that is already
// over by the time it fires. Run as root; a box with nothing scheduled answers
// the same.
func cancelStartScript() string {
	return "systemctl stop " + startTimer + ".timer 2>/dev/null; " +
		"systemctl reset-failed " + startTimer + ".timer " + startTimer + ".service 2>/dev/null; true"
}

// scheduleStartScript arms a timer that starts the run at t. It is the same
// `systemctl start` the immediate path runs, only issued by the box itself, so
// nothing on this machine has to be awake for it.
func scheduleStartScript(prdName string, t time.Time) string {
	return "systemd-run --quiet --unit=" + startTimer +
		" --description=" + shellQuote("Start the chief run at "+calendarUTC(t)) +
		" --on-calendar=" + shellQuote(calendarUTC(t)) +
		" --timer-property=AccuracySec=1s" +
		" systemctl start --no-block chief-run@" + shellQuote(prdName)
}

// deadlineDropIn moves the box's outside limit from "hours after boot" to
// "hours after the run starts". Without it a box created at seven for a run at
// eleven would lose four of its hours to waiting.
//
// Assigning OnBootSec= empty resets every trigger the unit had, the retry
// interval included, so that is put back alongside the new deadline.
func deadlineDropIn(deadline time.Time) string {
	return fmt.Sprintf("[Timer]\nOnBootSec=\nOnCalendar=%s\nOnUnitActiveSec=%s\n", calendarUTC(deadline), reapRetry)
}

// moveDeadlineScript installs deadlineDropIn and restarts the timer so it takes
// effect. Run as root, with the drop-in on stdin.
const moveDeadlineScript = "install -d -m 0755 /etc/systemd/system/chief-deadline.timer.d && " +
	"cat > /etc/systemd/system/chief-deadline.timer.d/start-at.conf && " +
	"systemctl daemon-reload && systemctl restart chief-deadline.timer"
