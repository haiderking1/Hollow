// PORT: mirrors backend/config/config.go

import path from "node:path";
import fs from "node:fs";
import { Effect } from "effect";
import { config_error, type config_error as config_error_type } from "./error";
import { home_dir } from "../enoughhome/home";
import {
  get_api_key,
  has_api_key as secrets_has_api_key,
  set_api_key as secrets_set_api_key,
} from "../secrets/store";
import {
  resolve_codex_credentials,
  has_codex_auth as auth_has_codex_auth,
  codex_default_base_url_fn,
} from "../auth/codex_oauth";

export const default_endpoint = "https://opencode.ai/zen/go/v1";
export const default_zen_endpoint = "https://opencode.ai/zen/v1";
export const default_neuralwatt_endpoint = "https://api.neuralwatt.com/v1";
export const default_model = "deepseek-v4-flash";
export const default_zen_model = "deepseek-v4-flash";
export const default_neuralwatt_model = "glm-5.2";
export const default_codex_model = "gpt-5-codex";

export const provider_opencode = "opencode-go";
export const provider_opencode_zen = "opencode-zen";
export const provider_neuralwatt = "neuralwatt";
export const provider_codex = "openai-codex";

export type compaction_settings = {
  enabled: boolean;
  reserve_tokens: number;
  keep_recent_tokens: number;
  context_window?: number;
};

export type evidence_config = {
  enabled: boolean;
  strict_verify_reset: boolean;
  max_completion_rounds: number;
  verifier_enabled: boolean;
  continuity_reads?: boolean;
  goal_lock?: boolean;
  step_scorer?: boolean;
  parallel_forks?: boolean;
  stuck_after_failures?: number;
  parallel_fork_count?: number;
};

export type skills_settings = {
  enabled: boolean;
  enable_skill_commands: boolean;
  paths: string[];
  disabled: string[];
  external_dirs: string[];
  platform_disabled: Record<string, string[]>;
  guard_agent_created: boolean;
  write_approval: boolean;
  inline_shell: boolean;
  inline_shell_timeout: number;
};

export type agent_settings = {
  coding_context: string;
};

export type plugins_settings = {
  disabled: string[];
};

export type workflow_settings = {
  ultracode: boolean;
  alt_screen: boolean;
  always_approve?: string[];
};

export type memory_settings = {
  memory_enabled: boolean;
  user_profile_enabled: boolean;
  nudge_interval: number;
  skill_nudge_interval: number;
  memory_char_limit: number;
  user_char_limit: number;
  write_approval: boolean;
};

export type curator_settings = {
  enabled: boolean;
  interval_hours: number;
  min_idle_hours: number;
  stale_after_days: number;
  archive_after_days: number;
  prune_builtins: boolean;
};

export type mcp_server_tools_config = {
  include?: string[];
  exclude?: string[];
};

export type mcp_server_config = {
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  cwd?: string;
  url?: string;
  headers?: Record<string, string>;
  enabled?: boolean;
  timeout?: number;
  connect_timeout?: number;
  tools?: mcp_server_tools_config;
};

export type config = {
  provider?: string;
  endpoint: string;
  model: string;
  thinking_level?: string;
  hide_thinking?: boolean;
  /** Model ids explicitly disabled in the composer/model picker. Persisted across sessions. */
  disabled_models?: string[];
  compaction?: compaction_settings;
  evidence?: evidence_config;
  skills?: skills_settings;
  memory?: memory_settings;
  curator?: curator_settings;
  agent?: agent_settings;
  plugins?: plugins_settings;
  workflows?: workflow_settings;
  shell_path?: string;
  mcp_servers?: Record<string, mcp_server_config>;
  // legacy field — migrated to secrets store on load, never written back
  api_key_legacy?: string;
};

export type runtime = {
  provider: string;
  endpoint: string;
  model: string;
  api_key: string;
  thinking_level: string;
  hide_thinking: boolean;
  compaction: compaction_settings;
  evidence: evidence_config;
  skills: skills_settings;
  memory: memory_settings;
  curator: curator_settings;
  agent: agent_settings;
  plugins: plugins_settings;
  workflows: workflow_settings;
  shell_path: string;
  mcp_servers: Record<string, mcp_server_config>;
  preloaded_skills: string[];
  preloaded_skills_prompt: string;
};

export const continuity_enabled = (e: evidence_config): boolean =>
  e.continuity_reads === undefined || e.continuity_reads;

