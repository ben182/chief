package tui

import (
	"fmt"
	"strings"

	"github.com/ben182/chief/internal/loop"
	"github.com/ben182/chief/internal/notify"
)

// desktopNotifier sends a desktop notification. notify.Send is the real one; a
// test substitutes its own to read what a run said as it came to a halt.
type desktopNotifier func(title, body string)

// notifyRunStopped pings the user's desktop about a run that has come to a halt,
// whatever the reason.
//
// The point of the ping was never to celebrate a finish. It answers "is it still
// going?" for someone who started a multi-hour run and walked away — and a run
// that died needs that answer soonest, because a completed one has left its work
// on the branch while a dead one leaves the rest of the PRD undone and will not
// start again by itself. That is why the failures share the onComplete.notify
// switch instead of asking for one of their own: whoever wants to hear about the
// good ending wants the bad one more.
//
// The run's cost rides along when it spent anything, since that is the one number
// worth having before deciding whether to pick the run back up.
func (a *App) notifyRunStopped(prdName, what string) {
	if a.config != nil && !a.config.OnComplete.Notify {
		return
	}
	send := a.notifier
	if send == nil {
		send = notify.Send
	}

	body := fmt.Sprintf("%s — %s", formatPRDTitle(prdName), what)
	if a.totalCost > 0 {
		body += fmt.Sprintf(" (%s)", formatCost(a.totalCost))
	}
	send("Chief", body)
}

// notifyReasonLimit caps how much of an error a notification carries. A desktop
// banner gets a line or two before the OS cuts it off mid-word, and the cut is
// better made here, where an ellipsis can say it happened.
const notifyReasonLimit = 100

// notifyReason is a failed run's cause of death in the few words a notification
// has room for: the first line of the error, trimmed. The wrapped layers below
// it are what the log is for.
func notifyReason(event loop.Event) string {
	text := event.Text
	if event.Err != nil {
		text = event.Err.Error()
	}
	text = strings.TrimSpace(text)
	if line, _, found := strings.Cut(text, "\n"); found {
		text = strings.TrimSpace(line)
	}
	if text == "" {
		return "unknown error"
	}

	runes := []rune(text)
	if len(runes) > notifyReasonLimit {
		text = strings.TrimSpace(string(runes[:notifyReasonLimit])) + "…"
	}
	return text
}
