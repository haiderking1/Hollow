// PORT: backend/auth/codex_oauth.go

import { Effect } from "effect";
import { auth_error, type auth_error as auth_error_type } from "./error";
import {
  load_codex_provider_state,
  save_codex_provider_state,
  type provider_state,
  type token_pair,
} from "./store";

const codex_oauth_client_id = "app_EMoamEEZ73f0CkXaXp7hrann";
const codex_oauth_issuer = "https://auth.openai.com";
const codex_oauth_token_url = "https://auth.openai.com/oauth/token";
const codex_default_base_url = "https://chatgpt.com/backend-api/codex";
const codex_device_auth_url = `${codex_oauth_issuer}/codex/device`;
const codex_refresh_skew_secs = 120;

export type codex_credentials = {
  access_token: string;
  refresh_token: string;
  base_url: string;
};

export type device_auth_start = {
  user_code: string;
  device_auth_id: string;
  verify_url: string;
  poll_interval: number; // milliseconds
};

// CodexDefaultBaseURL returns the ChatGPT Codex backend base URL.
export const codex_default_base_url_fn = (): string => codex_default_base_url;

const parse_interval = (value: unknown): number => {
  let interval = 5;
  if (typeof value === "number") {
    interval = Math.round(value);
  } else if (typeof value === "string") {
    const parsed = parseInt(value, 10);
    if (!Number.isNaN(parsed)) {
      interval = parsed;
    }
  }
  return interval < 3 ? 3 : interval;
};

// HasCodexAuth reports whether Codex OAuth tokens are stored.
export const has_codex_auth = (): Effect.Effect<boolean, never> =>
  Effect.gen(function* () {
    const result = yield* Effect.either(load_codex_provider_state());
    if (result._tag === "Left") {
      return false;
    }
    const [state, ok] = result.right;
    if (!ok) {
      return false;
    }
    return state.tokens.access_token !== "" && state.tokens.refresh_token !== "";
  });

// StartCodexDeviceAuth begins the OpenAI device-code OAuth flow.
export const start_codex_device_auth = (
  ctx: AbortSignal,
): Effect.Effect<device_auth_start, auth_error_type> =>
  Effect.tryPromise({
    try: async () => {
      const body = JSON.stringify({ client_id: codex_oauth_client_id });
      const resp = await fetch(`${codex_oauth_issuer}/api/accounts/deviceauth/usercode`, {
        method: "POST",
        signal: ctx,
        headers: { "Content-Type": "application/json" },
        body,
      });

      const raw = await resp.text();
      if (resp.status !== 200) {
        throw auth_error(
          `device code request returned ${resp.status}: ${raw.trim()}`,
          null,
        );
      }

      const device_data = JSON.parse(raw) as {
        user_code?: string;
        device_auth_id?: string;
        interval?: unknown;
      };
      if (device_data.user_code === undefined || device_data.device_auth_id === undefined) {
        throw auth_error("device code response missing required fields", null);
      }

      return {
        user_code: device_data.user_code,
        device_auth_id: device_data.device_auth_id,
        verify_url: codex_device_auth_url,
        poll_interval: parse_interval(device_data.interval) * 1000,
      };
    },
    catch: (cause) =>
      (cause as auth_error_type)._tag === "AuthError"
        ? (cause as auth_error_type)
        : auth_error("start device auth", cause),
  });

