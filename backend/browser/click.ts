// PORT: backend/browser/click.go

import { Effect } from "effect";
import { type CdpSession, EvaluateExpression } from "./cdp";

export interface ClickPlan {
  Selector: string | null;
  Index: number;
}

export interface ClickProbe {
  x: number;
  y: number;
  tag: string;
  id?: string;
  className?: string;
  href?: string;
  text?: string;
  selector?: string;
  index?: number;
  matchCount?: number;
}

export interface DownloadInfo {
  started: boolean;
  filename?: string;
  url?: string;
}

export interface ClickFeedback {
  clicked: boolean;
  x: number;
  y: number;
  tag: string;
  id?: string;
  className?: string;
  href?: string;
  text?: string;
  selector?: string;
  index?: number;
  matchCount?: number;
  download?: DownloadInfo;
}

export function buildClickProbeExpression(plan: ClickPlan): Effect.Effect<string, Error> {
  const clickableSelector = `a[href], button, [role="button"], input[type="submit"], input[type="button"], [onclick]`;
  const clickableSelectorJson = JSON.stringify(clickableSelector);

  if (plan.Selector !== null) {
    let selectorJson: string;
    try {
      selectorJson = JSON.stringify(plan.Selector);
    } catch (err) {
      return Effect.fail(err instanceof Error ? err : new Error(String(err)));
    }
    const expr = `(() => {
			let matches;
			try {
				matches = document.querySelectorAll(${selectorJson});
			} catch (error) {
				const message = error instanceof Error ? error.message : String(error);
				throw new Error("Invalid CSS selector: " + ${selectorJson} + ". " + message);
			}
			const matchCount = matches.length;
			if (matchCount === 0) {
				throw new Error("Selector not found: " + ${selectorJson});
			}
			const index = ${plan.Index};
			if (index < 0 || index >= matchCount) {
				throw new Error("Selector " + ${selectorJson} + " matched " + matchCount + " element(s), but index " + index + " is out of range");
			}
			const el = matches[index];
			el.scrollIntoView({ block: "center", inline: "center" });
			const rect = el.getBoundingClientRect();
			return JSON.stringify({
				x: rect.left + rect.width / 2,
				y: rect.top + rect.height / 2,
				tag: el.tagName,
				id: el.id || undefined,
				className: typeof el.className === "string" && el.className.length > 0 ? el.className : undefined,
				href: el.href || el.getAttribute("href") || undefined,
				text: (el.textContent || "").trim().slice(0, 200) || undefined,
				selector: ${selectorJson},
				index,
				matchCount,
			});
		})()`;
    return Effect.succeed(expr);
  }

  const expr = `(() => {
		const candidates = Array.from(document.querySelectorAll(${clickableSelectorJson}));
		const index = ${plan.Index};
		if (index < 0 || index >= candidates.length) {
			throw new Error("Element index " + index + " is out of range (" + candidates.length + " clickable elements)");
		}
		const el = candidates[index];
		el.scrollIntoView({ block: "center", inline: "center" });
		const rect = el.getBoundingClientRect();
		return JSON.stringify({
			x: rect.left + rect.width / 2,
			y: rect.top + rect.height / 2,
			tag: el.tagName,
			id: el.id || undefined,
			className: typeof el.className === "string" && el.className.length > 0 ? el.className : undefined,
			href: el.href || el.getAttribute("href") || undefined,
			text: (el.textContent || "").trim().slice(0, 200) || undefined,
			index,
			matchCount: candidates.length,
		});
	})()`;
  return Effect.succeed(expr);
}

export function parseClickProbe(raw: any): Effect.Effect<ClickProbe, Error> {
  return Effect.try({
    try: () => {
      if (typeof raw === "string") {
        return JSON.parse(raw) as ClickProbe;
      }
      if (typeof raw === "object" && raw !== null) {
        return {
          x: Number(raw.x),
          y: Number(raw.y),
          tag: String(raw.tag),
          id: raw.id !== undefined ? String(raw.id) : undefined,
          className: raw.className !== undefined ? String(raw.className) : undefined,
          href: raw.href !== undefined ? String(raw.href) : undefined,
          text: raw.text !== undefined ? String(raw.text) : undefined,
          selector: raw.selector !== undefined ? String(raw.selector) : undefined,
          index: raw.index !== undefined ? Number(raw.index) : undefined,
          matchCount: raw.matchCount !== undefined ? Number(raw.matchCount) : undefined,
        };
      }
      throw new Error(`Invalid raw click probe type: ${typeof raw}`);
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  });
}

export function dispatchMouseClick(
  session: CdpSession,
  x: number,
  y: number,
): Effect.Effect<void, Error> {
  const baseParams = {
    x,
    y,
    button: "left",
    clickCount: 1,
  };

  return session.Send("Input.dispatchMouseEvent", { ...baseParams, type: "mousePressed" }).pipe(
    Effect.flatMap(() =>
      session.Send("Input.dispatchMouseEvent", { ...baseParams, type: "mouseReleased" })
    ),
    Effect.map(() => undefined)
  );
}

export function waitForDownloadBegin(
  session: CdpSession,
  timeoutMs: number,
): Effect.Effect<DownloadInfo | null, Error> {
  return Effect.async<DownloadInfo | null, Error>((resume) => {
    let completed = false;

    const unsubscribe = session.OnEvent("Page.downloadWillBegin", (params: any) => {
      if (completed) return;
      completed = true;
      clearTimeout(timer);
      unsubscribe();

      const suggestedFilename = params?.suggestedFilename || "";
      const url = params?.url || "";

      resume(
        Effect.succeed({
          started: true,
          filename: suggestedFilename,
          url: url,
        })
      );
    });

    const timer = setTimeout(() => {
      if (completed) return;
      completed = true;
      unsubscribe();
      resume(Effect.succeed(null));
    }, timeoutMs);
  });
}

export function clickElementWithFeedback(
  session: CdpSession,
  plan: ClickPlan,
): Effect.Effect<ClickFeedback, Error> {
  return session.Send("Page.enable", null).pipe(
    Effect.flatMap(() => buildClickProbeExpression(plan)),
    Effect.flatMap((expr) => EvaluateExpression(session, expr, false)),
    Effect.flatMap((evalVal) => parseClickProbe(evalVal)),
    Effect.flatMap((probe) =>
      Effect.all([
        waitForDownloadBegin(session, 3000),
        dispatchMouseClick(session, probe.x, probe.y),
      ], { concurrency: "unbounded" }).pipe(
        Effect.map(([dl, _]) => {
          const feedback: ClickFeedback = {
            clicked: true,
            x: probe.x,
            y: probe.y,
            tag: probe.tag,
            id: probe.id,
            className: probe.className,
            href: probe.href,
            text: probe.text,
            selector: probe.selector,
            index: probe.index,
            matchCount: probe.matchCount,
          };
          if (dl !== null) {
            feedback.download = dl;
          }
          return feedback;
        })
      )
    )
  );
}

/*
PORT STATUS
source path: backend/browser/click.go
source lines: 240
draft lines: 211
confidence: high
status: phase_b_compile
*/
