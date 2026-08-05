import { describe, expect, it } from "bun:test"
import { IsContextOverflowError } from "./overflow"

describe("context overflow detection", () => {
  it("walks nested provider causes", () => {
    const provider = new Error("input exceeds the context window")
    const wrapped = new Error("chat_stream", { cause: { reason: "provider", cause: provider } })
    expect(IsContextOverflowError(wrapped)).toBe(true)
  })
})
