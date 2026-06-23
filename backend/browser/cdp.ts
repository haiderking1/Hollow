// PORT: backend/browser/cdp.go

import { Effect } from "effect";
import {
  should_auto_launch_browser,
  ensure_browser_launched,
  is_cdp_connection_error,
  format_cdp_connection_error,
} from "./launch";

export interface CdpTab {
  id: string;
  title: string;
  url: string;
  type: string;
  webSocketDebuggerUrl: string;
}

interface cdpResponse {
  id: number;
  result?: any;
  error?: cdpError;
}

interface cdpError {
  code: number;
  message: string;
  data?: any;
}

export class CdpSession {
  private ws: WebSocket;
  private nextId = 1;
  private pending = new Map<number, (res: cdpResponse) => void>();
  private eventHandlers = new Map<string, Array<{ id: number; handler: (params: any) => void }>>();
  private nextSubID = 1;
  closed = false;
  tabID: string;

  constructor(ws: WebSocket, tabId: string) {
    this.ws = ws;
    this.tabID = tabId;

    this.ws.onmessage = (event) => {
      try {
        const msgBytes = event.data;
        const msg = JSON.parse(typeof msgBytes === "string" ? msgBytes : msgBytes.toString());

        if (msg.id !== undefined && msg.id !== null) {
          const id = Number(msg.id);
          const resolve = this.pending.get(id);
          if (resolve) {
            this.pending.delete(id);
            resolve({
              id,
              result: msg.result,
              error: msg.error,
            });
          }
        } else if (msg.method) {
          const handlers = this.eventHandlers.get(msg.method);
          if (handlers) {
            for (const h of handlers) {
              setTimeout(() => h.handler(msg.params), 0);
            }
          }
        }
      } catch {}
    };

    this.ws.onerror = (err) => {
      this.failPending(err instanceof Error ? err : new Error(String(err)));
      this.Close();
    };

    this.ws.onclose = () => {
      this.failPending(new Error("Browser CDP session closed"));
      this.closed = true;
    };
  }

  Close(): void {
    if (!this.closed) {
      this.closed = true;
      try {
        this.ws.close();
      } catch {}
      this.failPending(new Error("Browser CDP session closed"));
    }
  }

  Send(method: string, params: any): Effect.Effect<any, Error> {
    if (this.closed) {
      return Effect.fail(new Error("Browser CDP session is closed"));
    }

    const id = this.nextId++;
    const payload = {
      id,
      method,
      params: params ?? undefined,
    };

    let reqBytes: string;
    try {
      reqBytes = JSON.stringify(payload);
    } catch (err) {
      return Effect.fail(err instanceof Error ? err : new Error(String(err)));
    }

    return Effect.async<any, Error>((resume) => {
      this.pending.set(id, (resp) => {
        if (resp.error) {
          resume(Effect.fail(new Error(resp.error.message)));
        } else {
          resume(Effect.succeed(resp.result));
        }
      });

      try {
        this.ws.send(reqBytes);
      } catch (err) {
        this.pending.delete(id);
        resume(Effect.fail(err instanceof Error ? err : new Error(String(err))));
      }
    });
  }

  OnEvent(method: string, handler: (params: any) => void): () => void {
    const subID = this.nextSubID++;
    let handlers = this.eventHandlers.get(method);
    if (!handlers) {
      handlers = [];
      this.eventHandlers.set(method, handlers);
    }
    handlers.push({ id: subID, handler });

    return () => {
      const current = this.eventHandlers.get(method);
      if (current) {
        this.eventHandlers.set(
          method,
          current.filter((sub) => sub.id !== subID)
        );
      }
    };
  }

  private failPending(err: Error): void {
    const pend = Array.from(this.pending.entries());
    this.pending.clear();
    for (const [_, resolve] of pend) {
      resolve({
        id: 0,
        error: { code: 0, message: err.message },
      });
    }
  }
}

const sessionCache = new Map<string, CdpSession>();

export function clearCdpSessionCache(): void {
  for (const s of sessionCache.values()) {
    s.Close();
  }
  sessionCache.clear();
}

export function get_browser_cdp_base_url(): string {
  const u = (process.env.HOLLOW_BROWSER_CDP_URL || "").trim();
  if (u === "") {
    return "http://127.0.0.1:9222";
  }
  return u;
}

