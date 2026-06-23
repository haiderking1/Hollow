// PORT: backend/opencode/models.go

import { Effect } from "effect";
import fs from "node:fs";
import fsPromises from "node:fs/promises";
import path from "node:path";
import { home_dir } from "../hollowhome/home";
import {
  provider_codex,
  provider_neuralwatt,
  provider_opencode,
  provider_opencode_zen,
  type model_info,
  codex_models
} from "./providers";
import {
  opencode_mandatory_thinking_id,
  supported_thinking_levels,
  format_thinking_label,
  format_thinking_level_for_model,
  supports_thinking
} from "./thinking";
import {
  catalog_model_for_provider,
  refresh_models_dev_catalog,
  get_catalog_models
} from "./catalog";
import { fetch_codex_models } from "./codex_catalog";
import {
  resolve_model,
  resolve_context_window as resolver_context_window,
  resolve_supports_images,
  codex_fallback_ids,
  neuralwatt_fallback_ids,
  load_user_model_overrides,
} from "./model_resolver";

export class registry {
  private _models: model_info[] = [];
  private _zen_models: model_info[] = [];
  private _neuralwatt_models: model_info[] = [];
  private _codex_models: model_info[] = [];
  private _err: Error | null = null;
  private _zen_err: Error | null = null;
  private _neuralwatt_err: Error | null = null;
  private _codex_err: Error | null = null;

  constructor() {
    this.load_from_cache();
  }

  private load_from_cache() {
    const providers = ["opencode-go", "opencode-zen", "neuralwatt", "openai-codex"];
    for (const p of providers) {
      try {
        const cachePath = path.join(home_dir(), "cache", `provider_${p}_models.json`);
        if (fs.existsSync(cachePath)) {
          const content = fs.readFileSync(cachePath, "utf8");
          const parsed = JSON.parse(content) as model_info[];
          if (Array.isArray(parsed)) {
            if (p === "opencode-zen") {
              this._zen_models = parsed;
            } else if (p === "neuralwatt") {
              this._neuralwatt_models = parsed;
            } else if (p === "openai-codex") {
              this._codex_models = parsed;
            } else {
              this._models = parsed;
            }
          }
        }
      } catch {}
    }
  }

  models(): model_info[] {
    return [...this._models];
  }

  err(): Error | null {
    return this._err;
  }

  lookup(id: string): [model_info, boolean] {
    const clean = id.trim();
    if (clean === "") {
      return [{ id: "", name: "", context_window: 0, reasoning: false }, false];
    }
    for (const m of this._models) {
      if (m.id === clean) {
        return [m, true];
      }
    }
    return [resolve_model(clean, provider_opencode), true];
  }

  async refresh(
    ctx: AbortSignal | undefined,
    provider: string,
    endpoint: string,
    api_key: string
  ): Promise<Error | null> {
    try {
      await Effect.runPromise(refresh_models_dev_catalog(ctx));
    } catch {}

    try {
      await Effect.runPromise(load_user_model_overrides());
    } catch {}

    try {
      const fetched = await Effect.runPromise(fetch_models(ctx, provider, endpoint, api_key));
      if (provider === provider_opencode_zen) {
        this._zen_models = fetched;
        this._zen_err = null;
      } else if (provider === provider_neuralwatt) {
        this._neuralwatt_models = fetched;
        this._neuralwatt_err = null;
      } else {
        this._models = fetched;
        this._err = null;
      }
      try {
        const cacheDir = path.join(home_dir(), "cache");
        await fsPromises.mkdir(cacheDir, { recursive: true });
        await fsPromises.writeFile(
          path.join(cacheDir, `provider_${provider}_models.json`),
          JSON.stringify(fetched, null, 2),
          "utf8"
        );
      } catch {}
      return null;
    } catch (err) {
      const error = err instanceof Error ? err : new Error(String(err));
      const models = fallback_models(provider);
      if (provider === provider_opencode_zen) {
        this._zen_models = models;
        this._zen_err = error;
      } else if (provider === provider_neuralwatt) {
        this._neuralwatt_models = models;
        this._neuralwatt_err = error;
      } else {
        this._models = models;
        this._err = error;
      }
      return error;
    }
  }

  err_for(provider: string): Error | null {
    if (provider === provider_opencode_zen) {
      return this._zen_err;
    }
    if (provider === provider_neuralwatt) {
      return this._neuralwatt_err;
    }
    return this._err;
  }

  zen_models_list(): model_info[] {
    if (this._zen_models.length > 0) {
      return [...this._zen_models];
    }
    return fallback_models(provider_opencode_zen);
  }

  neuralwatt_models_list(): model_info[] {
    if (this._neuralwatt_models.length > 0) {
      return [...this._neuralwatt_models];
    }
    return neuralwatt_fallback_ids.map((id) => resolve_model(id, provider_neuralwatt));
  }

  lookup_neuralwatt(id: string): [model_info, boolean] {
    const clean = id.trim();
    if (clean === "") {
      return [{ id: "", name: "", context_window: 0, reasoning: false }, false];
    }
    for (const m of this._neuralwatt_models) {
      if (m.id === clean) {
        return [m, true];
      }
    }
    return [resolve_model(clean, provider_neuralwatt), true];
  }

  async refresh_codex(ctx: AbortSignal | undefined, access_token: string): Promise<Error | null> {
    try {
      const fetched = await Effect.runPromise(fetch_codex_models(ctx, access_token));
      this._codex_models = fetched;
      this._codex_err = null;
      try {
        const cacheDir = path.join(home_dir(), "cache");
        await fsPromises.mkdir(cacheDir, { recursive: true });
        await fsPromises.writeFile(
          path.join(cacheDir, `provider_${provider_codex}_models.json`),
          JSON.stringify(fetched, null, 2),
          "utf8"
        );
      } catch {}
      return null;
    } catch (err) {
      const error = err instanceof Error ? err : new Error(String(err));
      this._codex_err = error;
      if (this._codex_models.length === 0) {
        this._codex_models = codex_models();
      }
      return error;
    }
  }

