// PORT: backend/skills/tool_list.go

import { type runtime } from "../config/config";
import { type Skill } from "./types";
import { SkillsDir } from "./paths";
import { DiscoverAllSkills } from "./discovery";
import { Effect } from "effect";
import fs from "node:fs";

export interface skillItem {
  name: string;
  description: string;
  category: string;
}

export interface listResult {
  success: boolean;
  skills: skillItem[];
  categories: string[];
  count: number;
  message?: string;
  hint: string;
}

export function ExecuteSkillsList(
  argsJSON: string,
  workDir: string,
  cfg: runtime,
): Effect.Effect<[string, boolean], never> {
  let args = { category: "" };
  try {
    args = JSON.parse(argsJSON);
  } catch {}

  const skillsDir = SkillsDir();
  try {
    if (!fs.existsSync(skillsDir)) {
      fs.mkdirSync(skillsDir, { recursive: true, mode: 0o700 });
    }
  } catch {}

  return DiscoverAllSkills(workDir, cfg).pipe(
    Effect.match({
      onFailure: (err) => {
        return [`{"success": false, "error": ${JSON.stringify(err.message)}}`, true] as [string, boolean];
      },
      onSuccess: ([allSkills]) => {
        const categoryFilter = (args.category || "").trim();
        const filtered: Skill[] = [];
        const categorySet = new Set<string>();

        for (const sk of allSkills) {
          if (categoryFilter !== "" && sk.Category !== categoryFilter) {
            continue;
          }
          filtered.push(sk);
          categorySet.add(sk.Category);
        }

        const list = filtered.map((sk) => ({
          name: sk.Name,
          description: sk.Description,
          category: sk.Category,
        }));

        const categories = Array.from(categorySet);
        categories.sort();

        const res: listResult = {
          success: true,
          skills: list,
          categories: categories,
          count: list.length,
          hint: "Use skill_view(name) to see full content, tags, and linked files",
        };

        if (list.length === 0) {
          res.message = "No skills found. Skills live in ~/.hollow/skills/<category>/<name>/SKILL.md";
        }

        try {
          const outBytes = JSON.stringify(res, null, "  ");
          return [outBytes, false] as [string, boolean];
        } catch {
          return [`{"success": false, "error": "json marshal error"}`, true] as [string, boolean];
        }
      },
    })
  );
}

/*
PORT STATUS
source path: backend/skills/tool_list.go
source lines: 85
draft lines: 88
confidence: high
status: phase_b_compile
*/
