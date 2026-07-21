package loop

import (
	"math"
	"testing"
)

func TestParseAssistantMessageAttachesUsage(t *testing.T) {
	line := `{"type":"assistant","message":{"model":"claude-opus-4-8","content":[{"type":"text","text":"working"}],"usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":100,"cache_read_input_tokens":1000}}}`
	ev := ParseLine(line)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.Type != EventAssistantText {
		t.Fatalf("type = %v, want AssistantText", ev.Type)
	}
	if ev.InputTokens != 10 || ev.OutputTokens != 20 || ev.CacheCreationTokens != 100 || ev.CacheReadTokens != 1000 {
		t.Fatalf("tokens = %+v", ev)
	}
	// opus: 10*15 + 20*75 + 100*18.75 + 1000*1.5 all per MTok
	want := (10*15 + 20*75 + 100*18.75 + 1000*1.5) / 1_000_000.0
	if math.Abs(ev.Cost-want) > 1e-9 {
		t.Fatalf("cost = %v, want %v", ev.Cost, want)
	}
}

func TestUsageOnlyMessageEmitsUsageEvent(t *testing.T) {
	// A message whose content we don't surface (e.g. only thinking) still carries
	// usage, so we must not drop it.
	line := `{"type":"assistant","message":{"model":"claude-sonnet-5","content":[{"type":"thinking"}],"usage":{"output_tokens":5}}}`
	ev := ParseLine(line)
	if ev == nil {
		t.Fatal("expected usage event")
	}
	if ev.Type != EventUsage {
		t.Fatalf("type = %v, want Usage", ev.Type)
	}
	if ev.OutputTokens != 5 {
		t.Fatalf("output tokens = %d, want 5", ev.OutputTokens)
	}
}

func TestCostForUnknownModelIsZero(t *testing.T) {
	u := &usageInfo{OutputTokens: 1000}
	if c := costForUsage("some-unknown-model", u); c != 0 {
		t.Fatalf("cost = %v, want 0 for unknown model", c)
	}
	if c := costForUsage("claude-haiku-4-5", u); c <= 0 {
		t.Fatal("expected non-zero cost for known haiku model")
	}
}
