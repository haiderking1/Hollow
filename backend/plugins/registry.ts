// PORT: backend/plugins/registry.go

import fs from "node:fs";
import path from "node:path";
import { Effect } from "effect";
import { home_dir } from "../hollowhome/home";
import type { runtime } from "../config/config";

const ns_regex = /^[a-zA-Z0-9_-]+$/;

export type plugins_error = {
  readonly _tag: "PluginsError";
  readonly reason: string;
  readonly cause: unknown;
};

export const plugins_error = (reason: string, cause: unknown): plugins_error => ({
  _tag: "PluginsError",
  reason,
  cause,
});

// IsValidNamespace reports whether the namespace is valid.
export const is_valid_namespace = (ns: string): boolean => ns_regex.test(ns);

// ParseQualifiedName splits a qualified name like "namespace:skill" into namespace and bare name.
export const parse_qualified_name = (name: string): [string, string] => {
  const idx = name.indexOf(":");
  if (idx < 0) {
    return ["", name];
  }
  return [name.slice(0, idx), name.slice(idx + 1)];
};

// IsPluginDisabled reports whether the plugin namespace is disabled.
export const is_plugin_disabled = (namespace: string, cfg: runtime): boolean => {
  for (const d of cfg.plugins.disabled) {
    if (d === namespace) {
      return true;
    }
  }
  return false;
};

// PluginsDir returns the directory where user plugins are installed.
export const plugins_dir = (): string => path.join(home_dir(), "plugins");

// FindPluginSkill finds the path to the plugin skill's SKILL.md file.
export const find_plugin_skill = (
  qualified_name: string,
): Effect.Effect<string, plugins_error> =>
  Effect.gen(function* () {
    const [ns, bare] = parse_qualified_name(qualified_name);
    if (ns === "" || bare === "") {
      return yield* Effect.fail(plugins_error(`invalid qualified name: ${qualified_name}`, null));
    }
    if (!is_valid_namespace(ns)) {
      return yield* Effect.fail(plugins_error(`invalid namespace: ${ns}`, null));
    }
    const p = path.join(plugins_dir(), ns, "skills", bare, "SKILL.md");
    yield* Effect.try({
      try: () => fs.statSync(p),
      catch: (cause) => plugins_error("stat plugin skill", cause),
    });
    return p;
  });

// ListPluginSkills returns all skills provided by the plugin namespace.
export const list_plugin_skills = (
  namespace: string,
): Effect.Effect<string[], never> =>
  Effect.sync(() => {
    const skills_dir = path.join(plugins_dir(), namespace, "skills");
    let entries: fs.Dirent[];
    try {
      entries = fs.readdirSync(skills_dir, { withFileTypes: true });
    } catch {
      return [];
    }

    const skills: string[] = [];
    for (const entry of entries) {
      if (entry.isDirectory()) {
        const skill_md = path.join(skills_dir, entry.name, "SKILL.md");
        if (fs.existsSync(skill_md)) {
          skills.push(entry.name);
        }
      }
    }
    skills.sort();
    return skills;
  });

// GetPluginSiblingBanner returns the siblings banner.
export const get_plugin_sibling_banner = (namespace: string, bare: string): string => {
  const siblings = Effect.runSync(list_plugin_skills(namespace));
  const clean: string[] = [];
  for (const s of siblings) {
    if (s !== bare) {
      clean.push(s);
    }
  }
  if (clean.length > 0) {
    return `[Bundle context: This skill is part of the '${namespace}' plugin.\nSibling skills: ${clean.join(", ")}.\nUse qualified form to invoke siblings (e.g. ${namespace}:${clean[0]}).]\n\n`;
  }
  return `[Bundle context: This skill is part of the '${namespace}' plugin.]\n\n`;
};

/*
PORT STATUS
source path: backend/plugins/registry.go
source lines: 96
draft lines: 121
confidence: high
status: phase_a_draft
todos:
  - decide whether list_plugin_skills should remain Effect<never> or plain string[]
notes:
  - FindPluginSkill returns (string, error), modeled as Effect.Effect<string, plugins_error>.
  - Reuses config runtime type and hollowhome home_dir port.
*/
