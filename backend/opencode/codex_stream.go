package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type codexStreamState struct {
	collectedItems  []responsesItem
	textDeltas      []string
	reasoningDeltas []string
	hasToolCalls    bool
	terminalStatus  string
	terminalUsage   *responsesUsage
	terminalError   *responsesError
	sawTerminal     bool
	sawData         bool
}

func (c *Client) chatResponsesStreamOnce(ctx context.Context, req ChatRequest, cb StreamCallbacks) (Message, error) {
	bodyReq, err := c.buildResponsesRequest(req)
	if err != nil {
		return Message{}, err
	}
	bodyReq.Stream = true

	body, err := json.Marshal(bodyReq)
	if err != nil {
		return Message{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.responsesURL(), bytes.NewReader(body))
	if err != nil {
		return Message{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")
	for k, v := range codexRequestHeaders(c.apiKey) {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return Message{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return Message{}, streamStatusError{
			status: resp.StatusCode,
			msg:    fmt.Sprintf("codex %d: %s", resp.StatusCode, strings.TrimSpace(string(raw))),
		}
	}

	state, err := consumeCodexResponsesSSE(resp.Body, cb)
	if err != nil {
		return Message{}, err
	}
	if !state.sawData {
		return Message{}, fmt.Errorf("codex: empty SSE body")
	}

	return messageFromCodexStreamState(state)
}

func consumeCodexResponsesSSE(r io.Reader, cb StreamCallbacks) (codexStreamState, error) {
	var state codexStreamState
	state.terminalStatus = "completed"

	err := forEachSSEBlock(r, func(block sseBlock) error {
		if block.Done {
			return ErrSSEDone
		}
		data := block.Data
		if data == "" {
			return nil
		}
		state.sawData = true

		var event map[string]json.RawMessage
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return nil
		}

		eventType := block.EventType
		if rawType, ok := event["type"]; ok {
			var typed string
			if err := json.Unmarshal(rawType, &typed); err == nil && typed != "" {
				eventType = typed
			}
		}

		if err := applyCodexStreamEvent(&state, eventType, event, cb); err != nil {
			return err
		}
		if state.sawTerminal {
			return ErrSSEDone
		}
		return nil
	})
	if err != nil {
		return state, err
	}

	if !state.sawTerminal && len(state.collectedItems) == 0 && len(state.textDeltas) == 0 {
		return state, fmt.Errorf("codex: stream ended without terminal response or content")
	}
	return state, nil
}

func applyCodexStreamEvent(state *codexStreamState, eventType string, event map[string]json.RawMessage, cb StreamCallbacks) error {
	if eventType == "error" {
		msg := "codex stream error"
		if raw, ok := event["message"]; ok {
			var s string
			if json.Unmarshal(raw, &s) == nil && s != "" {
				msg = s
			}
		}
		return fmt.Errorf("codex: %s", msg)
	}

	if strings.Contains(eventType, "output_text.delta") || eventType == "response.output_text.delta" {
		delta := codexEventString(event, "delta")
		if delta == "" {
			return nil
		}
		state.textDeltas = append(state.textDeltas, delta)
		if !state.hasToolCalls && cb.OnText != nil {
			cb.OnText(delta)
		}
		return nil
	}

	if strings.Contains(eventType, "reasoning") && strings.Contains(eventType, "delta") {
		delta := codexEventString(event, "delta")
		if delta == "" {
			return nil
		}
		state.reasoningDeltas = append(state.reasoningDeltas, delta)
		if cb.OnThinking != nil {
			cb.OnThinking(delta)
		}
		return nil
	}

	if strings.Contains(eventType, "function_call") {
		state.hasToolCalls = true
	}

	if eventType == "response.output_item.done" {
		if raw, ok := event["item"]; ok {
			var item responsesItem
			if err := json.Unmarshal(raw, &item); err == nil {
				state.collectedItems = append(state.collectedItems, item)
				if item.Type == "function_call" {
					state.hasToolCalls = true
				}
			}
		}
		return nil
	}

	switch eventType {
	case "response.completed", "response.incomplete", "response.failed":
		state.sawTerminal = true
		if raw, ok := event["response"]; ok {
			var terminal struct {
				Status string           `json:"status"`
				Usage  *responsesUsage  `json:"usage"`
				Error  *responsesError  `json:"error"`
			}
			if err := json.Unmarshal(raw, &terminal); err == nil {
				if terminal.Status != "" {
					state.terminalStatus = terminal.Status
				}
				state.terminalUsage = terminal.Usage
				state.terminalError = terminal.Error
			}
		}
		switch eventType {
		case "response.completed":
			if state.terminalStatus == "" {
				state.terminalStatus = "completed"
			}
		case "response.incomplete":
			if state.terminalStatus == "" {
				state.terminalStatus = "incomplete"
			}
		case "response.failed":
			if state.terminalStatus == "" {
				state.terminalStatus = "failed"
			}
		}
	}
	return nil
}

func codexEventString(event map[string]json.RawMessage, key string) string {
	raw, ok := event[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func messageFromCodexStreamState(state codexStreamState) (Message, error) {
	resp := responsesResponse{
		Status: state.terminalStatus,
		Output: state.collectedItems,
		Error:  state.terminalError,
		Usage:  state.terminalUsage,
	}
	if len(state.textDeltas) > 0 {
		resp.OutputText = strings.Join(state.textDeltas, "")
	}
	msg, err := parseResponsesMessage(resp)
	if err != nil {
		return Message{}, err
	}
	if msg.ReasoningContent == nil && len(state.reasoningDeltas) > 0 {
		s := strings.Join(state.reasoningDeltas, "")
		msg.ReasoningContent = &s
	}
	return msg, nil
}
