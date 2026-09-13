package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/loop"
)

// notifyingApp builds an App whose desktop pings land in a slice instead of on
// the desktop. handleLoopEvent works on a copy of the App, so the notifier
// closes over a pointer — that is what survives the copy.
func notifyingApp(t *testing.T, prdName string) (*App, *[]string) {
	t.Helper()
	a := newTestApp(nil, 100, 30)
	a.prdName = prdName
	a.state = StateRunning
	a.logViewer = NewLogViewer()
	a.logViewer.SetSize(100, 20)

	// A completing run walks into the completion screen, which asks the manager
	// about the PRD it is reporting on.
	a.baseDir = "/proj"
	a.completionScreen = NewCompletionScreen()
	m := loop.NewManager(10, nil)
	for _, name := range []string{prdName, "billing"} {
		if err := m.Register(name, "/proj/.chief/prds/"+name+"/prd.md"); err != nil {
			t.Fatalf("register %q: %v", name, err)
		}
	}
	a.manager = m

	sent := &[]string{}
	a.notifier = func(title, body string) {
		*sent = append(*sent, title+": "+body)
	}
	return a, sent
}

func TestFatalErrorPingsTheDesktop(t *testing.T) {
	a, sent := notifyingApp(t, "auth")

	a.handleLoopEvent("auth", loop.Event{
		Type:  loop.EventError,
		Err:   errors.New("max retries (3) exceeded: agent exited with status 1"),
		Fatal: true,
	})

	if len(*sent) != 1 {
		t.Fatalf("want one notification for a dead run, got %v", *sent)
	}
	got := (*sent)[0]
	if !strings.Contains(got, "Auth") || !strings.Contains(got, "max retries (3) exceeded") {
		t.Errorf("notification should name the PRD and why it died, got %q", got)
	}
}

func TestNonFatalErrorStaysQuiet(t *testing.T) {
	a, sent := notifyingApp(t, "auth")

	// A parser reporting a bad line, which the loop carries on from. Pinging the
	// user for one of these would make the ping worth ignoring.
	a.handleLoopEvent("auth", loop.Event{
		Type: loop.EventError,
		Err:  errors.New("could not parse line"),
	})

	if len(*sent) != 0 {
		t.Errorf("want no notification for an error the run survived, got %v", *sent)
	}
}

func TestFatalErrorInABackgroundPRDStillPings(t *testing.T) {
	a, sent := notifyingApp(t, "auth")

	// "billing" is not the PRD on screen, so nothing about its death is visible
	// anywhere — which makes the ping the only way to hear about it.
	a.handleLoopEvent("billing", loop.Event{
		Type:  loop.EventError,
		Err:   errors.New("agent crashed"),
		Fatal: true,
	})

	if len(*sent) != 1 || !strings.Contains((*sent)[0], "Billing") {
		t.Fatalf("want one notification naming the background PRD, got %v", *sent)
	}
}

func TestCappedRunPingsTheDesktop(t *testing.T) {
	a, sent := notifyingApp(t, "auth")

	a.handleLoopEvent("auth", loop.Event{Type: loop.EventMaxIterationsReached})

	if len(*sent) != 1 || !strings.Contains((*sent)[0], "max iterations") {
		t.Fatalf("want one notification for a capped run, got %v", *sent)
	}
}

func TestCompletionStillPingsWithCost(t *testing.T) {
	a, sent := notifyingApp(t, "auth")
	a.totalCost = 718.79

	a.handleLoopEvent("auth", loop.Event{Type: loop.EventComplete})

	if len(*sent) != 1 {
		t.Fatalf("want one notification on completion, got %v", *sent)
	}
	got := (*sent)[0]
	if !strings.Contains(got, "all stories complete") || !strings.Contains(got, "718") {
		t.Errorf("completion notification should carry the run's cost, got %q", got)
	}
}

func TestNotificationsHonourTheConfigSwitch(t *testing.T) {
	a, sent := notifyingApp(t, "auth")
	cfg := config.Default()
	cfg.OnComplete.Notify = false
	a.config = cfg

	a.handleLoopEvent("auth", loop.Event{Type: loop.EventError, Err: errors.New("boom"), Fatal: true})
	a.handleLoopEvent("auth", loop.Event{Type: loop.EventMaxIterationsReached})
	a.handleLoopEvent("auth", loop.Event{Type: loop.EventComplete})

	if len(*sent) != 0 {
		t.Errorf("onComplete.notify=false should silence every run-stopped ping, got %v", *sent)
	}
}

func TestNotifyReasonTrimsToOneShortLine(t *testing.T) {
	long := strings.Repeat("a very long wrapped error ", 20)
	got := notifyReason(loop.Event{Err: errors.New(long + "\nsecond line")})

	if strings.Contains(got, "\n") || strings.Contains(got, "second line") {
		t.Errorf("want only the first line, got %q", got)
	}
	if len([]rune(got)) > notifyReasonLimit+1 {
		t.Errorf("want at most %d runes plus an ellipsis, got %d: %q", notifyReasonLimit, len([]rune(got)), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a trimmed reason should say it was trimmed, got %q", got)
	}
}

func TestNotifyReasonFallsBackToTextThenToUnknown(t *testing.T) {
	// The loop sets both Err and Text on a fatal error; a provider's parser may
	// only manage one of them.
	if got := notifyReason(loop.Event{Text: "agent exited"}); got != "agent exited" {
		t.Errorf("want the event text when there is no error, got %q", got)
	}
	if got := notifyReason(loop.Event{}); got != "unknown error" {
		t.Errorf("want a stand-in for an error that says nothing, got %q", got)
	}
}
