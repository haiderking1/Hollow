// PORT: backend/skills/tool_manage.go

import { Effect } from "effect";
import { type runtime } from "../config/config";
import { type SkillManageResult } from "./types";
import { subsystem_skills, stage_write, skill_gist } from "../approval/write_approval";
import {
  createSkill,
  editSkill,
  patchSkill,
  archiveDeleteSkill,
  deleteSkill,
  writeSkillFile,
  removeSkillFile,
} from "./manage_actions";
import { MarkAgentCreated, BumpPatch, Forget } from "./usage";

export interface SkillManageOptions {
  GuardEnabled: boolean;
  MarkCreatedAsAgent: boolean;
  // ArchiveOnDelete turns 'delete' into an archive (move to .archive/).
  // Set for background-review and curator forks — autonomous passes must
  // never destroy data; archives are recoverable. It also hard-protects
  // the curator-protected builtins from autonomous removal.
  ArchiveOnDelete: boolean;
  WriteApproval: boolean;
  BypassGate: boolean;
  Origin: string;
}

export interface SkillManageArgs {
  action: string;
  name: string;
  content?: string;
  old_string?: string;
  new_string?: string;
  replace_all?: boolean;
  category?: string;
  file_path?: string;
  file_content?: string;
  absorbed_into?: string;
}

export function ExecuteSkillManage(
  argsJSON: string,
  opts: SkillManageOptions
): Effect.Effect<[string, boolean], never> {
  let args: SkillManageArgs = { action: "", name: "" };
  try {
    args = JSON.parse(argsJSON);
  } catch {}

  if (opts.WriteApproval && !opts.BypassGate) {
    const payload: Record<string, unknown> = {
      action: args.action,
      name: args.name,
      content: args.content || "",
      old_string: args.old_string || "",
      new_string: args.new_string || "",
      replace_all: !!args.replace_all,
      category: args.category || "",
      file_path: args.file_path || "",
      file_content: args.file_content || "",
      absorbed_into: args.absorbed_into || "",
    };

    const gist = skill_gist(
      args.action,
      args.name,
      args.content || "",
      args.file_path || "",
      args.old_string || "",
      args.new_string || ""
    );
    const origin = opts.Origin !== "" ? opts.Origin : "agent";

    return stage_write(subsystem_skills, payload, gist, origin).pipe(
      Effect.match({
        onFailure: (err) => {
          const result: SkillManageResult = {
            success: false,
            error: "Staging failed: " + err.reason,
          };
          return [JSON.stringify(result, null, "  "), true] as [string, boolean];
        },
        onSuccess: (record) => {
          const result: SkillManageResult = {
            success: true,
            staged: true,
            pending_id: record.id,
            gist: gist,
            message: `Staged for approval (skills.write_approval is on). Not yet saved — review with /skills pending.`,
          };
          return [JSON.stringify(result, null, "  "), false] as [string, boolean];
        },
      })
    );
  }

  let actionEffect: Effect.Effect<SkillManageResult, Error>;
  switch (args.action) {
    case "create":
      actionEffect = createSkill(args.name, args.content || "", args.category || "", opts.GuardEnabled);
      break;
    case "edit":
      actionEffect = editSkill(args.name, args.content || "", opts.GuardEnabled);
      break;
    case "patch":
      actionEffect = patchSkill(
        args.name,
        args.old_string || "",
        args.new_string || "",
        args.file_path || "",
        !!args.replace_all,
        opts.GuardEnabled
      );
      break;
    case "delete":
      if (opts.ArchiveOnDelete) {
        actionEffect = archiveDeleteSkill(args.name, args.absorbed_into || "");
      } else {
        actionEffect = deleteSkill(args.name, args.absorbed_into || "", opts.GuardEnabled);
      }
      break;
    case "write_file":
      actionEffect = writeSkillFile(args.name, args.file_path || "", args.file_content || "", opts.GuardEnabled);
      break;
    case "remove_file":
      actionEffect = removeSkillFile(args.name, args.file_path || "");
      break;
    default:
      actionEffect = Effect.fail(
        new Error(`Unknown action '${args.action}'. Use: create, edit, patch, delete, write_file, remove_file`)
      );
  }

  return actionEffect.pipe(
    Effect.flatMap((result) => {
      if (result.success) {
        switch (args.action) {
          case "create":
            if (opts.MarkCreatedAsAgent) {
              MarkAgentCreated(args.name);
            }
            break;
          case "patch":
          case "edit":
          case "write_file":
          case "remove_file":
            BumpPatch(args.name);
            break;
          case "delete":
            if (!opts.ArchiveOnDelete) {
              Forget(args.name);
            }
            break;
        }
      }
      return Effect.succeed([JSON.stringify(result, null, "  "), !result.success] as [string, boolean]);
    }),
    Effect.catchAll((err) => {
      const result: SkillManageResult = {
        success: false,
        error: err.message,
      };
      return Effect.succeed([JSON.stringify(result, null, "  "), true] as [string, boolean]);
    })
  );
}

export function ApplySkillPending(
  payload: Record<string, unknown>,
  opts: SkillManageOptions
): Effect.Effect<SkillManageResult, Error> {
  return Effect.try({
    try: () => {
      const payloadString = JSON.stringify(payload);
      opts.BypassGate = true;
      // We run ExecuteSkillManage using Effect.runSync because ApplySkillPending in Go
      // calls ExecuteSkillManage synchronously.
      // Since ExecuteSkillManage returns Effect.Effect<[string, boolean], never>, it is safe.
      const [resJSON, isErr] = Effect.runSync(ExecuteSkillManage(payloadString, opts));
      const res = JSON.parse(resJSON) as SkillManageResult;
      if (isErr && !res.error) {
        res.error = "Action failed";
      }
      return res;
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  });
}

/*
PORT STATUS
source path: backend/skills/tool_manage.go
source lines: 156
draft lines: 181
confidence: high
status: phase_b_compile
*/
