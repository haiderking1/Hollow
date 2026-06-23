// PORT: backend/web/types.go

import { Effect } from "effect";

export const ErrEmptyInput = new Error("query cannot be empty");
export const ErrNoSearchProvider = new Error("web search unavailable");

export enum fetch_failure_kind {
  fetch_timeout = "timeout",
  fetch_blocked = "blocked",
  fetch_rate_limited = "rate_limited",
  fetch_http_error = "http_error",
  fetch_network = "network",
  fetch_no_content = "no_content",
  fetch_js_rendered = "js_rendered",
  fetch_invalid_url = "invalid_url",
}

export class fetch_error extends Error {
  kind: fetch_failure_kind;
  httpStatus: number;
  message: string;
  partialHit?: page_hit;

  constructor(kind: fetch_failure_kind, httpStatus: number, message: string, partialHit?: page_hit) {
    super(message);
    this.kind = kind;
    this.httpStatus = httpStatus;
    this.message = message;
    this.partialHit = partialHit;
  }

  toString(): string {
    if (this.httpStatus > 0) {
      return `[${this.kind}] HTTP ${this.httpStatus}: ${this.message}`;
    }
    return `[${this.kind}] ${this.message}`;
  }

  Error(): string {
    return this.toString();
  }
}

export type search_result = {
  title: string;
  url: string;
  snippet: string;
  engine: string;
};

export type search_options = {
  maxResults: number;
  site: string;
  excludeSites: string[];
  engines: string;
};

export interface provider {
  search(
    ctx: AbortSignal,
    query: string,
    opts: search_options,
  ): Effect.Effect<search_result[], Error>;
}

export type page_hit = {
  title: string;
  url: string;
  content: string;
  fetch: fetch_error | null;
};

export type fetch_options = {
  maxURLs: number;
};

export const default_max_results = 8;
export const max_results_cap = 15;
export const default_max_fetch = 3;
export const max_fetch_cap = 5;
export const max_output_bytes = 96000;
export const fetch_timeout_sec = 20;

/*
PORT STATUS
source path: backend/web/types.go
source lines: 87
confidence: high
status: phase_b_compile
*/
