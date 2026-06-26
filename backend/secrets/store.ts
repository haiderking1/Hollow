import path from "node:path";
import fs from "node:fs";
import { Effect } from "effect";
import { verify_owner } from "./owner_unix";
import { keyring_get, keyring_set, keyring_delete } from "./keyring";

// Hollow keeps its own OS keyring entries (service "hollow").
export const keyring_service = "hollow";
export const keyring_account = "opencode-api-key";

const default_provider = "opencode-go";

const normalize_provider = (provider?: string): string => {
  const p = (provider ?? "").trim();
  return p === "" ? default_provider : p;
};

const keyring_account_for_provider = (provider?: string): string =>
  `${keyring_account}:${normalize_provider(provider)}`;

const legacy_keyring_account_for_provider = (provider?: string): string =>
  normalize_provider(provider) === default_provider ? keyring_account : keyring_account_for_provider(provider);

const safe_provider_filename = (provider?: string): string =>
  normalize_provider(provider).replace(/[^A-Za-z0-9._-]/g, "_");

export type secrets_error = {
  readonly _tag: "SecretsError";
  readonly reason: string;
  readonly cause: unknown;
};

export const secrets_error = (reason: string, cause: unknown): secrets_error => ({
  _tag: "SecretsError",
  reason,
  cause,
});

export const err_not_connected = (provider?: string): secrets_error =>
  secrets_error(
    `not connected for ${normalize_provider(provider)} — run: hollow auth add ${normalize_provider(provider)}`,
    null,
  );

// HOLLOW_CREDENTIALS_FILE forces file-based storage and skips the OS keyring.
// Tests set this so go test cannot overwrite the user's session keyring.
export const use_keyring = (): boolean => process.env.HOLLOW_CREDENTIALS_FILE === "" || process.env.HOLLOW_CREDENTIALS_FILE === undefined;

const is_enoent = (cause: unknown): boolean =>
  typeof cause === "object" &&
  cause !== null &&
  (cause as NodeJS.ErrnoException).code === "ENOENT";

export const active_credentials_path = (provider?: string): Effect.Effect<string, secrets_error> =>
  Effect.gen(function* () {
    const p = process.env.HOLLOW_CREDENTIALS_FILE;
    if (p !== undefined && p !== "") {
      if (normalize_provider(provider) === default_provider) return p;
      return `${p}.${safe_provider_filename(provider)}`;
    }
    return yield* credentials_path(provider);
  });

export const config_dir = (): Effect.Effect<string, secrets_error> =>
  Effect.gen(function* () {
    const home = yield* Effect.try({
      try: () => process.env.HOME ?? process.env.USERPROFILE ?? "",
      catch: (cause) => secrets_error("home dir", cause),
    });
    if (home === "") {
      return yield* Effect.fail(secrets_error("unable to determine user home dir", null));
    }
    return path.join(home, ".config", "hollow");
  });

export const credentials_path = (provider?: string): Effect.Effect<string, secrets_error> =>
  Effect.gen(function* () {
    const dir = yield* config_dir();
    const normalized = normalize_provider(provider);
    if (normalized === default_provider) {
      return path.join(dir, "credentials");
    }
    return path.join(dir, `credentials.${safe_provider_filename(normalized)}`);
  });

// SetAPIKey stores the key in the OS secret service, falling back to a
// user-only file (0600) when no keyring is available.
export const set_api_key = (key: string, provider?: string): Effect.Effect<void, secrets_error> => {
  const trimmed = key.trim();
  if (trimmed === "") {
    return Effect.fail(secrets_error("api key cannot be empty", null));
  }

  if (!use_keyring()) {
    return write_file(trimmed, provider);
  }

  return keyring_set(keyring_service, keyring_account_for_provider(provider), trimmed).pipe(
    Effect.andThen(() => Effect.ignore(remove_file(provider))),
    Effect.orElse(() => write_file(trimmed, provider)),
  );
};

// GetAPIKey returns the stored API key for the current user.
export const get_api_key = (provider?: string): Effect.Effect<string, secrets_error> =>
  Effect.gen(function* () {
    if (use_keyring()) {
      const account = keyring_account_for_provider(provider);
      const from_keyring = yield* Effect.either(keyring_get(keyring_service, account));
      if (from_keyring._tag === "Right" && from_keyring.right !== "") {
        return from_keyring.right;
      }

      const legacy_account = legacy_keyring_account_for_provider(provider);
      if (legacy_account !== account) {
        const from_legacy = yield* Effect.either(keyring_get(keyring_service, legacy_account));
        if (from_legacy._tag === "Right" && from_legacy.right !== "") {
          return from_legacy.right;
        }
      }
    }

    const key = yield* read_file(provider);
    if (key === "") {
      return yield* Effect.fail(err_not_connected(provider));
    }
    return key;
  });

// HasAPIKey reports whether a key is stored.
export const has_api_key = (provider?: string): Effect.Effect<boolean, never> =>
  get_api_key(provider).pipe(
    Effect.either,
    Effect.map((result) => result._tag === "Right"),
  );

// DeleteAPIKey removes stored credentials.
export const delete_api_key = (provider?: string): Effect.Effect<void, secrets_error> =>
  Effect.gen(function* () {
    if (use_keyring()) {
      yield* Effect.ignore(keyring_delete(keyring_service, keyring_account_for_provider(provider)));
    }
    yield* remove_file(provider);
  });

const write_file = (key: string, provider?: string): Effect.Effect<void, secrets_error> =>
  Effect.gen(function* () {
    const p = yield* active_credentials_path(provider);
    const dir = path.dirname(p);

    yield* Effect.try({
      try: () => fs.mkdirSync(dir, { recursive: true, mode: 0o700 }),
      catch: (cause) => secrets_error("mkdir credentials dir", cause),
    });

    yield* Effect.try({
      try: () => fs.writeFileSync(p, key, { mode: 0o600 }),
      catch: (cause) => secrets_error("write credentials", cause),
    });

    yield* Effect.try({
      try: () => fs.chmodSync(p, 0o600),
      catch: (cause) => secrets_error("chmod credentials", cause),
    });
  });

const read_file = (provider?: string): Effect.Effect<string, secrets_error> =>
  Effect.gen(function* () {
    const p = yield* active_credentials_path(provider);

    const data = yield* Effect.try({
      try: () => fs.readFileSync(p),
      catch: (cause) => secrets_error("read credentials", cause),
    }).pipe(
      Effect.catchAll((err: secrets_error) =>
        is_enoent(err.cause) ? Effect.fail(err_not_connected(provider)) : Effect.fail(err),
      ),
    );

    yield* verify_owner(p);
    return data.toString("utf8").trim();
  });

const remove_file = (provider?: string): Effect.Effect<void, secrets_error> =>
  Effect.gen(function* () {
    const p = yield* active_credentials_path(provider);

    yield* Effect.try({
      try: () => fs.unlinkSync(p),
      catch: (cause) => secrets_error("remove credentials", cause),
    }).pipe(
      Effect.catchAll((err: secrets_error) =>
        is_enoent(err.cause) ? Effect.void : Effect.fail(err),
      ),
    );
  });

