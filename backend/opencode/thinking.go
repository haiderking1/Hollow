package opencode

import (
	"strings"
)

// ThinkingLevel controls model reasoning effort (OpenCode + Codex + Responses API).
type ThinkingLevel string

const (
	ThinkingOff     ThinkingLevel = "off"
	ThinkingMinimal ThinkingLevel = "minimal"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
	ThinkingMax     ThinkingLevel = "max"
	ThinkingXHigh   ThinkingLevel = "xhigh"
)

type ThinkingParams struct {
	Type string `json:"type"`
}

// deepseekV4FlashLevels matches OpenCode's opencode-go deepseek-v4 efforts.
var deepseekV4FlashLevels = []ThinkingLevel{ThinkingLow, ThinkingMedium, ThinkingHigh, ThinkingMax}

// defaultReasoningLevels is the standard OpenAI/Codex reasoning effort ladder.
var defaultReasoningLevels = []ThinkingLevel{
	ThinkingOff, ThinkingMinimal, ThinkingLow, ThinkingMedium, ThinkingHigh, ThinkingXHigh,
}

func earlyReturnVariants(model string) bool {
	id := strings.ToLower(model)
	if strings.Contains(id, "minimax-m3") {
		return false
	}
	for _, part := range []string{
		"deepseek-chat", "deepseek-reasoner", "deepseek-r1", "deepseek-v3",
		"minimax", "glm", "kimi", "k2p", "qwen", "big-pickle",
	} {
		if strings.Contains(id, part) {
			return true
		}
	}
	return false
}

func SupportsThinking(model string) bool {
	return len(SupportedThinkingLevels(model)) > 1
}

func mandatoryThinking(model string) bool {
	return opencodeMandatoryThinkingID(model)
}

func SupportedThinkingLevels(model string) []ThinkingLevel {
	modelLower := strings.ToLower(model)
	if strings.Contains(modelLower, "gpt-") {
		return append([]ThinkingLevel(nil), defaultReasoningLevels...)
	}
	if strings.Contains(modelLower, "minimax-m3") {
		return []ThinkingLevel{ThinkingOff, ThinkingMedium}
	}
	if earlyReturnVariants(model) {
		if mandatoryThinking(model) {
			return []ThinkingLevel{ThinkingMedium}
		}
		return []ThinkingLevel{ThinkingOff}
	}
	if strings.Contains(modelLower, "deepseek-v4") {
		return append([]ThinkingLevel(nil), deepseekV4FlashLevels...)
	}
	return []ThinkingLevel{ThinkingLow, ThinkingMedium, ThinkingHigh}
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

func StepThinkingLevel(current ThinkingLevel, model string, delta int) ThinkingLevel {
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
	n := len(levels)
	idx = ((idx+delta)%n + n) % n
	return levels[idx]
}

func ApplyThinkingToRequest(req *ChatRequest, level ThinkingLevel, model string) {
	if !SupportsThinking(model) {
		return
	}
	modelLower := strings.ToLower(model)
	if strings.Contains(modelLower, "minimax-m3") {
		if level == ThinkingOff || level == "" {
			req.Thinking = &ThinkingParams{Type: "disabled"}
		} else {
			req.Thinking = &ThinkingParams{Type: "adaptive"}
		}
		req.ReasoningEffort = ""
		return
	}

	if level == ThinkingOff || level == "" {
		req.ReasoningEffort = ""
		req.Thinking = nil
		return
	}

	effort := ReasoningEffortForAPI(level, model)
	req.ReasoningEffort = effort
	req.Thinking = nil
}

// ReasoningEffortForAPI maps UI thinking levels to provider wire values.
func ReasoningEffortForAPI(level ThinkingLevel, model string) string {
	if level == ThinkingOff || level == "" {
		return ""
	}
	if strings.Contains(strings.ToLower(model), "deepseek-v4") {
		if level == ThinkingXHigh || level == ThinkingMax {
			return "max"
		}
		return string(level)
	}
	if level == ThinkingMax {
		return "xhigh"
	}
	return string(level)
}

// SupportsReasoning returns true if the model supports reasoning output.
func SupportsReasoning(model string) bool {
	if m, ok := LookupCatalogModel(model); ok {
		return m.Reasoning || m.ReasoningField != ""
	}
	// Fallback check before catalog is loaded/offline
	modelLower := strings.ToLower(model)
	if opencodeMandatoryThinkingID(model) {
		return true
	}
	if strings.Contains(modelLower, "deepseek-v4") || strings.Contains(modelLower, "deepseek-r1") || strings.Contains(modelLower, "reasoner") {
		return true
	}
	if strings.Contains(modelLower, "mimo") || strings.Contains(modelLower, "hy3") {
		return true
	}
	if strings.Contains(modelLower, "gpt-") {
		return true
	}
	return false
}

// NormalizeMessages ensures assistant messages include reasoning_content when required.
func NormalizeMessages(msgs []Message, model string) []Message {
	if !SupportsReasoning(model) {
		return msgs
	}
	field := "reasoning_content"
	if m, ok := LookupCatalogModel(model); ok && m.ReasoningField != "" {
		field = m.ReasoningField
	}

	out := make([]Message, len(msgs))
	copy(out, msgs)
	for i := range out {
		if out[i].Role != "assistant" {
			continue
		}
		reasoningText := out[i].GetReasoning()
		out[i].ReasoningContent = nil
		out[i].ReasoningDetails = nil
		out[i].ReasoningPlain = nil

		switch field {
		case "reasoning_details":
			out[i].ReasoningDetails = &reasoningText
		case "reasoning":
			out[i].ReasoningPlain = &reasoningText
		default:
			out[i].ReasoningContent = &reasoningText
		}
	}
	return out
}

func ParseThinkingLevel(s string) ThinkingLevel {
	switch ThinkingLevel(s) {
	case ThinkingMinimal, ThinkingLow, ThinkingMedium, ThinkingHigh, ThinkingMax, ThinkingXHigh:
		return ThinkingLevel(s)
	default:
		return ThinkingOff
	}
}

func FormatThinkingLabel(level ThinkingLevel) string {
	if level == "" || level == ThinkingOff {
		return "off"
	}
	return string(level)
}

// FormatThinkingLevelForModel returns the user-facing label for a thinking level.
func FormatThinkingLevelForModel(model string, level ThinkingLevel) string {
	if strings.Contains(strings.ToLower(model), "minimax-m3") {
		if level == ThinkingOff || level == "" {
			return "none"
		}
		return "thinking"
	}
	return FormatThinkingLabel(level)
}
