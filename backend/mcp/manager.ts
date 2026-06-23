// PORT: backend/mcp/manager.go

import { Effect } from "effect";
import { spawn, type ChildProcess } from "node:child_process";
import { type config, type mcp_server_config, type mcp_server_tools_config } from "../config/config";
import type { tool } from "../opencode/types";
import type { tool_content_block } from "../opencode/content";

// ToolCallTarget stores coordinates back to the original MCP server and tool.
export type tool_call_target = {
  server_name: string;
  original_tool_name: string;
};

// Session represents an active connection to an MCP server.
export class session {
  name: string;
  cfg: mcp_server_config;
  tools: tool[] = [];
  tool_map = new Map<string, string>(); // sanitized model name -> original name
  unhealthy = false;
  last_err: Error | null = null;

  // Internals
  proc: ChildProcess | null = null;
  sse_controller: AbortController | null = null;
  sse_endpoint_url: string | null = null;
  rpc_id_counter = 0;
  rpc_pending_requests = new Map<number, (res: any) => void>();
  line_buffer = "";
  sse_endpoint_resolver: (() => void) | null = null;

  constructor(name: string, cfg: mcp_server_config) {
    this.name = name;
    this.cfg = cfg;
  }

  name_str(): string {
    return this.name;
  }

  is_unhealthy(): boolean {
    return this.unhealthy;
  }

  last_error(): Error | null {
    return this.last_err;
  }

  get_tools(): tool[] {
    return this.tools;
  }

  close() {
    if (this.proc) {
      this.proc.kill();
      this.proc = null;
    }
    if (this.sse_controller) {
      this.sse_controller.abort();
      this.sse_controller = null;
    }
    this.rpc_pending_requests.clear();
  }

  send_request(method: string, params: any, signal?: AbortSignal): Promise<any> {
    return new Promise((resolve, reject) => {
      const id = ++this.rpc_id_counter;

      let timeout_id: any = null;
      const cleanup = () => {
        this.rpc_pending_requests.delete(id);
        if (timeout_id) clearTimeout(timeout_id);
      };

      if (signal) {
        if (signal.aborted) {
          reject(new Error("interrupted"));
          return;
        }
        signal.addEventListener("abort", () => {
          cleanup();
          reject(new Error("interrupted"));
        });
      }

      this.rpc_pending_requests.set(id, (msg: any) => {
        cleanup();
        if (msg.error) {
          reject(new Error(msg.error.message || `JSON-RPC error ${msg.error.code}`));
        } else {
          resolve(msg.result);
        }
      });

      const rpc_msg = { jsonrpc: "2.0", id, method, params };

      if (this.proc) {
        try {
          this.proc.stdin?.write(JSON.stringify(rpc_msg) + "\n");
        } catch (err) {
          cleanup();
          reject(err);
        }
      } else if (this.sse_endpoint_url) {
        fetch(this.sse_endpoint_url, {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            ...(this.cfg.headers || {}),
          },
          body: JSON.stringify(rpc_msg),
        }).catch((err) => {
          cleanup();
          reject(err);
        });
      } else {
        cleanup();
        reject(new Error("no active transport"));
      }
    });
  }

  send_notification(method: string, params: any): void {
    const rpc_msg = { jsonrpc: "2.0", method, params };
    if (this.proc) {
      try {
        this.proc.stdin?.write(JSON.stringify(rpc_msg) + "\n");
      } catch {}
    } else if (this.sse_endpoint_url) {
      fetch(this.sse_endpoint_url, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          ...(this.cfg.headers || {}),
        },
        body: JSON.stringify(rpc_msg),
      }).catch(() => {});
    }
  }
}

// Manager manages all configured MCP server sessions.
export class manager {
  private _sessions = new Map<string, session>();
  private _tool_mapping = new Map<string, tool_call_target>();

  tools(): tool[] {
    const list: tool[] = [];
    for (const s of this._sessions.values()) {
      if (!s.is_unhealthy()) {
        list.push(...s.tools);
      }
    }
    return list;
  }

  sessions(): Record<string, session> {
    const copy: Record<string, session> = {};
    for (const [k, v] of this._sessions.entries()) {
      copy[k] = v;
    }
    return copy;
  }

