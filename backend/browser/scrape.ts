// PORT: backend/browser/scrape.go

import { Effect } from "effect";

export const MaxElementList = 50;

export function buildScrapeExpression(
  selector: string | null,
  format: string,
): Effect.Effect<string, Error> {
  const clickableSelector = `a[href], button, [role="button"], input[type="submit"], input[type="button"], [onclick]`;
  const clickableSelectorJson = JSON.stringify(clickableSelector);

  if (format === "elements") {
    if (selector !== null) {
      let selectorJson: string;
      try {
        selectorJson = JSON.stringify(selector);
      } catch (err) {
        return Effect.fail(err instanceof Error ? err : new Error(String(err)));
      }
      const expr = `(() => {
				let matches;
				try {
					matches = document.querySelectorAll(${selectorJson});
				} catch (error) {
					const message = error instanceof Error ? error.message : String(error);
					throw new Error("Invalid CSS selector: " + ${selectorJson} + ". " + message + " (:contains() is jQuery, not CSS.)");
				}
				return Array.from(matches).slice(0, ${MaxElementList}).map((el, index) => ({
					index,
					tag: el.tagName,
					id: el.id || undefined,
					className: typeof el.className === "string" && el.className.length > 0 ? el.className : undefined,
					href: el.href || el.getAttribute("href") || undefined,
					text: (el.textContent || "").trim().slice(0, 200) || undefined,
				}));
			})()`;
      return Effect.succeed(expr);
    }
    const expr = `(() => {
			const candidates = Array.from(document.querySelectorAll(${clickableSelectorJson}));
			return candidates.slice(0, ${MaxElementList}).map((el, index) => ({
				index,
				tag: el.tagName,
				id: el.id || undefined,
				className: typeof el.className === "string" && el.className.length > 0 ? el.className : undefined,
				href: el.href || el.getAttribute("href") || undefined,
				text: (el.textContent || "").trim().slice(0, 200) || undefined,
				role: el.getAttribute("role") || undefined,
				type: el.getAttribute("type") || undefined,
			}));
		})()`;
    return Effect.succeed(expr);
  }

  if (format === "html") {
    let target = "document.documentElement";
    if (selector !== null) {
      let selectorJson: string;
      try {
        selectorJson = JSON.stringify(selector);
      } catch (err) {
        return Effect.fail(err instanceof Error ? err : new Error(String(err)));
      }
      target = `document.querySelector(${selectorJson})`;
    }
    const expr = `(() => { const node = ${target}; if (!node) return null; return node.outerHTML ?? node.textContent ?? ""; })()`;
    return Effect.succeed(expr);
  }

  if (format === "links") {
    let root = "document";
    if (selector !== null) {
      let selectorJson: string;
      try {
        selectorJson = JSON.stringify(selector);
      } catch (err) {
        return Effect.fail(err instanceof Error ? err : new Error(String(err)));
      }
      root = `document.querySelector(${selectorJson})`;
    }
    const expr = `(() => {
			const root = ${root};
			if (!root) return [];
			const anchors = root.querySelectorAll ? root.querySelectorAll("a[href]") : [];
			return Array.from(anchors).map((a) => ({
				href: a.href,
				text: (a.textContent || "").trim(),
			}));
		})()`;
    return Effect.succeed(expr);
  }

  // Default to "text"
  let target = "document.body";
  if (selector !== null) {
    let selectorJson: string;
    try {
      selectorJson = JSON.stringify(selector);
    } catch (err) {
      return Effect.fail(err instanceof Error ? err : new Error(String(err)));
    }
    target = `document.querySelector(${selectorJson})`;
  }
  const expr = `(() => { const node = ${target}; if (!node) return null; return node.innerText ?? node.textContent ?? ""; })()`;
  return Effect.succeed(expr);
}

/*
PORT STATUS
source path: backend/browser/scrape.go
source lines: 96
draft lines: 119
confidence: high
status: phase_b_compile
*/
