import { afterEach, describe, expect, it } from "bun:test"
import { Effect } from "effect"
import fs from "node:fs/promises"
import path from "node:path"
import os from "node:os"
import { manager } from "./manager"

describe("compaction persistence", () => {
  const cleanup: string[] = []

  afterEach(async () => {
    await Promise.all(cleanup.splice(0).map((dir) => fs.rm(dir, { recursive: true, force: true })))
  })

  it("rolls back the in-memory checkpoint when persistence fails", async () => {
    const session = new manager()
    const state = session as unknown as {
      _session_file: string
      _flushed: boolean
      _leaf_id: string | null
      _entries: string[]
    }
    state._session_file = path.join(os.tmpdir(), `hollow-missing-${crypto.randomUUID()}`, "session.jsonl")
    state._flushed = true
    state._leaf_id = "parent"
    state._entries = []

    const result = await Effect.runPromise(Effect.either(
      session.append_compaction("summary", "kept", 100, null, false),
    ))

    expect(result._tag).toBe("Left")
    expect(state._entries).toEqual([])
    expect(state._leaf_id).toBe("parent")
  })

  it("persists and reloads a compaction checkpoint from disk", async () => {
    const dir = await fs.mkdtemp(path.join(os.tmpdir(), "hollow-compaction-reload-"))
    cleanup.push(dir)
    const session = new manager()
    session.set_cwd(dir)
    session.set_session_dir(dir)
    await Effect.runPromise(session.new_session())
    await Effect.runPromise(session.append_message({ role: "user", content: new TextEncoder().encode('"hello"') }))
    await Effect.runPromise(session.append_message({ role: "assistant", content: new TextEncoder().encode('"answer"') }))
    const firstMessage = session.parsed_entries().find((entry) => entry.type === "message")
    expect(firstMessage?.id).toBeTruthy()
    await Effect.runPromise(session.append_compaction("durable summary", firstMessage!.id, 120, null, false))

    const reopened = new manager()
    await Effect.runPromise(reopened.open_file(session.session_file()))
    const summary = reopened.chat_lines().find((line) => line.role === "compactionSummary")
    expect(summary).toMatchObject({ text: "durable summary", tokens_before: 120 })
    expect((reopened.build_session_context().messages ?? [])[0]?.role).toBe("compactionSummary")
  })
})
