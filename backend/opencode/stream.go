package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"time"
)

type streamChunk struct {
	Choices []struct {
		Delta        streamDelta `json:"delta"`
		FinishReason *string     `json:"finish_reason"`
	} `json:"choices"`
	Error *APIError `json:"error"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
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

type streamStatusError struct {
	status int
	msg    string
}

func (e streamStatusError) Error() string { return e.msg }

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
	return c.chatStreamOnce(ctx, req, cb)
}

// ChatStreamRetry is used by swarm worker/planner streams. It removes the
// http.Client timeout ambiguity and retries transport/SSE failures that are
// usually transient at provider edges.
func (c *Client) ChatStreamRetry(ctx context.Context, req ChatRequest, cb StreamCallbacks) (Message, error) {
	workerClient := c.withoutTimeout()
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		msg, err := workerClient.chatStreamOnce(ctx, req, cb)
		if err == nil {
			return msg, nil
		}
		lastErr = err
		if ctx.Err() != nil || !isRetriableStreamError(err) || attempt == 3 {
			return Message{}, err
		}
		delay := 250 * time.Millisecond << attempt
		if delay > 5*time.Second {
			delay = 5 * time.Second
		}
		jitter := time.Duration(rand.Int64N(int64(delay / 4)))
		timer := time.NewTimer(delay + jitter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Message{}, ctx.Err()
		case <-timer.C:
		}
	}
	return Message{}, lastErr
}

func isRetriableStreamError(err error) bool {
	if err == nil {
		return false
	}
	var status streamStatusError
	if errors.As(err, &status) {
		return status.status == http.StatusTooManyRequests ||
			status.status == http.StatusBadGateway ||
			status.status == http.StatusServiceUnavailable
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "connection reset") ||
		strings.Contains(text, "unexpected eof") ||
		strings.Contains(text, "empty sse") ||
		strings.Contains(text, "stream reset")
}

func (c *Client) chatStreamOnce(ctx context.Context, req ChatRequest, cb StreamCallbacks) (Message, error) {
	if c.codex {
		return c.chatResponsesStreamOnce(ctx, req, cb)
	}
	if req.Model == "" {
		req.Model = c.model
	}
	req.Stream = true
	req.StreamOptions = &StreamOptions{IncludeUsage: true}
	req.Messages = PrepareRequestMessages(req.Messages, req.Model)

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
		var msg string
		if apiErr.Error != nil && apiErr.Error.Message != "" {
			msg = fmt.Sprintf("opencode %d: %s", resp.StatusCode, apiErr.Error.Message)
		} else {
			msg = fmt.Sprintf("opencode %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
		}
		return Message{}, streamStatusError{status: resp.StatusCode, msg: msg}
	}

	var content strings.Builder
	var reasoning strings.Builder
	var thinkSplit thinkStreamSplitter
	var lastUsage *Usage
	toolParts := make(map[int]*ToolCall)
	sawData := false

	emitText := func(t string) {
		content.WriteString(t)
		if cb.OnText != nil {
			cb.OnText(t)
		}
	}
	emitThink := func(t string) {
		reasoning.WriteString(t)
		if cb.OnThinking != nil {
			cb.OnThinking(t)
		}
	}

	err = forEachSSEBlock(resp.Body, func(block sseBlock) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if block.Done {
			return ErrSSEDone
		}
		data := block.Data
		if data == "" {
			return nil
		}
		sawData = true

		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return nil
		}
		if chunk.Error != nil && chunk.Error.Message != "" {
			return fmt.Errorf("opencode: %s", chunk.Error.Message)
		}
		if chunk.Usage != nil {
			lastUsage = &Usage{
				Input:       chunk.Usage.PromptTokens,
				Output:      chunk.Usage.CompletionTokens,
				TotalTokens: chunk.Usage.TotalTokens,
			}
		}
		if len(chunk.Choices) == 0 {
			return nil
		}

		delta := chunk.Choices[0].Delta
		if delta.Content != nil && *delta.Content != "" {
			thinkSplit.feed(*delta.Content, emitText, emitThink)
		}

		if r := reasoningDelta(delta); r != "" {
			emitThink(r)
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
		return nil
	})
	if err != nil {
		return Message{}, err
	}
	if !sawData {
		return Message{}, fmt.Errorf("opencode: empty SSE body")
	}

	thinkSplit.flush(emitText, emitThink)

	msg := Message{Role: "assistant", Usage: lastUsage}
	if content.Len() > 0 {
		s := content.String()
		msg.Content = StringContent(s)
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
				if tc.ID == "" {
					tc.ID = fmt.Sprintf("stream_call_%d", i)
				}
				if tc.Type == "" {
					tc.Type = "function"
				}
				msg.ToolCalls = append(msg.ToolCalls, *tc)
			}
		}
	}

	SanitizeEmbeddedThinking(&msg)

	return msg, nil
}
