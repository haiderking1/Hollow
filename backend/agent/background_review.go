package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/enough/enough/backend/approval"
	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/memory"
	"github.com/enough/enough/backend/opencode"
)

// Background memory/skill review — fork the agent to evaluate the turn.
//
// After a turn completes (response delivered, not interrupted), the agent may
// spawn a daemon goroutine that replays the conversation snapshot in a forked
// Agent and asks itself "should any skill/memory be saved or updated?".
// Writes go straight to the shared memory store and the skill library. The
// main conversation, its session JSONL, and the cached system prompt are
// never touched.
//
// The fork inherits the parent's live runtime (endpoint, model, API key,
// cached system prompt verbatim) so it hits the same prefix cache and uses
// the same auth. It runs with a hard tool whitelist limited to memory and
// skill management tools; everything else is denied at dispatch. Compaction
// is disabled on the fork.

// Write origins — skill provenance. Only skills created under
// WriteOriginBackgroundReview are marked agent-created (curator-eligible);
// foreground user-directed creations belong to the user.
const (
	WriteOriginForeground       = "foreground"
	WriteOriginBackgroundReview = "background_review"
)

// reviewMaxIterations bounds the fork's model calls per pass.
const reviewMaxIterations = 16

// reviewToolWhitelist is the fork's hard allowlist, enforced in guardTool.
var reviewToolWhitelist = map[string]bool{
	"memory":       true,
	"skills_list":  true,
	"skill_view":   true,
	"skill_manage": true,
}

// Review prompts — ported verbatim from Hermes agent/background_review.py,
// with Enough adaptations: the protected bundled skill is `enough` (Hermes:
// `hermes-agent`), pinning is done via /curator-pin (Hermes: `hermes curator
// pin`), and externally-installed skills replace "hub-installed".

const memoryReviewPrompt = "Review the conversation above and consider saving to memory if appropriate.\n\n" +
	"Focus on:\n" +
	"1. Has the user revealed things about themselves — their persona, desires, " +
	"preferences, or personal details worth remembering?\n" +
	"2. Has the user expressed expectations about how you should behave, their work " +
	"style, or ways they want you to operate?\n\n" +
	"If something stands out, save it using the memory tool. " +
	"If nothing is worth saving, just say 'Nothing to save.' and stop."

const skillReviewPrompt = "Review the conversation above and update the skill library. Be " +
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
	"  • Bundled skills (shipped with Enough, e.g. 'enough').\n" +
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
	"'Nothing to save.' is a real option but should NOT be the " +
	"default. If the session ran smoothly with no corrections and " +
	"produced no new technique, just say 'Nothing to save.' and stop. " +
	"Otherwise, act."

const combinedReviewPrompt = "Review the conversation above and update two things:\n\n" +
	"**Memory**: who the user is. Did the user reveal persona, " +
	"desires, preferences, personal details, or expectations about " +
	"how you should behave? Save facts about the user and durable " +
	"preferences with the memory tool.\n\n" +
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
	"pointer in SKILL.md so future agents find them.\n" +
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
	"  • Bundled skills (shipped with Enough, e.g. 'enough').\n" +
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
	"and stop — but don't reach for that conclusion as a default."

const reviewToolNote = "\n\nYou can only call memory and skill " +
	"management tools. Other tools will be denied at runtime — do not attempt them."

// toolMenu builds the tool definitions offered to the model: the native set
// plus the memory tool when enabled, filtered by the role allowlist.
func (a *Agent) toolMenu() []opencode.Tool {
	tools := nativeTools(a.cfg)
	if a.cfg.Memory.Enabled || a.cfg.Memory.UserProfileEnabled {
		tools = append(tools, memoryNativeTool())
	}
	if a.allowedTools == nil {
		return tools
	}
	var out []opencode.Tool
	for _, t := range tools {
		if a.allowedTools[t.Function.Name] {
			out = append(out, t)
		}
	}
	return out
}

