package opencode

import (
	"encoding/json"
	"strings"
)

type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type ChatRequest struct {
	Model           string          `json:"model"`
	Messages        []Message       `json:"messages"`
	Tools           []Tool          `json:"tools,omitempty"`
	Stream          bool            `json:"stream,omitempty"`
	Thinking        *ThinkingParams `json:"thinking,omitempty"`
	ReasoningEffort string          `json:"reasoning_effort,omitempty"`
	StreamOptions   *StreamOptions  `json:"stream_options,omitempty"`
}

type ChatResponse struct {
	Choices []Choice `json:"choices"`
	Error   *APIError `json:"error,omitempty"`
}

type APIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

type Choice struct {
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Usage struct {
	Input       int `json:"input"`
	Output      int `json:"output"`
	TotalTokens int `json:"totalTokens,omitempty"`
	CacheRead   int `json:"cacheRead,omitempty"`
	CacheWrite  int `json:"cacheWrite,omitempty"`
}

type ContentBlock struct {
	Type     string           `json:"type"`               // "text" | "image_url"
	Text     string           `json:"text,omitempty"`
	ImageURL *ContentImageURL `json:"image_url,omitempty"`
}

type ContentImageURL struct {
	URL string `json:"url"`
}

type Message struct {
	Role             string          `json:"role"`
	Content          json.RawMessage `json:"content"`
	ReasoningContent *string         `json:"reasoning_content,omitempty"`
	ReasoningDetails *string         `json:"reasoning_details,omitempty"`
	ReasoningPlain   *string         `json:"reasoning,omitempty"`
	ToolCalls        []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID       string          `json:"tool_call_id,omitempty"`
	Name             string          `json:"name,omitempty"`
	Usage            *Usage          `json:"usage,omitempty"`
}

func (m Message) GetReasoning() string {
	if m.ReasoningContent != nil {
		return *m.ReasoningContent
	}
	if m.ReasoningDetails != nil {
		return *m.ReasoningDetails
	}
	if m.ReasoningPlain != nil {
		return *m.ReasoningPlain
	}
	return ""
}

func (m *Message) UnmarshalJSON(data []byte) error {
	type Alias Message
	var aux struct {
		Content json.RawMessage `json:"content"`
		*Alias
	}
	aux.Alias = (*Alias)(m)
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	m.Content = aux.Content
	return nil
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func StringContent(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

func BlocksContent(blocks []ContentBlock) json.RawMessage {
	b, _ := json.Marshal(blocks)
	return b
}

func ContentString(m Message) string {
	if len(m.Content) == 0 || string(m.Content) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil {
		return s
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(m.Content, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func ContentBlocks(m Message) []ContentBlock {
	if len(m.Content) == 0 || string(m.Content) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil {
		return []ContentBlock{{Type: "text", Text: s}}
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(m.Content, &blocks); err == nil {
		return blocks
	}
	return nil
}