export function assert_allowed_cdp_url(baseUrl: string): Effect.Effect<URL, Error> {
  return Effect.try({
    try: () => {
      const parsed = new URL(baseUrl);
      if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
        throw new Error(`Browser CDP URL must be http(s): ${baseUrl}`);
      }
      const allowRemote = process.env.HOLLOW_BROWSER_ALLOW_REMOTE === "1";
      const host = parsed.hostname.toLowerCase();
      const isLocal = host === "localhost" || host === "127.0.0.1" || host === "::1" || host === "[::1]";
      if (!isLocal && !allowRemote) {
        throw new Error(
          `Browser CDP URL must be localhost (set HOLLOW_BROWSER_ALLOW_REMOTE=1 for remote debugging): ${baseUrl}`
        );
      }
      return parsed;
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  });
}

export function cdp_http_request(
  baseUrl: string,
  urlPath: string,
  method: string,
  allowLaunch: boolean,
): Effect.Effect<string, Error> {
  return assert_allowed_cdp_url(baseUrl).pipe(
    Effect.flatMap(() => {
      const fullUrl = new URL(urlPath, baseUrl).toString();

      const doFetch = (attemptLaunch: boolean): Effect.Effect<string, Error> => {
        return Effect.tryPromise({
          try: async () => {
            const headers: Record<string, string> = {};
            if (method === "PUT") {
              headers["Content-Length"] = "0";
            }

            const controller = new AbortController();
            const timer = setTimeout(() => controller.abort(), 10000);

            try {
              const resp = await fetch(fullUrl, {
                method,
                headers,
                signal: controller.signal,
              });
              clearTimeout(timer);

              if (resp.status < 200 || resp.status >= 300) {
                throw new Error(`Browser CDP HTTP ${resp.status} for ${urlPath}`);
              }
              return await resp.text();
            } catch (err: any) {
              clearTimeout(timer);
              throw err;
            }
          },
          catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
        }).pipe(
          Effect.catchAll((err) => {
            if (attemptLaunch && should_auto_launch_browser() && is_cdp_connection_error(err)) {
              return ensure_browser_launched(baseUrl).pipe(
                Effect.flatMap((launched) => {
                  if (launched) {
                    return doFetch(false);
                  }
                  return format_cdp_connection_error(baseUrl, err, true).pipe(
                    Effect.flatMap((detail) => Effect.fail(new Error(detail)))
                  );
                }),
                Effect.catchAll((lerr) =>
                  format_cdp_connection_error(baseUrl, lerr, true).pipe(
                    Effect.flatMap((detail) => Effect.fail(new Error(detail)))
                  )
                )
              );
            }

            return format_cdp_connection_error(baseUrl, err, false).pipe(
              Effect.flatMap((detail) => Effect.fail(new Error(detail)))
            );
          })
        );
      };

      return doFetch(allowLaunch);
    })
  );
}

export function list_cdp_tabs(baseUrl: string): Effect.Effect<CdpTab[], Error> {
  return cdp_http_request(baseUrl, "/json/list", "GET", true).pipe(
    Effect.flatMap((bodyText) =>
      Effect.try({
        try: () => {
          const tabs = JSON.parse(bodyText) as CdpTab[];
          const filtered: CdpTab[] = [];
          for (const t of tabs) {
            if (t.id && t.webSocketDebuggerUrl) {
              filtered.push({
                id: t.id,
                title: t.title || "",
                url: t.url || "",
                type: t.type || "",
                webSocketDebuggerUrl: t.webSocketDebuggerUrl,
              });
            }
          }
          return filtered;
        },
        catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
      })
    )
  );
}

export function open_cdp_tab(rawUrl: string, baseUrl: string): Effect.Effect<CdpTab, Error> {
  let urlPath = "/json/new";
  if (rawUrl !== "") {
    urlPath += "?" + new URLSearchParams({ url: rawUrl }).toString();
  }
  return cdp_http_request(baseUrl, urlPath, "PUT", true).pipe(
    Effect.flatMap((bodyText) =>
      Effect.try({
        try: () => {
          const tab = JSON.parse(bodyText) as CdpTab;
          if (!tab.id || !tab.webSocketDebuggerUrl) {
            throw new Error("Browser CDP did not return a new tab descriptor");
          }
          return {
            id: tab.id,
            title: tab.title || "",
            url: tab.url || "",
            type: tab.type || "",
            webSocketDebuggerUrl: tab.webSocketDebuggerUrl,
          };
        },
        catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
      })
    )
  );
}

export function close_cdp_tab(tabId: string, baseUrl: string): Effect.Effect<void, Error> {
  const session = sessionCache.get(tabId);
  if (session) {
    session.Close();
    sessionCache.delete(tabId);
  }
  const path = `/json/close/${encodeURIComponent(tabId)}`;
  return cdp_http_request(baseUrl, path, "GET", true).pipe(Effect.map(() => undefined));
}

