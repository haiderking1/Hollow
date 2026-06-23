// PORT: backend/agent/browser.go

import { Effect } from "effect";
import { type tool } from "../opencode/types";
import { Agent, type toolResult } from "./agent";
import { set_browser_fallback } from "../web/fetch";
import { scrape_url, ExecuteBrowser, type BrowserArgs } from "../browser/tool";

// Initialize browser fallback in web package
set_browser_fallback(scrape_url);

export function browserTool(): tool {
  const schema = {
    type: "object",
    properties: {
      action: {
        type: "string",
        enum: ["list", "open", "close", "activate", "cdp", "eval", "scrape", "download"],
        description: "Browser action: list/open/close/activate tabs, raw cdp, eval (including clicks), scrape, or download via page fetch"
      },
      tabId: {
        type: "string",
        description: "Target tab id from list/open (defaults to first page tab)"
      },
      url: {
        type: "string",
        description: "URL for open, navigate (cdp Page.navigate), or download"
      },
      expression: {
        type: "string",
        description: "JavaScript for eval. Omit to click selector via eval (document.querySelector(...).click())."
      },
      method: {
        type: "string",
        description: "Raw CDP method name for cdp action"
      },
      params: {
        type: "object",
        description: "Raw CDP params for cdp action"
      },
      selector: {
        type: "string",
        description: "CSS selector (standard CSS only, not jQuery). For eval: click target. For scrape format=elements: list matches."
      },
      index: {
        type: "integer",
        minimum: 0,
        description: "Zero-based match index when selector matches multiple elements (eval click, default: 0)"
      },
      format: {
        type: "string",
        enum: ["text", "html", "links", "elements"],
        description: "Scrape output format (default: text). elements lists clickable targets or selector matches."
      },
      savePath: {
        type: "string",
        description: "Output path for download action (relative to cwd unless absolute)"
      },
      awaitPromise: {
        type: "boolean",
        description: "Await promises in eval expressions (default: true)"
      }
    },
    required: ["action"]
  };

  return {
    type: "function",
    function: {
      name: "browser",
      description: "Control a Chrome/Edge browser over CDP (remote debugging port). " +
        "Auto-launches Chrome/Edge on first use if nothing is listening (disable with HOLLOW_BROWSER_AUTO_LAUNCH=0). " +
        "Tab list/open/close/activate are instant HTTP calls with no polling. " +
        "Use eval with selector/index for clicks (CDP mouse events + download detection), cdp for raw protocol calls. " +
        "Scrape format=elements lists clickable targets or all selector matches before clicking. " +
        "Requires Chrome/Edge. Default CDP URL http://127.0.0.1:9222 (HOLLOW_BROWSER_CDP_URL). " +
        "Override browser binary with HOLLOW_BROWSER_EXECUTABLE if auto-detection fails.",
      parameters: new TextEncoder().encode(JSON.stringify(schema)),
    }
  };
}

Agent.prototype.toolBrowser = function (
  this: Agent,
  ctx: AbortSignal,
  argsJSON: string
): Effect.Effect<toolResult, Error> {
  return Effect.gen(this, function* () {
    let args: BrowserArgs;
    try {
      args = JSON.parse(argsJSON);
    } catch (err) {
      return { output: err instanceof Error ? err.message : String(err), isErr: true };
    }

    const [out, details] = yield* ExecuteBrowser(ctx, this.workDir, args);
    if (ctx.aborted) {
      return { output: "[interrupted]", isErr: true };
    }

    let detailsBytes: string | undefined;
    try {
      detailsBytes = JSON.stringify(details);
    } catch {}

    return {
      output: out,
      details: detailsBytes
    };
  }).pipe(
    Effect.catchAll((err: any) => {
      if (ctx.aborted) {
        return Effect.succeed({ output: "[interrupted]", isErr: true });
      }
      return Effect.succeed({ output: err?.message || String(err), isErr: true });
    })
  );
};

/*
PORT STATUS
source path: backend/agent/browser.go
source lines: 104
draft lines: 111
confidence: high
status: phase_b_compile
*/
