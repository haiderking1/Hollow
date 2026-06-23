// PORT: backend/config/mcp_manage.go

import { config_error, type config_error as config_error_type } from "./error";
import type { config, mcp_server_config } from "./config";
import { Effect } from "effect";

const mcp_server_name_re = /^[a-zA-Z][a-zA-Z0-9_-]*$/;

// ValidateMCPServerName checks a config key for mcp_servers.
export const validate_mcp_server_name = (
  name: string,
): Effect.Effect<void, config_error_type> => {
  const trimmed = name.trim();
  if (trimmed === "") {
    return Effect.fail(config_error("server name is required", null));
  }
  if (!mcp_server_name_re.test(trimmed)) {
    return Effect.fail(
      config_error(
        `server name ${JSON.stringify(trimmed)} is invalid (use letters, digits, _ and -; must start with a letter)`,
        null,
      ),
    );
  }
  return Effect.void;
};

// ValidateMCPServerConfig ensures transport fields are usable by the MCP client.
export const validate_mcp_server_config = (
  cfg: mcp_server_config,
): Effect.Effect<void, config_error_type> => {
  const has_cmd = cfg.command?.trim() !== "";
  const has_url = cfg.url?.trim() !== "";
  if (has_cmd && has_url) {
    return Effect.fail(
      config_error("choose either stdio (command) or remote (url), not both", null),
    );
  }
  if (!has_cmd && !has_url) {
    return Effect.fail(config_error("command or url is required", null));
  }
  return Effect.void;
};

// AddMCPServer inserts or replaces an MCP server entry on cfg (not persisted).
export const add_mcp_server = (
  cfg: config,
  name: string,
  server: mcp_server_config,
): Effect.Effect<void, config_error_type> =>
  Effect.gen(function* () {
    if (cfg === undefined || cfg === null) {
      return yield* Effect.fail(config_error("config is nil", null));
    }
    yield* validate_mcp_server_name(name);
    yield* validate_mcp_server_config(server);
    if (cfg.mcp_servers === undefined) {
      cfg.mcp_servers = {};
    }
    cfg.mcp_servers[name] = server;
  });

// RemoveMCPServer deletes an MCP server entry from cfg (not persisted).
export const remove_mcp_server = (
  cfg: config,
  name: string,
): Effect.Effect<void, config_error_type> =>
  Effect.gen(function* () {
    if (cfg === undefined || cfg === null) {
      return yield* Effect.fail(config_error("config is nil", null));
    }
    yield* validate_mcp_server_name(name);
    if (cfg.mcp_servers === undefined) {
      return yield* Effect.fail(
        config_error(`MCP server ${JSON.stringify(name)} is not configured`, null),
      );
    }
    if (cfg.mcp_servers[name] === undefined) {
      return yield* Effect.fail(
        config_error(`MCP server ${JSON.stringify(name)} is not configured`, null),
      );
    }
    delete cfg.mcp_servers[name];
    if (Object.keys(cfg.mcp_servers).length === 0) {
      cfg.mcp_servers = {};
    }
  });

/*
PORT STATUS
source path: backend/config/mcp_manage.go
source lines: 73
draft lines: 100
confidence: high
status: phase_a_draft
todos:
  - none beyond verifying error strings match Go exactly
notes:
  - Functions returning (error) are modeled as Effect.Effect<void, config_error>.
*/
