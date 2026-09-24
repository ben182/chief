package box

import (
	"strings"
	"testing"
	"time"
)

func TestNextAtIsTheNightAhead(t *testing.T) {
	evening := time.Date(2026, 9, 24, 19, 30, 0, 0, berlin)
	cases := []struct {
		clock string
		want  time.Time
	}{
		{"23:00", time.Date(2026, 9, 24, 23, 0, 0, 0, berlin)},
		{"2:15", time.Date(2026, 9, 25, 2, 15, 0, 0, berlin)},
		// The moment itself is not "later".
		{"19:30", time.Date(2026, 9, 25, 19, 30, 0, 0, berlin)},
		{"00:00", time.Date(2026, 9, 25, 0, 0, 0, 0, berlin)},
	}
	for _, c := range cases {
		got, err := NextAt(evening, c.clock)
		if err != nil || !got.Equal(c.want) {
			t.Errorf("NextAt(%q) = %v, %v; want %v", c.clock, got, err, c.want)
		}
	}
}

func TestNextAtRefusesWhatIsNotATimeOfDay(t *testing.T) {
	for _, bad := range []string{"", "23", "23:0", "24:00", "12:60", "-1:00", "tonight", "123:00", "23:00:00"} {
		if _, err := NextAt(time.Now(), bad); err == nil {
			t.Errorf("NextAt(%q) was accepted", bad)
		}
	}
}

func TestScheduledStartIsInUTC(t *testing.T) {
	at := time.Date(2026, 9, 24, 23, 0, 0, 0, berlin)
	script := scheduleStartScript("auth", at)
	for _, want := range []string{
		"systemd-run", "--unit=chief-start",
		"--on-calendar='2026-09-24 21:00:00 UTC'",
		"systemctl start --no-block chief-run@'auth'",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("schedule script lacks %q:\n%s", want, script)
		}
	}
}

func TestDeadlineDropInKeepsTheRetry(t *testing.T) {
	d := deadlineDropIn(time.Date(2026, 9, 25, 11, 0, 0, 0, berlin))
	for _, want := range []string{"OnBootSec=\n", "OnCalendar=2026-09-25 09:00:00 UTC\n", "OnUnitActiveSec=" + reapRetry + "\n"} {
		if !strings.Contains(d, want) {
			t.Errorf("drop-in lacks %q:\n%s", want, d)
		}
	}
}

func TestStatusBeforeAScheduledStart(t *testing.T) {
	b := demoBox()
	b.StartAt = time.Date(2026, 9, 24, 21, 0, 0, 0, time.UTC)
	out := render(statusView{
		Box: b, Now: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC).In(berlin),
		Unit: "inactive\nsuccess", Tail: "from a run before",
	})
	for _, want := range []string{"starts at 23:00", "waiting — the box starts the run at 23:00, in 9h"} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "inactive") || strings.Contains(out, "from a run before") {
		t.Errorf("a waiting run is shown as an ended one:\n%s", out)
	}

	// Once the time has passed, an inactive unit is an ended run again.
	out = render(statusView{Box: b, Now: b.StartAt.Add(time.Hour), Unit: "inactive\nsuccess"})
	if strings.Contains(out, "waiting") || !strings.Contains(out, "inactive (success)") {
		t.Errorf("after the start time:\n%s", out)
	}
}
