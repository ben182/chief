package loop

import (
	"encoding/json"
)

// EventType represents the type of event parsed from Claude's stream-json output.
type EventType int

const (
	// EventUnknown represents an unrecognized event type.
	EventUnknown EventType = iota
	// EventIterationStart is emitted at the start of a Claude iteration (system init).
	EventIterationStart
	// EventAssistantText is emitted when Claude outputs text.
	EventAssistantText
	// EventToolStart is emitted when Claude invokes a tool.
	EventToolStart
	// EventToolResult is emitted when a tool returns a result.
	EventToolResult
	// EventStoryDone is emitted when Claude signals a story is done via <chief-done/>.
	EventStoryDone
	// EventComplete is emitted when all stories are complete (buildPrompt
	// returns errAllStoriesComplete).
	EventComplete
	// EventStoryNeedsReview is emitted when a story is parked for human review
	// after failing to complete within the per-story attempt limit.
	EventStoryNeedsReview
	// EventStoryNoCommit is emitted when the agent signalled <chief-done/> but no
	// matching commit was found, so the story is treated as an incomplete attempt.
	EventStoryNoCommit
	// EventMaxIterationsReached is emitted when max iterations are reached.
	EventMaxIterationsReached
	// EventError is emitted when an error occurs.
	EventError
	// EventRetrying is emitted when retrying after a crash.
	EventRetrying
	// EventWatchdogTimeout is emitted when the watchdog kills a hung process.
	EventWatchdogTimeout
	// EventResult is emitted at the end of an iteration with cost/usage totals.
	EventResult
	// EventNoGitRepo is emitted once when the work directory is not a git repo,
	// so <chief-done/> can't be commit-verified and work isn't persisted between
	// fresh-context iterations.
	EventNoGitRepo
	// EventReviewStart is emitted when the separate review agent starts reviewing
	// a story's committed changes.
	EventReviewStart
	// EventReviewDone is emitted when the review agent has finished (and committed
	// any fixes it made).
	EventReviewDone
	// EventConsolidateStart is emitted when the consolidation agent starts its
	// end-of-run pass over all the commits this run landed.
	EventConsolidateStart
	// EventConsolidateDone is emitted when the consolidation pass has finished
	// (and committed any refactor it made), was skipped, or gave up. It is always
	// emitted once a pass was started, so the UI never shows a hanging pass.
	EventConsolidateDone
	// EventUsage carries token usage (and derived cost) for an assistant message
	// that produced no other surfaced event, so per-story totals stay accurate.
	EventUsage
)

// String returns the string representation of an EventType.
func (e EventType) String() string {
	switch e {
	case EventIterationStart:
		return "IterationStart"
	case EventAssistantText:
		return "AssistantText"
	case EventToolStart:
		return "ToolStart"
	case EventToolResult:
		return "ToolResult"
	case EventStoryDone:
		return "StoryDone"
	case EventComplete:
		return "Complete"
	case EventStoryNeedsReview:
		return "StoryNeedsReview"
	case EventStoryNoCommit:
		return "StoryNoCommit"
	case EventMaxIterationsReached:
		return "MaxIterationsReached"
	case EventError:
		return "Error"
	case EventRetrying:
		return "Retrying"
	case EventWatchdogTimeout:
		return "WatchdogTimeout"
	case EventResult:
		return "Result"
	case EventNoGitRepo:
		return "NoGitRepo"
	case EventReviewStart:
		return "ReviewStart"
	case EventReviewDone:
		return "ReviewDone"
	case EventConsolidateStart:
		return "ConsolidateStart"
	case EventConsolidateDone:
		return "ConsolidateDone"
	case EventUsage:
		return "Usage"
	default:
		return "Unknown"
	}
}

