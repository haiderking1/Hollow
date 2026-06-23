// PORT: backend/agent/obligations/registry.go

import { command_matches_any } from "./match";

export type kind = string;

export const kind_must_read_before_write: kind = "must_read_before_write";
export const kind_must_run_verify: kind = "must_run_verify";
export const kind_diff_touches_scope: kind = "diff_touches_scope";
export const kind_verifier_signed_off: kind = "verifier_signed_off";

export type status = string;

export const status_open: status = "open";
export const status_closed: status = "closed";

export type obligation = {
  id: string;
  turn_id: string;
  kind: kind;
  description: string;
  command: string; // verify command for must_run_verify
  status: status;
  closed_by: string; // evidence entry ID
  closed_at: Date;
};

class mutex {
  private _locked = false;
  lock(): void { this._locked = true; }
  unlock(): void { this._locked = false; }
}

// Registry tracks the obligations of one turn. Safe for concurrent use.
export class registry {
  private readonly _mu = new mutex();
  private readonly _turn_id: string;
  private readonly _items: obligation[] = [];
  private _seq = 0;
  private readonly _verify_cmd: string;
  private readonly _extra_verify_cmds: string[];
  private readonly _strict_reset: boolean;
  private readonly _verifier_enabled: boolean;

  constructor(
    turn_id: string,
    verify_cmd: string,
    extra_verify_cmds: string[],
    strict_reset: boolean,
    verifier_enabled: boolean,
  ) {
    this._turn_id = turn_id;
    this._verify_cmd = verify_cmd;
    this._extra_verify_cmds = [...extra_verify_cmds];
    this._strict_reset = strict_reset;
    this._verifier_enabled = verifier_enabled;
  }

  extra_verify_commands(): string[] {
    this._mu.lock();
    this._mu.unlock();
    return [...this._extra_verify_cmds];
  }

  verify_command(): string {
    this._mu.lock();
    this._mu.unlock();
    return this._verify_cmd;
  }

  private add(kind: kind, desc: string, cmd: string): obligation {
    this._seq++;
    const ob: obligation = {
      id: `ob_${this._seq}`,
      turn_id: this._turn_id,
      kind,
      description: desc,
      command: cmd,
      status: status_open,
      closed_by: "",
      closed_at: new Date(0),
    };
    this._items.push(ob);
    return ob;
  }

  private find_kind(kind: kind): obligation | null {
    for (const ob of this._items) {
      if (ob.kind === kind) {
        return ob;
      }
    }
    return null;
  }

  // NoteMutation records that the workspace changed. It instantiates the verify
  // obligation on first mutation and — in strict mode — reopens a previously
  // closed verify run, since evidence gathered before the change proves nothing
  // about the code that exists now. Returns true if the obligation set changed.
  note_mutation(): boolean {
    this._mu.lock();
    this._mu.unlock();

    let changed = false;
    let ob = this.find_kind(kind_must_run_verify);
    if (ob === null) {
      let desc = "run project verification";
      if (this._verify_cmd !== "") {
        desc = `${this._verify_cmd} must exit 0 (run via bash)`;
      } else {
        desc =
          "no auto-verify detected — run an explicit verification command via bash (exit 0)";
      }
      if (this._extra_verify_cmds.length > 0) {
        desc += "; task checks: " + this._extra_verify_cmds.join("; ");
      }
      ob = this.add(kind_must_run_verify, desc, this._verify_cmd);
      changed = true;
    } else if (this._strict_reset && ob.status === status_closed) {
      ob.status = status_open;
      ob.closed_by = "";
      ob.closed_at = new Date(0);
      changed = true;
    }

    if (this._verifier_enabled) {
      let so = this.find_kind(kind_verifier_signed_off);
      if (so === null) {
        this.add(kind_verifier_signed_off, "verifier must sign off on this turn", "");
        changed = true;
      } else if (this._strict_reset && so.status === status_closed) {
        so.status = status_open;
        so.closed_by = "";
        so.closed_at = new Date(0);
        changed = true;
      }
    }
    return changed;
  }

  // NoteCommandRun closes must_run_verify when the executed command counts as
  // verification for this task. Returns true on state change.
  note_command_run(
    command: string,
    exit_code: number,
    evidence_id: string,
    touches_mutation: boolean,
  ): boolean {
    this._mu.lock();
    this._mu.unlock();

    const ob = this.find_kind(kind_must_run_verify);
    if (ob === null || ob.status === status_closed) {
      return false;
    }

    const manual_mode = this._verify_cmd === "" && this._extra_verify_cmds.length === 0;
    const matches_verify =
      manual_mode || command_matches_any(command, this._verify_cmd, this._extra_verify_cmds);
    const pytest_no_tests =
      exit_code === 5 &&
      command.includes("pytest") &&
      (manual_mode || command_matches_any(command, this._verify_cmd, this._extra_verify_cmds));

    const passed = exit_code === 0 && (matches_verify || touches_mutation);
    if (!passed && !pytest_no_tests) {
      return false;
    }

    const now = new Date();
    ob.status = status_closed;
    ob.closed_by = evidence_id;
    ob.closed_at = now;

    const so = this.find_kind(kind_verifier_signed_off);
    if (so !== null && so.status === status_open) {
      so.status = status_closed;
      so.closed_by = evidence_id;
      so.closed_at = now;
    }
    return true;
  }

  // NoteVerifierPass closes verifier_signed_off. The caller must have already
  // validated the verifier's claim against ledger evidence.
  note_verifier_pass(evidence_id: string): boolean {
    this._mu.lock();
    this._mu.unlock();

    const ob = this.find_kind(kind_verifier_signed_off);
    if (ob === null || ob.status === status_closed) {
      return false;
    }
    ob.status = status_closed;
    ob.closed_by = evidence_id;
    ob.closed_at = new Date();
    return true;
  }

  // VerifyClosed reports whether must_run_verify exists and is closed.
  verify_closed(): boolean {
    this._mu.lock();
    this._mu.unlock();
    const ob = this.find_kind(kind_must_run_verify);
    return ob !== null && ob.status === status_closed;
  }

  // Open returns copies of all open obligations in creation order.
  open(): obligation[] {
    this._mu.lock();
    this._mu.unlock();
    const out: obligation[] = [];
    for (const ob of this._items) {
      if (ob.status === status_open) {
        out.push({ ...ob });
      }
    }
    return out;
  }

  // Snapshot returns copies of all obligations in creation order.
  snapshot(): obligation[] {
    this._mu.lock();
    this._mu.unlock();
    return this._items.map((ob) => ({ ...ob }));
  }

  has_open(): boolean {
    return this.open().length > 0;
  }
}

/*
PORT STATUS
source path: backend/agent/obligations/registry.go
source lines: 237
draft lines: 245
confidence: high
status: phase_a_draft
todos:
  - confirm Date(0) is an acceptable stand-in for Go's zero time.Time
  - verify shallow copies ({ ...ob }) are sufficient for obligation immutability
notes:
  - No (T, error) returns; class method port.
*/
