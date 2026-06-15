import type { AgentModel } from "../agent/rpc"

/** User-facing label for a wire thinking level (mirrors backend FormatThinkingLevelForModel). */
export function formatThinkingLevelForModel(modelId: string, level: string): string {
  const id = modelId.toLowerCase()
  if (id.includes("minimax-m3")) {
    if (!level || level === "off") return "none"
    return "thinking"
  }
  if (!level || level === "off") return "off"
  return level
}

/** Badge shown in the model list (mirrors backend FormatThinkingBadge). */
export function formatThinkingBadge(model: AgentModel, level: string): string {
  const levels = model.thinkingLevels ?? []
  if (levels.length <= 1) {
    if (model.reasoning) return "reasoning"
    return ""
  }
  return formatThinkingLevelForModel(model.id, level)
}

export function thinkingLevelLabel(model: AgentModel | undefined, level: string, index: number): string {
  const labels = model?.thinkingLevelLabels
  if (labels && labels[index] !== undefined) return labels[index]
  return formatThinkingLevelForModel(model?.id ?? "", level)
}

export function thinkingLevelHint(modelId: string, level: string): string {
  const id = modelId.toLowerCase()
  if (id.includes("minimax-m3")) {
    if (!level || level === "off") return "Fastest responses"
    return "Adaptive reasoning"
  }
  const hints: Record<string, string> = {
    off: "Fastest responses",
    minimal: "Light reasoning",
    low: "Light reasoning",
    medium: "Balanced depth",
    high: "Deeper analysis",
    xhigh: "Maximum depth",
    max: "Maximum depth",
  }
  return hints[level] ?? "Reasoning depth"
}
