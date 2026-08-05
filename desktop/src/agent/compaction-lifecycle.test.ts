import { describe, expect, it } from "bun:test"
import { detachesAssistantStream } from "./compaction-lifecycle"

describe("compaction assistant stream ownership", () => {
  it("detaches only for standalone manual compaction", () => {
    expect(detachesAssistantStream("manual")).toBe(true)
    expect(detachesAssistantStream("threshold")).toBe(false)
    expect(detachesAssistantStream("overflow")).toBe(false)
  })
})
