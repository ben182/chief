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
// produce: a story-done signal when it carries the <chief-done/> tag, otherwise
// plain assistant text. Centralizing the tag check keeps all provider parsers in
// lockstep on how completion is detected.
func classifyAssistantText(text string) *Event {
	if strings.Contains(text, chiefDoneTag) {
		return &Event{Type: EventStoryDone, Text: text}
	}
	return &Event{Type: EventAssistantText, Text: text}
}
