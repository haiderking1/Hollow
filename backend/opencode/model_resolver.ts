// Native model metadata resolver.
// Replaces per-model hardcoded maps with a cascading resolution pipeline:
//   1. Provider catalog (models.dev for OpenCode Go/Zen, fetched Codex metadata).
//   2. Heuristic inference from the model id (context window, reasoning, thinking levels, image support).
//   3. User overrides from ~/.hollow/models.json.
// The only static data left is a tiny exception list for unavoidable provider quirks.

import { Effect } from "effect";
import fs from "node:fs/promises";
import path from "node:path";
import { home_dir } from "../enoughhome/home";
import {
  provider_codex,
  provider_neuralwatt,
  provider_opencode,
  provider_opencode_zen,
  type model_info,
  type thinking_level,
} from "./providers";
import {
  catalog_model,
  catalog_model_for_provider,
  title_case_model_id,
} from "./catalog";
import {
  format_thinking_label,
  opencode_mandatory_thinking_id,
  supported_thinking_levels,
} from "./thinking";

// ---------------------------------------------------------------------------
// User overrides from ~/.hollow/models.json
// ---------------------------------------------------------------------------

export type model_override = Partial<model_info>;

let cached_overrides: Record<string, model_override> | null = null;
let cached_overrides_mtime = 0;

export const user_models_path = (): string => {
  return path.join(home_dir(), "models.json");
};

export const invalidate_user_overrides = (): void => {
  cached_overrides = null;
  cached_overrides_mtime = 0;
};

export const load_user_model_overrides = (): Effect.Effect<Record<string, model_override>, Error> =>
  Effect.tryPromise({
    try: async () => {
      const p = user_models_path();
      const stat = await fs.stat(p);
      const mtime = stat.mtimeMs;
      if (cached_overrides !== null && cached_overrides_mtime === mtime) {
        return cached_overrides;
      }
      const raw = await fs.readFile(p, "utf8");
      const parsed = JSON.parse(raw) as Record<string, unknown>;
      const overrides: Record<string, model_override> = {};
      for (const [id, value] of Object.entries(parsed)) {
        if (value === null || typeof value !== "object") continue;
        overrides[id.trim()] = value as model_override;
      }
      cached_overrides = overrides;
      cached_overrides_mtime = mtime;
      return overrides;
    },
    catch: (cause) => cause instanceof Error ? cause : new Error(String(cause)),
  }).pipe(Effect.catchAll(() => Effect.succeed({} as Record<string, model_override>)));

export const current_user_overrides = (): Record<string, model_override> => {
  return cached_overrides ?? {};
};

// ---------------------------------------------------------------------------
// Heuristic inference
// ---------------------------------------------------------------------------

const context_token_re = /(\d+(?:\.\d+)?)(k|m)\b/i;

export const infer_context_window = (id: string, provider?: string): number => {
  const lower = id.trim().toLowerCase();
  if (lower === "") return 0;

  // Explicit token suffix like "32k", "1M", "200k".
  const match = lower.match(context_token_re);
  if (match) {
    const n = parseFloat(match[1]);
    const unit = match[2].toLowerCase();
    const tokens = unit === "m" ? Math.round(n * 1_000_000) : Math.round(n * 1_000);
    if (tokens >= 8_000) return tokens;
  }

  // Provider-specific families.
  if (provider === provider_codex || lower.startsWith("gpt-5")) {
    if (lower.includes("spark")) return 128_000;
    return 272_000;
  }
  if (lower.startsWith("gpt-")) return 128_000;

  if (lower.includes("kimi-k2")) return 262_144;
  if (lower.includes("minimax-m3")) return 512_000;
  if (lower.includes("minimax-m2")) return 204_800;
  if (lower.includes("mimo-v2.5-pro")) return 1_048_576;
  if (lower.includes("mimo")) return 1_000_000;
  if (lower.includes("hy3")) return 256_000;
  if (lower.includes("qwen3")) return 1_000_000;
  if (lower.includes("deepseek-v4")) return 1_000_000;
  if (lower.includes("deepseek")) return 128_000;
  // NeuralWatt currently exposes GLM-scale models with 1M contexts.
  if (provider === provider_neuralwatt) return 1_048_576;

  if (lower.includes("glm-5")) return 202_752;

  // Conservative default for any other OpenCode-style model.
  return 128_000;
};

const reasoning_family_parts = [
  "reasoner",
  "reasoning",
  "r1",
  "deepseek-chat",
  "deepseek-reasoner",
  "deepseek-r1",
  "deepseek-v3",
  "deepseek-v4",
  "o1",
  "o3",
  "mimo",
  "hy3",
  "glm",
  "kimi",
  "k2p",
  "qwen",
  "minimax",
  "big-pickle",
  "gpt-",
];

