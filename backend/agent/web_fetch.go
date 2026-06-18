package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/enough/enough/backend/opencode"
	"github.com/enough/enough/backend/web"
)

func webFetchTool() opencode.Tool {
	return opencode.Tool{
		Type: "function",
		Function: opencode.ToolFunction{
			Name: "web_fetch",
			Description: "Fetch and extract readable text from one or more http(s) URLs. " +
				"Uses readability with meta/HTML fallbacks. Returns structured errors: " +
				"js_rendered (JavaScript-only page), blocked, timeout, rate_limited, no_content. " +
				"Use after web_search to read pages you chose from snippets.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"urls": {
						"type": "array",
						"items": { "type": "string" },
						"description": "URLs to fetch (max 5)"
					},
					"url": {
						"type": "string",
						"description": "Single URL (alternative to urls array)"
					}
				}
			}`),
		},
	}
}

func (a *Agent) toolWebFetch(ctx context.Context, argsJSON string) toolResult {
	var args struct {
		URLs []string `json:"urls"`
		URL  string   `json:"url"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}

	urls := append([]string(nil), args.URLs...)
	if u := strings.TrimSpace(args.URL); u != "" {
		urls = append(urls, u)
	}
	urls = web.NormalizeFetchURLs(urls)
	if len(urls) == 0 {
		return toolResult{output: "no valid http(s) urls provided", isErr: true}
	}

	hits := web.FetchURLs(ctx, urls)
	if ctx.Err() != nil {
		return toolResult{output: "[interrupted]", isErr: true}
	}
	return toolResult{output: web.FormatPages(hits)}
}
