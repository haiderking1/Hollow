// PORT: backend/browser/eval.go

import { Effect } from "effect";
import { type ClickPlan, type ClickFeedback } from "./click";

const containsRe = /:contains\s*\(/i;
const allSingleRe = /^document\.querySelectorAll\(\s*'([\s\S]*?)'\s*\)\s*\[\s*(\d+)\s*\]\.click\(\)\s*$/;
const oneSingleRe = /^document\.querySelector\(\s*'([\s\S]*?)'\s*\)\.click\(\)\s*$/;
const allDoubleRe = /^document\.querySelectorAll\(\s*"([\s\S]*?)"\s*\)\s*\[\s*(\d+)\s*\]\.click\(\)\s*$/;
const oneDoubleRe = /^document\.querySelector\(\s*"([\s\S]*?)"\s*\)\.click\(\)\s*$/;
const allBacktickRe = /^document\.querySelectorAll\(\s*`([\s\S]*?)`\s*\)\s*\[\s*(\d+)\s*\]\.click\(\)\s*$/;
const oneBacktickRe = /^document\.querySelector\(\s*`([\s\S]*?)`\s*\)\.click\(\)\s*$/;
const kwRe = /^(document|window|return|function|const|let|var|if|for|while|async|await)\b/i;
const startRe = /^([.#\[]|[a-zA-Z_*])/;
const clickEndRe = /\b\.click\s*\(\s*\)\s*$/;

export function LooksLikeCssSelectorOnly(value: string): boolean {
  const trimmed = value.trim();
  if (trimmed === "" || trimmed.includes("(") || trimmed.includes(";")) {
    return false;
  }
  if (kwRe.test(trimmed)) {
    return false;
  }
  return startRe.test(trimmed);
}

export function ParseQuerySelectorClickExpression(expression: string): [string, number, boolean] {
  const trimmed = expression.trim();
  let m = trimmed.match(allSingleRe);
  if (m) {
    return [m[1], parseInt(m[2], 10), true];
  }
  m = trimmed.match(oneSingleRe);
  if (m) {
    return [m[1], 0, true];
  }
  m = trimmed.match(allDoubleRe);
  if (m) {
    return [m[1], parseInt(m[2], 10), true];
  }
  m = trimmed.match(oneDoubleRe);
  if (m) {
    return [m[1], 0, true];
  }
  m = trimmed.match(allBacktickRe);
  if (m) {
    return [m[1], parseInt(m[2], 10), true];
  }
  m = trimmed.match(oneBacktickRe);
  if (m) {
    return [m[1], 0, true];
  }
  return ["", 0, false];
}

export function ValidateCssSelector(selector: string): Error | null {
  if (containsRe.test(selector)) {
    return new Error(
      `Invalid CSS selector "${selector}": :contains() is jQuery syntax, not valid CSS. Use scrape format=elements to list matches, then click with a standard selector and optional index.`
    );
  }
  return null;
}

export function ResolveClickPlan(
  expression: string | null | undefined,
  selector: string | null | undefined,
  index: number | null | undefined,
): Effect.Effect<ClickPlan | null, Error> {
  if (selector !== null && selector !== undefined && selector !== "") {
    const err = ValidateCssSelector(selector);
    if (err) {
      return Effect.fail(err);
    }
    const idx = index !== null && index !== undefined ? index : 0;
    return Effect.succeed({ Selector: selector, Index: idx });
  }

  const expr = expression ? expression.trim() : "";
  if (expr === "") {
    if (index !== null && index !== undefined) {
      return Effect.succeed({ Selector: null, Index: index });
    }
    return Effect.succeed(null);
  }

  if (LooksLikeCssSelectorOnly(expr)) {
    const err = ValidateCssSelector(expr);
    if (err) {
      return Effect.fail(err);
    }
    const idx = index !== null && index !== undefined ? index : 0;
    return Effect.succeed({ Selector: expr, Index: idx });
  }

  const [sel, idx, ok] = ParseQuerySelectorClickExpression(expr);
  if (ok) {
    const err = ValidateCssSelector(sel);
    if (err) {
      return Effect.fail(err);
    }
    return Effect.succeed({ Selector: sel, Index: idx });
  }

  return Effect.succeed(null);
}

export function ResolveEvalExpression(
  expression: string | null | undefined,
  selector: string | null | undefined,
  index: number | null | undefined,
): Effect.Effect<string, Error> {
  return ResolveClickPlan(expression, selector, index).pipe(
    Effect.flatMap((plan) => {
      if (plan !== null) {
        return Effect.fail(new Error("Internal error: click plans must use clickElementWithFeedback"));
      }

      const expr = expression ? expression.trim() : "";
      if (expr === "") {
        return Effect.fail(new Error("expression, selector, or index is required for eval action"));
      }

      if (clickEndRe.test(expr)) {
        return Effect.fail(
          new Error(
            "Bare .click() expressions return undefined. Use selector (e.g. selector=.btn-hero-primary) or index from scrape format=elements instead of a raw click expression."
          )
        );
      }

      return Effect.succeed(expr);
    })
  );
}

export function FormatEvalResultText(result: any): string {
  if (result === null || result === undefined) {
    return "undefined";
  }

  const isFeedback = typeof result === "object" && "clicked" in result;
  if (isFeedback && result.clicked) {
    const feedback = result as ClickFeedback;
    const lines: string[] = ["Clicked element:"];
    if (feedback.tag) {
      lines.push(`  tag: ${feedback.tag}`);
    }
    if (feedback.className) {
      lines.push(`  class: ${feedback.className}`);
    }
    if (feedback.text) {
      lines.push(`  text: ${feedback.text}`);
    }
    if (feedback.href) {
      lines.push(`  href: ${feedback.href}`);
    }
    if (feedback.selector) {
      lines.push(`  selector: ${feedback.selector}`);
    }
    if (feedback.index !== undefined && feedback.index !== null) {
      let idxLine = `  index: ${feedback.index}`;
      if (feedback.matchCount !== undefined && feedback.matchCount !== null) {
        idxLine += ` of ${feedback.matchCount}`;
      }
      lines.push(idxLine);
    }
    if (feedback.download && feedback.download.started) {
      let filename = feedback.download.filename;
      if (!filename) {
        filename = "unknown file";
      }
      let dlLine = `  download started: ${filename}`;
      if (feedback.download.url) {
        dlLine += `\n  download url: ${feedback.download.url}`;
      }
      lines.push(dlLine);
    }
    return lines.join("\n");
  }

  if (typeof result === "string") {
    return result;
  }

  try {
    return JSON.stringify(result, null, "  ");
  } catch {
    return String(result);
  }
}

/*
PORT STATUS
source path: backend/browser/eval.go
source lines: 203
draft lines: 172
confidence: high
status: phase_b_compile
*/
