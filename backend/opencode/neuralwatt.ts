// PORT: backend/opencode/neuralwatt.go

import { Effect } from "effect";

export type neuralwatt_quota_response = {
  balance: { accounting_method: string };
  subscription?: { plan: string; status: string; kwh_included?: number; kwh_remaining?: number };
};

export let neuralwatt_quota_url = "https://api.neuralwatt.com/v1/quota";

// NeuralWattAccountSummary queries /v1/quota to report billing mode after connect.
// Energy vs token is an account setting on portal.neuralwatt.com — the API endpoint is the same.
export const neuralwatt_account_summary = (
  ctx: AbortSignal,
  api_key: string,
): Effect.Effect<string, Error> =>
  Effect.tryPromise({
    try: async () => {
      api_key = api_key.trim();
      if (api_key === "") throw new Error("api key cannot be empty");
      const resp = await fetch(neuralwatt_quota_url, { signal: ctx, headers: { Authorization: `Bearer ${api_key}` } });
      const raw = await resp.text();
      if (resp.status >= 400) throw new Error(`quota ${resp.status}: ${raw.trim()}`);
      const q = JSON.parse(raw) as neuralwatt_quota_response;
      const method = (q.balance?.accounting_method ?? "").trim().toLowerCase();
      switch (method) {
        case "energy": {
          let msg = "NeuralWatt billing: energy (kWh) — good, you're on energy pricing.";
          if (q.subscription !== undefined && q.subscription.status === "active") {
            msg += ` Subscription: ${q.subscription.plan}.`;
            if (q.subscription.kwh_remaining !== undefined && q.subscription.kwh_included !== undefined) {
              msg += ` Allowance: ${q.subscription.kwh_remaining.toFixed(2)} / ${q.subscription.kwh_included.toFixed(2)} kWh remaining.`;
            }
          } else {
            msg += " PAYG energy rate applies when not on a subscription.";
          }
          return msg;
        }
        case "token":
          return "NeuralWatt billing: TOKEN — switch to energy in portal.neuralwatt.com dashboard before heavy use, or you'll pay per-token rates.";
        default:
          if (method === "") throw new Error("quota response missing accounting_method");
          return `NeuralWatt billing: ${method} (unknown method — check portal.neuralwatt.com).`;
      }
    },
    catch: (cause) => cause instanceof Error ? cause : new Error(String(cause)),
  });

/*
PORT STATUS
source path: backend/opencode/neuralwatt.go
source lines: 118
draft lines: 84
confidence: high
status: phase_a_draft
todos:
  - verify response field casing against live NeuralWatt quota endpoint
notes:
  - NeuralWattAccountSummary returns (string, error), modeled as Effect.Effect<string, Error>.
*/
