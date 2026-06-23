import { Agent, type toolResult, WriteOriginForeground, WriteOriginBackgroundReview } from "./agent";
import { new_client_for_runtime } from "../opencode/runtime_client";
import { type runtime } from "../config/config";
import { type message, type tool, string_content, content_string } from "../opencode/types";
import { nativeTools } from "./tools_registry";
import { subsystem_memory, stage_write } from "../approval/write_approval";
import { ToolName, ToolDescription, ToolParameters, ExecuteMemoryTool, IsMutatingAction } from "../skills/../memory/tool";
import { Effect } from "effect";

// reviewMaxIterations bounds the fork's model calls per pass.
export const reviewMaxIterations = 16;

// reviewToolWhitelist is the fork's hard allowlist.
export const reviewToolWhitelist: Record<string, boolean> = {
  "memory": true,
  "skills_list": true,
  "skill_view": true,
  "skill_manage": true,
};

export const memoryReviewPrompt =
  "Review the conversation above and consider saving to memory if appropriate.\n\n" +
  "Focus on:\n" +
  "1. Has the user revealed things about themselves — their persona, desires, " +
  "preferences, or personal details worth remembering?\n" +
  "2. Has the user expressed expectations about how you should behave, their work " +
  "style, or ways they want you to operate?\n" +
  "3. Did the user correct something you said based on USER PROFILE or MEMORY, or " +
  "clarify how to interpret a profile entry (e.g. full name vs nickname, spelling " +
  "preference)? Use memory(action=replace, target=user) to fix the stored entry — " +
  "never leave ambiguous or wrong profile text on disk.\n\n" +
  "If something stands out, save it using the memory tool. " +
  "If nothing is worth saving, just say 'Nothing to save.' and stop.";

