// PORT: backend/skills/prompt_strings.go

export const GuidanceBlock =
  "After completing a complex task (5+ tool calls), fixing a tricky error, or discovering a non-trivial workflow, save the approach as a skill with skill_manage so you can reuse it next time.\nWhen using a skill and finding it outdated, incomplete, or wrong, patch it immediately with skill_manage(action='patch') — don't wait to be asked. Skills that aren't maintained become liabilities.";

export const SkillsIndexHeader =
  "## Skills (mandatory)\nBefore replying, scan the skills below. If a skill matches or is even partially relevant to your task, you MUST load it with skill_view(name) and follow its instructions. Err on the side of loading — it is always better to have context you don't need than to miss critical steps, pitfalls, or established workflows. Skills contain specialized knowledge — API endpoints, tool-specific commands, and proven workflows that outperform general-purpose approaches. Load the skill even if you think you could handle the task with basic tools like web_search or terminal. Skills also encode the user's preferred approach, conventions, and quality standards for tasks like code review, planning, and testing — load them even for tasks you already know how to do, because the skill defines how it should be done here.\nWhenever the user asks you to configure, set up, install, enable, disable, modify, or troubleshoot Hollow itself — its CLI, config, models, SOUL.md identity, memory, skills, curator, or any feature — load the `hollow-agent` skill first with skill_view(name=\"hollow-agent\"). It has the actual paths and commands for this install. Do not guess or invent workarounds.\nIf a skill has issues, fix it with skill_manage(action='patch').\nAfter difficult/iterative tasks, offer to save as a skill. If a skill you loaded was missing steps, had wrong commands, or needed pitfalls you discovered, update it before finishing.\n";

export const SkillsIndexFooter =
  "\nOnly proceed without loading a skill if genuinely none are relevant to the task.";

/*
PORT STATUS
source path: backend/skills/prompt_strings.go
source lines: 10
draft lines: 11
confidence: high
status: phase_b_compile
*/