  connect(
    ctx: AbortSignal | undefined,
    name: string,
    cfg: mcp_server_config,
  ): Effect.Effect<session, Error> {
    return Effect.tryPromise({
      try: async () => {
        const cmd = (cfg.command ?? "").trim();
        const url = (cfg.url ?? "").trim();

        if (cmd !== "" && url !== "") {
          throw new Error("mutually exclusive transports; both command and url specified");
        }
        if (cmd === "" && url === "") {
          throw new Error("no command or url specified");
        }

        const s = new session(name, cfg);

        const conn_timeout = cfg.connect_timeout || cfg.timeout || 30;
        const controller = new AbortController();
        const timeout_id = setTimeout(() => controller.abort(), conn_timeout * 1000);
        if (ctx) {
          ctx.addEventListener("abort", () => controller.abort());
        }

        try {
          if (cmd !== "") {
            s.proc = spawn(cmd, cfg.args ?? [], {
              cwd: cfg.cwd || undefined,
              env: { ...process.env, ...(cfg.env || {}) },
            });

            s.proc.on("error", (err) => {
              s.unhealthy = true;
              s.last_err = err;
            });

            s.proc.on("exit", (code, signal) => {
              s.unhealthy = true;
              s.last_err = new Error(`process exited with code ${code} (signal ${signal})`);
            });

            s.proc.stdout?.on("data", (chunk: Buffer) => {
              s.line_buffer += chunk.toString("utf8");
              let idx;
              while ((idx = s.line_buffer.indexOf("\n")) !== -1) {
                const line = s.line_buffer.substring(0, idx).trim();
                s.line_buffer = s.line_buffer.substring(idx + 1);
                if (line !== "") {
                  try {
                    const msg = JSON.parse(line);
                    if (msg.id !== undefined && msg.id !== null) {
                      const id = Number(msg.id);
                      const cb = s.rpc_pending_requests.get(id);
                      if (cb) {
                        s.rpc_pending_requests.delete(id);
                        cb(msg);
                      }
                    }
                  } catch {}
                }
              }
            });
          } else {
            s.sse_controller = new AbortController();
            const headers: Record<string, string> = {
              Accept: "text/event-stream",
              ...(cfg.headers || {}),
            };
            const resp = await fetch(url, {
              signal: s.sse_controller.signal,
              headers,
            });
            if (resp.status >= 400) {
              throw new Error(`SSE connect failed: ${resp.status}`);
            }
            if (!resp.body) {
              throw new Error("SSE response body is empty");
            }

            const reader = resp.body.getReader();
            const decoder = new TextDecoder();
            let buffer = "";

            (async () => {
              try {
                let current_event = "";
                let current_data = "";
                while (true) {
                  const { done, value } = await reader.read();
                  if (done) break;
                  buffer += decoder.decode(value, { stream: true });
                  let idx;
                  while ((idx = buffer.indexOf("\n")) !== -1) {
                    const line = buffer.substring(0, idx).trim();
                    buffer = buffer.substring(idx + 1);
                    if (line === "") {
                      if (current_event !== "" || current_data !== "") {
                        if (current_event === "endpoint") {
                          let endpoint = current_data.trim();
                          if (!endpoint.startsWith("http://") && !endpoint.startsWith("https://")) {
                            endpoint = new URL(endpoint, url).toString();
                          }
                          s.sse_endpoint_url = endpoint;
                          if (s.sse_endpoint_resolver) {
                            s.sse_endpoint_resolver();
                            s.sse_endpoint_resolver = null;
                          }
                        } else if (current_event === "message" || current_event === "") {
                          try {
                            const msg = JSON.parse(current_data);
                            if (msg.id !== undefined && msg.id !== null) {
                              const id = Number(msg.id);
                              const cb = s.rpc_pending_requests.get(id);
                              if (cb) {
                                s.rpc_pending_requests.delete(id);
                                cb(msg);
                              }
                            }
                          } catch {}
                        }
                        current_event = "";
                        current_data = "";
                      }
                    } else if (line.startsWith("event:")) {
                      current_event = line.substring("event:".length).trim();
                    } else if (line.startsWith("data:")) {
                      current_data = line.substring("data:".length).trim();
                    }
                  }
                }
              } catch (err) {
                s.unhealthy = true;
                s.last_err = err instanceof Error ? err : new Error(String(err));
              }
            })();

            if (!s.sse_endpoint_url) {
              await new Promise<void>((resolve, reject) => {
                s.sse_endpoint_resolver = resolve;
                setTimeout(() => {
                  if (!s.sse_endpoint_url) {
                    reject(new Error("Timeout waiting for SSE endpoint event"));
                  }
                }, conn_timeout * 1000);
              });
            }
          }

          // JSON-RPC Handshake
          await s.send_request(
            "initialize",
            {
              protocolVersion: "2024-11-05",
              capabilities: {},
              clientInfo: {
                name: "hollow-mcp-client",
                version: "1.0.0",
              },
            },
            controller.signal,
          );

          s.send_notification("notifications/initialized", {});

          const list_timeout = cfg.timeout || 30;
          const list_controller = new AbortController();
          const list_timeout_id = setTimeout(() => list_controller.abort(), list_timeout * 1000);

          let tools_result: any = { tools: [] };
          try {
            tools_result = await s.send_request("tools/list", {}, list_controller.signal);
          } finally {
            clearTimeout(list_timeout_id);
          }

          const opencode_tools: tool[] = [];
          for (const t of tools_result.tools || []) {
            if (!is_tool_allowed(t.name, cfg.tools)) {
              continue;
            }

            const params_json = JSON.stringify(t.inputSchema || { type: "object", properties: {} });
            const parameters = new TextEncoder().encode(params_json);

            const sanitized = sanitize_name(t.name);
            const model_name = `mcp_${name}_${sanitized}`;
            s.tool_map.set(model_name, t.name);

            opencode_tools.push({
              type: "function",
              function: {
                name: model_name,
                description: t.description || "",
                parameters,
              },
            });
          }

          s.tools = opencode_tools;
          return s;
        } catch (err) {
          s.close();
          throw err;
        } finally {
          clearTimeout(timeout_id);
        }
      },
      catch: (cause) => cause instanceof Error ? cause : new Error(String(cause)),
    });
  }