export const skillReviewPrompt =
  "Review the conversation above and update the skill library. Be " +
  "ACTIVE — most sessions produce at least one skill update, even if " +
  "small. A pass that does nothing is a missed learning opportunity, " +
  "not a neutral outcome.\n\n" +
  "Target shape of the library: CLASS-LEVEL skills, each with a rich " +
  "SKILL.md and a `references/` directory for session-specific detail. " +
  "Not a long flat list of narrow one-session-one-skill entries. This " +
  "shapes HOW you update, not WHETHER you update.\n\n" +
  "Signals to look for (any one of these warrants action):\n" +
  "  • User corrected your style, tone, format, legibility, or " +
  "verbosity. Frustration signals like 'stop doing X', 'this is too " +
  "verbose', 'don't format like this', 'why are you explaining', " +
  "'just give me the answer', 'you always do Y and I hate it', or an " +
  "explicit 'remember this' are FIRST-CLASS skill signals, not just " +
  "memory signals. Update the relevant skill(s) to embed the " +
  "preference so the next session starts already knowing.\n" +
  "  • User corrected your workflow, approach, or sequence of steps. " +
  "Encode the correction as a pitfall or explicit step in the skill " +
  "that governs that class of task.\n" +
  "  • Non-trivial technique, fix, workaround, debugging path, or " +
  "tool-usage pattern emerged that a future session would benefit " +
  "from. Capture it.\n" +
  "  • A skill that got loaded or consulted this session turned out " +
  "to be wrong, missing a step, or outdated. Patch it NOW.\n\n" +
  "Preference order — prefer the earliest action that fits, but do " +
  "pick one when a signal above fired:\n" +
  "  1. UPDATE A CURRENTLY-LOADED SKILL. Look back through the " +
  "conversation for skills the user loaded via /skill:<name> or you " +
  "read via skill_view. If any of them covers the territory of the " +
  "new learning, PATCH that one first. It is the skill that was in " +
  "play, so it's the right one to extend.\n" +
  "  2. UPDATE AN EXISTING UMBRELLA (via skills_list + skill_view). " +
  "If no loaded skill fits but an existing class-level skill does, " +
  "patch it. Add a subsection, a pitfall, or broaden a trigger.\n" +
  "  3. ADD A SUPPORT FILE under an existing umbrella. Skills can be " +
  "packaged with three kinds of support files — use the right " +
  "directory per kind:\n" +
  "     • `references/<topic>.md` — session-specific detail (error " +
  "transcripts, reproduction recipes, provider quirks) AND " +
  "condensed knowledge banks: quoted research, API docs, external " +
  "authoritative excerpts, or domain notes you found while working " +
  "on the problem. Write it concise and for the value of the task, " +
  "not as a full mirror of upstream docs.\n" +
  "     • `templates/<name>.<ext>` — starter files meant to be " +
  "copied and modified (boilerplate configs, scaffolding, a " +
  "known-good example the agent can `reproduce with modifications`).\n" +
  "     • `scripts/<name>.<ext>` — statically re-runnable actions " +
  "the skill can invoke directly (verification scripts, fixture " +
  "generators, deterministic probes, anything the agent should run " +
  "rather than hand-type each time).\n" +
  "     Add support files via skill_manage action=write_file with " +
  "file_path starting 'references/', 'templates/', or 'scripts/'. " +
  "The umbrella's SKILL.md should gain a one-line pointer to any " +
  "new support file so future agents know it exists.\n" +
  "  4. CREATE A NEW CLASS-LEVEL UMBRELLA SKILL when no existing " +
  "skill covers the class. The name MUST be at the class level. " +
  "The name MUST NOT be a specific PR number, error string, feature " +
  "codename, library-alone name, or 'fix-X / debug-Y / audit-Z-today' " +
  "session artifact. If the proposed name only makes sense for " +
  "today's task, it's wrong — fall back to (1), (2), or (3).\n\n" +
  "User-preference embedding (important): when the user expressed a " +
  "style/format/workflow preference, the update belongs in the " +
  "SKILL.md body, not just in memory. Memory captures 'who the user " +
  "is and what the current situation and state of your operations " +
  "are'; skills capture 'how to do this class of task for this " +
  "user'. When they complain about how you handled a task, the " +
  "skill that governs that task needs to carry the lesson.\n\n" +
  "If you notice two existing skills that overlap, note it in your " +
  "reply — the background curator handles consolidation at scale.\n\n" +
  "Protected skills (DO NOT edit these):\n" +
  "  • Bundled skills (shipped with Hollow, e.g. 'hollow-agent').\n" +
  "  • Skills installed from external sources.\n" +
  "Pinned skills (marked via /curator-pin) CAN be improved — " +
  "pin only blocks deletion/archive/consolidation by the curator, not " +
  "content updates. Patch them when a pitfall or missing step turns up, " +
  "same as any other agent-created skill.\n" +
  "If the only skills that need updating are protected, say\n" +
  "'Nothing to save.' and stop.\n\n" +
  "Do NOT capture (these become persistent self-imposed constraints " +
  "that bite you later when the environment changes):\n" +
  "  • Environment-dependent failures: missing binaries, fresh-install " +
  "errors, post-migration path mismatches, 'command not found', " +
  "unconfigured credentials, uninstalled packages. The user can fix " +
  "these — they are not durable rules.\n" +
  "  • Negative claims about tools or features ('browser tools do not " +
  "work', 'X tool is broken', 'cannot use Y from bash'). These " +
  "harden into refusals the agent cites against itself for months " +
  "after the actual problem was fixed.\n" +
  "  • Session-specific transient errors that resolved before the " +
  "conversation ended. If retrying worked, the lesson is the retry " +
  "pattern, not the original failure.\n" +
  "  • One-off task narratives. A user asking 'summarize today's " +
  "market' or 'analyze this PR' is not a class of work that warrants " +
  "a skill.\n\n" +
  "If a tool failed because of setup state, capture the FIX (install " +
  "command, config step, env var to set) under an existing setup or " +
  "troubleshooting skill — never 'this tool does not work' as a " +
  "standalone constraint.\n\n" +
  "If a tool failed because of setup state, capture the FIX (install " +
  "command, config step, env var to set) under an existing setup or " +
  "troubleshooting skill — never 'this tool does not work' as a " +
  "standalone constraint.\n\n" +
  "'Nothing to save.' is a real option but should NOT be the " +
  "default. If the session ran smoothly with no corrections and " +
  "produced no new technique, just say 'Nothing to save.' and stop. " +
  "Otherwise, act.";

