import type { ContentPart } from "./rpc"

export type StreamBlock =
  | { type: "thinking"; text: string }
  | { type: "text"; text: string }
  | { type: "tool"; id: string; part: ContentPart }

export function appendThinking(blocks: StreamBlock[], delta: string): StreamBlock[] {
  if (!delta) return blocks
  const next = [...blocks]
  const last = next[next.length - 1]
  if (last?.type === "thinking") {
    next[next.length - 1] = { type: "thinking", text: last.text + delta }
  } else {
    next.push({ type: "thinking", text: delta })
  }
  return next
}

export function appendText(blocks: StreamBlock[], delta: string): StreamBlock[] {
  if (!delta) return blocks
  const next = [...blocks]
  const last = next[next.length - 1]
  if (last?.type === "text") {
    next[next.length - 1] = { type: "text", text: last.text + delta }
  } else {
    next.push({ type: "text", text: delta })
  }
  return next
}

export function upsertTool(blocks: StreamBlock[], id: string, part: ContentPart): StreamBlock[] {
  const next = [...blocks]
  const idx = next.findIndex((b) => b.type === "tool" && b.id === id)
  if (idx >= 0) {
    next[idx] = { type: "tool", id, part }
  } else {
    next.push({ type: "tool", id, part })
  }
  return next
}

export function streamBlocksToContent(blocks: StreamBlock[]): ContentPart[] {
  return blocks.map((block) => {
    if (block.type === "thinking") return { type: "thinking", thinking: block.text }
    if (block.type === "text") return { type: "text", text: block.text }
    return block.part
  })
}