export const default_evidence = (): evidence_config => {
  // Disabled by default: the obligations/verifier/goal-lock/step-scorer
  // apparatus injected "TURN INCOMPLETE — open obligations: must_run_verify…"
  // notices that pushed the model to run a no-op verify command (e.g.
  // `echo "verified" && exit 0`) after tool use. The noise wasn't worth the
  // gating. Re-enable per-session via config if you want forced verification.
  return {
    enabled: false,
    strict_verify_reset: true,
    max_completion_rounds: 12,
    verifier_enabled: false,
    goal_lock: false,
    step_scorer: false,
    parallel_forks: false,
    stuck_after_failures: 2,
    parallel_fork_count: 4,
  };
};

export const goal_lock_enabled = (e: evidence_config): boolean =>
  e.goal_lock === undefined || e.goal_lock;

export const step_scorer_enabled = (e: evidence_config): boolean =>
  e.step_scorer === undefined || e.step_scorer;

export const parallel_forks_enabled = (e: evidence_config): boolean =>
  e.parallel_forks === undefined || e.parallel_forks;

export const stuck_threshold = (e: evidence_config): number =>
  e.stuck_after_failures === undefined || e.stuck_after_failures <= 0
    ? 2
    : e.stuck_after_failures;

export const fork_count = (e: evidence_config): number => {
  const count = e.parallel_fork_count ?? 0;
  if (count <= 0) return 4;
  if (count > 8) return 8;
  return count;
};

export const default_memory = (): memory_settings => ({
  memory_enabled: true,
  user_profile_enabled: true,
  nudge_interval: 10,
  skill_nudge_interval: 10,
  memory_char_limit: 2200,
  user_char_limit: 1375,
  write_approval: false,
});

export const default_curator = (): curator_settings => ({
  enabled: true,
  interval_hours: 168,
  min_idle_hours: 2,
  stale_after_days: 30,
  archive_after_days: 90,
  prune_builtins: true,
});

export const default_inline_shell_enabled = (): boolean =>
  process.platform === "linux" || process.platform === "win32";

export const default_config = (): config => ({
  endpoint: default_endpoint,
  model: default_model,
  compaction: {
    enabled: true,
    reserve_tokens: 16384,
    keep_recent_tokens: 20000,
  },
  skills: {
    enabled: true,
    enable_skill_commands: true,
    paths: [],
    disabled: [],
    external_dirs: [],
    platform_disabled: { cli: [], tui: [] },
    guard_agent_created: false,
    write_approval: false,
    inline_shell: default_inline_shell_enabled(),
    inline_shell_timeout: 10,
  },
  memory: default_memory(),
  curator: default_curator(),
  agent: {
    coding_context: "auto",
  },
  plugins: {
    disabled: [],
  },
  workflows: {
    ultracode: false,
    alt_screen: false,
    always_approve: [] as string[],
  },
  mcp_servers: {},
});

export const dir = (): string => home_dir();
export const config_path = (): string => path.join(dir(), "config.json");

type file_config = {
  provider?: string;
  endpoint: string;
  model: string;
  thinking_level?: string;
  hide_thinking?: boolean;
  disabled_models?: string[];
  api_key?: string;
  compaction?: compaction_settings;
  evidence?: evidence_config;
  skills?: skills_settings;
  memory?: memory_settings;
  curator?: curator_settings;
  agent?: agent_settings;
  plugins?: plugins_settings;
  workflows?: workflow_settings;
  shell_path?: string;
  mcp_servers?: Record<string, mcp_server_config>;
};

const is_enoent = (cause: unknown): boolean =>
  typeof cause === "object" &&
  cause !== null &&
  (cause as NodeJS.ErrnoException).code === "ENOENT";

export const load = (): Effect.Effect<config, config_error_type> =>
  Effect.gen(function* () {
    let cfg = default_config();
    const p = config_path();

    const read_result = yield* Effect.either(
      Effect.try({
        try: () => fs.readFileSync(p),
        catch: (cause) => config_error("read config", cause),
      }),
    );

    let raw: file_config | undefined;
    if (read_result._tag === "Left") {
      if (!is_enoent(read_result.left.cause)) {
        return yield* Effect.fail(read_result.left);
      }
      // No config yet — start fresh with defaults. Hollow is independent from
      // Enough, so we deliberately do not import ~/.config/enough/config.json.
      return cfg;
    }

    raw = JSON.parse(read_result.right.toString("utf8")) as file_config;
    cfg = apply_file_config(cfg, raw);

    if (raw.api_key !== undefined && raw.api_key !== "") {
      // Check the provider's own slot — with per-provider credential slots,
      // a default-slot key must not block migrating a NeuralWatt key, etc.
      const has_key = yield* secrets_has_api_key(cfg.provider);
      if (!has_key) {
        yield* Effect.ignore(secrets_set_api_key(raw.api_key, cfg.provider));
      }
    }

    return cfg;
  });

