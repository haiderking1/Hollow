package agent

import (
	"context"
	"encoding/json"

	"github.com/enough/enough/backend/browser"
	"github.com/enough/enough/backend/opencode"
	"github.com/enough/enough/backend/web"
)

func init() {
	web.BrowserFallback = func(ctx context.Context, url string) (web.PageHit, error) {
		return browser.ScrapeURL(ctx, url)
	}
}

func browserTool() opencode.Tool {
	return opencode.Tool{
		Type: "function",
		Function: opencode.ToolFunction{
			Name: "browser",
			Description: "Control a Chrome/Edge browser over CDP (remote debugging port). " +
				"Auto-launches Chrome/Edge on first use if nothing is listening (disable with ENOUGH_BROWSER_AUTO_LAUNCH=0). " +
				"Tab list/open/close/activate are instant HTTP calls with no polling. " +
				"Use eval with selector/index for clicks (CDP mouse events + download detection), cdp for raw protocol calls. " +
				"Scrape format=elements lists clickable targets or all selector matches before clicking. " +
				"Requires Chrome/Edge. Default CDP URL http://127.0.0.1:9222 (ENOUGH_BROWSER_CDP_URL). " +
				"Override browser binary with ENOUGH_BROWSER_EXECUTABLE if auto-detection fails.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"action": {
						"type": "string",
						"enum": ["list", "open", "close", "activate", "cdp", "eval", "scrape", "download"],
						"description": "Browser action: list/open/close/activate tabs, raw cdp, eval (including clicks), scrape, or download via page fetch"
					},
					"tabId": {
						"type": "string",
						"description": "Target tab id from list/open (defaults to first page tab)"
					},
					"url": {
						"type": "string",
						"description": "URL for open, navigate (cdp Page.navigate), or download"
					},
					"expression": {
						"type": "string",
						"description": "JavaScript for eval. Omit to click selector via eval (document.querySelector(...).click())."
					},
					"method": {
						"type": "string",
						"description": "Raw CDP method name for cdp action"
					},
					"params": {
						"type": "object",
						"description": "Raw CDP params for cdp action"
					},
					"selector": {
						"type": "string",
						"description": "CSS selector (standard CSS only, not jQuery). For eval: click target. For scrape format=elements: list matches."
					},
					"index": {
						"type": "integer",
						"minimum": 0,
						"description": "Zero-based match index when selector matches multiple elements (eval click, default: 0)"
					},
					"format": {
						"type": "string",
						"enum": ["text", "html", "links", "elements"],
						"description": "Scrape output format (default: text). elements lists clickable targets or selector matches."
					},
					"savePath": {
						"type": "string",
						"description": "Output path for download action (relative to cwd unless absolute)"
					},
					"awaitPromise": {
						"type": "boolean",
						"description": "Await promises in eval expressions (default: true)"
					}
				},
				"required": ["action"]
			}`),
		},
	}
}

func (a *Agent) toolBrowser(argsJSON string) toolResult {
	var args browser.BrowserArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}

	out, details, err := browser.ExecuteBrowser(context.Background(), a.workDir, args)
	if err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}

	detailsBytes, _ := json.Marshal(details)
	return toolResult{output: out, details: detailsBytes}
}
