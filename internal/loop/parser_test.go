package loop

import (
	"encoding/json"
	"testing"
)

func TestEventTypeString(t *testing.T) {
	tests := []struct {
		eventType EventType
		expected  string
	}{
		{EventUnknown, "Unknown"},
		{EventIterationStart, "IterationStart"},
		{EventAssistantText, "AssistantText"},
		{EventToolStart, "ToolStart"},
		{EventToolResult, "ToolResult"},
		{EventStoryDone, "StoryDone"},
		{EventComplete, "Complete"},
		{EventMaxIterationsReached, "MaxIterationsReached"},
		{EventError, "Error"},
		{EventRetrying, "Retrying"},
		{EventWatchdogTimeout, "WatchdogTimeout"},
		{EventStoryNeedsReview, "StoryNeedsReview"},
		{EventStoryNoCommit, "StoryNoCommit"},
		{EventResult, "Result"},
		{EventNoGitRepo, "NoGitRepo"},
		{EventReviewStart, "ReviewStart"},
		{EventReviewDone, "ReviewDone"},
		{EventUsage, "Usage"},
		{EventType(9999), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.eventType.String(); got != tt.expected {
				t.Errorf("EventType.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseLineEmpty(t *testing.T) {
	tests := []string{
		"",
		"   ",
		"\t",
		"\n",
	}

	for _, line := range tests {
		if got := ParseLine(line); got != nil {
			t.Errorf("ParseLine(%q) = %v, want nil", line, got)
		}
	}
}

func TestParseLineInvalidJSON(t *testing.T) {
	tests := []string{
		"not json",
		"{invalid}",
		"[1, 2, 3]",
	}

	for _, line := range tests {
		if got := ParseLine(line); got != nil {
			t.Errorf("ParseLine(%q) = %v, want nil", line, got)
		}
	}
}

func TestParseLineSystemInit(t *testing.T) {
	line := `{"type":"system","subtype":"init","cwd":"/test","session_id":"abc123","tools":["Read","Write"]}`

	event := ParseLine(line)
	if event == nil {
		t.Fatal("ParseLine returned nil, want event")
	}
	if event.Type != EventIterationStart {
		t.Errorf("event.Type = %v, want EventIterationStart", event.Type)
	}
}

func TestParseLineSystemOtherSubtype(t *testing.T) {
	line := `{"type":"system","subtype":"other"}`

	event := ParseLine(line)
	if event != nil {
		t.Errorf("ParseLine returned %v, want nil for non-init system message", event)
	}
}

func TestParseLineAssistantText(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"Hello, I will help you."}]}}`

	event := ParseLine(line)
	if event == nil {
		t.Fatal("ParseLine returned nil, want event")
	}
	if event.Type != EventAssistantText {
		t.Errorf("event.Type = %v, want EventAssistantText", event.Type)
	}
	if event.Text != "Hello, I will help you." {
		t.Errorf("event.Text = %q, want %q", event.Text, "Hello, I will help you.")
	}
}

func TestParseLineChiefDone(t *testing.T) {
	tests := []string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"All criteria pass! <chief-done/>"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"<chief-done/>"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Done\n<chief-done/>\nGoodbye"}]}}`,
	}

	for _, line := range tests {
		event := ParseLine(line)
		if event == nil {
			t.Fatalf("ParseLine(%q) returned nil, want event", line)
		}
		if event.Type != EventStoryDone {
			t.Errorf("ParseLine(%q): event.Type = %v, want EventStoryDone", line, event.Type)
		}
	}
}

func TestParseLineToolUse(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_123","name":"Read","input":{"file_path":"/test/file.go"}}]}}`

	event := ParseLine(line)
	if event == nil {
		t.Fatal("ParseLine returned nil, want event")
	}
	if event.Type != EventToolStart {
		t.Errorf("event.Type = %v, want EventToolStart", event.Type)
	}
	if event.Tool != "Read" {
		t.Errorf("event.Tool = %q, want %q", event.Tool, "Read")
	}
	if event.ToolInput == nil {
		t.Fatal("event.ToolInput is nil")
	}
	if filePath, ok := event.ToolInput["file_path"].(string); !ok || filePath != "/test/file.go" {
		t.Errorf("event.ToolInput[\"file_path\"] = %v, want %q", event.ToolInput["file_path"], "/test/file.go")
	}
}