const apply_file_config = (cfg: config, raw: file_config): config => {
  const c = { ...cfg };
  c.provider = raw.provider ?? c.provider;
  c.endpoint = raw.endpoint ?? c.endpoint;
  c.model = raw.model ?? c.model;
  c.thinking_level = raw.thinking_level ?? c.thinking_level;
  c.hide_thinking = raw.hide_thinking ?? c.hide_thinking;
  c.disabled_models = raw.disabled_models ?? c.disabled_models;
  c.api_key_legacy = raw.api_key ?? c.api_key_legacy;
  c.shell_path = raw.shell_path ?? c.shell_path;
  if (raw.compaction !== undefined) c.compaction = raw.compaction;
  if (raw.evidence !== undefined) c.evidence = raw.evidence;
  if (raw.skills !== undefined) c.skills = raw.skills;
  if (raw.memory !== undefined) c.memory = raw.memory;
  if (raw.curator !== undefined) c.curator = raw.curator;
  if (raw.agent !== undefined) c.agent = raw.agent;
  if (raw.plugins !== undefined) c.plugins = raw.plugins;
  if (raw.workflows !== undefined) c.workflows = raw.workflows;
  if (raw.mcp_servers !== undefined) c.mcp_servers = raw.mcp_servers;

  if (c.provider === undefined || c.provider === "") c.provider = provider_opencode;
  if (c.endpoint === undefined || c.endpoint === "") c.endpoint = default_endpoint;
  if (c.model === undefined || c.model === "") c.model = default_model;
  if (c.compaction === undefined) {
    c.compaction = { enabled: true, reserve_tokens: 16384, keep_recent_tokens: 20000 };
  }
  if (c.skills === undefined) {
    c.skills = {
      enabled: true,
      enable_skill_commands: true,
      paths: [],
      disabled: [],
      external_dirs: [],
      platform_disabled: { cli: [], tui: [] },
      guard_agent_created: false,
      write_approval: false,
      inline_shell: default_inline_shell_enabled(),
      inline_shell_timeout: 10,
    };
  } else {
    if (c.skills.platform_disabled === undefined)
      c.skills.platform_disabled = { cli: [], tui: [] };
    if (c.skills.paths === undefined) c.skills.paths = [];
    if (c.skills.disabled === undefined) c.skills.disabled = [];
    if (c.skills.external_dirs === undefined) c.skills.external_dirs = [];
    if (c.skills.inline_shell_timeout === undefined || c.skills.inline_shell_timeout <= 0)
      c.skills.inline_shell_timeout = 10;
  }
  if (c.agent === undefined) c.agent = { coding_context: "auto" };
  if (c.plugins === undefined) c.plugins = { disabled: [] };
  else if (c.plugins.disabled === undefined) c.plugins.disabled = [];
  if (c.workflows === undefined) c.workflows = { ultracode: false, alt_screen: false, always_approve: [] as string[] };
  else if (c.workflows.always_approve === undefined) c.workflows.always_approve = [];
  if (c.mcp_servers === undefined) c.mcp_servers = {};
  return c;
};

export const save = (cfg: config): Effect.Effect<void, config_error_type> =>
  Effect.gen(function* () {
    const c = { ...cfg };
    if (c.endpoint === undefined || c.endpoint === "") c.endpoint = default_endpoint;
    if (c.model === undefined || c.model === "") c.model = default_model;
    if (c.compaction === undefined)
      c.compaction = { enabled: true, reserve_tokens: 16384, keep_recent_tokens: 20000 };
    if (c.skills === undefined)
      c.skills = {
        enabled: true,
        enable_skill_commands: true,
        paths: [],
        disabled: [],
        external_dirs: [],
        platform_disabled: { cli: [], tui: [] },
        guard_agent_created: false,
        write_approval: false,
        inline_shell: default_inline_shell_enabled(),
        inline_shell_timeout: 10,
      };
    if (c.memory === undefined) c.memory = default_memory();
    if (c.curator === undefined) c.curator = default_curator();
    if (c.agent === undefined) c.agent = { coding_context: "auto" };
    if (c.plugins === undefined) c.plugins = { disabled: [] };
    if (c.workflows === undefined) c.workflows = { ultracode: false, alt_screen: false, always_approve: [] as string[] };
    if (c.mcp_servers === undefined) c.mcp_servers = {};

    const d = dir();
    yield* Effect.try({
      try: () => fs.mkdirSync(d, { recursive: true, mode: 0o700 }),
      catch: (cause) => config_error("mkdir config dir", cause),
    });

    const raw: file_config = {
      provider: c.provider,
      endpoint: c.endpoint,
      model: c.model,
      thinking_level: c.thinking_level,
      hide_thinking: c.hide_thinking,
      disabled_models: c.disabled_models,
      compaction: c.compaction,
      evidence: c.evidence,
      skills: c.skills,
      memory: c.memory,
      curator: c.curator,
      agent: c.agent,
      plugins: c.plugins,
      workflows: c.workflows,
      shell_path: c.shell_path,
      mcp_servers: c.mcp_servers,
    };

    const data = yield* Effect.try({
      try: () => JSON.stringify(raw, null, "  "),
      catch: (cause) => config_error("encode config", cause),
    });

    yield* Effect.try({
      try: () => fs.writeFileSync(config_path(), data, { mode: 0o600 }),
      catch: (cause) => config_error("write config", cause),
    });
  });

