// PORT: backend/web/extract.go

import { Effect } from "effect";
import {
  type page_hit,
  fetch_error,
  fetch_failure_kind,
  fetch_timeout_sec,
} from "./types";
import { user_agent } from "./url_guard";

const metaDescRe = /<meta[^>]+name=["']description["'][^>]+content=["']([^"']+)["']/i;
const ogDescRe = /<meta[^>]+property=["']og:description["'][^>]+content=["']([^"']+)["']/i;

export type html_fetch = {
  finalURL: string;
  status: number;
  body: string;
};

export const download_html = (
  ctx: AbortSignal,
  pageURL: string,
): Effect.Effect<html_fetch, fetch_error> => {
  return Effect.tryPromise({
    try: async () => {
      const controller = new AbortController();
      const onAbort = () => controller.abort();
      ctx.addEventListener("abort", onAbort);

      const timeoutId = setTimeout(() => controller.abort(), fetch_timeout_sec * 1000);

      try {
        const resp = await fetch(pageURL, {
          method: "GET",
          headers: {
            "User-Agent": user_agent(),
          },
          signal: controller.signal,
        });

        clearTimeout(timeoutId);
        ctx.removeEventListener("abort", onAbort);

        const status = resp.status;
        if (status === 429) {
          throw new fetch_error(fetch_failure_kind.fetch_rate_limited, status, "rate limited");
        }
        if (status === 403 || status === 401) {
          throw new fetch_error(fetch_failure_kind.fetch_blocked, status, "access denied");
        }
        if (status >= 400) {
          throw new fetch_error(fetch_failure_kind.fetch_http_error, status, `HTTP error ${status}`);
        }

        const bodyText = await resp.text();
        return {
          finalURL: resp.url || pageURL,
          status,
          body: bodyText,
        };
      } catch (err: any) {
        clearTimeout(timeoutId);
        ctx.removeEventListener("abort", onAbort);

        if (err instanceof fetch_error) {
          throw err;
        }

        if (controller.signal.aborted) {
          throw new fetch_error(fetch_failure_kind.fetch_timeout, 0, "request timed out");
        }
        throw new fetch_error(fetch_failure_kind.fetch_network, 0, err.message || String(err));
      }
    },
    catch: (cause) => {
      if (cause instanceof fetch_error) {
        return cause;
      }
      return new fetch_error(fetch_failure_kind.fetch_network, 0, String(cause));
    },
  });
};

type token =
  | { type: "tag-start"; name: string; isClose: boolean; isSelfClosing: boolean }
  | { type: "text"; text: string }
  | { type: "comment" };

const tokenize_html = (html: string): token[] => {
  const tokens: token[] = [];
  let i = 0;
  while (i < html.length) {
    if (html[i] === "<") {
      if (html.slice(i, i + 4) === "<!--") {
        const closeIdx = html.indexOf("-->", i + 4);
        if (closeIdx === -1) {
          i = html.length;
        } else {
          tokens.push({ type: "comment" });
          i = closeIdx + 3;
        }
        continue;
      }
      const closeIdx = html.indexOf(">", i);
      if (closeIdx === -1) {
        tokens.push({ type: "text", text: html.slice(i) });
        break;
      }
      const tagContent = html.slice(i + 1, closeIdx).trim();
      i = closeIdx + 1;
      if (tagContent === "") continue;

      const isClose = tagContent.startsWith("/");
      const isSelfClosing = tagContent.endsWith("/");
      let tagName = tagContent;
      if (isClose) tagName = tagName.slice(1);
      if (isSelfClosing) tagName = tagName.slice(0, -1);
      tagName = tagName.split(/\s+/)[0].toLowerCase();

      tokens.push({ type: "tag-start", name: tagName, isClose, isSelfClosing });
    } else {
      const nextOpen = html.indexOf("<", i);
      if (nextOpen === -1) {
        tokens.push({ type: "text", text: html.slice(i) });
        break;
      }
      tokens.push({ type: "text", text: html.slice(i, nextOpen) });
      i = nextOpen;
    }
  }
  return tokens;
};

type parse_node = {
  type: "element" | "text";
  name: string;
  text?: string;
  children: parse_node[];
};

const build_html_tree = (tokens: token[]): parse_node => {
  const root: parse_node = { type: "element", name: "root", children: [] };
  const stack: parse_node[] = [root];

  for (const tok of tokens) {
    if (tok.type === "comment") {
      continue;
    }
    if (tok.type === "text") {
      if (stack.length > 0) {
        stack[stack.length - 1].children.push({
          type: "text",
          name: "",
          text: tok.text,
          children: [],
        });
      }
    } else if (tok.type === "tag-start") {
      if (tok.isClose) {
        const idx = stack.map((n) => n.name).lastIndexOf(tok.name);
        if (idx > 0) {
          stack.splice(idx);
        }
      } else {
        const node: parse_node = {
          type: "element",
          name: tok.name,
          children: [],
        };
        if (stack.length > 0) {
          stack[stack.length - 1].children.push(node);
        }
        const selfClosingTags = new Set(["img", "br", "hr", "input", "meta", "link"]);
        if (!tok.isSelfClosing && !selfClosingTags.has(tok.name)) {
          stack.push(node);
        }
      }
    }
  }
  return root;
};

const remove_nodes = (node: parse_node, tags: Set<string>): void => {
  node.children = node.children.filter((child) => {
    if (child.type === "element" && tags.has(child.name)) {
      return false;
    }
    remove_nodes(child, tags);
    return true;
  });
};

const collect_text = (node: parse_node, parts: string[]): void => {
  if (node.type === "text" && node.text) {
    const trimmed = node.text.trim();
    if (trimmed !== "") {
      parts.push(trimmed);
    }
  }
  for (const child of node.children) {
    collect_text(child, parts);
  }
};

export const strip_html_text = (htmlStr: string): string => {
  const tokens = tokenize_html(htmlStr);
  const root = build_html_tree(tokens);
  remove_nodes(root, new Set(["script", "style", "noscript", "nav", "header", "footer"]));
  const parts: string[] = [];
  collect_text(root, parts);
  return parts.join(" ").replace(/\s+/g, " ").trim();
};

export const extract_html_title = (htmlStr: string): string => {
  const tokens = tokenize_html(htmlStr);
  const root = build_html_tree(tokens);

  let title = "";
  const findTitle = (node: parse_node) => {
    if (title !== "") return;
    if (node.type === "element" && node.name === "title") {
      const parts: string[] = [];
      collect_text(node, parts);
      title = parts.join(" ").trim();
      return;
    }
    for (const child of node.children) {
      findTitle(child);
    }
  };
  findTitle(root);
  return title;
};

export const html_unescape = (s: string): string => {
  return s
    .replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/&apos;/g, "'")
    .replace(/&#(\d+);/g, (_, dec) => String.fromCharCode(parseInt(dec, 10)))
    .replace(/&#x([0-9a-f]+);/gi, (_, hex) => String.fromCharCode(parseInt(hex, 16)));
};

export const extract_meta_description = (body: string): string => {
  let m = ogDescRe.exec(body);
  if (m && m.length === 2) {
    return html_unescape(m[1].trim());
  }
  m = metaDescRe.exec(body);
  if (m && m.length === 2) {
    return html_unescape(m[1].trim());
  }
  return "";
};

export const is_youtube_host = (host: string): boolean => {
  const h = host.toLowerCase();
  return (
    h === "youtube.com" ||
    h === "www.youtube.com" ||
    h === "m.youtube.com" ||
    h === "youtu.be"
  );
};

export const extract_youtube_from_html = (
  pageURL: string,
  body: string,
): [string, string, boolean] => {
  try {
    const u = new URL(pageURL);
    if (!is_youtube_host(u.hostname)) {
      return ["", "", false];
    }
    const title = extract_html_title(body);
    const desc = extract_meta_description(body);
    if (title === "" && desc === "") {
      return ["", "", false];
    }
    let out = "";
    if (title !== "") {
      out += `Title: ${title}\n`;
    }
    if (desc !== "") {
      out += `Description: ${desc}\n`;
    }
    out +=
      "\nNote: Full YouTube transcripts and comments require the watch page API or a browser; static fetch returns metadata only.";
    return [out.trim(), title, true];
  } catch {
    return ["", "", false];
  }
};

export const fetch_youtube_oembed = (
  ctx: AbortSignal,
  pageURL: string,
): Effect.Effect<page_hit, Error> => {
  return Effect.tryPromise({
    try: async () => {
      const oembed = `https://www.youtube.com/oembed?format=json&url=${encodeURIComponent(
        pageURL,
      )}`;
      const resp = await fetch(oembed, { signal: ctx });
      if (resp.status !== 200) {
        throw new Error(`youtube oembed: HTTP ${resp.status}`);
      }
      const data = (await resp.json()) as { title: string; author_name?: string };
      let content = `Title: ${data.title}\n`;
      if (data.author_name) {
        content += `Channel: ${data.author_name}\n`;
      }
      content += "\nNote: YouTube descriptions and comments are not available via static fetch.";
      return {
        title: data.title,
        url: pageURL,
        content,
        fetch: null,
      };
    },
    catch: (cause) => cause instanceof Error ? cause : new Error(String(cause)),
  });
};

const extract_readability_text = (root: parse_node): string => {
  remove_nodes(
    root,
    new Set(["script", "style", "noscript", "nav", "header", "footer", "aside", "iframe"]),
  );

  const paragraphs: string[] = [];
  const collectParagraphs = (node: parse_node) => {
    if (node.type === "element" && node.name === "p") {
      const parts: string[] = [];
      collect_text(node, parts);
      const txt = parts.join(" ").trim();
      if (txt !== "") {
        paragraphs.push(txt);
      }
      return;
    }
    for (const child of node.children) {
      collectParagraphs(child);
    }
  };
  collectParagraphs(root);

  if (paragraphs.length > 0) {
    const text = paragraphs.join("\n\n");
    if (text.length >= 200) {
      return text;
    }
  }

  const parts: string[] = [];
  collect_text(root, parts);
  return parts.join(" ").replace(/\s+/g, " ").trim();
};

export const extract_page_content = (
  pageURL: string,
  body: string,
): [string, string, fetch_error | null] => {
  const [ytText, ytTitle, ytOk] = extract_youtube_from_html(pageURL, body);
  if (ytOk) {
    return [ytTitle, ytText, null];
  }

  const tokens = tokenize_html(body);
  const root = build_html_tree(tokens);

  const title = extract_html_title(body);
  const content = extract_readability_text(root);

  if (content !== "") {
    return [title, content, null];
  }

  const meta = extract_meta_description(body);
  if (meta !== "") {
    return [title, meta, null];
  }

  const plain = strip_html_text(body);
  if (plain.length >= 120) {
    return [title, plain, null];
  }

  if (body.includes("<script") && plain.length < 120) {
    return [
      "",
      "",
      new fetch_error(
        fetch_failure_kind.fetch_js_rendered,
        0,
        "no article content in static HTML — page likely requires JavaScript",
      ),
    ];
  }

  return [
    "",
    "",
    new fetch_error(
      fetch_failure_kind.fetch_no_content,
      0,
      "no readable content extracted",
    ),
  ];
};

/*
PORT STATUS
source path: backend/web/extract.go
source lines: 267
confidence: high
status: phase_b_compile
*/