func TestParseLineToolResult(t *testing.T) {
	line := `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_123","content":"File contents here"}]}}`

	event := ParseLine(line)
	if event == nil {
		t.Fatal("ParseLine returned nil, want event")
	}
	if event.Type != EventToolResult {
		t.Errorf("event.Type = %v, want EventToolResult", event.Type)
	}
	if event.Text != "File contents here" {
		t.Errorf("event.Text = %q, want %q", event.Text, "File contents here")
	}
}

func TestParseLineResultMessage(t *testing.T) {
	line := `{"type":"result","subtype":"success","is_error":false,"result":"Done"}`

	event := ParseLine(line)
	// Result messages are not emitted as events
	if event != nil {
		t.Errorf("ParseLine returned %v, want nil for result message", event)
	}
}

func TestParseLineUnknownType(t *testing.T) {
	line := `{"type":"unknown_type"}`

	event := ParseLine(line)
	if event != nil {
		t.Errorf("ParseLine returned %v, want nil for unknown type", event)
	}
}

func TestParseLineEmptyContent(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[]}}`

	event := ParseLine(line)
	if event != nil {
		t.Errorf("ParseLine returned %v, want nil for empty content", event)
	}
}

func TestParseLineRealWorldSystemInit(t *testing.T) {
	line := `{"type":"system","subtype":"init","cwd":"/Users/codemonkey/projects/chief","session_id":"7cdf33f6-72ec-4c0e-94fb-e2b637e109da","tools":["Task","Bash","Read"],"model":"claude-opus-4-5-20251101","permissionMode":"default"}`

	event := ParseLine(line)
	if event == nil {
		t.Fatal("ParseLine returned nil, want event")
	}
	if event.Type != EventIterationStart {
		t.Errorf("event.Type = %v, want EventIterationStart", event.Type)
	}
}

func TestParseLineRealWorldToolUse(t *testing.T) {
	line := `{"type":"assistant","message":{"model":"claude-opus-4-5-20251101","id":"msg_01XPBzHqPaCuQMUFm77eHe4D","type":"message","role":"assistant","content":[{"type":"tool_use","id":"toolu_01MiR5Ps9inHigemS2gxEz5R","name":"Read","input":{"file_path":"/Users/codemonkey/projects/chief/go.mod"}}]}}`

	event := ParseLine(line)
	if event == nil {
		t.Fatal("ParseLine returned nil, want event")
	}
	if event.Type != EventToolStart {
		t.Errorf("event.Type = %v, want EventToolStart", event.Type)
	}
	if event.Tool != "Read" {
		t.Errorf("event.Tool = %q, want %q", event.Tool, "Read")
	}
}

func TestParseLineMultipleContentBlocks(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"First"},{"type":"tool_use","name":"Read","input":{}}]}}`

	event := ParseLine(line)
	if event == nil {
		t.Fatal("ParseLine returned nil, want event")
	}
	if event.Type != EventAssistantText {
		t.Errorf("event.Type = %v, want EventAssistantText", event.Type)
	}
	if event.Text != "First" {
		t.Errorf("event.Text = %q, want %q", event.Text, "First")
	}
}

func TestParseLineToolUseFirst(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":"/test"}},{"type":"text","text":"Second"}]}}`

	event := ParseLine(line)
	if event == nil {
		t.Fatal("ParseLine returned nil, want event")
	}
	if event.Type != EventToolStart {
		t.Errorf("event.Type = %v, want EventToolStart", event.Type)
	}
	if event.Tool != "Write" {
		t.Errorf("event.Tool = %q, want %q", event.Tool, "Write")
	}
}

