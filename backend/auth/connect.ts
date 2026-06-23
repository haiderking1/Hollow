// PORT: backend/auth/connect.go

import { Effect } from "effect";
import { auth_error, type auth_error as auth_error_type } from "./error";
import { set_api_key, has_api_key as secrets_has_api_key } from "../secrets/store";
import {
  has_codex_auth,
  complete_codex_device_login,
  type device_auth_start,
} from "./codex_oauth";

// SaveAPIKey stores the OpenCode API key for the current user.
export const save_api_key = (key: string, provider?: string): Effect.Effect<void, auth_error_type> => {
  const trimmed = key.trim();
  if (trimmed === "") {
    return Effect.fail(auth_error("api key cannot be empty", null));
  }
  return set_api_key(trimmed, provider).pipe(
    Effect.mapError((e) => auth_error("secrets set api key", e)),
  );
};

// Connected reports whether any credentials are stored.
export const connected = (): Effect.Effect<boolean, never> =>
  Effect.gen(function* () {
    const has_secrets = yield* secrets_has_api_key();
    if (has_secrets) {
      return true;
    }
    return yield* has_codex_auth();
  });

// AddOpenAICodex runs browser OAuth and saves Codex credentials to auth.json.
export const add_openai_codex = (
  ctx: AbortSignal,
): Effect.Effect<device_auth_start, auth_error_type> => complete_codex_device_login(ctx);

/*
PORT STATUS
source path: backend/auth/connect.go
source lines: 31
draft lines: 50
confidence: high
status: phase_a_draft
todos:
  - mapError for secrets errors may want more granular reason strings
notes:
  - Connect/SaveAPIKey/AddOpenAICodex functions returning (error) are Effect effects.
  - Imports existing secrets and codex_oauth ports to avoid duplication.
*/