export const infer_reasoning = (id: string): boolean => {
  const lower = id.trim().toLowerCase();
  if (opencode_mandatory_thinking_id(lower)) return true;
  for (const part of reasoning_family_parts) {
    if (lower.includes(part)) return true;
  }
  return false;
};

export const infer_supports_images = (id: string): boolean => {
  const lower = id.trim().toLowerCase();
  if (
    lower.includes("vision") ||
    lower.includes("image") ||
    lower.includes("omni") ||
    lower.includes("multimodal")
  ) {
    return true;
  }
  if (lower.includes("kimi-k2")) return true;
  if (lower.includes("minimax-m3")) return true;
  if (lower.includes("mimo-v2-omni")) return true;
  if (lower.includes("gpt-5")) return true;
  return false;
};

export const infer_thinking_levels = (id: string): thinking_level[] => {
  const levels = supported_thinking_levels(id.trim().toLowerCase());
  return levels.map((lvl) => ({ id: lvl, name: format_thinking_label(lvl) }));
};

// ---------------------------------------------------------------------------
// Cascading resolution
// ---------------------------------------------------------------------------

export const heuristic_model = (id: string, provider?: string): model_info => {
  const clean = id.trim();
  return {
    id: clean,
    name: title_case_model_id(clean),
    context_window: infer_context_window(clean, provider),
    reasoning: infer_reasoning(clean),
    supports_images: infer_supports_images(clean),
    reasoning_field: "",
    mandatory_thinking: opencode_mandatory_thinking_id(clean),
    thinking_levels: infer_thinking_levels(clean),
  };
};

export const resolve_model = (id: string, provider = provider_opencode): model_info => {
  const clean = id.trim();
  if (clean === "") {
    return { id: "", name: "", context_window: 0, reasoning: false };
  }

  let base: model_info | null = null;

  if (provider === provider_opencode_zen) {
    const [m, ok] = catalog_model_for_provider(provider_opencode_zen, clean);
    if (ok) base = m;
  } else if (provider === provider_codex) {
    // Codex metadata comes from its own /models endpoint; when that data isn't
    // cached we fall through to heuristics for the family.
    base = null;
  } else if (provider === provider_neuralwatt) {
    // NeuralWatt /models only returns ids, so we rely on heuristics + overrides.
    base = null;
  } else {
    const [m, ok] = catalog_model(clean);
    if (ok) base = m;
    if (!base) {
      const [m2, ok2] = catalog_model_for_provider(provider_opencode_zen, clean);
      if (ok2) base = m2;
    }
  }

  if (base === null) {
    base = heuristic_model(clean, provider);
  }

  // Apply user overrides (last writer wins).
  const overrides = current_user_overrides()[clean];
  if (overrides !== undefined) {
    base = {
      ...base,
      ...overrides,
      id: clean,
      // Preserve type safety: these are computed if not explicitly overridden.
      mandatory_thinking:
        overrides.mandatory_thinking ?? opencode_mandatory_thinking_id(clean),
      thinking_levels:
        overrides.thinking_levels ?? infer_thinking_levels(clean),
    };
  }

  return base;
};

export const resolve_context_window = (id: string, provider = provider_opencode): number => {
  const m = resolve_model(id, provider);
  if (m.context_window > 0) return m.context_window;
  return infer_context_window(id, provider);
};

export const resolve_supports_images = (id: string, provider = provider_opencode): boolean => {
  const m = resolve_model(id, provider);
  return !!m.supports_images;
};

export const resolve_reasoning = (id: string, provider = provider_opencode): boolean => {
  const m = resolve_model(id, provider);
  return m.reasoning || (m.reasoning_field !== undefined && m.reasoning_field !== "");
};

// ---------------------------------------------------------------------------
// Provider fallback ids (slug-only; all metadata comes from the resolver)
// ---------------------------------------------------------------------------

export const codex_fallback_ids = [
  "gpt-5.5",
  "gpt-5.4",
  "gpt-5.4-mini",
  "gpt-5.3-codex",
  "gpt-5.3-codex-spark",
  "gpt-5-codex",
];

export const neuralwatt_fallback_ids = ["glm-5.2"];

/*
PORT STATUS
new file
confidence: medium
status: phase_a_draft
notes:
  - Cascading resolver: catalog -> heuristic -> user overrides.
  - Keeps only small provider fallback slug lists for offline resilience.
  - User overrides file: ~/.hollow/models.json (partial model_info per id).
*/
