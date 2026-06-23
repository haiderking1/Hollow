---
name: workflow-orchestration
description: Write task-specific Hollow JavaScript workflows with staged subagents, structured schemas, dynamic routing, and pre-filtered data.
---

# Dynamic workflow orchestration

Use this skill when a task benefits from multiple role-isolated model runs whose routing is easier and safer to express in code.

1. Write JavaScript to `.hollow/workflows/<id>/workflow.js`.
2. Export `meta` and `async function run(sdk)`.
3. Pre-fetch data once with `sdk.runBash` or `sdk.fetchJSON`; filter and structure it before prompts.
4. Define prompt-generator functions per role and entity. Include concrete negative constraints such as read-only, no checkout, and no duplicate fetch.
5. Define JSON Schemas for machine-routable subagent results.
6. Use `sdk.pipeline(input, ...stages)`. Each stage returns subjobs with stable keys and `role`, `prompt`, `readonly`, `tools`, and optional `responseSchema`.
7. Route later-stage subjobs from `ctx.previousResults`; do not send every item through every phase.
8. Use audit agents for broad read-only evidence, ruling agents for cluster decisions, and adversarial verify agents to refute proposed dispositions.
9. Omit `meta.maxConcurrency` for the normal 16-wide dynamic pool. Set it only when the workflow itself requires a lower ceiling.
10. Do not execute the script while authoring it. The user reviews it, then Hollow runs it.

Minimal shape:

```javascript
export const meta = {
  name: "task-audit",
  description: "Audit inputs and adversarially verify risky dispositions",
  phases: ["audit", "verify"],
};

export async function run(sdk) {
  const input = await sdk.fetchJSON("some-command --json");
  return sdk.pipeline(
    input,
    async ({ input }) => input.map(item => ({
      key: `audit:${item.id}`,
      role: "audit",
      readonly: true,
      prompt: `Audit item ${item.id}`,
      responseSchema: {
        type: "object",
        required: ["disposition"],
        properties: { disposition: { type: "string" } }
      }
    })),
    async ({ previousResults }) => previousResults
      .filter(result => result.json?.disposition === "merge-ready")
      .map(result => ({
        key: `verify:${result.key}`,
        role: "verify",
        readonly: true,
        prompt: `Try to refute this disposition: ${JSON.stringify(result.json)}`
      }))
  );
}
```