// PollCodexDeviceAuth waits for the user to finish browser sign-in.
export const poll_codex_device_auth = (
  ctx: AbortSignal,
  start: device_auth_start,
): Effect.Effect<void, auth_error_type> =>
  Effect.tryPromise({
    try: async () => {
      const deadline = Date.now() + 15 * 60 * 1000;

      while (!ctx.aborted) {
        if (Date.now() > deadline) {
          throw auth_error("login timed out after 15 minutes", null);
        }

        const body = JSON.stringify({
          device_auth_id: start.device_auth_id,
          user_code: start.user_code,
        });

        const resp = await fetch(`${codex_oauth_issuer}/api/accounts/deviceauth/token`, {
          method: "POST",
          signal: ctx,
          headers: { "Content-Type": "application/json" },
          body,
        });

        const raw = await resp.text();

        if (resp.status === 200) {
          const code_resp = JSON.parse(raw) as {
            authorization_code?: string;
            code_verifier?: string;
          };
          if (
            code_resp.authorization_code === undefined ||
            code_resp.code_verifier === undefined
          ) {
            throw auth_error(
              "device auth response missing authorization_code or code_verifier",
              null,
            );
          }
          await Effect.runSync(
            save_codex_tokens_from_auth_code(
              ctx,
              code_resp.authorization_code,
              code_resp.code_verifier,
            ),
          );
          return;
        }

        if (resp.status === 403 || resp.status === 404) {
          await new Promise((resolve) => setTimeout(resolve, start.poll_interval));
          continue;
        }

        throw auth_error(`device auth polling returned ${resp.status}: ${raw.trim()}`, null);
      }

      throw auth_error("device auth aborted", null);
    },
    catch: (cause) =>
      (cause as auth_error_type)._tag === "AuthError"
        ? (cause as auth_error_type)
        : auth_error("poll device auth", cause),
  });

// CompleteCodexDeviceLogin runs device auth start + poll until tokens are saved.
export const complete_codex_device_login = (
  ctx: AbortSignal,
): Effect.Effect<device_auth_start, auth_error_type> =>
  Effect.gen(function* () {
    const start = yield* start_codex_device_auth(ctx);
    yield* poll_codex_device_auth(ctx, start);
    return start;
  });

const save_codex_tokens_from_auth_code = (
  ctx: AbortSignal,
  authorization_code: string,
  code_verifier: string,
): Effect.Effect<void, auth_error_type> =>
  Effect.tryPromise({
    try: async () => {
      const params = new URLSearchParams({
        grant_type: "authorization_code",
        code: authorization_code,
        redirect_uri: `${codex_oauth_issuer}/deviceauth/callback`,
        client_id: codex_oauth_client_id,
        code_verifier: code_verifier,
      });

      const resp = await fetch(codex_oauth_token_url, {
        method: "POST",
        signal: ctx,
        headers: { "Content-Type": "application/x-www-form-urlencoded" },
        body: params.toString(),
      });

      const raw = await resp.text();
      if (resp.status !== 200) {
        throw auth_error(`token exchange returned ${resp.status}: ${raw.trim()}`, null);
      }

      const tokens = JSON.parse(raw) as {
        access_token?: string;
        refresh_token?: string;
      };
      if (tokens.access_token === undefined || tokens.access_token === "") {
        throw auth_error("token exchange did not return access_token", null);
      }

      await Effect.runSync(
        save_codex_provider_state({
          tokens: {
            access_token: tokens.access_token,
            refresh_token: tokens.refresh_token ?? "",
          },
          base_url: codex_default_base_url,
          last_refresh: new Date().toISOString(),
          auth_mode: "chatgpt",
          source: "device-code",
        }),
      );
    },
    catch: (cause) =>
      (cause as auth_error_type)._tag === "AuthError"
        ? (cause as auth_error_type)
        : auth_error("save tokens from auth code", cause),
  });