// Event represents a parsed event from Claude's stream-json output.
type Event struct {
	Type       EventType
	Iteration  int
	Text       string
	Tool       string
	ToolInput  map[string]interface{}
	StoryID    string
	Err        error
	RetryCount int      // Current retry attempt (1-based)
	RetryMax   int      // Maximum retries allowed
	CrashLog   []string // last stderr lines from the crashed process (EventRetrying)
	Cost       float64  // USD cost carried by this event (EventResult total_cost_usd, or per-message cost derived from token usage)

	// Token usage carried by an assistant message (Claude). Summed per story by
	// the UI. cache_read dominates cost when a large context is reused each turn.
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
}

// streamMessage represents the top-level structure of a stream-json line.
type streamMessage struct {
	Type         string          `json:"type"`
	Subtype      string          `json:"subtype,omitempty"`
	Message      json.RawMessage `json:"message,omitempty"`
	TotalCostUSD float64         `json:"total_cost_usd,omitempty"`
}

// assistantMessage represents the structure of an assistant message.
type assistantMessage struct {
	Model   string         `json:"model"`
	Content []contentBlock `json:"content"`
	Usage   *usageInfo     `json:"usage,omitempty"`
}

// usageInfo holds per-message token counts reported by Claude.
type usageInfo struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// contentBlock represents a block of content in an assistant message.
type contentBlock struct {
	Type  string                 `json:"type"`
	Text  string                 `json:"text,omitempty"`
	ID    string                 `json:"id,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`
}

// userMessage represents a tool result message.
type userMessage struct {
	Content []toolResultBlock `json:"content"`
}

// toolResultBlock represents a tool result in a user message.
type toolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
}

// ParseLine parses a single line of stream-json output and returns an Event.
// If the line cannot be parsed or is not relevant, it returns nil.
func ParseLine(line string) *Event {
	msg, ok := decodeLine[streamMessage](line)
	if !ok {
		return nil
	}

	switch msg.Type {
	case "system":
		if msg.Subtype == "init" {
			return &Event{Type: EventIterationStart}
		}
		return nil

	case "assistant":
		return parseAssistantMessage(msg.Message)

	case "user":
		return parseUserMessage(msg.Message)

	case "result":
		if msg.TotalCostUSD > 0 {
			return &Event{Type: EventResult, Cost: msg.TotalCostUSD}
		}
		return nil

	default:
		return nil
	}
}

// parseAssistantMessage parses an assistant message and returns appropriate events.
func parseAssistantMessage(raw json.RawMessage) *Event {
	if raw == nil {
		return nil
	}

	var msg assistantMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil
	}

	ev := eventForContent(msg.Content)

	// Attach per-message token usage and derived cost. Claude reports usage on
	// every assistant message; the final `result` event (which carries
	// total_cost_usd) never arrives because the loop kills the process on
	// <chief-done/>, so accumulating per-message usage is the reliable source.
	if msg.Usage != nil {
		if ev == nil {
			ev = &Event{Type: EventUsage}
		}
		ev.InputTokens = msg.Usage.InputTokens
		ev.OutputTokens = msg.Usage.OutputTokens
		ev.CacheCreationTokens = msg.Usage.CacheCreationInputTokens
		ev.CacheReadTokens = msg.Usage.CacheReadInputTokens
		ev.Cost = costForUsage(msg.Model, msg.Usage)
	}

	return ev
}

// eventForContent maps the first actionable content block to an event.
func eventForContent(content []contentBlock) *Event {
	for _, block := range content {
		switch block.Type {
		case "text":
			return classifyAssistantText(block.Text)

		case "tool_use":
			return &Event{
				Type:      EventToolStart,
				Tool:      block.Name,
				ToolInput: block.Input,
			}
		}
	}
	return nil
}

// parseUserMessage parses a user message (typically tool results).
func parseUserMessage(raw json.RawMessage) *Event {
	if raw == nil {
		return nil
	}

	var msg userMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil
	}

	for _, block := range msg.Content {
		if block.Type == "tool_result" {
			return &Event{
				Type: EventToolResult,
				Text: block.Content,
			}
		}
	}

	return nil
}