export const combinedReviewPrompt =
  "Review the conversation above and update two things:\n\n" +
  "**Memory**: who the user is. Did the user reveal persona, " +
  "desires, preferences, personal details, or expectations about " +
  "how you should behave? Did the user correct a misread of USER PROFILE or " +
  "clarify name/spelling/nickname usage? Use memory replace on target=user to " +
  "fix stored profile entries — not add duplicates. Save other durable facts with " +
  "the memory tool.\n\n" +
  "**Skills**: how to do this class of task. Be ACTIVE — most " +
  "sessions produce at least one skill update. A pass that does " +
  "nothing is a missed learning opportunity, not a neutral outcome.\n\n" +
  "Target shape of the skill library: CLASS-LEVEL skills with a rich " +
  "SKILL.md and a `references/` directory for session-specific detail. " +
  "Not a long flat list of narrow one-session-one-skill entries.\n\n" +
  "Signals that warrant a skill update (any one is enough):\n" +
  "  • User corrected your style, tone, format, legibility, " +
  "verbosity, or approach. Frustration is a FIRST-CLASS skill " +
  "signal, not just a memory signal. 'stop doing X', 'don't format " +
  "like this', 'I hate when you Y' — embed the lesson in the skill " +
  "that governs that task so the next session starts fixed.\n" +
  "  • Non-trivial technique, fix, workaround, or debugging path " +
  "emerged.\n" +
  "  • A skill that was loaded or consulted turned out wrong, " +
  "missing, or outdated — patch it now.\n\n" +
  "Preference order for skills — pick the earliest that fits:\n" +
  "  1. UPDATE A CURRENTLY-LOADED SKILL. Check what skills were " +
  "loaded via /skill:<name> or skill_view in the conversation. If one " +
  "of them covers the learning, PATCH it first. It was in play; " +
  "it's the right place.\n" +
  "  2. UPDATE AN EXISTING UMBRELLA (skills_list + skill_view to " +
  "find the right one). Patch it.\n" +
  "  3. ADD A SUPPORT FILE under an existing umbrella via " +
  "skill_manage action=write_file. Three kinds: " +
  "`references/<topic>.md` for session-specific detail OR condensed " +
  "knowledge banks (quoted research, API docs excerpts, domain " +
  "notes) written concise and task-focused; `templates/<name>.<ext>` " +
  "for starter files meant to be copied and modified; " +
  "`scripts/<name>.<ext>` for statically re-runnable actions " +
  "(verification, fixture generators, probes). Add a one-line " +
  "pointer in SKILL.md so future agents know it exists.\n" +
  "  4. CREATE A NEW CLASS-LEVEL UMBRELLA when nothing exists. " +
  "Name at the class level — NOT a PR number, error string, " +
  "codename, library-alone name, or 'fix-X / debug-Y' session " +
  "artifact. If the name only fits today's task, fall back to (1), " +
  "(2), or (3).\n\n" +
  "User-preference embedding: when the user complains about how " +
  "you handled a task, update the skill that governs that task — " +
  "memory alone isn't enough. Memory says 'who the user is and " +
  "what the current situation and state of your operations are'; " +
  "skills say 'how to do this class of task for this user'. Both " +
  "should carry user-preference lessons when relevant.\n\n" +
  "If you notice overlapping existing skills, mention it — the " +
  "background curator handles consolidation.\n\n" +
  "Protected skills (DO NOT edit these):\n" +
  "  • Bundled skills (shipped with Hollow, e.g. 'hollow-agent').\n" +
  "  • Skills installed from external sources.\n" +
  "Pinned skills (marked via /curator-pin) CAN be improved — " +
  "pin only blocks deletion/archive/consolidation by the curator, not " +
  "content updates. Patch them when a pitfall or missing step turns up, " +
  "same as any other agent-created skill.\n" +
  "If the only skills that need updating are protected, say\n" +
  "'Nothing to save.' and stop.\n\n" +
  "Do NOT capture as skills (these become persistent self-imposed " +
  "constraints that bite you later when the environment changes):\n" +
  "  • Environment-dependent failures: missing binaries, fresh-install " +
  "errors, post-migration path mismatches, 'command not found', " +
  "unconfigured credentials, uninstalled packages. The user can fix " +
  "these — they are not durable rules.\n" +
  "  • Negative claims about tools or features ('browser tools do not " +
  "work', 'X tool is broken', 'cannot use Y from bash'). These " +
  "harden into refusals the agent cites against itself for months " +
  "after the actual problem was fixed.\n" +
  "  • Session-specific transient errors that resolved before the " +
  "conversation ended. If retrying worked, the lesson is the retry " +
  "pattern, not the original failure.\n" +
  "  • One-off task narratives. A user asking 'summarize today's " +
  "market' or 'analyze this PR' is not a class of work that warrants " +
  "a skill.\n\n" +
  "If a tool failed because of setup state, capture the FIX (install " +
  "command, config step, env var to set) under an existing setup or " +
  "troubleshooting skill — never 'this tool does not work' as a " +
  "standalone constraint.\n\n" +
  "Act on whichever of the two dimensions has real signal. If " +
  "genuinely nothing stands out on either, say 'Nothing to save.' " +
  "and stop — but don't reach for that conclusion as a default.";