// ResolveCodexCredentials returns a fresh access token, refreshing when needed.
export const resolve_codex_credentials = (
  ctx: AbortSignal,
): Effect.Effect<codex_credentials, auth_error_type> =>
  Effect.gen(function* () {
    const result = yield* Effect.either(load_codex_provider_state());
    if (result._tag === "Left") {
      return yield* Effect.fail(result.left);
    }
    const [state, ok] = result.right;
    if (!ok) {
      return yield* Effect.fail(
        auth_error("not connected — run: hollow auth add openai-codex", null),
      );
    }

    let base_url = (state.base_url ?? "").replace(/\/$/, "");
    if (base_url === "") {
      base_url = codex_default_base_url;
    }

    let access = state.tokens.access_token;
    const refresh = state.tokens.refresh_token;
    if (access === "" || refresh === "") {
      return yield* Effect.fail(
        auth_error("codex auth incomplete — run: hollow auth add openai-codex", null),
      );
    }

    if (codex_access_token_expiring(access, codex_refresh_skew_secs)) {
      const refreshed = yield* refresh_codex_tokens(ctx, refresh);
      access = refreshed.access_token;
      state.tokens.access_token = access;
      state.tokens.refresh_token = refreshed.refresh_token;
      state.last_refresh = new Date().toISOString();
      yield* save_codex_provider_state(state);
    }

    return {
      access_token: access,
      refresh_token: state.tokens.refresh_token,
      base_url,
    };
  });

const refresh_codex_tokens = (
  ctx: AbortSignal,
  refresh_token: string,
): Effect.Effect<token_pair, auth_error_type> =>
  Effect.tryPromise({
    try: async () => {
      const params = new URLSearchParams({
        grant_type: "refresh_token",
        refresh_token: refresh_token,
        client_id: codex_oauth_client_id,
      });

      const resp = await fetch(codex_oauth_token_url, {
        method: "POST",
        signal: ctx,
        headers: {
          "Content-Type": "application/x-www-form-urlencoded",
          Accept: "application/json",
        },
        body: params.toString(),
      });

      const raw = await resp.text();
      if (resp.status !== 200) {
        throw auth_error(`codex token refresh failed (${resp.status}): ${raw.trim()}`, null);
      }

      const payload = JSON.parse(raw) as {
        access_token?: string;
        refresh_token?: string;
      };
      if (payload.access_token === undefined || payload.access_token === "") {
        throw auth_error("codex refresh response missing access_token", null);
      }

      return {
        access_token: payload.access_token,
        refresh_token: payload.refresh_token !== undefined && payload.refresh_token !== ""
          ? payload.refresh_token
          : refresh_token,
      };
    },
    catch: (cause) =>
      (cause as auth_error_type)._tag === "AuthError"
        ? (cause as auth_error_type)
        : auth_error("refresh codex tokens", cause),
  });

const codex_access_token_expiring = (token: string, skew_seconds: number): boolean => {
  const exp = jwt_exp_unix(token);
  if (exp === null) {
    return true;
  }
  return Math.floor(Date.now() / 1000) >= exp - skew_seconds;
};

const jwt_exp_unix = (token: string): number | null => {
  const parts = token.split(".");
  if (parts.length < 2) {
    return null;
  }

  let payload_b64 = parts[1];
  payload_b64 += "=".repeat((4 - (payload_b64.length % 4)) % 4);
  payload_b64 = payload_b64.replace(/-/g, "+").replace(/_/g, "/");

  let raw: Uint8Array;
  try {
    raw = new Uint8Array(Buffer.from(payload_b64.replace(/=+$/, ""), "base64url"));
  } catch {
    try {
      raw = new Uint8Array(Buffer.from(payload_b64, "base64"));
    } catch {
      return null;
    }
  }

  const claims = JSON.parse(new TextDecoder().decode(raw)) as { exp?: number };
  if (claims.exp === undefined || claims.exp === 0) {
    return null;
  }
  return claims.exp;
};

/*
PORT STATUS
source path: backend/auth/codex_oauth.go
source lines: 348
draft lines: 381
confidence: medium
status: phase_a_draft
todos:
  - decide whether internal helpers like save_codex_tokens_from_auth_code should be exported
  - replace Effect.runSync inside async stubs with proper Effect composition
  - verify poll interval / deadline math matches Go time.Duration behavior
notes:
  - Functions returning (T, error) are modeled as Effect.Effect<T, auth_error>.
  - Uses global fetch for HTTP; AbortSignal replaces Go context.Context.
*/
