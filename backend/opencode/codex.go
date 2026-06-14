package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/enough/enough/backend/auth"
)

func codexRequestHeaders(accessToken string) map[string]string {
	return auth.CodexCloudflareHeaders(accessToken)
}

type responsesRequest struct {
	Model        string              `json:"model"`
	Instructions string              `json:"instructions"`
	Input        any                 `json:"input"`
	Tools        any                 `json:"tools,omitempty"`
	Store        bool                `json:"store"`
	Stream       bool                `json:"stream,omitempty"`
	Reasoning    *responsesReasoning `json:"reasoning,omitempty"`
}

type responsesReasoning struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type responsesResponse struct {
	Status     string           `json:"status"`
	Output     []responsesItem  `json:"output"`
	OutputText string           `json:"output_text,omitempty"`
	Error      *responsesError  `json:"error,omitempty"`
	Usage      *responsesUsage  `json:"usage,omitempty"`
}

type responsesError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type responsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type responsesItem struct {
	Type      string `json:"type"`
	Role      string `json:"role,omitempty"`
	Status    string `json:"status,omitempty"`
	Name      string `json:"name,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	ID        string `json:"id,omitempty"`
	Content   []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content,omitempty"`
	Summary []struct {
		Text string `json:"text"`
	} `json:"summary,omitempty"`
}

func (c *Client) responsesURL() string {
	base := strings.TrimRight(c.baseURL, "/")
	if strings.HasSuffix(base, "/responses") {
		return base
	}
	return base + "/responses"
}

func (c *Client) buildResponsesRequest(req ChatRequest) (responsesRequest, error) {
	if req.Model == "" {
		req.Model = c.model
	}
	msgs := PrepareRequestMessages(req.Messages, req.Model)
	instructions, convMsgs := splitInstructionsMessages(msgs)
	if strings.TrimSpace(instructions) == "" {
		instructions = "You are a helpful coding assistant."
	}
	input := messagesToResponsesInput(convMsgs)
	tools := chatToolsToResponses(req.Tools)

	out := responsesRequest{
		Model:        req.Model,
		Instructions: instructions,
		Input:        input,
		Tools:        tools,
		Store:        false,
		Reasoning:    reasoningFromChatRequest(req),
	}
	return out, nil
}

func splitInstructionsMessages(msgs []Message) (instructions string, rest []Message) {
	var parts []string
	for _, msg := range msgs {
		switch msg.Role {
		case "system", "developer":
			if text := strings.TrimSpace(ContentString(msg)); text != "" {
				parts = append(parts, text)
			}
		default:
			rest = append(rest, msg)
		}
	}
	return strings.Join(parts, "\n\n"), rest
}

func reasoningFromChatRequest(req ChatRequest) *responsesReasoning {
	if req.Thinking != nil && req.Thinking.Type == "disabled" {
		return nil
	}
	effort := req.ReasoningEffort
	if effort == "" {
		return nil
	}
	if effort == "max" {
		effort = "xhigh"
	}
	return &responsesReasoning{Effort: effort, Summary: "auto"}
}

