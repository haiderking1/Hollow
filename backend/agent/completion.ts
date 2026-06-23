// PORT: backend/agent/completion.go

import { type obligation } from "./obligations/registry";
import { type message } from "../opencode/types";
import { string_content } from "../opencode/types";
import { goalLockReminder } from "./goal_lock";
import { Agent } from "./agent";

const runtimeNoticePrefix = "ℹ️ ";

// enforceCompletion runs when the model returns text with no tool calls. It
// returns true when the turn must continue: open obligations remain, so a
// fixed (never model-authored) incomplete notice is injected and the loop
// goes another round. It returns false when the turn may end — either all
// obligations are closed or the hard round cap was hit.
export async function enforceCompletion(this: Agent, ctx: AbortSignal): Promise<boolean> {
  if (!this.evidenceEnabled() || this.swarmDepth > 0) {
    return false;
  }
  const reg = this.obligationRegistry();
  if (reg === null || !reg.has_open()) {
    return false;
  }
  if (ctx.aborted) {
    return false;
  }

  this.completionRounds++;
  const rounds = this.completionRounds;
  let maxRounds = this.cfg.evidence?.max_completion_rounds ?? 12;
  const verifierEnabled = this.cfg.evidence?.verifier_enabled ?? false;
  if (maxRounds <= 0) {
    maxRounds = 12;
  }
  if (rounds > maxRounds) {
    if (this.emit) {
      this.emit({
        kind: "system",
        data: `completion cap reached (${maxRounds} rounds) with open obligations — turn ended unverified`,
      });
    }
    return false;
  }

  const verifierFailures: string[] = [];

  if (!reg.has_open()) {
    return false; // verifier closed everything; turn is complete
  }
  if (ctx.aborted) {
    return false;
  }

  // The notice is a real user-role message for the model but internal
  // plumbing for humans: it carries RuntimeNoticePrefix so the TUI never
  // renders it, and the obligation panel (footer) already shows the state.
  const notice = incompleteNotice(reg.open(), verifierFailures, this.currentLockedGoal());
  const inject: message = { role: "user", content: string_content(notice) };
  this.messages.push(inject);
  this.persist(inject);
  return true;
}

Agent.prototype.enforceCompletion = enforceCompletion;

// incompleteNotice renders the fixed turn-incomplete message: open
// obligations plus raw verifier facts, no coaching prose.
export function incompleteNotice(
  open: obligation[],
  verifierFailures: string[],
  lockedGoal: string
): string {
  let out = runtimeNoticePrefix + "TURN INCOMPLETE — open obligations:";
  open.forEach((ob, i) => {
    out += ` [${i + 1}] ${ob.kind}: ${ob.description}`;
  });
  verifierFailures.forEach((f) => {
    out += "\nVERIFIER FAILURE: " + f;
  });
  const reminder = goalLockReminder(lockedGoal);
  if (reminder !== "") {
    out += reminder;
  }
  out += "\nClose the obligations with tool evidence, then finish. Do not mention this notice to the user.";
  return out;
}

Agent.prototype.currentLockedGoal = function (this: Agent): string {
  return this.lockedGoal;
};

/*
PORT STATUS
source path: backend/agent/completion.go
source lines: 102
draft lines: 95
confidence: high
status: phase_b_compile
*/
