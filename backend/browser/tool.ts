// PORT: backend/browser/tool.go

import { Effect } from "effect";
import fs from "node:fs/promises";
import path from "node:path";
import { type page_hit, fetch_failure_kind } from "../web/types";
import { extract_page_content } from "../web/extract";
import {
  type CdpTab,
  type CdpSession,
  get_browser_cdp_base_url,
  open_cdp_tab,
  close_cdp_tab,
  connectCdpSession,
  list_cdp_tabs,
  resolve_cdp_tab,
  activate_cdp_tab,
  with_cdp_session,
  EvaluateExpression,
} from "./cdp";
import { ensure_browser_launched } from "./launch";
import { ResolveClickPlan, ResolveEvalExpression, FormatEvalResultText, ValidateCssSelector } from "./eval";
import { clickElementWithFeedback } from "./click";
import { buildScrapeExpression } from "./scrape";
import { truncateScrape } from "./truncate";
import {
  waitForCloudflareClearance,
  IsCloudflareChallengeText,
  waitForPageReady,
  CloudflareWaitTimeout,
} from "./cloudflare";

export interface BrowserArgs {
  action: string;
  tabId?: string;
  url?: string;
  expression?: string;
  method?: string;
  params?: Record<string, any>;
  selector?: string;
  index?: number;
  format?: string;
  savePath?: string;
  awaitPromise?: boolean;
}

export interface BrowserTabSummary {
  id: string;
  title: string;
  url: string;
  type: string;
}

export interface BrowserToolDetails {
  action: string;
  tabId?: string;
  url?: string;
  selector?: string;
  savePath?: string;
  tabs?: BrowserTabSummary[];
  result?: any;
  bytes?: number;
  truncated?: boolean;
}

export const MaxDownloadBytes = 10 * 1024 * 1024;

