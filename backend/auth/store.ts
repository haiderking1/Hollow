// PORT: backend/auth/store.go

import path from "node:path";
import fs from "node:fs";
import { Effect } from "effect";
import { auth_error, type auth_error as auth_error_type } from "./error";
import { home_dir } from "../hollowhome/home";

export const provider_openai_codex = "openai-codex";

export type token_pair = {
  access_token: string;
  refresh_token: string;
};

export type provider_state = {
  tokens: token_pair;
  base_url?: string;
  last_refresh?: string;
  source?: string;
  auth_mode?: string;
};

export type auth_store = {
  providers: Record<string, provider_state>;
};

class mutex {
  private _locked = false;
  lock(): void { this._locked = true; }
  unlock(): void { this._locked = false; }
}

const auth_store_mu = new mutex();

const empty_provider_state = (): provider_state => ({
  tokens: { access_token: "", refresh_token: "" },
});

const is_enoent = (cause: unknown): boolean =>
  typeof cause === "object" &&
  cause !== null &&
  (cause as NodeJS.ErrnoException).code === "ENOENT";

export const auth_store_path = (): string => path.join(home_dir(), "auth.json");

export const load_auth_store = (): Effect.Effect<auth_store, auth_error_type> =>
  Effect.gen(function* () {
    const p = auth_store_path();
    const read_result = yield* Effect.either(
      Effect.try({
        try: () => fs.readFileSync(p),
        catch: (cause) => auth_error("read auth.json", cause),
      }),
    );

    let data: Buffer | undefined;
    if (read_result._tag === "Left") {
      if (is_enoent(read_result.left.cause)) {
        data = undefined;
      } else {
        return yield* Effect.fail(read_result.left);
      }
    } else {
      data = read_result.right;
    }

    if (data === undefined) {
      return { providers: {} };
    }

    const store = yield* Effect.try({
      try: () => JSON.parse(data.toString("utf8")) as auth_store,
      catch: (cause) => auth_error("decode auth.json", cause),
    });

    if (store.providers === undefined) {
      store.providers = {};
    }
    return store;
  });

export const save_auth_store = (
  store: auth_store,
): Effect.Effect<void, auth_error_type> =>
  Effect.gen(function* () {
    const p = auth_store_path();
    if (store.providers === undefined) {
      store.providers = {};
    }
    const data = yield* Effect.try({
      try: () => JSON.stringify(store, null, "  "),
      catch: (cause) => auth_error("encode auth.json", cause),
    });

    yield* Effect.try({
      try: () => fs.mkdirSync(path.dirname(p), { recursive: true, mode: 0o700 }),
      catch: (cause) => auth_error("mkdir auth dir", cause),
    });

    yield* Effect.try({
      try: () => fs.writeFileSync(p, data, { mode: 0o600 }),
      catch: (cause) => auth_error("write auth.json", cause),
    });
  });

export const load_codex_provider_state = (): Effect.Effect<
  [provider_state, boolean],
  auth_error_type
> =>
  Effect.gen(function* () {
    const store = yield* load_auth_store();
    const state = store.providers[provider_openai_codex] ?? empty_provider_state();
    const ok = state.tokens.access_token !== "";
    return [state, ok];
  });

export const save_codex_provider_state = (
  state: provider_state,
): Effect.Effect<void, auth_error_type> =>
  Effect.gen(function* () {
    auth_store_mu.lock();
    auth_store_mu.unlock();

    const store = yield* load_auth_store();
    store.providers[provider_openai_codex] = state;
    yield* save_auth_store(store);
  });

/**
 * Clear stored Codex OAuth tokens (disconnect). Writes an empty provider state
 * so has_codex_auth() subsequently returns false. Best-effort: ENOENT is fine.
 */
export const clear_codex_auth = (): Effect.Effect<void, auth_error_type> =>
  save_codex_provider_state(empty_provider_state());

/*
PORT STATUS
source path: backend/auth/store.go
source lines: 101
draft lines: 143
confidence: high
status: phase_a_draft
todos:
  - verify ENOENT detection works across Bun and Node error shapes
  - confirm 0o600/0o700 permission bits are meaningful on target runtime
notes:
  - Functions returning (T, error) are modeled as Effect.Effect<T, auth_error>.
  - Reuses existing home_dir from backend/hollowhome.
*/
