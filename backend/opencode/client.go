package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	apiKey     string
	model      string
	codex      bool
	httpClient *http.Client
}

func NewClient(baseURL, apiKey, model string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// NewCodexClient creates a client for the ChatGPT Codex Responses API.
func NewCodexClient(baseURL, accessToken, model string) *Client {
	c := NewClient(baseURL, accessToken, model)
	c.codex = true
	return c
}

func (c *Client) withoutTimeout() *Client {
	cp := *c
	if c.httpClient != nil {
		next := *c.httpClient
		next.Timeout = 0
		cp.httpClient = &next
	}
	return &cp
}

func (c *Client) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if c.codex {
		msg, err := c.chatResponsesOnce(ctx, req)
		if err != nil {
			return ChatResponse{}, err
		}
		return ChatResponse{Choices: []Choice{{Message: msg, FinishReason: "stop"}}}, nil
	}
	if req.Model == "" {
		req.Model = c.model
	}
	req.Messages = PrepareRequestMessages(req.Messages, req.Model)

	body, err := json.Marshal(req)
	if err != nil {
		return ChatResponse{}, err
	}

	url := c.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return ChatResponse{}, err
	}

	if resp.StatusCode >= 400 {
		var apiErr ChatResponse
		_ = json.Unmarshal(raw, &apiErr)
		if apiErr.Error != nil && apiErr.Error.Message != "" {
			return ChatResponse{}, fmt.Errorf("opencode %d: %s", resp.StatusCode, apiErr.Error.Message)
		}
		return ChatResponse{}, fmt.Errorf("opencode %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var out ChatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return ChatResponse{}, fmt.Errorf("decode response: %w", err)
	}
	if out.Error != nil && out.Error.Message != "" {
		return ChatResponse{}, fmt.Errorf("opencode: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return ChatResponse{}, fmt.Errorf("opencode: empty response")
	}

	for i := range out.Choices {
		SanitizeEmbeddedThinking(&out.Choices[i].Message)
	}

	return out, nil
}
