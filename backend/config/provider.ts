import { Effect } from "effect";
import { config_error, type config_error as config_error_type } from "./error";
import {
  load,
  save,
  provider_opencode,
  provider_opencode_zen,
  provider_neuralwatt,
  provider_minimax,
  provider_codex,
  default_endpoint,
  default_zen_endpoint,
  default_neuralwatt_endpoint,
  default_minimax_endpoint,
  default_model,
  default_zen_model,
  default_neuralwatt_model,
  default_minimax_model,
  default_codex_model,
  type config,
} from "./config";
import { save_api_key as auth_save_api_key } from "../auth/connect";
import { has_api_key as secrets_has_api_key, err_not_connected } from "../secrets/store";
import { has_codex_auth as auth_has_codex_auth, codex_default_base_url_fn } from "../auth/codex_oauth";
import {
  default_thinking_level,
  normalize_thinking_level,
  parse_thinking_level,
  supported_thinking_levels,
} from "../opencode/thinking";

// EnableOpenCodeProvider stores an API key and switches the active provider to OpenCode Go.
export const enable_opencode_provider = (
  key: string,
): Effect.Effect<void, config_error_type> =>
  enable_opencode_provider_impl(key, provider_opencode, default_endpoint);

// EnableOpenCodeZenProvider stores an API key and switches the active provider to Zen.
export const enable_opencode_zen_provider = (
  key: string,
): Effect.Effect<void, config_error_type> =>
  enable_opencode_provider_impl(key, provider_opencode_zen, default_zen_endpoint);

// EnableNeuralWattProvider stores an API key and switches the active provider to NeuralWatt.
export const enable_neuralwatt_provider = (
  key: string,
): Effect.Effect<void, config_error_type> =>
  enable_opencode_provider_impl(key, provider_neuralwatt, default_neuralwatt_endpoint);

export const enable_minimax_provider = (
  key: string,
): Effect.Effect<void, config_error_type> =>
  enable_opencode_provider_impl(key, provider_minimax, default_minimax_endpoint);

const enable_opencode_provider_impl = (
  key: string,
  provider: string,
  endpoint: string,
): Effect.Effect<void, config_error_type> =>
  Effect.gen(function* () {
    yield* auth_save_api_key(key, provider).pipe(
      Effect.mapError((e) => config_error("save api key", e)),
    );
    const cfg = yield* load();
    cfg.provider = provider;
    cfg.endpoint = endpoint;
    if (cfg.model === undefined || cfg.model === "") {
      switch (provider) {
        case provider_opencode_zen:
          cfg.model = default_zen_model;
          break;
        case provider_neuralwatt:
          cfg.model = default_neuralwatt_model;
          break;
        case provider_minimax:
          cfg.model = default_minimax_model;
          break;
        default:
          cfg.model = default_model;
      }
    }
    const model = cfg.model;
    const levels = supported_thinking_levels(model);
    const current = parse_thinking_level(cfg.thinking_level || "");
    if (
      levels.length > 0 &&
      (cfg.thinking_level === undefined ||
        cfg.thinking_level === "" ||
        !levels.includes(current))
    ) {
      cfg.thinking_level = default_thinking_level(model);
    }
    yield* save(cfg);
  });

// EnableCodexProvider switches runtime to OpenAI Codex OAuth.
export const enable_codex_provider = (): Effect.Effect<void, config_error_type> =>
  Effect.gen(function* () {
    const has_codex = yield* auth_has_codex_auth();
    if (!has_codex) {
      return yield* Effect.fail(config_error("not connected", err_not_connected()));
    }
    const cfg = yield* load();
    cfg.provider = provider_codex;
    cfg.endpoint = codex_default_base_url_fn();
    if (cfg.model === undefined || cfg.model === "" || cfg.model === default_model) {
      cfg.model = default_codex_model;
    }
    yield* save(cfg);
  });

// ConnectionSettings returns non-secret connection settings.
export const connection_settings = (): Effect.Effect<
  [string, string, string],
  config_error_type
> =>
  Effect.gen(function* () {
    const cfg = yield* load();
    let provider = cfg.provider ?? provider_opencode;
    let endpoint = cfg.endpoint;
    if (endpoint === undefined || endpoint === "") {
      switch (provider) {
        case provider_codex:
          endpoint = codex_default_base_url_fn();
          break;
        case provider_opencode_zen:
          endpoint = default_zen_endpoint;
          break;
        case provider_neuralwatt:
          endpoint = default_neuralwatt_endpoint;
          break;
        case provider_minimax:
          endpoint = default_minimax_endpoint;
          break;
        default:
          endpoint = default_endpoint;
      }
    }
    let model = cfg.model;
    if (model === undefined || model === "") {
      switch (provider) {
        case provider_codex:
          model = default_codex_model;
          break;
        case provider_opencode_zen:
          model = default_zen_model;
          break;
        case provider_neuralwatt:
          model = default_neuralwatt_model;
          break;
        case provider_minimax:
          model = default_minimax_model;
          break;
        default:
          model = default_model;
      }
    }
    return [provider, endpoint, model];
  });