export function activate_cdp_tab(tabId: string, baseUrl: string): Effect.Effect<void, Error> {
  const path = `/json/activate/${encodeURIComponent(tabId)}`;
  return cdp_http_request(baseUrl, path, "GET", true).pipe(Effect.map(() => undefined));
}

export function resolve_cdp_tab(tabId: string, baseUrl: string): Effect.Effect<CdpTab, Error> {
  return list_cdp_tabs(baseUrl).pipe(
    Effect.flatMap((tabs) => {
      if (tabs.length === 0) {
        return Effect.fail(new Error("No browser tabs are available on the CDP endpoint"));
      }
      if (tabId === "") {
        for (const t of tabs) {
          if (t.type === "page") {
            return Effect.succeed(t);
          }
        }
        return Effect.succeed(tabs[0]);
      }
      for (const t of tabs) {
        if (t.id === tabId) {
          return Effect.succeed(t);
        }
      }
      const available = tabs.map((t) => t.id).join(", ");
      return Effect.fail(new Error(`Tab ${tabId} was not found. Available tabs: ${available}`));
    })
  );
}

export function connectCdpSession(tab: CdpTab): Effect.Effect<CdpSession, Error> {
  const cached = sessionCache.get(tab.id);
  if (cached && !cached.closed) {
    return Effect.succeed(cached);
  }

  return Effect.async<CdpSession, Error>((resume) => {
    let ws: WebSocket;
    try {
      ws = new WebSocket(tab.webSocketDebuggerUrl);
    } catch (err) {
      resume(Effect.fail(err instanceof Error ? err : new Error(String(err))));
      return;
    }

    let resolved = false;
    const timer = setTimeout(() => {
      if (resolved) return;
      resolved = true;
      try {
        ws.close();
      } catch {}
      resume(Effect.fail(new Error(`Failed to connect to browser tab ${tab.id}: timeout`)));
    }, 5000);

    ws.onopen = () => {
      if (resolved) return;
      resolved = true;
      clearTimeout(timer);
      const session = new CdpSession(ws, tab.id);
      sessionCache.set(tab.id, session);
      resume(Effect.succeed(session));
    };

    ws.onerror = () => {
      if (resolved) return;
      resolved = true;
      clearTimeout(timer);
      resume(Effect.fail(new Error(`Failed to connect to browser tab ${tab.id}`)));
    };
  });
}

export function with_cdp_session<A>(
  tabId: string,
  baseUrl: string,
  fn: (session: CdpSession, tab: CdpTab) => Effect.Effect<A, Error>,
): Effect.Effect<A, Error> {
  return resolve_cdp_tab(tabId, baseUrl).pipe(
    Effect.flatMap((tab) => connectCdpSession(tab).pipe(Effect.flatMap((session) => fn(session, tab))))
  );
}

export function EvaluateExpression(
  session: CdpSession,
  expression: string,
  awaitPromise: boolean,
): Effect.Effect<any, Error> {
  return session.Send("Runtime.enable", null).pipe(
    Effect.flatMap(() => {
      const params = {
        expression,
        awaitPromise,
        returnByValue: true,
        userGesture: true,
      };
      return session.Send("Runtime.evaluate", params);
    }),
    Effect.flatMap((resRaw) => {
      return Effect.try({
        try: () => {
          const evalRes = resRaw as {
            result: {
              type: string;
              value?: any;
              description?: string;
            };
            exceptionDetails?: {
              text: string;
              exception?: {
                description?: string;
              };
            };
          };

          if (evalRes.exceptionDetails) {
            let detail = evalRes.exceptionDetails.text;
            if (evalRes.exceptionDetails.exception && evalRes.exceptionDetails.exception.description) {
              detail = evalRes.exceptionDetails.exception.description;
            }
            if (detail === "") {
              detail = "eval failed";
            }
            throw new Error(detail);
          }

          const remote = evalRes.result;
          if (remote.type === "undefined") {
            return null;
          }
          if (remote.value !== undefined && remote.value !== null) {
            return remote.value;
          }
          if (remote.type === "object" && remote.description) {
            try {
              return JSON.parse(remote.description);
            } catch {}
          }
          if (remote.description) {
            return remote.description;
          }
          return null;
        },
        catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
      });
    })
  );
}

/*
PORT STATUS
source path: backend/browser/cdp.go
source lines: 500
draft lines: 406
confidence: high
status: phase_b_compile
*/