export const reviewToolNote = "\n\nYou can only call memory and skill " +
  "management tools. Other tools will be denied at runtime — do not attempt them.";

// toolMenu builds the tool definitions offered to the model.
export function toolMenu(this: Agent): tool[] {
  let tools = nativeTools(this.cfg);
  if (this.cfg.memory?.memory_enabled || this.cfg.memory?.user_profile_enabled) {
    tools = [...tools, memoryNativeTool()];
  }
  if (this.swarmDepth === 0 && this.mcpManager !== null) {
    try {
      tools = [...tools, ...this.mcpManager.tools()];
    } catch {}
  }
  if (this.allowedTools === null) {
    return tools;
  }
  const out: tool[] = [];
  for (const t of tools) {
    if (this.allowedTools[t.function.name]) {
      out.push(t);
    }
  }
  return out;
}

Agent.prototype.toolMenu = toolMenu;

export function memoryNativeTool(): tool {
  return {
    type: "function",
    function: {
      name: ToolName,
      description: ToolDescription,
      parameters: new TextEncoder().encode(ToolParameters),
    },
  };
}

export function toolMemory(this: Agent, argsJSON: string): Effect.Effect<toolResult, Error> {
  const self = this;
  return Effect.gen(function* () {
    const isMutating = IsMutatingAction(argsJSON);
    if (self.cfg.memory?.write_approval && isMutating) {
      let args: {
        action?: string;
        target?: string;
        content?: string;
        match?: string;
        replacement?: string;
      } = {};
      try {
        args = JSON.parse(argsJSON);
      } catch {
        // ignore
      }

      const target = args.target || "memory";
      const payload = {
        action: args.action,
        target: target,
        content: args.content,
        match: args.match,
        replacement: args.replacement,
      };

      let summary = "";
      const label = target === "user" ? "user profile" : "memory";
      if (args.action === "add") {
        summary = `add to ${label}: ${args.content || ""}`;
      } else if (args.action === "replace") {
        summary = `replace in ${label}: "${args.match || ""}" with "${args.replacement || ""}"`;
      } else if (args.action === "remove") {
        summary = `remove from ${label}: "${args.match || ""}"`;
      }

      const record = yield* stage_write(subsystem_memory, payload, summary, self.writeOrigin).pipe(
        Effect.mapError(err => new Error(err.reason))
      );

      const output = JSON.stringify({
        success: true,
        staged: true,
        pending_id: record.id,
        gist: summary,
        message: "Staged for approval (memory.write_approval is on). Not yet saved — review with /memory pending.",
      }, null, 2);

      return { output, isErr: false };
    }

    const [output, isErr] = yield* ExecuteMemoryTool(argsJSON, self.memStore);
    if (!isErr && self.writeOrigin === WriteOriginForeground && isMutating) {
      self.turnsSinceMemory = 0;
    }
    return { output, isErr };
  });
}

Agent.prototype.toolMemory = toolMemory;

// maybeSpawnBackgroundReview checks the complete triggers and fires the review fork.
export function maybeSpawnBackgroundReview(this: Agent, shouldReviewMemory: boolean): void {
  const reviewMemory = shouldReviewMemory && !!this.cfg.memory?.memory_enabled && this.memStore !== null;
  let reviewSkills = false;
  if (
    this.cfg.memory?.skill_nudge_interval &&
    this.cfg.memory.skill_nudge_interval > 0 &&
    this.itersSinceSkill >= this.cfg.memory.skill_nudge_interval &&
    this.cfg.skills?.enabled
  ) {
    reviewSkills = true;
    this.itersSinceSkill = 0;
  }

  if (!reviewMemory && !reviewSkills) {
    return;
  }

  if (this.messages.length === 0) {
    return;
  }
  const last = this.messages[this.messages.length - 1];
  if (
    last.role !== "assistant" ||
    (last.tool_calls && last.tool_calls.length > 0) ||
    content_string(last).trim() === ""
  ) {
    return;
  }

  const snapshot: message[] = [];
  for (const m of this.messages) {
    if (m.role === "system") {
      continue;
    }
    snapshot.push(m);
  }

  const cachedPrompt = this.systemPrompt();
  const cfg = this.cfg;
  const notify = this.notify;

  let prompt = skillReviewPrompt;
  if (reviewMemory && reviewSkills) {
    prompt = combinedReviewPrompt;
  } else if (reviewMemory) {
    prompt = memoryReviewPrompt;
  }

  const self = this;
  const reviewPromise = (async () => {
    try {
      if (notify !== null) {
        let kind = "skill library";
        if (reviewMemory && !reviewSkills) {
          kind = "memory";
        } else if (reviewMemory && reviewSkills) {
          kind = "memory + skills";
        }
        notify(`💾 Self-improvement review running (${kind})…`);
      }
      await runBackgroundReview.call(self, cfg, cachedPrompt, snapshot, prompt, notify);
    } catch (e) {
      // ignore
    }
  })();
  this.reviewWG.push(reviewPromise);
}

