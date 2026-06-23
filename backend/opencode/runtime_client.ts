// PORT: backend/opencode/runtime_client.go

import type { runtime } from "../config/config";
import { client, new_client, new_codex_client } from "./client";
import { provider_codex } from "./providers";

// NewClientForRuntime builds the correct HTTP client for the active provider.
export const new_client_for_runtime = (cfg: runtime): client => {
  if (cfg.provider === provider_codex) {
    return new_codex_client(cfg.endpoint, cfg.api_key, cfg.model);
  }
  return new_client(cfg.endpoint, cfg.api_key, cfg.model);
};

/*
PORT STATUS
source path: backend/opencode/runtime_client.go
source lines: 12
draft lines: 14
confidence: high
status: phase_b_compile
*/