// A subagent quoting the tag is not the agent saying it is done: the loop kills
// the process group on <chief-done/>, and every subagent still running with it.
func TestASubagentsDoneTagDoesNotEndTheIteration(t *testing.T) {
	sub := `{"type":"assistant","parent_tool_use_id":"toolu_1","message":{"id":"m1","model":"claude-sonnet-5","content":[{"type":"text","text":"the prompt says to emit <chief-done/> when done"}],"usage":{"input_tokens":1,"output_tokens":1}}}`
	ev := ParseLine(sub)
	if ev == nil || ev.Type == EventStoryDone {
		t.Fatalf("a subagent's text ended the story: %+v", ev)
	}
	if ev.Cost == 0 {
		t.Error("a subagent's usage was dropped along with its tag — it is paid for all the same")
	}
	main := `{"type":"assistant","message":{"content":[{"type":"text","text":"all criteria met <chief-done/>"}]}}`
	if ev := ParseLine(main); ev == nil || ev.Type != EventStoryDone {
		t.Errorf("the agent's own tag no longer ends the story: %+v", ev)
	}
}

// <chief-blocked> ends the story with a reason; in a subagent's message it is
// only text, like <chief-done/>.
func TestParseLine_ChiefBlocked(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"Committed the UI.\n<chief-blocked> codesign waits on a keychain prompt · allow the key </chief-blocked>"}]}}`
	ev := ParseLine(line)
	if ev == nil || ev.Type != EventStoryBlocked {
		t.Fatalf("got %+v, want EventStoryBlocked", ev)
	}
	if ev.Text != "codesign waits on a keychain prompt · allow the key" {
		t.Errorf("Text = %q", ev.Text)
	}

	sub := `{"type":"assistant","parent_tool_use_id":"toolu_1","message":{"content":[{"type":"text","text":"<chief-blocked>x</chief-blocked>"}]}}`
	if ev := ParseLine(sub); ev == nil || ev.Type != EventAssistantText {
		t.Errorf("a subagent's <chief-blocked> became %+v", ev)
	}
}

// Only a signal counts, not a mention: a tag in inline code or a code block is
// quoted, <chief-blocked> needs its closing tag and a reason, and of two
// signals the one written last is the verdict. RCP-028 was marked done from a
// sentence that said it was not, and a blocked tag in passing beat a real done.
func TestClassifyAssistantText_OnlyRealSignalsCount(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		want       EventType
		reason     string
	}{
		{"done", "All criteria met.\n<chief-done/>", EventStoryDone, ""},
		{"blocked", "<chief-blocked>1Password is locked · unlock it</chief-blocked>", EventStoryBlocked, "1Password is locked · unlock it"},
		{"blocked with code in its reason", "<chief-blocked>`codesign` waits on the keychain</chief-blocked>", EventStoryBlocked, "`codesign` waits on the keychain"},
		{"done in backticks", "I will output `<chief-done/>` once the tests pass.", EventAssistantText, ""},
		{"blocked in backticks", "This is not a case for `<chief-blocked>x</chief-blocked>`.", EventAssistantText, ""},
		{"blocked in double backticks", "Not a ``<chief-blocked>x</chief-blocked>`` case.", EventAssistantText, ""},
		{"blocked in a code block", "The prompt says:\n```\n<chief-blocked>reason</chief-blocked>\n```\nNot my case.", EventAssistantText, ""},
		{"done in an unclosed code block", "Example:\n```text\n<chief-done/>", EventAssistantText, ""},
		{"opening tag in a sentence", "Nothing here needs <chief-blocked> — the tests pass now.", EventAssistantText, ""},
		{"opening tag without closing, then done", "No <chief-blocked> needed.\n<chief-done/>", EventStoryDone, ""},
		{"empty reason", "<chief-blocked>  </chief-blocked>", EventAssistantText, ""},
		{"blocked then done", "<chief-blocked>keychain</chief-blocked> — resolved it after all.\n<chief-done/>", EventStoryDone, ""},
		{"done then blocked", "<chief-done/> no, wait:\n<chief-blocked>the team ID contradicts the project</chief-blocked>", EventStoryBlocked, "the team ID contradicts the project"},
		{"quoted blocked, real done", "Skipped `<chief-blocked>…</chief-blocked>`: nothing blocks it.\n<chief-done/>", EventStoryDone, ""},
		{"stray backtick before a signal", "It's in the `foo dir.\n<chief-done/>", EventStoryDone, ""},
		{"fence closed, signal after it", "```go\nx := 1\n```\n<chief-done/>", EventStoryDone, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev := classifyAssistantText(tc.text)
			if ev.Type != tc.want {
				t.Fatalf("Type = %v, want %v", ev.Type, tc.want)
			}
			if tc.want == EventStoryBlocked && ev.Text != tc.reason {
				t.Errorf("reason = %q, want %q", ev.Text, tc.reason)
			}
		})
	}
}

