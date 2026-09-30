package loop

import (
	"encoding/json"
	"strings"
)

// Completion tags the agents emit in their assistant text. They live here as
// single constants so the five provider parsers can't drift apart on the exact
// spelling of a tag.
const (
	// chiefDoneTag marks a single story as finished. All providers use it.
	chiefDoneTag = "<chief-done/>"
	// chiefCompleteTag marks the whole PRD as finished. Only the Cursor parser
	// recognizes it today; the others surface overall completion structurally.
	chiefCompleteTag = "<chief-complete/>"
	// chiefBlockedOpen and chiefBlockedClose enclose the reason a story cannot
	// be finished without a person: <chief-blocked>reason</chief-blocked>.
	chiefBlockedOpen  = "<chief-blocked>"
	chiefBlockedClose = "</chief-blocked>"
)

// decodeLine trims a stream-json line and unmarshals it into T. It returns
// ok=false for blank lines and lines that aren't valid JSON, matching the
// "skip lines we can't parse" contract every ParseLine* entry point shares.
func decodeLine[T any](line string) (T, bool) {
	var v T
	if strings.TrimSpace(line) == "" {
		return v, false
	}
	if err := json.Unmarshal([]byte(line), &v); err != nil {
		return v, false
	}
	return v, true
}

// classifyAssistantText maps a block of assistant text to the event it should
// produce: a story-blocked signal carrying the reason when it holds a
// <chief-blocked> tag, a story-done signal when it carries the <chief-done/>
// tag, otherwise plain assistant text. Centralizing the tag check keeps all
// provider parsers in lockstep on how completion is detected.
//
// Blocked wins over done when an agent writes both: it is the one that says a
// person has something to do, and a story wrongly held back costs a question in
// the morning, where one wrongly marked done costs a broken feature.
func classifyAssistantText(text string) *Event {
	if reason, ok := blockedReason(text); ok {
		return &Event{Type: EventStoryBlocked, Text: reason}
	}
	if strings.Contains(text, chiefDoneTag) {
		return &Event{Type: EventStoryDone, Text: text}
	}
	return &Event{Type: EventAssistantText, Text: text}
}

// blockedReason extracts the reason from a <chief-blocked>…</chief-blocked> tag.
// A missing closing tag takes the rest of the text: the agent said it is
// blocked, and losing that over a typo would retry a story nobody can finish.
func blockedReason(text string) (string, bool) {
	start := strings.Index(text, chiefBlockedOpen)
	if start < 0 {
		return "", false
	}
	rest := text[start+len(chiefBlockedOpen):]
	if end := strings.Index(rest, chiefBlockedClose); end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest), true
}
