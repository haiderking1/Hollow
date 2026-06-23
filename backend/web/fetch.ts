// PORT: backend/web/fetch.go

import { Effect } from "effect";
import { type page_hit, fetch_error, fetch_failure_kind, max_fetch_cap } from "./types";
import { is_http_url, validate_fetch_url } from "./url_guard";
import {
  is_youtube_host,
  fetch_youtube_oembed,
  download_html,
  extract_page_content,
} from "./extract";

export let browser_fallback:
  | ((ctx: AbortSignal, url: string) => Effect.Effect<page_hit, Error>)
  | null = null;

export function set_browser_fallback(
  fn: (ctx: AbortSignal, url: string) => Effect.Effect<page_hit, Error>
) {
  browser_fallback = fn;
}

export const fetch_page = (
  ctx: AbortSignal,
  rawURL: string,
): Effect.Effect<page_hit, Error> => {
  if (ctx.aborted) {
    return Effect.fail(new Error("context canceled"));
  }

  return Effect.tryPromise({
    try: () => validate_fetch_url(rawURL),
    catch: (err) =>
      new fetch_error(
        fetch_failure_kind.fetch_invalid_url,
        0,
        err instanceof Error ? err.message : String(err),
        { title: "", url: rawURL, content: "", fetch: null },
      ),
  }).pipe(
    Effect.flatMap((u) => {
      const pageURL = u.toString();

      let youtubeEff: Effect.Effect<page_hit, Error> = Effect.fail(
        new Error("not youtube or skipped"),
      );
      if (is_youtube_host(u.hostname)) {
        youtubeEff = fetch_youtube_oembed(ctx, pageURL);
      }

      const downloadAndExtractEff = download_html(ctx, pageURL).pipe(
        Effect.mapError((ferr) => {
          ferr.partialHit = { title: "", url: pageURL, content: "", fetch: null };
          return ferr;
        }),
        Effect.flatMap((fetched) => {
          const [title, content, extractErr] = extract_page_content(
            fetched.finalURL,
            fetched.body,
          );
          if (extractErr) {
            extractErr.partialHit = { title, url: fetched.finalURL, content: "", fetch: null };
            return Effect.fail(extractErr);
          }
          return Effect.succeed({
            title,
            url: fetched.finalURL,
            content,
            fetch: null,
          });
        }),
        Effect.catchAll((ferr) => {
          if (
            (ferr.kind === fetch_failure_kind.fetch_blocked ||
              ferr.kind === fetch_failure_kind.fetch_js_rendered) &&
            browser_fallback !== null
          ) {
            return browser_fallback(ctx, pageURL).pipe(
              Effect.catchAll(() => Effect.fail(ferr)),
            );
          }
          return Effect.fail(ferr);
        }),
      );

      return youtubeEff.pipe(Effect.catchAll(() => downloadAndExtractEff));
    }),
  );
};

export const normalize_fetch_urls = (raw: string[]): string[] => {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const u of raw) {
    const trimmed = u.trim();
    if (trimmed === "" || seen.has(trimmed)) {
      continue;
    }
    if (!is_http_url(trimmed)) {
      continue;
    }
    seen.add(trimmed);
    out.push(trimmed);
    if (out.length >= max_fetch_cap) {
      break;
    }
  }
  return out;
};

/*
PORT STATUS
source path: backend/web/fetch.go
source lines: 74
confidence: high
status: phase_b_compile
*/
