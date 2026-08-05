import { describe, expect, it } from "bun:test"
import { consume_codex_responses_sse, message_from_codex_stream_state } from "./codex_stream"
import { content_string } from "./types"

const streamOf = (text: string) => new ReadableStream<Uint8Array>({
  start(controller) {
    controller.enqueue(new TextEncoder().encode(text))
    controller.close()
  },
})

describe("Codex compaction SSE", () => {
  it("accepts a completed summary response", async () => {
    const stream = streamOf([
      'event: response.output_text.delta\ndata: {"type":"response.output_text.delta","delta":"summary"}\n\n',
      'event: response.completed\ndata: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}\n\n',
    ].join(""))
    const state = await consume_codex_responses_sse(stream, new AbortController().signal, {})
    const message = message_from_codex_stream_state(state)
    expect(content_string(message)).toBe("summary")
    expect(message.usage?.totalTokens).toBe(12)
  })

  it("rejects an incomplete summary response", async () => {
    const stream = streamOf([
      'event: response.output_text.delta\ndata: {"type":"response.output_text.delta","delta":"partial"}\n\n',
      'event: response.incomplete\ndata: {"type":"response.incomplete","response":{"status":"incomplete"}}\n\n',
    ].join(""))
    const state = await consume_codex_responses_sse(stream, new AbortController().signal, {})
    expect(() => message_from_codex_stream_state(state)).toThrow("incomplete")
  })
})
