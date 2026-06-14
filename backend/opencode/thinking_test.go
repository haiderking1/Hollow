package opencode

import (
	"encoding/json"
	"testing"
)

func TestApplyThinkingToRequestEnabled(t *testing.T) {
	req := &ChatRequest{Model: "deepseek-v4-flash"}
	ApplyThinkingToRequest(req, ThinkingHigh, "deepseek-v4-flash")

	if req.Thinking != nil {
		t.Fatalf("thinking should be nil for deepseek-v4 (no enabled object), got %+v", req.Thinking)
	}
	if req.ReasoningEffort != "high" {
		t.Fatalf("reasoning effort: %q", req.ReasoningEffort)
	}
}


func TestSupportsThinkingCodex(t *testing.T) {
	if !SupportsThinking("gpt-5.4") {
		t.Fatal("gpt-5.4 should support thinking variants")
	}
	levels := SupportedThinkingLevels("gpt-5.4")
	if len(levels) < 4 {
		t.Fatalf("expected multiple levels, got %v", levels)
	}
}

func TestApplyThinkingCodexResponses(t *testing.T) {
	req := &ChatRequest{Model: "gpt-5.4"}
	ApplyThinkingToRequest(req, ThinkingMedium, "gpt-5.4")
	r := reasoningFromChatRequest(*req)
	if r == nil || r.Effort != "medium" || r.Summary != "auto" {
		t.Fatalf("reasoning = %#v", r)
	}
}

func TestStepThinkingLevel(t *testing.T) {
	got := StepThinkingLevel(ThinkingOff, "gpt-5.4", 1)
	if got != ThinkingMinimal {
		t.Fatalf("step = %q", got)
	}
}

func TestSupportedThinkingLevelsParity(t *testing.T) {
	// Kimi has early return (earlyReturnVariants = true) and mandatory thinking (mandatoryThinking = true)
	// It has only 1 thinking level, so SupportsThinking returns false (no picker in TUI)
	kimiLevels := SupportedThinkingLevels("kimi-k2.7-code")
	if len(kimiLevels) != 1 || kimiLevels[0] != ThinkingMedium {
		t.Fatalf("kimi thinking levels: expected [medium], got %v", kimiLevels)
	}
	if SupportsThinking("kimi-k2.7-code") {
		t.Fatal("kimi-k2.7-code should NOT support manual thinking levels selection in TUI")
	}

	// DeepSeek V4 Flash should have [low, medium, high, max] (no off)
	dsLevels := SupportedThinkingLevels("deepseek-v4-flash")
	expectedDs := []ThinkingLevel{ThinkingLow, ThinkingMedium, ThinkingHigh, ThinkingMax}
	if len(dsLevels) != len(expectedDs) {
		t.Fatalf("deepseek-v4-flash levels len: expected %d, got %d", len(expectedDs), len(dsLevels))
	}
	for i, l := range expectedDs {
		if dsLevels[i] != l {
			t.Fatalf("deepseek-v4-flash level at %d: expected %s, got %s", i, l, dsLevels[i])
		}
	}
	if !SupportsThinking("deepseek-v4-flash") {
		t.Fatal("deepseek-v4-flash should support thinking levels selection")
	}

	// Qwen should early return and be mandatory
	qwenLevels := SupportedThinkingLevels("qwen3.7-plus")
	if len(qwenLevels) != 1 || qwenLevels[0] != ThinkingMedium {
		t.Fatalf("qwen thinking levels: expected [medium], got %v", qwenLevels)
	}
	if SupportsThinking("qwen3.7-plus") {
		t.Fatal("qwen3.7-plus should NOT support thinking levels selection")
	}
}

func TestMessageReasoningNormalization(t *testing.T) {
	// Custom catalog models with different interleaved fields
	catalogMu.Lock()
	if opencodeCatalog == nil {
		opencodeCatalog = make(map[string]ModelInfo)
	}
	opencodeCatalog["test-details-model"] = ModelInfo{
		ID:             "test-details-model",
		Name:           "Test Details Model",
		Reasoning:      true,
		ReasoningField: "reasoning_details",
	}
	opencodeCatalog["test-plain-model"] = ModelInfo{
		ID:             "test-plain-model",
		Name:           "Test Plain Model",
		Reasoning:      true,
		ReasoningField: "reasoning",
	}
	catalogMu.Unlock()

	text := "my reasoning content"
	msgs := []Message{
		{
			Role:             "assistant",
			Content:          []byte(`"hello"`),
			ReasoningContent: &text,
		},
	}

	// Normalize with test-details-model
	normalized := NormalizeMessages(msgs, "test-details-model")
	if normalized[0].ReasoningDetails == nil || *normalized[0].ReasoningDetails != text {
		t.Fatalf("expected reasoning_details to be set to %q, got nil or different", text)
	}
	if normalized[0].ReasoningContent != nil || normalized[0].ReasoningPlain != nil {
		t.Fatalf("expected other reasoning fields to be cleared")
	}

	// Normalize with test-plain-model
	normalizedPlain := NormalizeMessages(msgs, "test-plain-model")
	if normalizedPlain[0].ReasoningPlain == nil || *normalizedPlain[0].ReasoningPlain != text {
		t.Fatalf("expected reasoning to be set to %q, got nil or different", text)
	}
	if normalizedPlain[0].ReasoningContent != nil || normalizedPlain[0].ReasoningDetails != nil {
		t.Fatalf("expected other reasoning fields to be cleared")
	}

	// Normalize with kimi-k2.7-code (has Reasoning: true but SupportsThinking is false)
	textKimi := "kimi reasoning"
	msgsKimi := []Message{
		{
			Role:           "assistant",
			Content:        []byte(`"hello"`),
			ReasoningPlain: &textKimi,
		},
	}
	normalizedKimi := NormalizeMessages(msgsKimi, "kimi-k2.7-code")
	if normalizedKimi[0].ReasoningContent == nil || *normalizedKimi[0].ReasoningContent != textKimi {
		t.Fatalf("expected kimi reasoning_content to be populated, got nil or different")
	}
	if normalizedKimi[0].ReasoningPlain != nil {
		t.Fatalf("expected original reasoning field to be cleared, got non-nil")
	}

	// Test empty reasoning injection: assistant message with zero reasoning fields gets empty reasoning_content injected
	msgsEmpty := []Message{
		{
			Role:    "assistant",
			Content: []byte(`"hello"`),
		},
	}
	normalizedEmpty := NormalizeMessages(msgsEmpty, "kimi-k2.7-code")
	if normalizedEmpty[0].ReasoningContent == nil || *normalizedEmpty[0].ReasoningContent != "" {
		t.Fatalf("expected empty reasoning_content to be injected, got nil or non-empty")
	}
}