// ApplyProviderModel switches provider, endpoint, and model settings.
export const apply_provider_model = (
  provider: string,
  model: string,
  thinking_level: string,
): Effect.Effect<void, config_error_type> =>
  Effect.gen(function* () {
    switch (provider) {
      case provider_codex: {
        const has_codex = yield* auth_has_codex_auth();
        if (!has_codex) {
          return yield* Effect.fail(config_error("not connected", err_not_connected()));
        }
        break;
      }
      case provider_opencode:
      case provider_opencode_zen:
      case provider_neuralwatt:
      case provider_minimax: {
        const key_provider = provider === provider_opencode_zen ? provider_opencode : provider;
        const has_key = yield* secrets_has_api_key(key_provider);
        if (!has_key) {
          return yield* Effect.fail(config_error("not connected", err_not_connected(key_provider)));
        }
        break;
      }
      default:
        provider = provider_opencode;
    }

    const cfg = yield* load();
    cfg.provider = provider;
    cfg.model = model;
    const levels = supported_thinking_levels(model);
    const parsed = parse_thinking_level(thinking_level);
    if (thinking_level === "" || (levels.length > 0 && !levels.includes(parsed))) {
      cfg.thinking_level = default_thinking_level(model);
    } else {
      cfg.thinking_level = normalize_thinking_level(thinking_level, model);
    }

    switch (provider) {
      case provider_codex:
        cfg.endpoint = codex_default_base_url_fn();
        break;
      case provider_opencode_zen:
        if (
          cfg.endpoint === undefined ||
          cfg.endpoint === "" ||
          cfg.endpoint === default_endpoint ||
          cfg.endpoint === default_neuralwatt_endpoint ||
          cfg.endpoint === default_minimax_endpoint ||
          cfg.endpoint === codex_default_base_url_fn()
        ) {
          cfg.endpoint = default_zen_endpoint;
        }
        break;
      case provider_neuralwatt:
        cfg.endpoint = default_neuralwatt_endpoint;
        break;
      case provider_minimax:
        cfg.endpoint = default_minimax_endpoint;
        break;
      default:
        if (
          cfg.endpoint === undefined ||
          cfg.endpoint === "" ||
          cfg.endpoint === default_zen_endpoint ||
          cfg.endpoint === default_neuralwatt_endpoint ||
          cfg.endpoint === default_minimax_endpoint ||
          cfg.endpoint === codex_default_base_url_fn()
        ) {
          cfg.endpoint = default_endpoint;
        }
    }
    yield* save(cfg);
  });

// DisabledModels returns the persisted disabled-model ids.
export const disabled_models = (): Effect.Effect<string[], Error> =>
  Effect.gen(function* () {
    const cfg = yield* load().pipe(Effect.mapError(asError));
    return cfg.disabled_models ?? [];
  });

// ToggleModelEnabled adds or removes a model id from the disabled list. Returns
// the new list.
export const toggle_model_enabled = (
  modelId: string,
): Effect.Effect<string[], Error> =>
  Effect.gen(function* () {
    const cfg = yield* load().pipe(Effect.mapError(asError));
    const prev = cfg.disabled_models ?? [];
    const next = prev.includes(modelId)
      ? prev.filter((id) => id !== modelId)
      : [...prev, modelId];
    cfg.disabled_models = next;
    yield* save(cfg).pipe(Effect.mapError(asError));
    return next;
  });

/** Add model ids to the disabled list (opt-in picker: new connects start with none enabled). */
export const disable_model_ids = (
  modelIds: string[],
): Effect.Effect<void, Error> =>
  Effect.gen(function* () {
    if (modelIds.length === 0) return;
    const cfg = yield* load().pipe(Effect.mapError(asError));
    const next = new Set(cfg.disabled_models ?? []);
    for (const id of modelIds) next.add(id);
    cfg.disabled_models = [...next];
    yield* save(cfg).pipe(Effect.mapError(asError));
  });

const asError = (e: unknown): Error => {
  if (e instanceof Error) return e;
  if (e && typeof e === "object") {
    const o = e as Record<string, unknown>;
    const r = String(o.reason ?? o.message ?? "");
    if (r) return new Error(r);
  }
  return new Error(typeof e === "string" && e ? e : "unknown error");
};

