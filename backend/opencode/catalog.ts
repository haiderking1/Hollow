// PORT: mirrors backend/opencode/catalog.go

import { Effect } from "effect";
import fs from "node:fs/promises";
import path from "node:path";
import { home_dir } from "../enoughhome/home";
import { provider_opencode, provider_opencode_zen, type model_info } from "./providers";
import { normalize_model } from "./models";
import { format_thinking_label } from "./thinking";
import type { json_raw_message } from "./types";

export const models_dev_url = "https://models.dev/api.json";
export const min_opencode_go_catalog_models = 5;
export const models_dev_provider_go = "opencode-go";
export const models_dev_provider_zen = "opencode";

export type models_dev_reasoning_option = {
  type: string;
  values: string[];
};

export type models_dev_model = {
  id: string;
  name: string;
  family: string;
  reasoning: boolean;
  reasoning_options?: models_dev_reasoning_option[];
  modalities: {
    input: string[];
  };
  limit: {
    context: number;
    output: number;
  };
  interleaved?: json_raw_message;
};

export type models_dev_provider = {
  id: string;
  models: Record<string, models_dev_model>;
};

let opencode_catalog: Record<string, model_info> = {};
let opencode_zen_catalog: Record<string, model_info> = {};
let catalog_loaded = false;
let background_refresh_started = false;

export const cache_file_path = (): string => {
  return path.join(home_dir(), "cache", "models.json");
};

export const load_catalog_from_cache = (): Effect.Effect<
  { go_catalog: Record<string, model_info>; zen_catalog: Record<string, model_info>; mod_time: Date },
  Error
> => {
  return Effect.tryPromise({
    try: async () => {
      const p = cache_file_path();
      const stat = await fs.stat(p);
      const data = await fs.readFile(p, "utf8");
      const all = JSON.parse(data) as Record<string, models_dev_provider>;

      const go_catalog = catalog_from_models_dev_provider(all[models_dev_provider_go]);
      const zen_catalog = catalog_from_models_dev_provider(all[models_dev_provider_zen]);

      if (Object.keys(go_catalog).length === 0) {
        throw new Error("missing go provider in cached catalog");
      }
      return { go_catalog, zen_catalog, mod_time: stat.mtime };
    },
    catch: (cause) => cause instanceof Error ? cause : new Error(String(cause)),
  });
};

export const catalog_from_models_dev_provider = (provider?: models_dev_provider): Record<string, model_info> => {
  if (!provider || !provider.models || Object.keys(provider.models).length === 0) {
    return {};
  }
  const next: Record<string, model_info> = {};
  for (const [id, m] of Object.entries(provider.models)) {
    next[id] = model_info_from_models_dev(m);
  }
  return next;
};

export const save_catalog_to_cache = (data: string): Effect.Effect<void, Error> => {
  return Effect.tryPromise({
    try: async () => {
      const p = cache_file_path();
      const dir = path.dirname(p);
      await fs.mkdir(dir, { recursive: true });
      await fs.writeFile(p, data, "utf8");
    },
    catch: (cause) => cause instanceof Error ? cause : new Error(String(cause)),
  });
};

const start_background_refresh_loop = () => {
  setInterval(async () => {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 30000);
    try {
      await Effect.runPromise(refresh_models_dev_catalog(controller.signal));
    } catch {} finally {
      clearTimeout(timeout);
    }
  }, 60 * 60 * 1000); // 60 minutes
};