  codex_err(): Error | null {
    return this._codex_err;
  }

  codex_models_list(): model_info[] {
    if (this._codex_models.length > 0) {
      return [...this._codex_models];
    }
    return codex_models();
  }

  lookup_codex(id: string): [model_info, boolean] {
    const clean = id.trim();
    if (clean === "") {
      return [{ id: "", name: "", context_window: 0, reasoning: false }, false];
    }
    for (const m of this._codex_models) {
      if (m.id === clean) {
        return [m, true];
      }
    }
    return [resolve_model(clean, provider_codex), true];
  }

  resolve_context_window(provider: string, model_id: string): number {
    provider = provider === "" ? provider_opencode : provider;
    model_id = model_id.trim();
    if (model_id === "") {
      return 0;
    }

    const fetched =
      provider === provider_codex
        ? this._codex_models
        : provider === provider_opencode_zen
          ? this._zen_models
          : provider === provider_neuralwatt
            ? this._neuralwatt_models
            : this._models;

    for (const m of fetched) {
      if (m.id === model_id && m.context_window > 0) {
        return m.context_window;
      }
    }

    return resolver_context_window(model_id, provider);
  }
}

export const default_registry = new registry();

export const lookup_model = (id: string): [model_info, boolean] => {
  return default_registry.lookup(id);
};

export const model_context_window = (id: string): number => {
  return resolver_context_window(id, provider_opencode);
};

export const fetch_models = (
  ctx: AbortSignal | undefined,
  provider: string,
  endpoint: string,
  api_key: string
): Effect.Effect<model_info[], Error> => {
  return Effect.tryPromise({
    try: async () => {
      const trimmed_endpoint = endpoint.replace(/\/+$/, "");
      const url = `${trimmed_endpoint}/models`;

      const headers: Record<string, string> = {};
      if (api_key !== "") {
        headers["Authorization"] = `Bearer ${api_key}`;
      }

      const controller = new AbortController();
      if (ctx) {
        ctx.addEventListener("abort", () => controller.abort());
      }
      const timeout = setTimeout(() => controller.abort(), 15000);

      try {
        const resp = await fetch(url, { signal: controller.signal, headers });
        const raw = await resp.text();
        clearTimeout(timeout);

        if (resp.status >= 400) {
          throw new Error(`models ${resp.status}: ${raw.trim()}`);
        }

        const list = JSON.parse(raw) as { data: { id: string }[] };
        const seen = new Set<string>();
        const out: model_info[] = [];
        for (const entry of list.data ?? []) {
          const id = (entry.id ?? "").trim();
          if (id === "") {
            continue;
          }
          if (seen.has(id)) {
            continue;
          }
          seen.add(id);

          out.push(resolve_model(id, provider));
        }

        if (out.length === 0) {
          return fallback_models(provider);
        }

        sort_models(out);
        return out;
      } catch (err) {
        clearTimeout(timeout);
        throw err instanceof Error ? err : new Error(String(err));
      }
    },
    catch: (cause) => cause instanceof Error ? cause : new Error(String(cause)),
  });
};

export const fallback_models = (provider: string): model_info[] => {
  const out = get_catalog_models(provider);
  if (out.length > 0) {
    sort_models(out);
    return out;
  }
  if (provider === provider_opencode_zen) {
    return [];
  }
  if (provider === provider_neuralwatt) {
    return neuralwatt_fallback_ids.map((id) => resolve_model(id, provider_neuralwatt));
  }
  if (provider === provider_codex) {
    return codex_fallback_ids.map((id) => resolve_model(id, provider_codex));
  }
  return [];
};

export const fallback_models_default = (): model_info[] => {
  return fallback_models(provider_opencode);
};

export const merge_model = (id: string): model_info => {
  const clean = id.trim();
  if (clean === "") {
    return { id: "", name: "", context_window: 0, reasoning: false };
  }
  return resolve_model(clean, provider_opencode);
};

export const normalize_model = (m: model_info): model_info => {
  if (m.mandatory_thinking === undefined) {
    m.mandatory_thinking = opencode_mandatory_thinking_id(m.id);
  }
  if (m.thinking_levels === undefined) {
    const levels = supported_thinking_levels(m.id);
    m.thinking_levels = levels.map((lvl) => ({
      id: lvl,
      name: format_thinking_label(lvl)
    }));
  }
  return m;
};

export const sort_models = (models: model_info[]): void => {
  models.sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()));
};

export const format_context_window = (n: number): string => {
  if (n >= 1_000_000) {
    if (n % 1_000_000 === 0) {
      return `${n / 1_000_000}M`;
    }
    return `${(n / 1_000_000).toFixed(1)}M`;
  }
  if (n >= 1000) {
    if (n % 1000 === 0) {
      return `${n / 1000}k`;
    }
    return `${(n / 1000).toFixed(1)}k`;
  }
  return `${n}`;
};

export const format_thinking_badge = (m: model_info, level: string): string => {
  if (!supports_thinking(m.id)) {
    if (m.reasoning) {
      return "reasoning";
    }
    return "";
  }
  return format_thinking_level_for_model(m.id, level as any);
};

export const supports_images = (model: string): boolean => {
  return resolve_supports_images(model, provider_opencode);
};

/*
PORT STATUS
source path: backend/opencode/models.go
source lines: 450
draft lines: 440
confidence: high
status: phase_b_compile
*/
