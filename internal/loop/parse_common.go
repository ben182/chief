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
// produce: a story-blocked signal carrying the reason, a story-done signal, or
// plain assistant text. Centralizing the tag check keeps all provider parsers in
// lockstep on how completion is detected.
//
// Only a signal counts, not a mention of one. A tag inside inline code or a
// fenced code block is quoted, not written; <chief-blocked> needs its closing
// tag and a reason between the two. When the text carries both signals, the one
// written last is the agent's verdict — "I was blocked on X, fixed it,
// <chief-done/>" is done.
func classifyAssistantText(text string) *Event {
	code := codeSpans(text)
	donePos := lastSignalIndex(text, chiefDoneTag, code)
	reason, blockedPos := lastBlocked(text, code)
	switch {
	case blockedPos >= 0 && blockedPos > donePos:
		return &Event{Type: EventStoryBlocked, Text: reason}
	case donePos >= 0:
		return &Event{Type: EventStoryDone, Text: text}
	}
	return &Event{Type: EventAssistantText, Text: text}
}

// span is a half-open byte range [start, end) of a text.
type span struct{ start, end int }

// inSpans reports whether byte offset i lies inside one of spans.
func inSpans(i int, spans []span) bool {
	for _, s := range spans {
		if i >= s.start && i < s.end {
			return true
		}
	}
	return false
}

// lastSignalIndex is the offset of the last occurrence of tag outside code that
// ends its line, or -1. A signal closes what the agent says; a tag with words
// after it on the same line ("so I do not output <chief-done/> yet") is a
// sentence about the signal. Closing punctuation or emphasis may follow.
func lastSignalIndex(text, tag string, code []span) int {
	last := -1
	for from := 0; ; {
		i := strings.Index(text[from:], tag)
		if i < 0 {
			return last
		}
		i += from
		if !inSpans(i, code) && endsLine(text[i+len(tag):]) {
			last = i
		}
		from = i + len(tag)
	}
}

// lastBlocked finds the last complete <chief-blocked>reason</chief-blocked>
// outside code with a non-empty reason, and returns the reason and the offset of
// its opening tag (-1 when there is none). A tag without its closing tag is not a
// signal: an agent that writes the opening tag in a sentence has not said it is
// blocked, and taking the rest of its text as the reason set finished stories
// aside.
func lastBlocked(text string, code []span) (string, int) {
	reason, pos := "", -1
	for from := 0; ; {
		i := strings.Index(text[from:], chiefBlockedOpen)
		if i < 0 {
			return reason, pos
		}
		i += from
		from = i + len(chiefBlockedOpen)
		if inSpans(i, code) {
			continue
		}
		// The closing tag has to be a real one too; a quoted one inside the
		// reason is part of the reason.
		end := -1
		for search := from; ; {
			j := strings.Index(text[search:], chiefBlockedClose)
			if j < 0 {
				break
			}
			j += search
			if !inSpans(j, code) {
				end = j
				break
			}
			search = j + len(chiefBlockedClose)
		}
		if end < 0 {
			continue
		}
		if r := strings.TrimSpace(text[from:end]); r != "" {
			reason, pos = r, i
		}
		from = end + len(chiefBlockedClose)
	}
}

// codeSpans returns the byte ranges of text that are code in Markdown: fenced
// blocks (``` or ~~~, an unclosed one running to the end, as a fence still being
// streamed does) and inline code spans (a run of backticks closed by a run of the
// same length; an unmatched run is a literal backtick).
func codeSpans(text string) []span {
	var spans []span
	var prose []span // the ranges outside fences, for inline code
	pos := 0
	fenceStart, fenceChar, fenceLen := -1, byte(0), 0
	proseStart := 0
	for pos <= len(text) {
		lineEnd := strings.IndexByte(text[pos:], '\n')
		next := len(text) + 1
		if lineEnd >= 0 {
			lineEnd += pos
			next = lineEnd + 1
		} else {
			lineEnd = len(text)
		}
		line := text[pos:lineEnd]
		trimmed := strings.TrimLeft(line, " ")
		if len(line)-len(trimmed) <= 3 && len(trimmed) >= 3 && (trimmed[0] == '`' || trimmed[0] == '~') {
			c := trimmed[0]
			n := 0
			for n < len(trimmed) && trimmed[n] == c {
				n++
			}
			switch {
			case fenceStart < 0 && n >= 3:
				fenceStart, fenceChar, fenceLen = pos, c, n
				prose = append(prose, span{proseStart, pos})
			case fenceStart >= 0 && c == fenceChar && n >= fenceLen && strings.TrimSpace(trimmed[n:]) == "":
				spans = append(spans, span{fenceStart, lineEnd})
				fenceStart = -1
				proseStart = min(next, len(text))
			}
		}
		pos = next
	}
	if fenceStart >= 0 {
		spans = append(spans, span{fenceStart, len(text)})
	} else {
		prose = append(prose, span{proseStart, len(text)})
	}
	for _, p := range prose {
		spans = append(spans, inlineCode(text, p)...)
	}
	return spans
}

// inlineCode returns the inline code spans within the range p of text.
func inlineCode(text string, p span) []span {
	var spans []span
	i := p.start
	for i < p.end {
		if text[i] != '`' {
			i++
			continue
		}
		n := 0
		for i+n < p.end && text[i+n] == '`' {
			n++
		}
		closeAt := -1
		for j := i + n; j < p.end; {
			if text[j] != '`' {
				j++
				continue
			}
			m := 0
			for j+m < p.end && text[j+m] == '`' {
				m++
			}
			if m == n {
				closeAt = j
				break
			}
			j += m
		}
		if closeAt < 0 {
			i += n
			continue
		}
		spans = append(spans, span{i, closeAt + n})
		i = closeAt + n
	}
	return spans
}

// endsLine reports whether rest, the text after a tag, has nothing on the tag's
// line but whitespace, closing punctuation or markdown emphasis.
func endsLine(rest string) bool {
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[:nl]
	}
	return strings.Trim(rest, " \t\r.!*_") == ""
}
