// PORT: backend/agent/notify_helpers.go

import { subsystem_skills, subsystem_memory } from "../approval/write_approval";
import { Agent } from "./agent";

// notifyStagedWrite surfaces write-approval staging to the TUI (Hermes-style
// persistent system line) in addition to the inline tool result block.
export function notifyStagedWrite(this: Agent, toolOutput: string): void {
  let data: {
    staged?: boolean;
    pending_id?: string;
    gist?: string;
    message?: string;
    target?: string;
  };
  try {
    data = JSON.parse(toolOutput);
  } catch (err) {
    return;
  }
  // JSON.parse("null") / a non-object yields null — guard before reading .staged.
  if (!data || !data.staged) {
    return;
  }

  let subsystem = subsystem_skills;
  let where = "/skills";
  const msgLower = (data.message || "").toLowerCase();
  if (msgLower.includes("memory.write_approval") || data.target === "memory" || data.target === "user") {
    subsystem = subsystem_memory;
    where = "/memory";
  }

  let gist = (data.gist || "").trim();
  if (gist === "") {
    gist = (data.message || "").trim();
  }

  if (this.notify !== null) {
    if (data.pending_id) {
      this.notify(`⏳ Staged for approval: ${gist} — use ${where} approve ${data.pending_id}`);
    } else {
      this.notify(`⏳ Staged for approval — check ${where} pending`);
    }
  }
  if (this.approvalPrompt !== null && data.pending_id) {
    this.approvalPrompt(subsystem, data.pending_id);
  }
}

Agent.prototype.notifyStagedWrite = notifyStagedWrite;

// notifyDirectMemoryWrite surfaces successful immediate memory writes to the TUI.
// Staged writes are handled by notifyStagedWrite; this covers the common case
// where memory.write_approval is off and the tool applies directly.
export function notifyDirectMemoryWrite(this: Agent, argsJSON: string, toolOutput: string): void {
  if (this.notify === null) {
    return;
  }

  let result: {
    success?: boolean;
    staged?: boolean;
    target?: string;
    message?: string;
  };
  try {
    result = JSON.parse(toolOutput);
  } catch (err) {
    return;
  }
  // JSON.parse can yield null — guard before reading .success/.staged.
  if (!result || !result.success || result.staged) {
    return;
  }

  let args: {
    action?: string;
    target?: string;
    content?: string;
    match?: string;
    replacement?: string;
  };
  try {
    args = JSON.parse(argsJSON);
  } catch (err) {
    args = {};
  }

  let target = (args.target || "").trim();
  if (target === "") {
    target = (result.target || "").trim();
  }
  if (target === "") {
    target = "memory";
  }
  let label = "MEMORY.md";
  if (target === "user") {
    label = "USER.md";
  }

  let detail = "";
  switch (args.action) {
    case "add":
      detail = (args.content || "").trim();
      break;
    case "replace":
      detail = (args.replacement || "").trim();
      if (args.match && args.match !== "") {
        detail = `"${args.match}" → ${detail}`;
      }
      break;
    case "remove":
      detail = (args.match || "").trim();
      if (detail !== "") {
        detail = "remove " + detail;
      }
      break;
    default:
      detail = (result.message || "").trim();
  }

  if (detail === "") {
    detail = (result.message || "").trim();
  }
  if (detail === "") {
    detail = args.action || "";
  }
  if (detail.length > 120) {
    detail = detail.slice(0, 117) + "...";
  }

  this.notify(`💾 Saved to ${label}: ${detail}`);
}

Agent.prototype.notifyDirectMemoryWrite = notifyDirectMemoryWrite;

/*
PORT STATUS
source path: backend/agent/notify_helpers.go
source lines: 131
draft lines: 122
confidence: high
status: phase_b_compile
*/