  reload(
    ctx: AbortSignal | undefined,
    servers: Record<string, mcp_server_config>,
  ): Effect.Effect<void, Error> {
    return Effect.tryPromise({
      try: async () => {
        for (const s of this._sessions.values()) {
          s.close();
        }

        this._sessions.clear();
        this._tool_mapping.clear();

        const errs: string[] = [];

        for (const [name, cfg] of Object.entries(servers)) {
          const enabled = cfg.enabled !== undefined ? cfg.enabled : true;
          if (!enabled) {
            continue;
          }

          try {
            const s = await Effect.runPromise(this.connect(ctx, name, cfg));
            this._sessions.set(name, s);
            for (const [modelName, origName] of s.tool_map.entries()) {
              this._tool_mapping.set(modelName, {
                server_name: name,
                original_tool_name: origName,
              });
            }
          } catch (err) {
            const error = err instanceof Error ? err : new Error(String(err));
            errs.push(`${name}: ${error.message}`);
            const s = new session(name, cfg);
            s.unhealthy = true;
            s.last_err = error;
            this._sessions.set(name, s);
          }
        }

        if (errs.length > 0) {
          throw new Error(`MCP reload failures: ${errs.join("; ")}`);
        }
      },
      catch: (cause) => cause instanceof Error ? cause : new Error(String(cause)),
    });
  }