func (c *Client) chatResponsesOnce(ctx context.Context, req ChatRequest) (Message, error) {
	bodyReq, err := c.buildResponsesRequest(req)
	if err != nil {
		return Message{}, err
	}

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
	for k, v := range codexRequestHeaders(c.apiKey) {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return Message{}, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Message{}, err
	}
	if resp.StatusCode >= 400 {
		return Message{}, fmt.Errorf("codex %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var out responsesResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return Message{}, fmt.Errorf("decode codex response: %w", err)
	}

	return parseResponsesMessage(out)
}

func messagesToResponsesInput(msgs []Message) []any {
	var items []any
	for _, msg := range msgs {
		switch msg.Role {
		case "system":
			continue
		case "user":
			blocks := ContentBlocks(msg)
			hasImage := false
			for _, b := range blocks {
				if b.Type == "image_url" {
					hasImage = true
					break
				}
			}
			if hasImage {
				var content []map[string]any
				for _, b := range blocks {
					switch b.Type {
					case "text":
						if b.Text != "" {
							content = append(content, map[string]any{"type": "input_text", "text": b.Text})
						}
					case "image_url":
						if b.ImageURL != nil {
							content = append(content, map[string]any{
								"type":      "input_image",
								"image_url": b.ImageURL.URL,
								"detail":    "auto",
							})
						}
					}
				}
				items = append(items, map[string]any{
					"role":    "user",
					"content": content,
				})
			} else {
				text := ContentString(msg)
				items = append(items, map[string]any{
					"role":    "user",
					"content": text,
				})
			}
		case "assistant":
			text := ContentString(msg)
			if text != "" {
				items = append(items, map[string]any{
					"role": "assistant",
					"content": []map[string]string{
						{"type": "output_text", "text": text},
					},
				})
			}
			for _, tc := range msg.ToolCalls {
				callID := tc.ID
				if callID == "" {
					callID = fmt.Sprintf("call_%d", len(items))
				}
				items = append(items, map[string]any{
					"type":      "function_call",
					"call_id":   callID,
					"name":      tc.Function.Name,
					"arguments": tc.Function.Arguments,
				})
			}
		case "tool":
			callID := msg.ToolCallID
			if callID == "" {
				continue
			}
			blocks := ContentBlocks(msg)
			hasImage := false
			for _, b := range blocks {
				if b.Type == "image_url" {
					hasImage = true
					break
				}
			}
			if hasImage {
				var outputItems []map[string]any
				for _, b := range blocks {
					if b.Type == "image_url" && b.ImageURL != nil {
						outputItems = append(outputItems, map[string]any{
							"type":      "input_image",
							"image_url": b.ImageURL.URL,
						})
					} else {
						outputItems = append(outputItems, map[string]any{
							"type": "input_text",
							"text": b.Text,
						})
					}
				}
				items = append(items, map[string]any{
					"type":    "function_call_output",
					"call_id": callID,
					"output":  outputItems,
				})
			} else {
				items = append(items, map[string]any{
					"type":    "function_call_output",
					"call_id": callID,
					"output":  ContentString(msg),
				})
			}
		}
	}
	return items
}

func chatToolsToResponses(tools []Tool) any {
	if len(tools) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		if t.Function.Name == "" {
			continue
		}
		params := t.Function.Parameters
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, map[string]any{
			"type":        "function",
			"name":        t.Function.Name,
			"description": t.Function.Description,
			"strict":      false,
			"parameters":  json.RawMessage(params),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseResponsesMessage(resp responsesResponse) (Message, error) {
	status := strings.ToLower(strings.TrimSpace(resp.Status))
	if status == "failed" || status == "cancelled" {
		msg := "codex request failed"
		if resp.Error != nil && resp.Error.Message != "" {
			msg = resp.Error.Message
		}
		return Message{}, fmt.Errorf("codex: %s", msg)
	}

	msg := Message{Role: "assistant"}
	if resp.Usage != nil {
		msg.Usage = &Usage{
			Input:  resp.Usage.InputTokens,
			Output: resp.Usage.OutputTokens,
			TotalTokens: resp.Usage.TotalTokens,
		}
	}

	output := resp.Output
	if len(output) == 0 && strings.TrimSpace(resp.OutputText) != "" {
		s := strings.TrimSpace(resp.OutputText)
		msg.Content = StringContent(s)
		return msg, nil
	}
	if len(output) == 0 {
		return Message{}, fmt.Errorf("codex: empty response")
	}

	var textParts []string
	var reasoningParts []string
	for _, item := range output {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" || part.Type == "text" {
					if part.Text != "" {
						textParts = append(textParts, part.Text)
					}
				}
			}
		case "reasoning":
			for _, part := range item.Summary {
				if part.Text != "" {
					reasoningParts = append(reasoningParts, part.Text)
				}
			}
		case "function_call":
			if item.Status != "" && item.Status != "completed" {
				continue
			}
			callID := item.CallID
			if callID == "" && strings.HasPrefix(item.ID, "fc_") {
				callID = "call_" + strings.TrimPrefix(item.ID, "fc_")
			}
			if callID == "" {
				callID = fmt.Sprintf("call_%d", len(msg.ToolCalls))
			}
			args := item.Arguments
			if args == "" {
				args = "{}"
			}
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{
				ID:   callID,
				Type: "function",
				Function: ToolCallFunction{
					Name:      item.Name,
					Arguments: args,
				},
			})
		}
	}

	if len(textParts) > 0 {
		s := strings.Join(textParts, "")
		msg.Content = StringContent(s)
	}
	if len(reasoningParts) > 0 {
		s := strings.Join(reasoningParts, "\n")
		msg.ReasoningContent = &s
	}

	if len(msg.Content) == 0 && len(msg.ToolCalls) == 0 {
		return Message{}, fmt.Errorf("codex: no assistant output")
	}
	return msg, nil
}
