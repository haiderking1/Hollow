// PORT: backend/web/search.go

import { Effect } from "effect";
import {
  type search_result,
  type search_options,
  type page_hit,
  ErrEmptyInput,
  max_output_bytes,
} from "./types";
import { trim_input, new_search_provider } from "./provider";
import { is_http_url } from "./url_guard";
import { fetch_urls_parallel } from "./fetch_batch";

export const search_web = (
  ctx: AbortSignal,
  query: string,
  opts: search_options,
): Effect.Effect<search_result[], Error> => {
  const cleanQuery = trim_input(query);
  if (cleanQuery === "") {
    return Effect.fail(ErrEmptyInput);
  }
  if (is_http_url(cleanQuery)) {
    return Effect.succeed([
      {
        title: cleanQuery,
        url: cleanQuery,
        snippet: "",
        engine: "",
      },
    ]);
  }

  return new_search_provider(ctx).pipe(
    Effect.flatMap((prov) => prov.search(ctx, cleanQuery, opts)),
  );
};

export const fetch_urls = (
  ctx: AbortSignal,
  urls: string[],
): Effect.Effect<page_hit[], never> => {
  return fetch_urls_parallel(ctx, urls);
};

export const format_search_results = (results: search_result[]): string => {
  const lines: string[] = [];
  for (let i = 0; i < results.length; i++) {
    if (i > 0) {
      lines.push("");
    }
    const r = results[i];
    const title = r.title || r.url;
    lines.push(`${i + 1}. ${title}`);
    lines.push(`   URL: ${r.url}`);
    if (r.engine !== "") {
      lines.push(`   Engine: ${r.engine}`);
    }
    if (r.snippet !== "") {
      lines.push(`   Snippet: ${r.snippet}`);
    }
  }

  let out = lines.join("\n");
  if (out.length > max_output_bytes) {
    out = out.slice(0, max_output_bytes) + "\n\n... truncated ...";
  }
  return out;
};

export const format_pages = (hits: page_hit[]): string => {
  const lines: string[] = [];
  for (let i = 0; i < hits.length; i++) {
    if (i > 0) {
      lines.push("");
    }
    const hit = hits[i];
    const title = hit.title || hit.url;
    lines.push(`=== ${title} ===`);
    lines.push(`URL: ${hit.url}\n`);
    if (hit.fetch !== null) {
      lines.push(`Error ${hit.fetch.Error()}`);
      continue;
    }
    lines.push(hit.content);
  }

  let out = lines.join("\n");
  if (out.length > max_output_bytes) {
    out = out.slice(0, max_output_bytes) + "\n\n... truncated ...";
  }
  return out;
};

/*
PORT STATUS
source path: backend/web/search.go
source lines: 86
confidence: high
status: phase_b_compile
*/