// Every provider parser shares the rule.
func TestEveryProviderIgnoresAQuotedSignal(t *testing.T) {
	quoted := "I must not write `<chief-done/>` yet"
	for name, line := range map[string]string{
		"claude":   `{"type":"assistant","message":{"content":[{"type":"text","text":"I must not write ` + "`<chief-done/>`" + ` yet"}]}}`,
		"codex":    `{"type":"item.completed","item":{"type":"agent_message","text":"I must not write ` + "`<chief-done/>`" + ` yet"}}`,
		"opencode": `{"type":"text","part":{"text":"I must not write ` + "`<chief-done/>`" + ` yet"}}`,
		"cursor":   `{"type":"assistant","message":{"content":[{"type":"text","text":"I must not write ` + "`<chief-done/>`" + ` yet"}]}}`,
		"gemini":   `{"type":"message","role":"assistant","content":"I must not write ` + "`<chief-done/>`" + ` yet","delta":true}`,
	} {
		var ev *Event
		switch name {
		case "claude":
			ev = ParseLine(line)
		case "codex":
			ev = ParseLineCodex(line)
		case "opencode":
			ev = ParseLineOpenCode(line)
		case "cursor":
			ev = ParseLineCursor(line)
		case "gemini":
			ev = NewGeminiLineParser()(line)
		}
		if ev == nil {
			t.Errorf("%s: no event for %q", name, quoted)
			continue
		}
		if ev.Type == EventStoryDone {
			t.Errorf("%s: a quoted tag ended the story", name)
		}
	}
}

// Gemini streams its answer in deltas. A <chief-blocked> whose reason comes in
// the next delta must wait for it instead of ending the story with none, and a
// tag split across two deltas is still a tag.
func TestGeminiLineParser_JudgesDeltasTogether(t *testing.T) {
	delta := func(s string) string {
		b, _ := json.Marshal(map[string]any{"type": "message", "role": "assistant", "content": s, "delta": true})
		return string(b)
	}
	parse := NewGeminiLineParser()
	for _, part := range []string{"Stuck. <chief-blocked>", "codesign waits on the keychain", " · allow it"} {
		if ev := parse(delta(part)); ev == nil || ev.Type != EventAssistantText {
			t.Fatalf("delta %q gave %+v, want plain text until the closing tag", part, ev)
		}
	}
	ev := parse(delta("</chief-blocked>"))
	if ev == nil || ev.Type != EventStoryBlocked || ev.Text != "codesign waits on the keychain · allow it" {
		t.Fatalf("closing delta gave %+v", ev)
	}

	parse = NewGeminiLineParser()
	_ = parse(delta("All criteria met. <chief-"))
	if ev := parse(delta("done/>")); ev == nil || ev.Type != EventStoryDone {
		t.Errorf("a done tag split over two deltas gave %+v", ev)
	}

	// A tool call ends the message; what came before it is not carried over.
	parse = NewGeminiLineParser()
	_ = parse(delta("<chief-"))
	_ = parse(`{"type":"tool_use","tool_name":"read_file","tool_id":"c1","parameters":{}}`)
	if ev := parse(delta("done/>")); ev == nil || ev.Type == EventStoryDone {
		t.Errorf("text from before a tool call was joined to the next message: %+v", ev)
	}
}