  call_tool(
    ctx: AbortSignal | undefined,
    model_tool_name: string,
    args_json: string,
  ): Effect.Effect<[tool_content_block, tool_content_block[], boolean], Error> {
    return Effect.tryPromise({
      try: async () => {
        const target = this._tool_mapping.get(model_tool_name);
        let s: session | undefined;
        if (target) {
          s = this._sessions.get(target.server_name);
        }

        if (!target || !s) {
          throw new Error(`tool "${model_tool_name}" not found`);
        }

        if (s.is_unhealthy()) {
          throw new Error(`server "${s.name}" is unhealthy: ${s.last_error()?.message || "unknown error"}`);
        }

        let args: Record<string, any> = {};
        try {
          args = JSON.parse(args_json);
        } catch (err) {
          throw new Error(`invalid arguments JSON: ${err instanceof Error ? err.message : String(err)}`);
        }

        const params = {
          name: target.original_tool_name,
          arguments: args,
        };

        const timeout = s.cfg.timeout || 30;
        let call_controller: AbortController | null = null;
        let timeout_id: any = null;

        if (timeout > 0) {
          call_controller = new AbortController();
          timeout_id = setTimeout(() => call_controller?.abort(), timeout * 1000);
          if (ctx) {
            ctx.addEventListener("abort", () => call_controller?.abort());
          }
        }

        try {
          const res = await s.send_request("tools/call", params, call_controller?.signal);

          const text_parts: string[] = [];
          const content_blocks: tool_content_block[] = [];
          let total_len = 0;
          let truncated = false;

          for (const c of res.content || []) {
            if (c.type === "text") {
              let txt = c.text ?? "";
              if (!truncated) {
                if (total_len + txt.length > 32000) {
                  const room = 32000 - total_len;
                  if (room > 0) {
                    txt = txt.substring(0, room);
                  } else {
                    txt = "";
                  }
                  txt += "\n... truncated ...";
                  truncated = true;
                }
                total_len += txt.length;
                if (txt !== "") {
                  text_parts.push(txt);
                  content_blocks.push({
                    type: "text",
                    text: txt,
                    data: "",
                    mime_type: "",
                  });
                }
              }
            } else if (c.type === "image") {
              content_blocks.push({
                type: "image",
                text: "",
                data: c.data ?? "",
                mime_type: c.mimeType ?? "",
              });
            }
          }

          if (text_parts.length === 0 && res.structuredContent !== undefined && res.structuredContent !== null) {
            try {
              let txt = JSON.stringify(res.structuredContent);
              if (txt.length > 32000) {
                txt = txt.substring(0, 32000) + "\n... truncated ...";
              }
              text_parts.push(txt);
              content_blocks.push({
                type: "text",
                text: txt,
                data: "",
                mime_type: "",
              });
            } catch {}
          }

          const output_text = text_parts.join("\n");
          return [
            {
              type: "text",
              text: output_text,
              data: "",
              mime_type: "",
            },
            content_blocks,
            !!res.isError,
          ] as [tool_content_block, tool_content_block[], boolean];
        } catch (err) {
          const err_msg = err instanceof Error ? err.message : String(err);
          const is_cancelled = ctx?.aborted || (call_controller && call_controller.signal.aborted);

          if (!is_cancelled && (err_msg.includes("closed") || err_msg.includes("connection") || err_msg.includes("fetch"))) {
            s.unhealthy = true;
            s.last_err = err instanceof Error ? err : new Error(err_msg);
          }

          if (is_cancelled) {
            return [
              {
                type: "text",
                text: "[interrupted]",
                data: "",
                mime_type: "",
              },
              [],
              true,
            ] as [tool_content_block, tool_content_block[], boolean];
          }

          throw err;
        } finally {
          if (timeout_id) clearTimeout(timeout_id);
        }
      },
      catch: (cause) => cause instanceof Error ? cause : new Error(String(cause)),
    });
  }

  close() {
    for (const s of this._sessions.values()) {
      s.close();
    }
    this._sessions.clear();
    this._tool_mapping.clear();
  }
}

export const new_manager = (): manager => {
  return new manager();
};

export const sanitize_name = (name: string): string => {
  return name.replace(/[^a-zA-Z0-9_]/g, "_");
};

export const is_tool_allowed = (name: string, filter?: mcp_server_tools_config): boolean => {
  if (filter === undefined || filter === null) {
    return true;
  }
  if (filter.include !== undefined && filter.include.length > 0) {
    let found = false;
    for (const inc of filter.include) {
      if (inc === name) {
        found = true;
        break;
      }
    }
    if (!found) {
      return false;
    }
  }
  if (filter.exclude !== undefined && filter.exclude.length > 0) {
    for (const exc of filter.exclude) {
      if (exc === name) {
        return false;
      }
    }
  }
  return true;
};

/*
PORT STATUS
source path: backend/mcp/manager.go
source lines: 464
draft lines: 494
confidence: high
status: phase_b_compile
*/
