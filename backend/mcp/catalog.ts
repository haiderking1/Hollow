// PORT: backend/mcp/catalog.go

import { Effect } from "effect";
import { type config, type mcp_server_config } from "../config/config";
import { add_mcp_server, remove_mcp_server } from "../config/mcp_manage";

// CatalogKind groups installable extensions; more kinds (skills, etc.) later.
export type catalog_kind = "mcp";
export const catalog_kind_mcp: catalog_kind = "mcp";

// CatalogSecret describes optional user input when installing a catalog entry.
export type catalog_secret = {
  key: string; // header or env key
  label: string;
  optional: boolean;
};

// CatalogEntry is an installable integration shown in the TUI /plugins picker.
export type catalog_entry = {
  id: string;
  kind: catalog_kind;
  name: string;
  description: string;
  server_name: string;
  secrets?: catalog_secret[];
  build: (secrets: Record<string, string>) => mcp_server_config;
};

export const mcp_catalog_entries = (): catalog_entry[] => [
  {
    id: "mcp-exa",
    kind: catalog_kind_mcp,
    name: "Exa",
    description: "Web search MCP (remote, no API key required)",
    server_name: "exa",
    build: () => {
      const enabled = true;
      return {
        url: "https://mcp.exa.ai/mcp",
        enabled,
        timeout: 30,
        connect_timeout: 45,
      };
    },
  },
  {
    id: "mcp-context7",
    kind: catalog_kind_mcp,
    name: "Context7",
    description: "Up-to-date library docs for LLM prompts",
    server_name: "context7",
    secrets: [
      {
        key: "CONTEXT7_API_KEY",
        label: "Context7 API key",
        optional: true,
      },
    ],
    build: (secrets: Record<string, string>) => {
      const enabled = true;
      const cfg: mcp_server_config = {
        url: "https://mcp.context7.com/mcp",
        enabled,
        timeout: 30,
        connect_timeout: 45,
      };
      const key = (secrets["CONTEXT7_API_KEY"] ?? "").trim();
      if (key !== "") {
        cfg.headers = { CONTEXT7_API_KEY: key };
      }
      return cfg;
    },
  },
];

// Catalog returns installable MCP integrations for the /plugins picker.
export const catalog = (): catalog_entry[] => {
  return mcp_catalog_entries();
};

// CatalogEntryByID finds a catalog entry.
export const catalog_entry_by_id = (id: string): [catalog_entry, boolean] => {
  for (const e of catalog()) {
    if (e.id === id) {
      return [e, true];
    }
  }
  return [
    {
      id: "",
      kind: catalog_kind_mcp,
      name: "",
      description: "",
      server_name: "",
      build: () => ({ url: "", enabled: false }),
    },
    false,
  ];
};

// IsCatalogInstalled reports whether the entry's MCP server is in config.
export const is_catalog_installed = (cfg: config, entry: catalog_entry): boolean => {
  if (entry.server_name === "" || cfg.mcp_servers === undefined) {
    return false;
  }
  const srv = cfg.mcp_servers[entry.server_name];
  if (srv === undefined) {
    return false;
  }
  if (srv.enabled !== undefined && !srv.enabled) {
    return false;
  }
  return true;
};

// InstallCatalogEntry adds the MCP server for entry to cfg (not persisted).
export const install_catalog_entry = (
  cfg: config,
  entry: catalog_entry,
  secrets: Record<string, string>,
): Effect.Effect<void, Error> => {
  if (entry.kind !== catalog_kind_mcp) {
    return Effect.fail(new Error(`unsupported catalog kind "${entry.kind}"`));
  }
  if (entry.build === undefined) {
    return Effect.fail(new Error(`catalog entry "${entry.id}" has no installer`));
  }
  const server = entry.build(secrets);
  return add_mcp_server(cfg, entry.server_name, server).pipe(
    Effect.mapError((err) => new Error(err.reason)),
  );
};

// RemoveCatalogEntry removes the MCP server for entry from cfg (not persisted).
export const remove_catalog_entry = (
  cfg: config,
  entry: catalog_entry,
): Effect.Effect<void, Error> => {
  if (entry.server_name === "") {
    return Effect.fail(new Error(`catalog entry "${entry.id}" has no server name`));
  }
  return remove_mcp_server(cfg, entry.server_name).pipe(
    Effect.mapError((err) => new Error(err.reason)),
  );
};

/*
PORT STATUS
source path: backend/mcp/catalog.go
source lines: 130
draft lines: 147
confidence: high
status: phase_b_compile
*/