export const load_runtime = (
  ctx: AbortSignal = new AbortController().signal,
): Effect.Effect<runtime, config_error_type> =>
  Effect.gen(function* () {
    const cfg = yield* load();

    let provider = cfg.provider ?? provider_opencode;

    let key = "";
    if (provider === provider_codex) {
      const creds = yield* resolve_codex_credentials(ctx).pipe(
        Effect.mapError((e) => config_error("resolve codex credentials", e)),
      );
      key = creds.access_token;
      if (cfg.endpoint === "" || cfg.endpoint === default_endpoint) {
        cfg.endpoint = creds.base_url;
      }
    } else {
      const key_provider = provider === provider_opencode_zen ? provider_opencode : provider;
      key = yield* get_api_key(key_provider).pipe(
        Effect.mapError((e) => config_error("get api key", e)),
      );
      if (cfg.endpoint === undefined || cfg.endpoint === "") {
        switch (provider) {
          case provider_opencode_zen:
            cfg.endpoint = default_zen_endpoint;
            break;
          case provider_neuralwatt:
            cfg.endpoint = default_neuralwatt_endpoint;
            break;
          default:
            cfg.endpoint = default_endpoint;
        }
      }
    }

    const comp: compaction_settings =
      cfg.compaction ?? { enabled: true, reserve_tokens: 16384, keep_recent_tokens: 20000 };
    const ev: evidence_config = cfg.evidence ?? default_evidence();
    const sk: skills_settings =
      cfg.skills ?? {
        enabled: true,
        enable_skill_commands: true,
        paths: [],
        disabled: [],
        external_dirs: [],
        platform_disabled: { cli: [], tui: [] },
        guard_agent_created: false,
        write_approval: false,
        inline_shell: default_inline_shell_enabled(),
        inline_shell_timeout: 10,
      };
    const mem: memory_settings = cfg.memory ?? default_memory();
    const cur: curator_settings = cfg.curator ?? default_curator();
    const ag: agent_settings = cfg.agent ?? { coding_context: "auto" };
    const pl: plugins_settings = cfg.plugins ?? { disabled: [] };
    const wf: workflow_settings = cfg.workflows ?? { ultracode: false, alt_screen: false, always_approve: [] as string[] };
    const mcp_servers = cfg.mcp_servers ?? {};

    return {
      provider,
      endpoint: cfg.endpoint,
      model: cfg.model,
      api_key: key,
      thinking_level: cfg.thinking_level ?? "",
      hide_thinking: cfg.hide_thinking ?? false,
      compaction: comp,
      evidence: ev,
      skills: sk,
      memory: mem,
      curator: cur,
      agent: ag,
      plugins: pl,
      workflows: wf,
      shell_path: cfg.shell_path ?? "",
      mcp_servers,
      preloaded_skills: [],
      preloaded_skills_prompt: "",
    };
  });

export const connected = (): Effect.Effect<boolean, never> =>
  Effect.gen(function* () {
    const result = yield* Effect.either(load());
    if (result._tag === "Left") {
      return false;
    }
    const cfg = result.right;
    const provider = cfg.provider ?? provider_opencode;
    if (provider === provider_codex) {
      return yield* auth_has_codex_auth();
    }
    return yield* secrets_has_api_key(provider);
  });

/*
PORT STATUS
source path: backend/config/config.go
source lines: 768
draft lines: 555
confidence: medium
status: phase_a_draft
todos:
  - verify migration fallback behavior matches Go exactly (old config path)
  - confirm runtime.GOOS mapping to process.platform
  - tighten Effect.mapError wrappers to preserve original error context
notes:
  - Load/Save/LoadRuntime/Connected use Effect.Effect with config_error.
  - Imports existing auth, secrets, and enoughhome ports.
*/
