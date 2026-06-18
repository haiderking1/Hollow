package agent

import (
	"context"
	"encoding/json"

	"github.com/enough/enough/backend/opencode"
	"github.com/enough/enough/backend/web"
)

func webSearchTool() opencode.Tool {
	return opencode.Tool{
		Type: "function",
		Function: opencode.ToolFunction{
			Name: "web_search",
			Description: "Search the web via bundled SearXNG. Returns numbered results with title, URL, engine, and snippet — does NOT fetch full pages. " +
				"Use web_fetch on URLs you want to read in full. Pass a full http(s) URL to list that URL only (then web_fetch it).",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {
						"type": "string",
						"description": "Search query or full URL"
					},
					"max_results": {
						"type": "integer",
						"description": "Max search results (default 8, max 15)"
					},
					"site": {
						"type": "string",
						"description": "Restrict to a domain, e.g. reddit.com (adds site: operator)"
					},
					"exclude_sites": {
						"type": "array",
						"items": { "type": "string" },
						"description": "Drop results from these domains, e.g. [\"fandom.com\"]"
					},
					"engines": {
						"type": "string",
						"description": "Comma-separated SearXNG engines, e.g. google,duckduckgo"
					}
				},
				"required": ["query"]
			}`),
		},
	}
}

func (a *Agent) toolWebSearch(ctx context.Context, argsJSON string) toolResult {
	var args struct {
		Query        string   `json:"query"`
		MaxResults   int      `json:"max_results"`
		Site         string   `json:"site"`
		ExcludeSites []string `json:"exclude_sites"`
		Engines      string   `json:"engines"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}

	results, err := web.SearchWeb(ctx, args.Query, web.SearchOptions{
		MaxResults:   args.MaxResults,
		Site:         args.Site,
		ExcludeSites: args.ExcludeSites,
		Engines:      args.Engines,
	})
	if err != nil {
		if ctx.Err() != nil {
			return toolResult{output: "[interrupted]", isErr: true}
		}
		return toolResult{output: err.Error(), isErr: true}
	}
	return toolResult{output: web.FormatSearchResults(results)}
}
