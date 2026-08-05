import { Effect } from "effect";
import { type provider } from "./types";
import { ensure_running, stop as searxng_stop, type searxng_status } from "./searxng/manager";
import { searxng_provider } from "./searxng";

export const trim_input = (s: string): string => {
  return s.trim();
};

export const stop = (): Effect.Effect<void, never> => {
  return searxng_stop().pipe(Effect.catchAll(() => Effect.void));
};

export const new_search_provider = (
  ctx: AbortSignal,
  on_status: searxng_status = () => {},
): Effect.Effect<provider, Error> => {
  return ensure_running(ctx, on_status).pipe(
    Effect.map((base) => new searxng_provider(base)),
    Effect.mapError((err: any) => {
      if (err instanceof Error) return err;
      if (err && typeof err === "object" && typeof err.reason === "string") {
        return new Error(err.reason);
      }
      return new Error(String(err));
    }),
  );
};