func TestJSONSerializationParity(t *testing.T) {
	// Helper to marshal and unmarshal request into a map for precise checking
	checkRequest := func(t *testing.T, req *ChatRequest) map[string]interface{} {
		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("failed to marshal ChatRequest: %v", err)
		}
		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}
		return parsed
	}

	// 1. Kimi K2.7 Code (mandatory thinking, early return, no variants)
	// Both ThinkingOff and ThinkingMedium should produce NO thinking and NO reasoning_effort in the request body
	for _, lvl := range []ThinkingLevel{ThinkingOff, ThinkingMedium} {
		req := &ChatRequest{Model: "kimi-k2.7-code"}
		ApplyThinkingToRequest(req, lvl, "kimi-k2.7-code")
		parsed := checkRequest(t, req)

		if _, ok := parsed["thinking"]; ok {
			t.Fatalf("kimi with level %q should NOT serialize thinking parameter", lvl)
		}
		if _, ok := parsed["reasoning_effort"]; ok {
			t.Fatalf("kimi with level %q should NOT serialize reasoning_effort parameter", lvl)
		}
	}

	// 2. DeepSeek V4 Flash (has variants: low, medium, high, max)
	// ThinkingHigh -> reasoning_effort: "high", thinking: nil
	{
		req := &ChatRequest{Model: "deepseek-v4-flash"}
		ApplyThinkingToRequest(req, ThinkingHigh, "deepseek-v4-flash")
		parsed := checkRequest(t, req)

		if _, ok := parsed["thinking"]; ok {
			t.Fatal("deepseek-v4-flash should NOT serialize thinking parameter when enabled")
		}
		effort, ok := parsed["reasoning_effort"]
		if !ok || effort != "high" {
			t.Fatalf("expected reasoning_effort 'high', got %v", effort)
		}
	}

	// DeepSeek V4 Flash with level "" (first request with no level selected)
	// Both "" and ThinkingOff should produce NO reasoning_effort and NO thinking
	for _, lvl := range []ThinkingLevel{"", ThinkingOff} {
		req := &ChatRequest{Model: "deepseek-v4-flash"}
		ApplyThinkingToRequest(req, lvl, "deepseek-v4-flash")
		parsed := checkRequest(t, req)

		if _, ok := parsed["thinking"]; ok {
			t.Fatalf("deepseek-v4-flash with empty/off level should NOT serialize thinking parameter")
		}
		if _, ok := parsed["reasoning_effort"]; ok {
			t.Fatalf("deepseek-v4-flash with empty/off level should NOT serialize reasoning_effort parameter")
		}
	}

	// 3. MiniMax M3 (has custom toggle variants: ThinkingOff -> disabled, ThinkingMedium -> adaptive)
	// ThinkingOff -> thinking: { type: "disabled" }
	{
		req := &ChatRequest{Model: "minimax-m3"}
		ApplyThinkingToRequest(req, ThinkingOff, "minimax-m3")
		parsed := checkRequest(t, req)

		thinkingRaw, ok := parsed["thinking"]
		if !ok {
			t.Fatal("expected minimax-m3 ThinkingOff to serialize thinking parameter")
		}
		thinking, ok := thinkingRaw.(map[string]interface{})
		if !ok || thinking["type"] != "disabled" {
			t.Fatalf("expected thinking type 'disabled', got %v", thinking)
		}
		if _, ok := parsed["reasoning_effort"]; ok {
			t.Fatal("minimax-m3 should NOT serialize reasoning_effort")
		}
	}

	// ThinkingMedium -> thinking: { type: "adaptive" }
	{
		req := &ChatRequest{Model: "minimax-m3"}
		ApplyThinkingToRequest(req, ThinkingMedium, "minimax-m3")
		parsed := checkRequest(t, req)

		thinkingRaw, ok := parsed["thinking"]
		if !ok {
			t.Fatal("expected minimax-m3 ThinkingMedium to serialize thinking parameter")
		}
		thinking, ok := thinkingRaw.(map[string]interface{})
		if !ok || thinking["type"] != "adaptive" {
			t.Fatalf("expected thinking type 'adaptive', got %v", thinking)
		}
		if _, ok := parsed["reasoning_effort"]; ok {
			t.Fatal("minimax-m3 should NOT serialize reasoning_effort")
		}
	}
}
