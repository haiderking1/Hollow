import { afterEach, describe, expect, it } from "bun:test"
import { HollowClient } from "./hollowClient"
import type { AgentEvent } from "./rpc"

const originalWindow = globalThis.window

afterEach(() => {
  Object.defineProperty(globalThis, "window", { configurable: true, value: originalWindow })
})

describe("HollowClient request lifecycle", () => {
  it("finishes manual compaction exactly once even if dispatch settles later", async () => {
    let backendEvent: ((message: unknown) => void) | undefined
    let resolvePrompt: ((value: { ok: boolean; error?: string }) => void) | undefined
    let promptRequest: Record<string, unknown> | undefined
    const promptResult = new Promise<{ ok: boolean; error?: string }>((resolve) => {
      resolvePrompt = resolve
    })
    const bridge = {
      isElectron: true,
      onEvent: (listener: (message: unknown) => void) => {
        backendEvent = listener
        return () => {}
      },
      dispatch: (message: Record<string, unknown>) => {
        if (message.type === "prompt") {
          promptRequest = message
          return promptResult
        }
        return Promise.resolve({ ok: true, data: { type: "ready" } })
      },
    }
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: { hollowDesktop: bridge },
    })

    const client = new HollowClient()
    const events: AgentEvent[] = []
    client.onEvent((event) => events.push(event))
    client.send({ type: "prompt", message: "/compact" })

    const requestId = String(promptRequest?.requestId)
    expect(events.filter((event) => event.type === "compaction_start")).toHaveLength(0)
    backendEvent?.({
      type: "compaction.start",
      requestId,
      operationId: requestId,
      reason: "manual",
    })
    expect(events.filter((event) => event.type === "compaction_start")).toHaveLength(1)
    backendEvent?.({
      type: "compaction.end",
      requestId,
      operationId: requestId,
      reason: "manual",
      result: { summary: "summary", firstKeptEntryId: "entry", tokensBefore: 100 },
      aborted: false,
      willRetry: false,
    })
    resolvePrompt?.({ ok: false, error: "late dispatch error" })
    await Promise.resolve()

    expect(events.filter((event) => event.type === "compaction_end")).toHaveLength(1)
    expect(events.filter((event) => event.type === "agent_end")).toHaveLength(0)
  })

  it("ignores duplicate and stale done events", async () => {
    let backendEvent: ((message: unknown) => void) | undefined
    let promptRequest: Record<string, unknown> | undefined
    const bridge = {
      isElectron: true,
      onEvent: (listener: (message: unknown) => void) => {
        backendEvent = listener
        return () => {}
      },
      dispatch: (message: Record<string, unknown>) => {
        if (message.type === "prompt") promptRequest = message
        return Promise.resolve({ ok: true, data: { type: "ready" } })
      },
    }
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: { hollowDesktop: bridge },
    })

    const client = new HollowClient()
    const events: AgentEvent[] = []
    client.onEvent((event) => events.push(event))
    client.send({ type: "prompt", message: "hello" })
    await Promise.resolve()
    const requestId = String(promptRequest?.requestId)

    backendEvent?.({ type: "done", requestId: "stale" })
    backendEvent?.({ type: "done", requestId })
    backendEvent?.({ type: "done", requestId })

    expect(events.filter((event) => event.type === "agent_end")).toHaveLength(1)
  })

  for (const reason of ["threshold", "overflow"] as const) {
    it(`preserves and resumes assistant output across ${reason} compaction`, async () => {
      let backendEvent: ((message: unknown) => void) | undefined
      let promptRequest: Record<string, unknown> | undefined
      const bridge = {
        isElectron: true,
        onEvent: (listener: (message: unknown) => void) => {
          backendEvent = listener
          return () => {}
        },
        dispatch: (message: Record<string, unknown>) => {
          if (message.type === "prompt") promptRequest = message
          return Promise.resolve({ ok: true, data: { type: "ready" } })
        },
      }
      Object.defineProperty(globalThis, "window", {
        configurable: true,
        value: { hollowDesktop: bridge },
      })

      const client = new HollowClient()
      const events: AgentEvent[] = []
      client.onEvent((event) => events.push(event))
      client.send({ type: "prompt", message: "continue after compaction" })
      await Promise.resolve()
      const requestId = String(promptRequest?.requestId)
      const operationId = `${requestId}-${reason}`

      backendEvent?.({ type: "token", text: "before " })
      backendEvent?.({ type: "compaction.start", requestId, operationId, reason })
      backendEvent?.({
        type: "compaction.end",
        requestId,
        operationId,
        reason,
        result: { summary: "summary", firstKeptEntryId: "entry", tokensBefore: 100 },
        aborted: false,
        willRetry: reason === "overflow",
      })
      backendEvent?.({ type: "token", text: "after" })
      backendEvent?.({ type: "done", requestId })

      const updates = events.filter((event) => event.type === "message_update")
      const final = updates.at(-1)
      expect(final?.type === "message_update" && final.assistantMessageEvent?.partial?.content).toEqual([
        { type: "text", text: "before after" },
      ])
      expect(events.filter((event) => event.type === "agent_end")).toHaveLength(1)
    })
  }

  it("processes a duplicate automatic compaction.end exactly once", async () => {
    let backendEvent: ((message: unknown) => void) | undefined
    let promptRequest: Record<string, unknown> | undefined
    const bridge = {
      isElectron: true,
      onEvent: (listener: (message: unknown) => void) => {
        backendEvent = listener
        return () => {}
      },
      dispatch: (message: Record<string, unknown>) => {
        if (message.type === "prompt") promptRequest = message
        return Promise.resolve({ ok: true, data: { type: "ready" } })
      },
    }
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: { hollowDesktop: bridge },
    })

    const client = new HollowClient()
    const events: AgentEvent[] = []
    client.onEvent((event) => events.push(event))
    client.send({ type: "prompt", message: "continue" })
    await Promise.resolve()
    const requestId = String(promptRequest?.requestId)
    const operationId = `${requestId}-auto`

    backendEvent?.({ type: "compaction.start", requestId, operationId, reason: "threshold" })
    const end = {
      type: "compaction.end" as const,
      requestId,
      operationId,
      reason: "threshold",
      result: { summary: "summary", firstKeptEntryId: "entry", tokensBefore: 100 },
      aborted: false,
      willRetry: false,
    }
    backendEvent?.(end)
    backendEvent?.(end)
    backendEvent?.({ type: "done", requestId })

    expect(events.filter((event) => event.type === "compaction_end")).toHaveLength(1)
    expect(events.filter((event) => event.type === "agent_end")).toHaveLength(1)
  })

  it("finalizes a duplicate manual compaction.end exactly once", async () => {
    let backendEvent: ((message: unknown) => void) | undefined
    let promptRequest: Record<string, unknown> | undefined
    const bridge = {
      isElectron: true,
      onEvent: (listener: (message: unknown) => void) => {
        backendEvent = listener
        return () => {}
      },
      dispatch: (message: Record<string, unknown>) => {
        if (message.type === "prompt") promptRequest = message
        return Promise.resolve({ ok: true, data: { type: "ready" } })
      },
    }
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: { hollowDesktop: bridge },
    })

    const client = new HollowClient()
    const events: AgentEvent[] = []
    client.onEvent((event) => events.push(event))
    client.send({ type: "prompt", message: "/compact" })
    await Promise.resolve()
    const requestId = String(promptRequest?.requestId)

    backendEvent?.({ type: "compaction.start", requestId, operationId: requestId, reason: "manual" })
    const end = {
      type: "compaction.end" as const,
      requestId,
      operationId: requestId,
      reason: "manual",
      result: { summary: "summary", firstKeptEntryId: "entry", tokensBefore: 100 },
      aborted: false,
      willRetry: false,
    }
    backendEvent?.(end)
    backendEvent?.(end)

    expect(events.filter((event) => event.type === "compaction_end")).toHaveLength(1)
    expect(events.filter((event) => event.type === "agent_end")).toHaveLength(0)
  })

  it("ignores a compaction.start replayed after its end", async () => {
    let backendEvent: ((message: unknown) => void) | undefined
    let promptRequest: Record<string, unknown> | undefined
    const bridge = {
      isElectron: true,
      onEvent: (listener: (message: unknown) => void) => {
        backendEvent = listener
        return () => {}
      },
      dispatch: (message: Record<string, unknown>) => {
        if (message.type === "prompt") promptRequest = message
        return Promise.resolve({ ok: true, data: { type: "ready" } })
      },
    }
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: { hollowDesktop: bridge },
    })

    const client = new HollowClient()
    const events: AgentEvent[] = []
    client.onEvent((event) => events.push(event))
    client.send({ type: "prompt", message: "continue" })
    await Promise.resolve()
    const requestId = String(promptRequest?.requestId)
    const operationId = `${requestId}-auto`

    backendEvent?.({ type: "compaction.start", requestId, operationId, reason: "threshold" })
    backendEvent?.({
      type: "compaction.end",
      requestId,
      operationId,
      reason: "threshold",
      result: { summary: "summary", firstKeptEntryId: "entry", tokensBefore: 100 },
      aborted: false,
      willRetry: false,
    })
    backendEvent?.({ type: "compaction.start", requestId, operationId, reason: "threshold" })

    expect(events.filter((event) => event.type === "compaction_start")).toHaveLength(1)
    expect(events.filter((event) => event.type === "compaction_end")).toHaveLength(1)
  })

  it("sequences new_session acknowledgement before session.history and get_state confirmation", async () => {
    let backendEvent: ((message: unknown) => void) | undefined
    let dispatchedCommand: Record<string, unknown> | undefined
    const bridge = {
      isElectron: true,
      onEvent: (listener: (message: unknown) => void) => {
        backendEvent = listener
        return () => {}
      },
      dispatch: (message: Record<string, unknown>) => {
        dispatchedCommand = message
        return Promise.resolve({ ok: true, data: undefined })
      },
    }
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: { hollowDesktop: bridge },
    })

    const client = new HollowClient()
    const events: AgentEvent[] = []
    client.onEvent((event) => events.push(event))

    client.send({ type: "new_session", cwd: "/tmp/test-project" })
    await Promise.resolve()

    expect(dispatchedCommand?.type).toBe("newSession")
    expect(dispatchedCommand?.cwd).toBe("/tmp/test-project")

    const newSessionAck = events.find(
      (e) => e.type === "response" && e.command === "new_session",
    )
    expect(newSessionAck).toBeDefined()

    const getStateEventsBefore = events.filter(
      (e) => e.type === "response" && e.command === "get_state",
    )
    const latestStateBefore = getStateEventsBefore.at(-1)
    if (latestStateBefore && latestStateBefore.type === "response" && latestStateBefore.command === "get_state") {
      expect(latestStateBefore.data.sessionId).toBeFalsy()
    }

    backendEvent?.({
      type: "session.history",
      sessionId: "session-abc-123",
      cwd: "/tmp/test-project",
      messages: [],
    })

    const getStateEventsAfter = events.filter(
      (e) => e.type === "response" && e.command === "get_state",
    )
    const latestStateAfter = getStateEventsAfter.at(-1)
    expect(latestStateAfter?.type).toBe("response")
    if (latestStateAfter && latestStateAfter.type === "response" && latestStateAfter.command === "get_state") {
      expect(latestStateAfter.data.sessionId).toBe("session-abc-123")
    }
  })
})