Agent.prototype.maybeSpawnBackgroundReview = maybeSpawnBackgroundReview;

export async function WaitForBackgroundReviews(this: Agent): Promise<void> {
  await Promise.all(this.reviewWG);
}

// runBackgroundReview executes one review pass in a forked Agent.
export async function runBackgroundReview(
  this: Agent,
  cfg: runtime,
  cachedPrompt: string,
  snapshot: message[],
  prompt: string,
  notify: ((msg: string) => void) | null
): Promise<void> {
  const childCfg = { ...cfg };
  childCfg.compaction = { ...childCfg.compaction, enabled: false };
  childCfg.evidence = { ...childCfg.evidence, enabled: false };
  childCfg.memory = { ...childCfg.memory, nudge_interval: 0, skill_nudge_interval: 0 };

  const review = new Agent();
  review.cfg = childCfg;
  review.client = new_client_for_runtime(childCfg);
  review.workDir = this.workDir;
  review.session = null;
  review.allowedTools = reviewToolWhitelist;
  review.memStore = this.memStore;
  review.writeOrigin = WriteOriginBackgroundReview;
  review.cachedSystemPrompt = cachedPrompt;
  review.maxIterations = reviewMaxIterations;
  review.swarmDepth = 3;
  review.notify = notify;
  review.executeTool = review.executeTool || Agent.prototype.executeTool;

  review.messages = [
    { role: "system", content: string_content(cachedPrompt) },
    ...snapshot,
  ];
  const startIdx = review.messages.length;
  review.messages.push({
    role: "user",
    content: string_content(prompt + reviewToolNote),
  });

  try {
    const controller = new AbortController();
    await review.runLoop(controller.signal);
  } catch (err) {
    return;
  }

  const actions = summarizeReviewActions(review.messages.slice(startIdx));
  if (actions.length > 0 && notify !== null) {
    notify(`💾 Self-improvement review: ${actions.join(" · ")}`);
  }
}

Agent.prototype.runBackgroundReview = runBackgroundReview;

// summarizeReviewActions builds the action summary from the review fork's new tool results.
export function summarizeReviewActions(reviewMessages: message[]): string[] {
  const actions: string[] = [];
  const seen = new Set<string>();
  const add = (s: string) => {
    if (s !== "" && !seen.has(s)) {
      seen.add(s);
      actions.push(s);
    }
  };

  for (const msg of reviewMessages) {
    if (msg.role !== "tool") {
      continue;
    }
    let data: {
      success?: boolean;
      staged?: boolean;
      pending_id?: string;
      gist?: string;
      message?: string;
      target?: string;
    } = {};
    try {
      data = JSON.parse(content_string(msg));
    } catch {
      continue;
    }
    // JSON.parse can yield null — guard before reading .success/.staged.
    if (!data || !data.success) {
      continue;
    }
    if (data.staged) {
      let gist = (data.gist || "").trim();
      if (gist === "") {
        gist = (data.message || "").trim();
      }
      if (gist !== "") {
        add("staged: " + gist);
      }
      continue;
    }
    const lower = (data.message || "").toLowerCase();
    let label = data.target || "";
    if (data.target === "memory") {
      label = "Memory";
    } else if (data.target === "user") {
      label = "User profile";
    }
    if (lower.includes("created") || lower.includes("updated") || lower.includes("patched")) {
      add(data.message || "");
    } else if (lower.includes("added") || lower.includes("removed") || lower.includes("replaced")) {
      if (label !== "") {
        add(label + " updated");
      }
    }
  }
  return actions;
}

/*
PORT STATUS
source path: backend/agent/background_review.go
source lines: 546
draft lines: 470
confidence: high
status: phase_b_compile
*/
