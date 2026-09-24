package box

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ben182/chief/internal/prd"
)

var berlin = time.FixedZone("CEST", 2*60*60)

// journalAt builds what storyEventsProbe prints: the box's clock, then the
// journal lines, each at a number of minutes after 12:00 UTC.
func journalAt(nowMin int, lines ...string) string {
	base := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC).Unix()
	out := []string{fmt.Sprint(base + int64(nowMin)*60)}
	for _, l := range lines {
		var min int
		var rest string
		_, _ = fmt.Sscanf(l, "%d", &min)
		rest = l[strings.Index(l, " ")+1:]
		out = append(out, fmt.Sprintf("%d.5 chief[1]: 2026-09-24 00:00:00  %s", base+int64(min)*60, rest))
	}
	return strings.Join(out, "\n")
}

func demoPRD() *prd.PRD {
	return &prd.PRD{UserStories: []prd.UserStory{
		{ID: "US-1", Passes: true},
		{ID: "US-2", NeedsReview: true},
		{ID: "US-3", InProgress: true, Title: "Login"},
		{ID: "US-4"},
		{ID: "US-5"},
		{ID: "US-6"},
	}}
}

func demoBox() State {
	return State{
		Name: "chief-demo", IP: "1.2.3.4", PRD: "default", Type: "cpx12", Location: "fsn1",
		Created: time.Date(2026, 9, 24, 11, 50, 0, 0, time.UTC), HourlyEUR: 0.0184,
	}
}

func render(v statusView) string {
	var buf bytes.Buffer
	renderStatus(&buf, v)
	return buf.String()
}

func TestStatusWhileRunning(t *testing.T) {
	// Setup until 12:00; US-1 takes 20m, US-2 is parked after 20m more, US-3
	// has been going for 5m.
	log := parseRunLog(journalAt(45,
		"0 story      US-1 started (iteration 1)",
		"20 story      US-1 done",
		"20 cost       $1.20 so far",
		"20 story      US-2 started (iteration 2)",
		"40 story      US-2 parked for human review after too many failed attempts",
		"40 cost       $2.50 so far",
		"40 story      US-3 started (iteration 3)",
	))
	got := render(statusView{
		Box: demoBox(), Running: true, Unit: "activating\nsuccess", PRD: demoPRD(), Log: log,
		Now: time.Date(2026, 9, 24, 12, 45, 0, 0, time.UTC).In(berlin),
	})
	// 20m per story × 4 left − 5m on the current one = 1h15m, from 14:45.
	want := `chief-demo
  running · 55m · cpx12 in fsn1 · 1.2.3.4 · PRD default

  Progress   █████░░░░░░░░░░░░░░░░░░░░░░░░░  1/6 stories · 16% · 1 parked for review
  Now        US-3  Login
             for 5m, since 14:40
  Left       about 1h15m — done around 16:00
             ≈20m per story, 4 stories to go
  Cost       machine 2 cents · agent $2.50

  Recent     US-2        20m   parked 14:40
             US-1        20m   done 14:20
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestStatusBeforeTheFirstStory(t *testing.T) {
	got := render(statusView{
		Box: demoBox(), Running: true, PRD: demoPRD(), Log: parseRunLog(journalAt(5)),
		Now: time.Date(2026, 9, 24, 12, 5, 0, 0, time.UTC),
	})
	for _, want := range []string{"Now        setting up — no story started yet", "Left       no estimate until the first story is done"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestStatusOfAnEndedRun(t *testing.T) {
	got := render(statusView{
		Box: demoBox(), Running: false, Unit: "failed\nexit-code", PRD: demoPRD(),
		Log:  parseRunLog(journalAt(90, "0 story      US-1 started (iteration 1)", "20 story      US-1 done", "90 run        default after 1h30m · 1/6 stories, $4.56")),
		Tail: "2026-09-24 13:30:00  run        max iterations reached with stories left\n",
		Now:  time.Date(2026, 9, 24, 13, 30, 0, 0, time.UTC),
	})
	for _, want := range []string{
		"  failed (exit-code) · ",
		"Cost       machine 3 cents · agent $4.56",
		"  Log\n    2026-09-24 13:30:00  run        max iterations reached",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// An ended run has no current story and nothing left to estimate.
	for _, unwanted := range []string{"Now ", "Left "} {
		if strings.Contains(got, unwanted) {
			t.Errorf("%q in an ended run:\n%s", unwanted, got)
		}
	}
}

func TestEstimateIsDoneAnyMinuteWhenOverdue(t *testing.T) {
	log := parseRunLog(journalAt(300, "0 story      US-1 started (iteration 1)", "20 story      US-1 done"))
	remaining, _, ok := log.estimate(1)
	if !ok || remaining != 0 {
		t.Errorf("got %v, %v", remaining, ok)
	}
	if _, _, ok := log.estimate(0); ok {
		t.Error("nothing left means no estimate")
	}
}

// A story that runs over several iterations is only announced once, and a
// story that picks up where the previous one ended is timed from that end.
func TestFinishedStoryTimes(t *testing.T) {
	log := parseRunLog(journalAt(60,
		"0 story      US-1 started (iteration 1)",
		"25 story      US-1 done",
		"40 story      US-2 done",
	))
	got := log.finished()
	if len(got) != 2 || got[0].Took != 25*time.Minute || got[1].Took != 15*time.Minute {
		t.Errorf("got %+v", got)
	}
}

func TestClockAt(t *testing.T) {
	now := time.Date(2026, 9, 24, 19, 0, 0, 0, berlin)
	for d, want := range map[time.Duration]string{
		2 * time.Hour:  "21:00",
		10 * time.Hour: "05:00 tomorrow",
		40 * time.Hour: "Sat 26 Sep 11:00",
	} {
		if got := clockAt(now, now.Add(d)); got != want {
			t.Errorf("clockAt(+%v) = %q, want %q", d, got, want)
		}
	}
}
