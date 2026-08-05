import { describe, expect, it } from "bun:test"
import { Effect } from "effect"
import { client, client_error } from "../opencode/client"
import { generate_summary } from "./compaction"
import { string_content, type chat_request, type chat_response } from "../opencode/types"

const response = (text: string, finish_reason = "stop"): chat_response => ({
  choices: [{ message: { role: "assistant", content: string_content(text) }, finish_reason }],
})

describe("compaction summarization", () => {
  it("caps output and retries transient failures", async () => {
    const api = new client("https://provider.invalid", "key", "model")
    let calls = 0
    let captured: chat_request | undefined
    api.chat = ((_signal: AbortSignal, request: chat_request) => {
      calls++
      captured = request
      return calls < 3
        ? Effect.fail(client_error("chat", Object.assign(new Error("unavailable"), { status: 503 })))
        : Effect.succeed(response("summary"))
    }) as client["chat"]

    const summary = await Effect.runPromise(generate_summary(
      new AbortController().signal,
      api,
      [{ role: "user", content: string_content("conversation") }],
      1_000,
      "",
      "",
      8_000,
    ))

    expect(summary).toBe("summary")
    expect(calls).toBe(3)
    expect(captured?.max_tokens).toBe(800)
  })

  it("rejects truncated and empty summaries", async () => {
    const api = new client("https://provider.invalid", "key", "model")
    api.chat = (() => Effect.succeed(response("partial", "length"))) as client["chat"]
    const truncated = await Effect.runPromise(Effect.either(generate_summary(
      new AbortController().signal,
      api,
      [{ role: "user", content: string_content("conversation") }],
      1_000,
      "",
      "",
      8_000,
    )))
    expect(truncated._tag).toBe("Left")

    api.chat = (() => Effect.succeed(response(""))) as client["chat"]
    const empty = await Effect.runPromise(Effect.either(generate_summary(
      new AbortController().signal,
      api,
      [{ role: "user", content: string_content("conversation") }],
      1_000,
      "",
      "",
      8_000,
    )))
    expect(empty._tag).toBe("Left")
  })
})
