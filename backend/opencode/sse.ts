// PORT: backend/opencode/sse.go

import { Effect } from "effect";

// ErrSSEDone signals the consumer should stop reading SSE data.
export const err_sse_done = new Error("sse done");

// sseBlock is one SSE event block (lines between blank-line delimiters).
export type sse_block = { event_type: string; data: string; done: boolean };

// forEachSSEBlock parses SSE like Flame packages/ai parseSSE: accumulate bytes,
// split on "\n\n", then extract data: (and optional event:) lines per block.
export const for_each_sse_block = (
  chunks: Iterable<Uint8Array | string>,
  fn: (block: sse_block) => void | Error,
): Effect.Effect<void, Error> =>
  Effect.try({
    try: () => {
      let buf = "";
      const dec = new TextDecoder();
      for (const chunk of chunks) {
        buf += typeof chunk === "string" ? chunk : dec.decode(chunk);
        buf = drain_sse_buffer(buf, fn);
      }
      if (buf.length > 0) consume_sse_block(buf, fn);
    },
    catch: (cause) => cause instanceof Error ? cause : new Error(String(cause)),
  });

export const drain_sse_buffer = (buf: string, fn: (block: sse_block) => void | Error): string => {
  let s = buf;
  while (true) {
    const idx = s.indexOf("\n\n");
    if (idx < 0) return s;
    const block_text = s.slice(0, idx);
    s = s.slice(idx + 2);
    consume_sse_block(block_text, fn);
  }
};

export const consume_sse_block = (block_text: string, fn: (block: sse_block) => void | Error): void => {
  block_text = block_text.trim();
  if (block_text === "") return;
  const parsed = parse_sse_block(block_text);
  if (parsed.data === "" && !parsed.done) return;
  const err = fn(parsed);
  if (err instanceof Error) {
    if (err.message === err_sse_done.message) return;
    throw err;
  }
};

export const parse_sse_block = (block_text: string): sse_block => {
  let event_type = "";
  const data_lines: string[] = [];
  for (let line of block_text.split("\n")) {
    line = line.trim();
    if (line === "" || line.startsWith(":")) continue;
    if (line.startsWith("event:")) {
      event_type = line.slice("event:".length).trim();
      continue;
    }
    if (line.startsWith("data:")) data_lines.push(line.slice("data:".length).trim());
  }
  const data = data_lines.join("\n");
  return { event_type, data, done: data === "[DONE]" };
};

/*
PORT STATUS
source path: backend/opencode/sse.go
source lines: 108
draft lines: 80
confidence: medium
status: phase_a_draft
todos:
  - adapt for true streaming ReadableStream when client files are ported
notes:
  - forEachSSEBlock returns error in Go; modeled as Effect.Effect<void, Error>.
*/
