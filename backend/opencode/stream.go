package opencode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type streamChunk struct {
	Choices []struct {
		Delta        streamDelta `json:"delta"`
		FinishReason *string     `json:"finish_reason"`
	} `json:"choices"`
	Error *APIError `json:"error"`
}

type streamDelta struct {
	Role             string            `json:"role,omitempty"`
	Content          *string           `json:"content,omitempty"`
	ReasoningContent *string           `json:"reasoning_content,omitempty"`
	Reasoning        *string           `json:"reasoning,omitempty"`
	ReasoningText    *string           `json:"reasoning_text,omitempty"`
	ToolCalls        []toolCallPartial `json:"tool_calls,omitempty"`
}

type StreamCallbacks struct {
	OnText     func(string)
	OnThinking func(string)
}

type toolCallPartial struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function,omitempty"`
}

func reasoningDelta(d streamDelta) string {
	for _, s := range []*string{d.ReasoningContent, d.Reasoning, d.ReasoningText} {
		if s != nil && *s != "" {
			return *s
		}
	}
	return ""
}

// ChatStream streams an assistant reply with separate text and thinking deltas.
func (c *Client) ChatStream(ctx context.Context, req ChatRequest, cb StreamCallbacks) (Message, error) {
	if req.Model == "" {
		req.Model = c.model
	}
	req.Stream = true
	req.Messages = NormalizeMessages(req.Messages, req.Model)

	body, err := json.Marshal(req)
	if err != nil {
		return Message{}, err
	}

	url := c.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Message{}, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return Message{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		var apiErr ChatResponse
		_ = json.Unmarshal(raw, &apiErr)
		if apiErr.Error != nil && apiErr.Error.Message != "" {
			return Message{}, fmt.Errorf("opencode %d: %s", resp.StatusCode, apiErr.Error.Message)
		}
		return Message{}, fmt.Errorf("opencode %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var content strings.Builder
	var reasoning strings.Builder
	toolParts := make(map[int]*ToolCall)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Error != nil && chunk.Error.Message != "" {
			return Message{}, fmt.Errorf("opencode: %s", chunk.Error.Message)
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta
		if delta.Content != nil && *delta.Content != "" {
			content.WriteString(*delta.Content)
			if cb.OnText != nil {
				cb.OnText(*delta.Content)
			}
		}

		if r := reasoningDelta(delta); r != "" {
			reasoning.WriteString(r)
			if cb.OnThinking != nil {
				cb.OnThinking(r)
			}
		}

		for _, tc := range delta.ToolCalls {
			part, ok := toolParts[tc.Index]
			if !ok {
				part = &ToolCall{Type: "function"}
				toolParts[tc.Index] = part
			}
			if tc.ID != "" {
				part.ID = tc.ID
			}
			if tc.Type != "" {
				part.Type = tc.Type
			}
			if tc.Function.Name != "" {
				part.Function.Name = tc.Function.Name
			}
			part.Function.Arguments += tc.Function.Arguments
		}
	}
	if err := scanner.Err(); err != nil {
		return Message{}, err
	}

	msg := Message{Role: "assistant"}
	if content.Len() > 0 {
		s := content.String()
		msg.Content = &s
	}
	if reasoning.Len() > 0 {
		s := reasoning.String()
		msg.ReasoningContent = &s
	}

	if len(toolParts) > 0 {
		maxIdx := -1
		for i := range toolParts {
			if i > maxIdx {
				maxIdx = i
			}
		}
		for i := 0; i <= maxIdx; i++ {
			if tc, ok := toolParts[i]; ok {
				msg.ToolCalls = append(msg.ToolCalls, *tc)
			}
		}
	}

	return msg, nil
}
