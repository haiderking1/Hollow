// PORT: backend/agent/web_search.go

import { Effect } from "effect";
import { type tool } from "../opencode/types";
import { Agent, type toolResult } from "./agent";
import { search_web, format_search_results } from "../web/search";

export function webSearchTool(): tool {
  const schema = {
    type: "object",
    properties: {
      query: {
        type: "string",
        description: "Search query or full URL"
      },
      max_results: {
        type: "integer",
        description: "Max search results (default 8, max 15)"
      },
      site: {
        type: "string",
        description: "Restrict to a domain, e.g. reddit.com (adds site: operator)"
      },
      exclude_sites: {
        type: "array",
        items: { type: "string" },
        description: "Drop results from these domains, e.g. [\"fandom.com\"]"
      },
      engines: {
        type: "string",
        description: "Comma-separated SearXNG engines, e.g. google,duckduckgo"
      }
    },
    required: ["query"]
  };
  return {
    type: "function",
    function: {
      name: "web_search",
      description: "Search the web via bundled SearXNG. Returns numbered results with title, URL, engine, and snippet — does NOT fetch full pages. Use web_fetch on URLs you want to read in full. Pass a full http(s) URL to list that URL only (then web_fetch it).",
      parameters: new TextEncoder().encode(JSON.stringify(schema)),
    },
  };
}

Agent.prototype.toolWebSearch = function (
  this: Agent,
  ctx: AbortSignal,
  argsJSON: string
): Effect.Effect<toolResult, Error> {
  return Effect.gen(this, function* () {
    let args: {
      query: string;
      max_results?: number;
      site?: string;
      exclude_sites?: string[];
      engines?: string;
    };
    try {
      args = JSON.parse(argsJSON);
    } catch (err) {
      return { output: err instanceof Error ? err.message : String(err), isErr: true };
    }

    if (!args.query) {
      return { output: "query is required", isErr: true };
    }

    const results = yield* search_web(ctx, args.query, {
      maxResults: args.max_results || 8,
      site: args.site || "",
      excludeSites: args.exclude_sites || [],
      engines: args.engines || "",
    });

    if (ctx.aborted) {
      return { output: "[interrupted]", isErr: true };
    }

    return {
      output: format_search_results(results),
    };
  }).pipe(
    Effect.catchAll((err) =>
      Effect.succeed({ output: err.message, isErr: true })
    )
  );
};

/*
PORT STATUS
source path: backend/agent/web_search.go
source lines: 75
draft lines: 80
confidence: high
status: phase_b_compile
*/