func memoryNativeTool() opencode.Tool {
	return opencode.Tool{
		Type: "function",
		Function: opencode.ToolFunction{
			Name:        memory.ToolName,
			Description: memory.ToolDescription,
			Parameters:  json.RawMessage(memory.ToolParameters),
		},
	}
}

func (a *Agent) toolMemory(argsJSON string) toolResult {
	if a.cfg.Memory.WriteApproval && memory.IsMutatingAction(argsJSON) {
		var args struct {
			Action      string `json:"action"`
			Target      string `json:"target"`
			Content     string `json:"content"`
			Match       string `json:"match"`
			Replacement string `json:"replacement"`
		}
		_ = json.Unmarshal([]byte(argsJSON), &args)

		if args.Target == "" {
			args.Target = "memory"
		}

		payload := map[string]interface{}{
			"action":      args.Action,
			"target":      args.Target,
			"content":     args.Content,
			"match":       args.Match,
			"replacement": args.Replacement,
		}

		summary := ""
		label := "memory"
		if args.Target == "user" {
			label = "user profile"
		}
		if args.Action == "add" {
			summary = fmt.Sprintf("add to %s: %s", label, args.Content)
		} else if args.Action == "replace" {
			summary = fmt.Sprintf("replace in %s: %q with %q", label, args.Match, args.Replacement)
		} else if args.Action == "remove" {
			summary = fmt.Sprintf("remove from %s: %q", label, args.Match)
		}

		record, err := approval.StageWrite(approval.SubsystemMemory, payload, summary, a.writeOrigin)
		if err != nil {
			return toolResult{output: fmt.Sprintf(`{"success": false, "error": "Staging failed: %v"}`, err), isErr: true}
		}

		output := fmt.Sprintf(`{
  "success": true,
  "staged": true,
  "pending_id": %q,
  "gist": %q,
  "message": "Staged for approval (memory.write_approval is on). Not yet saved — review with /memory pending."
}`, record.ID, summary)
		return toolResult{output: output, isErr: false}
	}

	output, isErr := memory.ExecuteMemoryTool(argsJSON, a.memStore)
	// A direct foreground memory write means the model just reviewed its
	// memory — reset the nudge countdown.
	if !isErr && a.writeOrigin == WriteOriginForeground && memory.IsMutatingAction(argsJSON) {
		a.mu.Lock()
		a.turnsSinceMemory = 0
		a.mu.Unlock()
	}
	return toolResult{output: output, isErr: isErr}
}

// maybeSpawnBackgroundReview checks the turn-complete triggers and fires the
// review fork. Never blocks the caller; the user has already received the
// response.
func (a *Agent) maybeSpawnBackgroundReview(shouldReviewMemory bool) {
	a.mu.Lock()

	reviewMemory := shouldReviewMemory && a.cfg.Memory.Enabled && a.memStore != nil
	reviewSkills := false
	if a.cfg.Memory.SkillNudgeInterval > 0 &&
		a.itersSinceSkill >= a.cfg.Memory.SkillNudgeInterval &&
		a.cfg.Skills.Enabled {
		reviewSkills = true
		a.itersSinceSkill = 0
	}

	if !reviewMemory && !reviewSkills {
		a.mu.Unlock()
		return
	}

	// Require a delivered assistant response: the last transcript message
	// must be assistant text (no dangling tool calls, no interrupt stubs).
	if len(a.messages) == 0 {
		a.mu.Unlock()
		return
	}
	last := a.messages[len(a.messages)-1]
	if last.Role != "assistant" || len(last.ToolCalls) > 0 || strings.TrimSpace(opencode.ContentString(last)) == "" {
		a.mu.Unlock()
		return
	}

	// Snapshot the conversation (minus the system message — the fork
	// replays the parent's cached system prompt itself).
	var snapshot []opencode.Message
	for _, m := range a.messages {
		if m.Role == "system" {
			continue
		}
		snapshot = append(snapshot, m)
	}
	cachedPrompt := a.systemPrompt()
	cfg := a.cfg
	notify := a.notify
	a.mu.Unlock()

	prompt := skillReviewPrompt
	if reviewMemory && reviewSkills {
		prompt = combinedReviewPrompt
	} else if reviewMemory {
		prompt = memoryReviewPrompt
	}

	a.reviewWG.Add(1)
	go func() {
		defer a.reviewWG.Done()
		defer func() { _ = recover() }() // background review is best-effort
		a.runBackgroundReview(cfg, cachedPrompt, snapshot, prompt, notify)
	}()
}

