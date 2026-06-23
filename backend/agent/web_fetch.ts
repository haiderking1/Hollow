// PORT: backend/agent/web_fetch.go

import { Effect } from "effect";
import { type tool } from "../opencode/types";
import { Agent, type toolResult } from "./agent";
import { normalize_fetch_urls } from "../web/fetch";
import { fetch_urls, format_pages } from "../web/search";

export function webFetchTool(): tool {
  const schema = {
    type: "object",
    properties: {
      urls: {
        type: "array",
        items: { type: "string" },
        description: "URLs to fetch (max 5)"
      },
      url: {
        type: "string",
        description: "Single URL (alternative to urls array)"
      }
    }
  };
  return {
    type: "function",
    function: {
      name: "web_fetch",
      description: "Fetch and extract readable text from one or more http(s) URLs. Uses readability with meta/HTML fallbacks. Returns structured errors: js_rendered (JavaScript-only page), blocked, timeout, rate_limited, no_content. Use after web_search to read pages you chose from snippets.",
      parameters: new TextEncoder().encode(JSON.stringify(schema)),
    },
  };
}

Agent.prototype.toolWebFetch = function (
  this: Agent,
  ctx: AbortSignal,
  argsJSON: string
): Effect.Effect<toolResult, Error> {
  return Effect.gen(this, function* () {
    let args: { urls?: string[]; url?: string };
    try {
      args = JSON.parse(argsJSON);
    } catch (err) {
      return { output: err instanceof Error ? err.message : String(err), isErr: true };
    }

    let urls: string[] = args.urls ? [...args.urls] : [];
    if (args.url && args.url.trim() !== "") {
      urls.push(args.url.trim());
    }

    urls = normalize_fetch_urls(urls);
    if (urls.length === 0) {
      return { output: "no valid http(s) urls provided", isErr: true };
    }

    const hits = yield* fetch_urls(ctx, urls);
    if (ctx.aborted) {
      return { output: "[interrupted]", isErr: true };
    }

    return {
      output: format_pages(hits),
    };
  }).pipe(
    Effect.catchAll((err: any) =>
      Effect.succeed({ output: err?.message || String(err), isErr: true })
    )
  );
};

/*
PORT STATUS
source path: backend/agent/web_fetch.go
source lines: 63
draft lines: 66
confidence: high
status: phase_b_compile
*/
