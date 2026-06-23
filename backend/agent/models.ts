// PORT: backend/agent/models.go

import { provider_opencode } from "../opencode/providers";
import { resolve_context_window } from "../opencode/context";

export function ModelContextWindow(provider: string, model: string, configOverride: number): number {
  if (configOverride > 0) {
    return configOverride;
  }
  if (provider === "") {
    provider = provider_opencode;
  }
  const w = resolve_context_window(provider, model);
  if (w > 0) {
    return w;
  }
  const modelLower = model.toLowerCase();
  if (modelLower.includes("deepseek-chat")) {
    return 128000;
  }
  if (modelLower.includes("claude-3-5-sonnet")) {
    return 200000;
  }
  if (modelLower.includes("gpt-4o")) {
    return 128000;
  }
  if (modelLower.includes("gemini-1.5-pro")) {
    return 2000000;
  }
  if (modelLower.includes("gemini-1.5-flash")) {
    return 1000000;
  }
  return 128000;
}

/*
PORT STATUS
source path: backend/agent/models.go
source lines: 37
draft lines: 33
confidence: high
status: phase_b_compile
*/
