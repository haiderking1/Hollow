package opencode

// ThinkingLevel controls model reasoning effort (Flame-compatible).
type ThinkingLevel string

const (
	ThinkingOff   ThinkingLevel = "off"
	ThinkingHigh  ThinkingLevel = "high"
	ThinkingXHigh ThinkingLevel = "xhigh"
)

type ThinkingParams struct {
	Type string `json:"type"`
}

// deepseekV4FlashLevels matches Flame's opencode-go deepseek-v4-flash thinkingLevelMap.
var deepseekV4FlashLevels = []ThinkingLevel{ThinkingOff, ThinkingHigh, ThinkingXHigh}

func SupportsThinking(model string) bool {
	switch model {
	case "deepseek-v4-flash", "deepseek-v4-pro":
		return true
	default:
		return false
	}
}

func SupportedThinkingLevels(model string) []ThinkingLevel {
	if !SupportsThinking(model) {
		return []ThinkingLevel{ThinkingOff}
	}
	return append([]ThinkingLevel(nil), deepseekV4FlashLevels...)
}

func CycleThinkingLevel(current ThinkingLevel, model string) ThinkingLevel {
	levels := SupportedThinkingLevels(model)
	if len(levels) <= 1 {
		return ThinkingOff
	}
	idx := 0
	for i, l := range levels {
		if l == current {
			idx = i
			break
		}
	}
	return levels[(idx+1)%len(levels)]
}

func ApplyThinkingToRequest(req *ChatRequest, level ThinkingLevel, model string) {
	if !SupportsThinking(model) {
		return
	}
	if level == ThinkingOff || level == "" {
		req.Thinking = &ThinkingParams{Type: "disabled"}
		return
	}
	req.Thinking = &ThinkingParams{Type: "enabled"}
	switch level {
	case ThinkingXHigh:
		req.ReasoningEffort = "max"
	default:
		req.ReasoningEffort = "high"
	}
}

// NormalizeMessages ensures assistant messages include reasoning_content when required.
func NormalizeMessages(msgs []Message, model string) []Message {
	if !SupportsThinking(model) {
		return msgs
	}
	out := make([]Message, len(msgs))
	copy(out, msgs)
	for i := range out {
		if out[i].Role != "assistant" {
			continue
		}
		if out[i].ReasoningContent == nil {
			empty := ""
			out[i].ReasoningContent = &empty
		}
	}
	return out
}

func ParseThinkingLevel(s string) ThinkingLevel {
	switch ThinkingLevel(s) {
	case ThinkingHigh, ThinkingXHigh:
		return ThinkingLevel(s)
	default:
		return ThinkingOff
	}
}