export function downloadViaPageFetch(
  cwd: string,
  tabId: string,
  downloadUrl: string,
  savePath: string,
  baseUrl: string,
): Effect.Effect<[string, number], Error> {
  let destination = savePath;
  if (!path.isAbsolute(destination)) {
    destination = path.join(cwd, savePath);
  }

  return Effect.tryPromise({
    try: async () => {
      await fs.mkdir(path.dirname(destination), { recursive: true, mode: 0o700 });
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  }).pipe(
    Effect.flatMap(() =>
      with_cdp_session(tabId, baseUrl, (session, tab) => {
        const urlJson = JSON.stringify(downloadUrl);
        const expr = `(async () => {
					const res = await fetch(${urlJson}, { credentials: "include" });
					const contentType = res.headers.get("content-type") || "";
					const ab = await res.arrayBuffer();
					if (ab.byteLength > ${MaxDownloadBytes}) {
						throw new Error("Download exceeds ${MaxDownloadBytes} bytes");
					}
					const bytes = Array.from(new Uint8Array(ab));
					return { ok: res.ok, status: res.status, contentType, bytes };
				})()`;

        return EvaluateExpression(session, expr, true);
      })
    ),
    Effect.flatMap((rawPayload) => {
      if (!rawPayload) {
        return Effect.fail(new Error("Browser download returned no data"));
      }
      return Effect.try({
        try: () => {
          const payload = rawPayload as {
            ok: boolean;
            status: number;
            bytes: number[];
          };
          if (!payload.ok) {
            throw new Error(`Browser download failed with HTTP ${payload.status}`);
          }
          return Buffer.from(payload.bytes);
        },
        catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
      });
    }),
    Effect.flatMap((buf) =>
      Effect.tryPromise({
        try: async () => {
          await fs.writeFile(destination, buf, { mode: 0o600 });
          return [destination, buf.length] as [string, number];
        },
        catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
      })
    )
  );
}

export function ExecuteBrowser(
  ctx: AbortSignal,
  cwd: string,
  args: BrowserArgs,
): Effect.Effect<[string, BrowserToolDetails], Error> {
  const baseUrl = get_browser_cdp_base_url();

  switch (args.action) {
    case "list":
      return list_cdp_tabs(baseUrl).pipe(
        Effect.flatMap((tabs) => {
          const summaries = tabs.map((t) => ({
            id: t.id,
            title: t.title,
            url: t.url,
            type: t.type,
          }));
          let output = "No tabs.";
          if (summaries.length > 0) {
            output = summaries.map((s) => `${s.id} [${s.type}] ${s.title} - ${s.url}`).join("\n");
          }
          return Effect.succeed([output, { action: "list", tabs: summaries }] as [string, BrowserToolDetails]);
        })
      );

    case "open":
      return open_cdp_tab(args.url || "", baseUrl).pipe(
        Effect.map((tab) => {
          const summary = {
            id: tab.id,
            title: tab.title,
            url: tab.url,
            type: tab.type,
          };
          const output = `Opened tab ${tab.id}: ${tab.url}`;
          return [
            output,
            {
              action: "open",
              tabId: tab.id,
              url: tab.url,
              tabs: [summary],
            },
          ] as [string, BrowserToolDetails];
        })
      );

    case "close":
      return resolve_cdp_tab(args.tabId || "", baseUrl).pipe(
        Effect.flatMap((tab) =>
          close_cdp_tab(tab.id, baseUrl).pipe(
            Effect.map(() => {
              const output = `Closed tab ${tab.id}`;
              return [
                output,
                {
                  action: "close",
                  tabId: tab.id,
                  url: tab.url,
                },
              ] as [string, BrowserToolDetails];
            })
          )
        )
      );

    case "activate":
      return resolve_cdp_tab(args.tabId || "", baseUrl).pipe(
        Effect.flatMap((tab) =>
          activate_cdp_tab(tab.id, baseUrl).pipe(
            Effect.map(() => {
              const output = `Activated tab ${tab.id}: ${tab.url}`;
              return [
                output,
                {
                  action: "activate",
                  tabId: tab.id,
                  url: tab.url,
                },
              ] as [string, BrowserToolDetails];
            })
          )
        )
      );

    case "cdp": {
      const method = args.method;
      if (!method) {
        return Effect.fail(new Error("method is required for cdp action"));
      }
      return with_cdp_session(args.tabId || "", baseUrl, (session, tab) => {
        if (method === "Page.navigate") {
          let navigateUrl = args.url || "";
          if (navigateUrl === "" && args.params && typeof args.params.url === "string") {
            navigateUrl = args.params.url;
          }
          if (navigateUrl === "") {
            return Effect.fail(new Error("url is required for Page.navigate"));
          }
          const params = { ...args.params, url: navigateUrl };
          return session.Send(method, params);
        }
        const params = args.params || {};
        return session.Send(method, params);
      }).pipe(
        Effect.flatMap((rawRes) =>
          resolve_cdp_tab(args.tabId || "", baseUrl).pipe(
            Effect.flatMap((tab) =>
              Effect.try({
                try: () => {
                  const jsonText = JSON.stringify(rawRes, null, "  ");
                  return [
                    jsonText,
                    {
                      action: "cdp",
                      tabId: tab.id,
                      url: tab.url,
                      result: rawRes,
                    },
                  ] as [string, BrowserToolDetails];
                },
                catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
              })
            )
          )
        )
      );
    }

    case "eval":
      return ResolveClickPlan(args.expression, args.selector, args.index).pipe(
        Effect.flatMap((plan) =>
          with_cdp_session(args.tabId || "", baseUrl, (session, tab) => {
            if (plan !== null) {
              return clickElementWithFeedback(session, plan);
            }
            const awaitPromise = args.awaitPromise !== undefined ? args.awaitPromise : true;
            return ResolveEvalExpression(args.expression, args.selector, args.index).pipe(
              Effect.flatMap((expr) => EvaluateExpression(session, expr, awaitPromise))
            );
          })
        ),
        Effect.flatMap((rawRes) =>
          resolve_cdp_tab(args.tabId || "", baseUrl).pipe(
            Effect.map((tab) => {
              const text = FormatEvalResultText(rawRes);
              return [
                text,
                {
                  action: "eval",
                  tabId: tab.id,
                  url: tab.url,
                  selector: args.selector,
                  result: rawRes,
                },
              ] as [string, BrowserToolDetails];
            })
          )
        )
      );

    case "scrape": {
      const format = args.format || "text";
      if (args.selector && format === "elements") {
        const err = ValidateCssSelector(args.selector);
        if (err) {
          return Effect.fail(err);
        }
      }
      const selPtr = args.selector || null;
      return buildScrapeExpression(selPtr, format).pipe(
        Effect.flatMap((expr) =>
          with_cdp_session(args.tabId || "", baseUrl, (session, tab) => EvaluateExpression(session, expr, true))
        ),
        Effect.flatMap((rawScraped) =>
          resolve_cdp_tab(args.tabId || "", baseUrl).pipe(
            Effect.flatMap((tab) => {
              if (rawScraped === null || rawScraped === undefined) {
                if (args.selector) {
                  return Effect.fail(
                    new Error(`Scrape selector not found: ${args.selector}`)
                  ).pipe(
                    Effect.catchAll((err) =>
                      Effect.fail(
                        new Error(err.message) // ensures error type
                      )
                    ),
                    Effect.either,
                    Effect.flatMap((res) => {
                      return Effect.fail(
                        new Error(`Scrape selector not found: ${args.selector}`)
                      );
                    })
                  );
                }
                return Effect.fail(new Error("Scrape returned no content"));
              }

              if (format === "elements") {
                const list = Array.isArray(rawScraped) ? rawScraped : [];
                if (list.length === 0) {
                  if (args.selector) {
                    return Effect.fail(new Error(`Selector matched no elements: ${args.selector}`));
                  }
                  return Effect.fail(new Error("No clickable elements found on page"));
                }
              }

              let rawText = "";
              if (typeof rawScraped === "string") {
                rawText = rawScraped;
              } else {
                try {
                  rawText = JSON.stringify(rawScraped, null, "  ");
                } catch {
                  rawText = String(rawScraped);
                }
              }

              const [truncatedText, isTruncated] = truncateScrape(rawText);
              return Effect.succeed([
                truncatedText,
                {
                  action: "scrape",
                  tabId: tab.id,
                  url: tab.url,
                  selector: args.selector,
                  truncated: isTruncated,
                },
              ] as [string, BrowserToolDetails]);
            })
          )
        )
      );
    }

    case "download":
      if (!args.url) {
        return Effect.fail(new Error("url is required for download action"));
      }
      if (!args.savePath) {
        return Effect.fail(new Error("savePath is required for download action"));
      }
      return downloadViaPageFetch(cwd, args.tabId || "", args.url, args.savePath, baseUrl).pipe(
        Effect.flatMap(([savedPath, bytesCount]) =>
          resolve_cdp_tab(args.tabId || "", baseUrl).pipe(
            Effect.map((tab) => {
              const output = `Downloaded ${bytesCount} bytes to ${savedPath}`;
              return [
                output,
                {
                  action: "download",
                  tabId: tab.id,
                  url: args.url,
                  savePath: savedPath,
                  bytes: bytesCount,
                },
              ] as [string, BrowserToolDetails];
            })
          )
        )
      );
  }

  return Effect.fail(new Error(`Unsupported browser action: ${args.action}`));
}

export function scrape_url(
  ctx: AbortSignal,
  targetUrl: string,
): Effect.Effect<page_hit, Error> {
  const baseUrl = get_browser_cdp_base_url();

  return ensure_browser_launched(baseUrl).pipe(
    Effect.flatMap(() => open_cdp_tab(targetUrl, baseUrl)),
    Effect.flatMap((tab) => {
      const runScrape = connectCdpSession(tab).pipe(
        Effect.flatMap((session) => {
          return waitForPageReady(session, 5000).pipe(
            Effect.flatMap(() => buildScrapeExpression(null, "html")),
            Effect.flatMap((htmlExpr) => {
              const attemptLoop = (attempt: number): Effect.Effect<page_hit, Error> => {
                return waitForCloudflareClearance(session).pipe(
                  Effect.flatMap(() => EvaluateExpression(session, htmlExpr, true)),
                  Effect.flatMap((htmlVal) => {
                    if (typeof htmlVal !== "string" || htmlVal === "") {
                      return Effect.fail(new Error("browser returned empty html"));
                    }

                    const htmlStr = htmlVal;

                    if (IsCloudflareChallengeText(htmlStr)) {
                      if (attempt === 0) {
                        return Effect.sleep(CloudflareWaitTimeout).pipe(Effect.flatMap(() => attemptLoop(1)));
                      }
                      return Effect.fail(new Error("cloudflare challenge still present after wait"));
                    }

                    const [title, content, extractErr] = extract_page_content(targetUrl, htmlStr);
                    if (extractErr === null) {
                      return Effect.succeed({
                        title,
                        url: targetUrl,
                        content,
                        fetch: null,
                      });
                    }

                    if (attempt === 0) {
                      if (
                        extractErr.kind === fetch_failure_kind.fetch_js_rendered ||
                        IsCloudflareChallengeText(htmlStr)
                      ) {
                        return Effect.sleep(CloudflareWaitTimeout).pipe(
                          Effect.flatMap(() => waitForCloudflareClearance(session)),
                          Effect.flatMap(() => attemptLoop(1))
                        );
                      }
                    }

                    return Effect.succeed({
                      title,
                      url: targetUrl,
                      content: "",
                      fetch: extractErr,
                    });
                  })
                );
              };

              return attemptLoop(0);
            })
          );
        })
      );

      return Effect.ensuring(
        runScrape,
        close_cdp_tab(tab.id, baseUrl).pipe(Effect.orDie)
      );
    })
  );
}

/*
PORT STATUS
source path: backend/browser/tool.go
source lines: 469
draft lines: 421
confidence: high
status: phase_b_compile
*/