export const refresh_models_dev_catalog = (ctx?: AbortSignal): Effect.Effect<void, Error> => {
  if (!background_refresh_started) {
    background_refresh_started = true;
    start_background_refresh_loop();
  }

  return Effect.tryPromise({
    try: async () => {
      // 1. Try to load from disk cache first (only if using default URL)
      let cached_go: Record<string, model_info> | null = null;
      let cached_zen: Record<string, model_info> | null = null;
      let mod_time: Date | null = null;

      if (models_dev_url === "https://models.dev/api.json") {
        try {
          const cache_res = await Effect.runPromise(load_catalog_from_cache());
          cached_go = cache_res.go_catalog;
          cached_zen = cache_res.zen_catalog;
          mod_time = cache_res.mod_time;

          if (Object.keys(cached_go).length >= min_opencode_go_catalog_models) {
            const age_ms = Date.now() - mod_time.getTime();
            if (age_ms < 60 * 60 * 1000) { // 60 minutes
              opencode_catalog = cached_go;
              opencode_zen_catalog = cached_zen;
              catalog_loaded = true;
              return;
            }
          }
        } catch {}
      }

      // 2. Fetch from network
      try {
        const resp = await fetch(models_dev_url, { signal: ctx });
        const raw = await resp.text();
        if (resp.status >= 400) {
          if (cached_go !== null && cached_zen !== null) {
            opencode_catalog = cached_go;
            opencode_zen_catalog = cached_zen;
            catalog_loaded = true;
            return;
          }
          throw new Error(`models.dev ${resp.status}: ${raw.trim()}`);
        }
        const all = JSON.parse(raw) as Record<string, models_dev_provider>;
        const go_provider = all[models_dev_provider_go];
        if (!go_provider) {
          if (cached_go !== null && cached_zen !== null) {
            opencode_catalog = cached_go;
            opencode_zen_catalog = cached_zen;
            catalog_loaded = true;
            return;
          }
          throw new Error(`models.dev: missing "${models_dev_provider_go}" provider`);
        }

        opencode_catalog = catalog_from_models_dev_provider(go_provider);
        opencode_zen_catalog = catalog_from_models_dev_provider(all[models_dev_provider_zen]);
        catalog_loaded = true;

        if (models_dev_url === "https://models.dev/api.json") {
          try {
            await Effect.runPromise(save_catalog_to_cache(raw));
          } catch {}
        }
      } catch (err) {
        if (cached_go !== null && cached_zen !== null) {
          opencode_catalog = cached_go;
          opencode_zen_catalog = cached_zen;
          catalog_loaded = true;
          return;
        }
        throw err;
      }
    },
    catch: (cause) => cause instanceof Error ? cause : new Error(String(cause)),
  });
};

export const catalog_model = (id: string): [model_info, boolean] => {
  return catalog_model_for_provider(provider_opencode, id);
};

export const catalog_model_for_provider = (provider: string, id: string): [model_info, boolean] => {
  let cat = opencode_catalog;
  if (provider === provider_opencode_zen) {
    cat = opencode_zen_catalog;
  }
  const m = cat[id];
  if (m !== undefined) {
    return [m, true];
  }
  return [{ id: "", name: "", context_window: 0, reasoning: false }, false];
};

export const catalog_loaded_once = (): boolean => {
  return catalog_loaded;
};

export const parse_reasoning_field = (raw?: json_raw_message | unknown): string => {
  if (raw === undefined || raw === null) {
    return "";
  }
  // Already parsed from models.dev JSON (interleaved object/boolean).
  if (typeof raw === "boolean") {
    return raw ? "reasoning_content" : "";
  }
  if (typeof raw === "object" && !Array.isArray(raw) && !(raw instanceof Uint8Array)) {
    const val = raw as Record<string, unknown>;
    if ("field" in val && typeof val.field === "string") {
      return val.field;
    }
    return "";
  }
  if (!(raw instanceof Uint8Array) && !Buffer.isBuffer(raw)) {
    return "";
  }
  const buf = raw instanceof Uint8Array ? raw : new Uint8Array(raw as Buffer);
  if (buf.length === 0) {
    return "";
  }
  const text = new TextDecoder().decode(buf);
  try {
    const val = JSON.parse(text);
    if (typeof val === "boolean") {
      return val ? "reasoning_content" : "";
    }
    if (val && typeof val === "object" && "field" in val && typeof val.field === "string") {
      return val.field;
    }
  } catch {}
  return "";
};

export const model_info_from_models_dev = (m: models_dev_model): model_info => {
  const info: model_info = {
    id: m.id,
    name: m.name,
    context_window: m.limit.context,
    reasoning: m.reasoning,
    reasoning_field: parse_reasoning_field(m.interleaved),
    supports_images: model_supports_images_from_modalities(m.modalities.input),
  };
  if (info.name === "") {
    info.name = title_case_model_id(m.id);
  }
  if (m.reasoning_options !== undefined && m.reasoning_options.length > 0) {
    for (const option of m.reasoning_options) {
      if (option.values !== undefined && option.values.length > 0) {
        info.thinking_levels = option.values.map((v) => ({
          id: v,
          name: format_thinking_label(v as any),
        }));
        info.reasoning = true;
        break;
      }
    }
  }
  return normalize_model(info);
};

export const model_supports_images_from_modalities = (input: string[]): boolean => {
  return input.includes("image");
};

export const title_case_model_id = (id: string): string => {
  return id
    .split("-")
    .map((p) => (p.length > 0 ? p[0].toUpperCase() + p.slice(1) : p))
    .join(" ");
};

export const get_catalog_models = (provider: string): model_info[] => {
  const cat = provider === provider_opencode_zen ? opencode_zen_catalog : opencode_catalog;
  return Object.values(cat);
};

/*
PORT STATUS
source path: backend/opencode/catalog.go
source lines: 328
draft lines: 231
confidence: high
status: phase_b_compile
*/
