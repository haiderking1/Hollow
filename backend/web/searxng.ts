// PORT: backend/web/searxng.go

import { Effect } from "effect";
import {
  type provider,
  type search_result,
  type search_options,
  default_max_results,
  max_results_cap,
} from "./types";

export class searxng_provider implements provider {
  base_url: string;
  timeout_ms = 20 * 1000;

  constructor(base_url: string) {
    this.base_url = base_url;
  }

  search(
    ctx: AbortSignal,
    query: string,
    opts: search_options,
  ): Effect.Effect<search_result[], Error> {
    return Effect.tryPromise({
      try: async () => {
        let maxResults = opts.maxResults || default_max_results;
        if (maxResults <= 0) {
          maxResults = default_max_results;
        }
        if (maxResults > max_results_cap) {
          maxResults = max_results_cap;
        }

        const buildQuery = build_search_query(query, opts.site ?? "");

        const base = this.base_url.replace(/\/+$/, "");
        const u = new URL(`${base}/search`);
        u.searchParams.set("q", buildQuery);
        u.searchParams.set("format", "json");
        if (opts.engines && typeof opts.engines === "string" && opts.engines.trim() !== "") {
          u.searchParams.set("engines", opts.engines.trim());
        }

        const controller = new AbortController();
        const onAbort = () => controller.abort();
        ctx.addEventListener("abort", onAbort);

        const timeoutId = setTimeout(() => controller.abort(), this.timeout_ms);

        try {
          const resp = await fetch(u.toString(), {
            method: "GET",
            headers: {
              Accept: "application/json",
            },
            signal: controller.signal,
          });

          clearTimeout(timeoutId);
          ctx.removeEventListener("abort", onAbort);

          const bodyText = await resp.text();
          if (resp.status !== 200) {
            throw new Error(`searxng: ${resp.statusText}: ${bodyText.trim()}`);
          }

          const parsed = JSON.parse(bodyText) as {
            results?: Array<{
              title?: string;
              url?: string;
              content?: string;
              engine?: string;
            }>;
          };

          const out: search_result[] = [];
          for (const r of parsed.results ?? []) {
            if (!r.url || url_excluded(r.url, opts.excludeSites)) {
              continue;
            }
            out.push({
              title: (r.title ?? "").trim(),
              url: r.url,
              snippet: (r.content ?? "").trim(),
              engine: (r.engine ?? "").trim(),
            });
            if (out.length >= maxResults) {
              break;
            }
          }

          if (out.length === 0) {
            throw new Error(`searxng: no results for "${query}"`);
          }
          return out;
        } catch (err: any) {
          clearTimeout(timeoutId);
          ctx.removeEventListener("abort", onAbort);
          throw classify_search_error(err);
        }
      },
      catch: (cause) => cause instanceof Error ? cause : new Error(String(cause)),
    });
  }
}

export const build_search_query = (query: string, site?: string): string => {
  const q = query.trim();
  const s = (site ?? "").trim();
  if (s === "") {
    return q;
  }
  const cleanSite = s.replace(/^site:/i, "");
  if (q.toLowerCase().includes("site:")) {
    return q;
  }
  return `site:${cleanSite} ${q}`;
};

export const url_excluded = (rawURL: string, excludes?: string[]): boolean => {
  if (!excludes || excludes.length === 0) {
    return false;
  }
  try {
    const u = new URL(rawURL);
    const host = u.hostname.toLowerCase();
    for (const ex of excludes) {
      let cleanEx = ex.trim().toLowerCase();
      cleanEx = cleanEx.replace(/^www\./i, "");
      if (cleanEx === "") {
        continue;
      }
      if (host === cleanEx || host.endsWith(`.${cleanEx}`)) {
        return true;
      }
    }
  } catch {}
  return false;
};

export const classify_search_error = (err: Error): Error => {
  if (!err) return err;
  const msg = err.message.toLowerCase();
  if (msg.includes("timeout") || msg.includes("deadline") || msg.includes("aborted")) {
    return new Error(`searxng: timeout: ${err.message}`);
  }
  return err;
};

/*
PORT STATUS
source path: backend/web/searxng.go
source lines: 149
confidence: high
status: phase_b_compile
*/
