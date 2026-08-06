import type { AgentSessionInfo } from "../agent/rpc"
import { sameProjectCwd } from "./path"

export type ProjectSessionChoice =
  | { action: "select"; session: AgentSessionInfo }
  | { action: "none" }

/**
 * Determine whether selecting a project folder should switch to its most recent
 * existing session or simply select the folder without creating a new session.
 */
export function pickProjectSession(
  sessions: AgentSessionInfo[],
  projectDir: string,
): ProjectSessionChoice {
  const matching = sessions
    .filter((s) => sameProjectCwd(s.cwd, projectDir))
    .sort((a, b) => new Date(b.modified).getTime() - new Date(a.modified).getTime())

  if (matching.length > 0) {
    return { action: "select", session: matching[0] }
  }
  return { action: "none" }
}

/**
 * Determine whether a valid project is currently selected.
 */
export function isProjectSelected(
  currentSessionId: string | null,
  projectCwd: string | null,
): boolean {
  if (currentSessionId && currentSessionId.trim() !== "") return true
  if (projectCwd && projectCwd.trim() !== "" && projectCwd !== "~") return true
  return false
}
