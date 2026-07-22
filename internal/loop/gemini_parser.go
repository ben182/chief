package loop

// geminiStreamEvent is the top-level structure for a Gemini stream-json line.
type geminiStreamEvent struct {
	Type string `json:"type"`
}

// geminiMessageEvent represents a "message" event (user or assistant delta).
type geminiMessageEvent struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content string `json:"content"`
	Delta   bool   `json:"delta"`
}

// geminiToolUseEvent represents a "tool_use" event (tool call request).
type geminiToolUseEvent struct {
	Type       string                 `json:"type"`
	ToolName   string                 `json:"tool_name"`
	ToolID     string                 `json:"tool_id"`
	Parameters map[string]interface{} `json:"parameters"`
}

// geminiToolResultEvent represents a "tool_result" event.
type geminiToolResultEvent struct {
	Type   string `json:"type"`
	ToolID string `json:"tool_id"`
	Status string `json:"status"`
	Output string `json:"output,omitempty"`
}

// ParseLineGemini parses a single line of Gemini's stream-json output and
// returns an Event. Returns nil for lines that are not relevant to Chief.
func ParseLineGemini(line string) *Event {
	// Peek at the type field first.
	base, ok := decodeLine[geminiStreamEvent](line)
	if !ok {
		return nil
	}

	switch base.Type {
	case "init":
		// Session start maps to EventIterationStart.
		return &Event{Type: EventIterationStart}

	case "message":
		msg, ok := decodeLine[geminiMessageEvent](line)
		if !ok {
			return nil
		}
		if msg.Role != "assistant" || msg.Content == "" {
			return nil
		}
		return classifyAssistantText(msg.Content)

	case "tool_use":
		tu, ok := decodeLine[geminiToolUseEvent](line)
		if !ok {
			return nil
		}
		return &Event{
			Type:      EventToolStart,
			Tool:      tu.ToolName,
			ToolInput: tu.Parameters,
		}

	case "tool_result":
		tr, ok := decodeLine[geminiToolResultEvent](line)
		if !ok {
			return nil
		}
		return &Event{Type: EventToolResult, Text: tr.Output}

	case "result", "error":
		// Terminal / metadata events — not actionable inside the loop.
		return nil

	default:
		return nil
	}
}
