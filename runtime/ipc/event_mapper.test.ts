import { describe, expect, it } from "bun:test"
import { mapAgentEvent, mapDispatchResponse } from "./event_mapper"

describe("compaction IPC mapping", () => {
  it("maps request-scoped start and successful end events", () => {
    expect(mapAgentEvent({
      kind: "compaction_start",
      data: { request_id: "req", operation_id: "op", session_id: "session", reason: "threshold" },
    })).toEqual({
      type: "compaction.start",
      requestId: "req",
      operationId: "op",
      sessionId: "session",
      reason: "threshold",
    })

    expect(mapAgentEvent({
      kind: "compaction_end",
      data: {
        request_id: "req",
        operation_id: "op",
        session_id: "session",
        reason: "threshold",
        result: {
          summary: "summary",
          firstKeptEntryId: "entry",
          tokensBefore: 100,
          estimatedTokensAfter: 20,
        },
        aborted: false,
        will_retry: false,
        error_message: "",
      },
    })).toEqual({
      type: "compaction.end",
      requestId: "req",
      operationId: "op",
      sessionId: "session",
      reason: "threshold",
      result: {
        summary: "summary",
        firstKeptEntryId: "entry",
        tokensBefore: 100,
        estimatedTokensAfter: 20,
      },
      aborted: false,
      willRetry: false,
      errorMessage: undefined,
    })
  })

  it("treats prompt success as an acknowledgement, not done", () => {
    expect(mapDispatchResponse({ type: "prompt.success", requestId: "req" })).toEqual({ type: "ready" })
    expect(mapAgentEvent({ kind: "request_end", data: { request_id: "req" } })).toEqual({
      type: "done",
      requestId: "req",
    })
  })
})
