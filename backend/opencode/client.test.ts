import { afterEach, describe, expect, it } from "bun:test"
import { Effect } from "effect"
import { client } from "./client"
import { string_content } from "./types"

const originalFetch = globalThis.fetch

afterEach(() => {
  globalThis.fetch = originalFetch
})

describe("provider request lifecycle", () => {
  it("enforces timeout_ms and identifies the timeout", async () => {
    globalThis.fetch = ((_input: RequestInfo | URL, init?: RequestInit) =>
      new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener("abort", () => reject(init.signal?.reason), { once: true })
      })) as typeof fetch

    const api = new client("https://provider.invalid", "key", "model")
    api.timeout_ms = 5
    const result = await Effect.runPromise(Effect.either(api.chat(new AbortController().signal, {
      model: "model",
      messages: [{ role: "user", content: string_content("hello") }],
    })))

    expect(result._tag).toBe("Left")
    if (result._tag === "Left") {
      expect(result.left.timeout).toBe(true)
      expect(result.left.reason).toContain("timed out")
    }
  })

  it("preserves HTTP status and provider message", async () => {
    globalThis.fetch = (async () => new Response(
      JSON.stringify({ error: { message: "context window exceeded", type: "context_length_exceeded" } }),
      { status: 400 },
    )) as typeof fetch

    const api = new client("https://provider.invalid", "key", "model")
    const result = await Effect.runPromise(Effect.either(api.chat(new AbortController().signal, {
      model: "model",
      messages: [{ role: "user", content: string_content("hello") }],
    })))

    expect(result._tag).toBe("Left")
    if (result._tag === "Left") {
      expect(result.left.status).toBe(400)
      expect(result.left.reason).toContain("context window exceeded")
    }
  })

  it("converts compactionSummary role in session context into a provider-supported user role during serialization", async () => {
    let capturedBody: any = null
    globalThis.fetch = (async (_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.body && typeof init.body === "string") {
        capturedBody = JSON.parse(init.body)
      }
      return new Response(
        JSON.stringify({ choices: [{ message: { role: "assistant", content: string_content("ok") }, finish_reason: "stop" }] }),
        { status: 200 },
      )
    }) as typeof fetch

    const api = new client("https://provider.invalid", "key", "model")
    await Effect.runPromise(
      api.chat(new AbortController().signal, {
        model: "model",
        messages: [
          { role: "system", content: string_content("System prompt") },
          { role: "compactionSummary", content: string_content("Summary of previous turns") },
          { role: "user", content: string_content("Current question") },
        ],
      }),
    )

    expect(capturedBody).not.toBeNull()
    expect(capturedBody.messages).toHaveLength(3)
    expect(capturedBody.messages[0].role).toBe("system")
    expect(capturedBody.messages[1].role).toBe("user")
    expect(capturedBody.messages[1].content).toContain("The conversation history before this point was compacted into the following summary:")
    expect(capturedBody.messages[1].content).toContain("Summary of previous turns")
    expect(capturedBody.messages[2].role).toBe("user")
    expect(capturedBody.messages.map((m: any) => m.role)).not.toContain("compactionSummary")
  })
})
