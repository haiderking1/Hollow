// PORT: backend/opencode/providers.go

export const provider_opencode = "opencode-go";
export const provider_opencode_zen = "opencode-zen";
export const provider_neuralwatt = "neuralwatt";
export const provider_codex = "openai-codex";

export type provider_info = { id: string; name: string };
export type thinking_level = { id: string; name: string };
export type model_info = {
  id: string;
  name: string;
  context_window: number;
  reasoning: boolean;
  supports_images?: boolean;
  thinking_levels?: thinking_level[];
  reasoning_field?: string;
  mandatory_thinking?: boolean;
};

export const default_reasoning_levels: thinking_level[] = [];

export const model_providers = (): provider_info[] => [
  { id: provider_opencode, name: "OpenCode Go" },
  { id: provider_opencode_zen, name: "OpenCode Zen" },
  { id: provider_neuralwatt, name: "NeuralWatt" },
  { id: provider_codex, name: "OpenAI Codex" },
];

import {
  resolve_model,
  codex_fallback_ids,
} from "./model_resolver";
import {
  sort_models,
  fallback_models,
} from "./models";

export const codex_models = (): model_info[] => {
  return codex_fallback_ids.map((id) => resolve_model(id, provider_codex));
};

export type registry_like = {
  codex_models_list?: () => model_info[];
  zen_models_list?: () => model_info[];
  neuralwatt_models_list?: () => model_info[];
  models?: () => model_info[];
  lookup_neuralwatt?: (id: string) => [model_info, boolean];
  lookup_codex?: (id: string) => [model_info, boolean];
};

export const models_for_provider = (provider: string, registry: registry_like | null): model_info[] => {
  switch (provider) {
    case provider_codex:
      return registry?.codex_models_list?.() ?? codex_models();
    case provider_opencode_zen: {
      const out = registry?.zen_models_list?.() ?? fallback_models(provider_opencode_zen);
      sort_models(out); return out;
    }
    case provider_neuralwatt: {
      const out = registry?.neuralwatt_models_list?.() ?? [];
      sort_models(out); return out;
    }
    default: {
      let out = registry?.models?.() ?? fallback_models(provider_opencode);
      if (out.length === 0) {
        out = fallback_models(provider_opencode);
      }
      sort_models(out);
      return out;
    }
  }
};

export const lookup_catalog_model = (id: string): [model_info, boolean] => {
  const clean = id.trim();
  if (clean === "") {
    return [{ id: "", name: "", context_window: 0, reasoning: false }, false];
  }
  const m = resolve_model(clean, provider_opencode);
  if (m.id !== "") {
    return [m, true];
  }
  return [{ id: "", name: "", context_window: 0, reasoning: false }, false];
};

export const provider_index = (provider: string): number => {
  const providers = model_providers();
  const idx = providers.findIndex((p) => p.id === provider);
  return idx < 0 ? 0 : idx;
};

/*
PORT STATUS
source path: backend/opencode/providers.go
source lines: 145
draft lines: 111
confidence: high
status: phase_b_compile
*/
