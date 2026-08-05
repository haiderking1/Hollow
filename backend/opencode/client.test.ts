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
})
