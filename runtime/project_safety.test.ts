import { describe, expect, it } from "bun:test"
import { Effect } from "effect"
import fs from "node:fs/promises"
import os from "node:os"
import path from "node:path"
import { continue_recent_if_exists } from "../backend/session/manager"
import { AgentRuntimeImpl } from "./agent_runtime"

describe("runtime project and session safety", () => {
  it("continue_recent_if_exists returns null when directory has no sessions", async () => {
    const tmpDir = await fs.mkdtemp(path.join(os.tmpdir(), "hollow-test-safety-"))
    try {
      const sm = await Effect.runPromise(continue_recent_if_exists(tmpDir))
      expect(sm).toBeNull()
    } finally {
      await fs.rm(tmpDir, { recursive: true, force: true })
    }
  })

  it("agent_runtime prompt fails when no session is loaded", async () => {
    const runtime = new AgentRuntimeImpl()
    await Effect.runPromise(runtime.bootDegraded())
    runtime.available = true // pretend connected

    const result = await Effect.runPromise(Effect.either(runtime.prompt("hello")))
    expect(result._tag).toBe("Left")
    if (result._tag === "Left") {
      expect(result.left.message).toBe("Select a project to start")
    }
  })

  it("agent_runtime tracks projectSelected state correctly across lifecycle operations", async () => {
    const runtime = new AgentRuntimeImpl()
    await Effect.runPromise(runtime.bootDegraded())
    expect(runtime.projectSelected).toBe(false)

    const tmpDir = await fs.mkdtemp(path.join(os.tmpdir(), "hollow-test-proj-"))
    try {
      // Starting a new session with cwd sets projectSelected to true
      const sm = await Effect.runPromise(runtime.newSession(tmpDir))
      expect(runtime.projectSelected).toBe(true)
      expect(sm.session_id()).toBeTruthy()

      const sessionFile = sm.session_file()

      // Deleting active session resets projectSelected to false
      await Effect.runPromise(runtime.deleteSession(sessionFile))
      expect(runtime.projectSelected).toBe(false)

      // Creating another session sets projectSelected to true again
      const sm2 = await Effect.runPromise(runtime.newSession(tmpDir))
      expect(runtime.projectSelected).toBe(true)
      expect(sm2.session_id()).toBeTruthy()

      // Deleting project sessions for cwd resets projectSelected to false
      await Effect.runPromise(runtime.deleteSessionsForCwd(tmpDir))
      expect(runtime.projectSelected).toBe(false)
    } finally {
      await fs.rm(tmpDir, { recursive: true, force: true })
    }
  })
})
