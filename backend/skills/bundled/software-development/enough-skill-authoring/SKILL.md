---
name: enough-skill-authoring
description: "Author in-repo SKILL.md for Enough: frontmatter, layout, skill_manage vs write_file."
version: 1.0.0
author: Enough
license: MIT
platforms: [linux, darwin, windows]
metadata:
  hermes:
    tags: [skills, authoring, enough, conventions, skill-md]
    related_skills: [enough, plan, requesting-code-review]
---

# Authoring Enough Skills

## Two locations

1. **User-local (runtime):** `~/.enough/skills/<category>/<name>/SKILL.md` — created with `skill_manage(action='create')`.
2. **In-repo (this doc):** `backend/skills/bundled/<category>/<name>/SKILL.md` — committed, embedded, synced by `enough skills sync`. Use `write_file` / git; `skill_manage(create)` does **not** write here.

Optional hub-only skills live under `backend/skills/optional/` (not auto-synced).

## Frontmatter (required shape)

```yaml
---
name: my-skill-name          # ≤64 chars, lowercase + hyphens
description: Use when …      # ≤1024 chars, trigger-first
version: 1.0.0
author: Enough
license: MIT
platforms: [linux, darwin, windows]
metadata:
  hermes:                    # schema key name — keep as hermes
    tags: [tag1, tag2]
    related_skills: [enough, other-skill]
---
```

Keep `metadata.hermes` even though the agent is Enough — the parser expects this namespace.

## Body structure

Match peers under `backend/skills/bundled/software-development/`:

- `# Title` → `## Overview` → `## When to Use` → topic sections → `## Common Pitfalls` → `## Enough Integration` (tool names: `bash`, `read_file`, `skill_view`, …)

Supporting files: `references/`, `templates/`, `scripts/`, `assets/` only.

## Python helper scripts

Scripts in `scripts/` run via **`bash`**, not a Go↔Python linker. Use `~/.enough/` paths and `ENOUGH_HOME`. Import `backend/skills/bundled/.../scripts/_enough_home.py` pattern for home resolution.

## Workflow

1. Read 2–3 peer skills in the target category.
2. `write_file` → `backend/skills/bundled/<category>/<name>/SKILL.md`
3. `go test ./backend/skills/...`
4. Commit; users get it on next `enough skills sync` (if not user-modified).
5. Current session index may be cached — `/reload-skills` or new session to verify.

## Pitfalls

- Don't use `skill_manage(create)` for in-repo bundled skills.
- Don't reference `hermes` CLI, `~/.enough/`, or gateway features — use **`enough`** skill for agent self-config.
- Don't assume Hermes sandbox/Docker — Enough uses local `bash` in the project workspace.
- `related_skills: [enough]` → use `[enough]` instead.
