import { describe, expect, it } from "bun:test"
import { pickProjectSession, isProjectSelected } from "./project-session-safety"
import type { AgentSessionInfo } from "../agent/rpc"

describe("project-session-safety", () => {
  describe("pickProjectSession", () => {
    it("returns none when no sessions exist for the project", () => {
      const result = pickProjectSession([], "/home/user/project-a")
      expect(result).toEqual({ action: "none" })
    })

    it("returns select with the most recent session when sessions exist", () => {
      const sessions: AgentSessionInfo[] = [
        {
          id: "sess-1",
          path: "/path/1",
          cwd: "/home/user/project-a",
          name: "Old session",
          created: "2026-01-01T00:00:00.000Z",
          modified: "2026-01-01T10:00:00.000Z",
          messageCount: 5,
          firstMessage: "Hello",
        },
        {
          id: "sess-2",
          path: "/path/2",
          cwd: "/home/user/project-a",
          name: "New session",
          created: "2026-01-02T00:00:00.000Z",
          modified: "2026-01-02T12:00:00.000Z",
          messageCount: 2,
          firstMessage: "Hi",
        },
        {
          id: "sess-other",
          path: "/path/3",
          cwd: "/home/user/project-b",
          name: "Other project session",
          created: "2026-01-03T00:00:00.000Z",
          modified: "2026-01-03T15:00:00.000Z",
          messageCount: 1,
          firstMessage: "Hey",
        },
      ]

      const result = pickProjectSession(sessions, "/home/user/project-a")
      expect(result.action).toBe("select")
      if (result.action === "select") {
        expect(result.session.id).toBe("sess-2")
      }
    })

    it("handles trailing slash differences in project paths", () => {
      const sessions: AgentSessionInfo[] = [
        {
          id: "sess-1",
          path: "/path/1",
          cwd: "/home/user/project-a/",
          name: "Session 1",
          created: "2026-01-01T00:00:00.000Z",
          modified: "2026-01-01T10:00:00.000Z",
          messageCount: 1,
          firstMessage: "Hello",
        },
      ]

      const result = pickProjectSession(sessions, "/home/user/project-a")
      expect(result.action).toBe("select")
      if (result.action === "select") {
        expect(result.session.id).toBe("sess-1")
      }
    })
  })

  describe("isProjectSelected", () => {
    it("returns false when both session and cwd are empty/null", () => {
      expect(isProjectSelected(null, null)).toBe(false)
      expect(isProjectSelected("", "")).toBe(false)
      expect(isProjectSelected(null, "~")).toBe(false)
    })

    it("returns true when currentSessionId is present", () => {
      expect(isProjectSelected("sess-123", null)).toBe(true)
    })

    it("returns true when projectCwd is set to a real folder", () => {
      expect(isProjectSelected(null, "/home/user/my-project")).toBe(true)
    })
  })
})
