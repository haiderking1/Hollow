package opencode

import (
	"encoding/json"
	"fmt"
	"strings"
)

const toolIncompleteMsg = "Error: tool call was not completed"

// StripResponseFields removes response-only metadata before sending messages to
// the provider. OpenCode Go rejects requests that include usage on messages.
func StripResponseFields(msgs []Message) []Message {
	if len(msgs) == 0 {
		return msgs
	}
	out := make([]Message, len(msgs))
	for i, msg := range msgs {
		msg.Usage = nil
		out[i] = msg
	}
	return out
}

// PrepareRequestMessages sanitizes session history for chat/completions requests.
func PrepareRequestMessages(msgs []Message, model string) []Message {
	return stripImagesIfNeeded(
		RepairToolMessages(
			SanitizeToolCallArguments(NormalizeMessages(StripResponseFields(msgs), model)),
		),
		model,
	)
}

// SanitizeToolCallArguments ensures assistant tool_call arguments are valid JSON
// objects. Providers reject replayed history when the model emitted malformed
// JSON (e.g. raw newlines inside string literals). The tool result already
// captured the parse error locally, so replacing bad arguments with {} is safe.
func SanitizeToolCallArguments(msgs []Message) []Message {
	if len(msgs) == 0 {
		return msgs
	}
	out := make([]Message, len(msgs))
	for i, msg := range msgs {
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			out[i] = msg
			continue
		}
		copied := msg
		copied.ToolCalls = make([]ToolCall, len(msg.ToolCalls))
		for j, tc := range msg.ToolCalls {
			copied.ToolCalls[j] = tc
			copied.ToolCalls[j].Function.Arguments = validToolArgumentsJSON(tc.Function.Arguments)
		}
		out[i] = copied
	}
	return out
}

func validToolArgumentsJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "{}"
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &obj); err != nil || obj == nil {
		return "{}"
	}
	normalized, err := json.Marshal(obj)
	if err != nil {
		return "{}"
	}
	return string(normalized)
}

func stripImagesIfNeeded(msgs []Message, model string) []Message {
	if SupportsImages(model) {
		return msgs
	}
	out := make([]Message, len(msgs))
	for i, msg := range msgs {
		blocks := ContentBlocks(msg)
		if len(blocks) == 0 {
			out[i] = msg
			continue
		}

		var filtered []ContentBlock
		var imageCount int
		for _, b := range blocks {
			if isImageContentBlock(b) {
				imageCount++
			} else {
				filtered = append(filtered, b)
			}
		}
		if imageCount == 0 {
			out[i] = msg
			continue
		}

		note := fmt.Sprintf("[%d image(s) omitted — current model does not support images.]", imageCount)
		if len(filtered) > 0 && filtered[0].Type == "text" {
			filtered[0].Text = filtered[0].Text + "\n" + note
		} else {
			filtered = append([]ContentBlock{{
				Type: "text",
				Text: note,
			}}, filtered...)
		}

		if len(filtered) == 0 {
			msg.Content = nil
		} else if len(filtered) == 1 && filtered[0].Type == "text" {
			msg.Content = StringContent(filtered[0].Text)
		} else {
			msg.Content = BlocksContent(filtered)
		}
		out[i] = msg
	}
	return out
}

func isImageContentBlock(b ContentBlock) bool {
	return b.Type == "image_url" || b.Type == "image"
}

// RepairToolMessages ensures every assistant tool_call has a matching tool response.
// OpenAI-compatible APIs reject requests when tool_calls are missing replies.
func RepairToolMessages(msgs []Message) []Message {
	out := make([]Message, 0, len(msgs))

	for i := 0; i < len(msgs); i++ {
		msg := msgs[i]
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			for j := range msg.ToolCalls {
				if msg.ToolCalls[j].ID == "" {
					msg.ToolCalls[j].ID = fmt.Sprintf("call_%d_%d", i, j)
				}
			}
		}

		out = append(out, msg)

		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}

		required := make([]ToolCall, len(msg.ToolCalls))
		copy(required, msg.ToolCalls)

		answered := make(map[string]bool)
		i++
		for i < len(msgs) && msgs[i].Role == "tool" {
			tm := msgs[i]
			if tm.ToolCallID == "" {
				tm.ToolCallID = required[0].ID
			}
			if !answered[tm.ToolCallID] {
				out = append(out, tm)
				answered[tm.ToolCallID] = true
			}
			i++
		}
		i--

		for _, tc := range required {
			if answered[tc.ID] {
				continue
			}
			out = append(out, Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
				Content:    StringContent(toolIncompleteMsg),
			})
		}
	}

	return out
}
