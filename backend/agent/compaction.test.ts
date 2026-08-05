import { describe, expect, it } from "bun:test"
import { Agent } from "./register"
import { client } from "../opencode/client"

describe("manual compaction transaction", () => {
  it("always emits one request-scoped terminal event and releases ownership", async () => {
    const agent = new Agent()
    agent.client = new client("https://provider.invalid", "key", "model")
    const events: Array<{ kind: string; data: Record<string, unknown> }> = []
    agent.emit = (event) => events.push(event)

    await expect(agent.Compact(new AbortController().signal, "", "request-1")).rejects.toThrow(
      "No session manager available",
    )

    expect(events.map((event) => event.kind)).toEqual(["compaction_start", "compaction_end"])
    expect(events[0]?.data).toMatchObject({
      request_id: "request-1",
      operation_id: "request-1",
      reason: "manual",
    })
    expect(events[1]?.data).toMatchObject({
      request_id: "request-1",
      operation_id: "request-1",
      aborted: false,
      error_message: "No session manager available",
    })
    expect(agent.compactionOperationId).toBeNull()
    expect(agent.compactionCancel).toBeNull()
  })

  it("rejects a concurrent compaction without replacing its owner", async () => {
    const agent = new Agent()
    agent.client = new client("https://provider.invalid", "key", "model")
    agent.compactionOperationId = "existing"

    await expect(agent.Compact(new AbortController().signal, "", "request-2")).rejects.toThrow(
      "Compaction is already in progress",
    )
    expect(agent.compactionOperationId).toBe("existing")
  })

  it("can be cancelled while waiting for the previous agent turn to settle", async () => {
    const agent = new Agent()
    agent.client = new client("https://provider.invalid", "key", "model")
    agent.busy = true
    agent.cancel = () => {}
    const controller = new AbortController()
    const events: Array<{ kind: string; data: Record<string, unknown> }> = []
    agent.emit = (event) => events.push(event)

    const compacting = agent.Compact(controller.signal, "", "request-settle")
    await new Promise((resolve) => setTimeout(resolve, 0))
    controller.abort(new DOMException("cancelled", "AbortError"))

    await expect(compacting).rejects.toThrow("Compaction cancelled")
    expect(events.map((event) => event.kind)).toEqual(["compaction_start", "compaction_end"])
    expect(events[1]?.data).toMatchObject({ aborted: true, error_message: "Compaction cancelled" })
    expect(agent.compactionOperationId).toBeNull()
  })
})
