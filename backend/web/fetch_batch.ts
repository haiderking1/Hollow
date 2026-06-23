// PORT: backend/web/fetch_batch.go

import { Effect } from "effect";
import { type page_hit, fetch_error, fetch_failure_kind, max_fetch_cap } from "./types";
import { fetch_page } from "./fetch";

export const fetch_urls_parallel = (
  ctx: AbortSignal,
  urls: string[],
): Effect.Effect<page_hit[], never> => {
  if (urls.length === 0) {
    return Effect.succeed([]);
  }

  const limitUrls = urls.slice(0, max_fetch_cap);

  const fetchTasks = limitUrls.map((rawURL) => {
    return fetch_page(ctx, rawURL).pipe(
      Effect.catchAll((err) => {
        let fe: fetch_error;
        if (err instanceof fetch_error) {
          fe = err;
        } else {
          fe = new fetch_error(
            fetch_failure_kind.fetch_network,
            0,
            err instanceof Error ? err.message : String(err),
          );
        }
        const partial = fe.partialHit ?? { title: "", url: rawURL, content: "", fetch: null };
        return Effect.succeed({
          title: partial.title,
          url: partial.url,
          content: partial.content,
          fetch: fe,
        });
      }),
    );
  });

  return Effect.all(fetchTasks, { concurrency: 3 });
};

/*
PORT STATUS
source path: backend/web/fetch_batch.go
source lines: 47
confidence: high
status: phase_b_compile
*/