// WaitForBackgroundReviews blocks until in-flight review goroutines finish.
// Used by tests and graceful shutdown.
func (a *Agent) WaitForBackgroundReviews() {
	a.reviewWG.Wait()
}

// runBackgroundReview executes one review pass in a forked Agent.
//
// Invariants: the fork never writes to the main session JSONL (session nil),
// never compacts (compaction disabled + no session), inherits the parent's
// cached system prompt verbatim (prefix-cache parity), and only memory/skill
// tools are dispatchable (whitelist enforced in guardTool). Built-in memory
// writes land on the parent's store/disk via the shared *memory.Store.
func (a *Agent) runBackgroundReview(
	cfg config.Runtime,
	cachedPrompt string,
	snapshot []opencode.Message,
	prompt string,
	notify func(string),
) {
	childCfg := cfg
	childCfg.Compaction.Enabled = false
	childCfg.Evidence.Enabled = false
	// Disable recursive nudges — the review must never spawn its own review.
	childCfg.Memory.NudgeInterval = 0
	childCfg.Memory.SkillNudgeInterval = 0

	review := &Agent{
		cfg:                childCfg,
		client:             opencode.NewClient(childCfg.Endpoint, childCfg.APIKey, childCfg.Model),
		workDir:            a.workDir,
		session:            nil,
		allowedTools:       reviewToolWhitelist,
		memStore:           a.memStore,
		writeOrigin:        WriteOriginBackgroundReview,
		cachedSystemPrompt: cachedPrompt,
		maxIterations:      reviewMaxIterations,
		swarmDepth:         maxSwarmDepth, // defensive: no nested swarms even if whitelisted
	}

	review.messages = make([]opencode.Message, 0, len(snapshot)+2)
	review.messages = append(review.messages, opencode.Message{
		Role: "system", Content: opencode.StringContent(cachedPrompt),
	})
	review.messages = append(review.messages, snapshot...)
	startIdx := len(review.messages)
	review.messages = append(review.messages, opencode.Message{
		Role: "user", Content: opencode.StringContent(prompt + reviewToolNote),
	})

	if err := review.runLoop(context.Background()); err != nil {
		return
	}

	actions := summarizeReviewActions(review.messages[startIdx:])
	if len(actions) > 0 && notify != nil {
		notify("💾 Self-improvement review: " + strings.Join(actions, " · "))
	}
}

// summarizeReviewActions builds the human-facing action summary from the
// review fork's NEW tool results (tool messages from the inherited snapshot
// are excluded by the caller via slicing, so stale "created"/"updated"
// results from the prior conversation are never re-surfaced).
func summarizeReviewActions(reviewMessages []opencode.Message) []string {
	var actions []string
	seen := make(map[string]bool)
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			actions = append(actions, s)
		}
	}

	for _, msg := range reviewMessages {
		if msg.Role != "tool" {
			continue
		}
		var data struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
			Target  string `json:"target"`
		}
		if err := json.Unmarshal([]byte(opencode.ContentString(msg)), &data); err != nil {
			continue
		}
		if !data.Success {
			continue
		}
		lower := strings.ToLower(data.Message)
		label := data.Target
		switch data.Target {
		case "memory":
			label = "Memory"
		case "user":
			label = "User profile"
		}
		switch {
		case strings.Contains(lower, "created"):
			add(data.Message)
		case strings.Contains(lower, "updated") || strings.Contains(lower, "patched"):
			add(data.Message)
		case strings.Contains(lower, "added") || strings.Contains(lower, "removed") || strings.Contains(lower, "replaced"):
			if label != "" {
				add(label + " updated")
			}
		}
	}
	return actions
}
