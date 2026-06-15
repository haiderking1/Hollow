import { memo, useCallback, useEffect, useLayoutEffect, useRef } from "react"
import { capitalizeProseStart } from "../lib/text"
import type { Message } from "../types"
import { MarkdownContent } from "./markdown-content"
import { ToolBlock } from "./tool-block"
import { ThinkingBlock } from "./thinking-block"
import { TodoBlock } from "./todo-block"

const BOTTOM_THRESHOLD_PX = 80

interface ChatViewProps {
  messages: Message[]
  sessionId: string | null
  isStreaming: boolean
}

export const ChatView = memo(function ChatView({ messages, sessionId, isStreaming }: ChatViewProps) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const contentRef = useRef<HTMLDivElement>(null)
  const pinnedToBottomRef = useRef(true)
  const ignoreScrollRef = useRef(false)

  const isNearBottom = useCallback(() => {
    const el = scrollRef.current
    if (!el) return true
    return el.scrollHeight - el.scrollTop - el.clientHeight <= BOTTOM_THRESHOLD_PX
  }, [])

  const scrollToBottom = useCallback(() => {
    const el = scrollRef.current
    if (!el) return
    ignoreScrollRef.current = true
    el.scrollTop = el.scrollHeight
    requestAnimationFrame(() => {
      ignoreScrollRef.current = false
      pinnedToBottomRef.current = isNearBottom()
    })
  }, [isNearBottom])

  const handleScroll = useCallback(() => {
    if (ignoreScrollRef.current) return
    pinnedToBottomRef.current = isNearBottom()
  }, [isNearBottom])

  // New session — land at latest messages.
  useLayoutEffect(() => {
    pinnedToBottomRef.current = true
    scrollToBottom()
  }, [sessionId, scrollToBottom])

  // New tokens / messages — follow only while pinned; sending always pins.
  useLayoutEffect(() => {
    const last = messages[messages.length - 1]
    if (last?.role === "user") {
      pinnedToBottomRef.current = true
    }
    if (pinnedToBottomRef.current) {
      scrollToBottom()
    }
  }, [messages, isStreaming, scrollToBottom])

  // Markdown / tools grow after paint — keep up while pinned.
  useEffect(() => {
    const content = contentRef.current
    if (!content) return

    const ro = new ResizeObserver(() => {
      if (pinnedToBottomRef.current) {
        scrollToBottom()
      }
    })
    ro.observe(content)
    return () => ro.disconnect()
  }, [sessionId, scrollToBottom])

  return (
    <div ref={scrollRef} onScroll={handleScroll} className="min-h-0 flex-1 overflow-y-auto">
      <div ref={contentRef} className="w-full px-6 pt-6 pb-36">
        <div className="space-y-6">
          {messages.map((m) => (
            <MessageRow key={m.id} message={m} />
          ))}
        </div>
      </div>
    </div>
  )
})

const MessageRow = memo(function MessageRow({ message }: { message: Message }) {
  if (message.role === "user") {
    return (
      <div className="flex justify-end">
        <div className="max-w-[85%] rounded-2xl rounded-br-md bg-elevated px-4 py-2.5">
          <MarkdownContent id={message.id} text={message.text} className="text-foreground" />
        </div>
      </div>
    )
  }

  const blocks = message.blocks
  if (blocks.length === 0) return null

  const renderBlock = (block: (typeof blocks)[number], i: number) => {
    const isLast = i === blocks.length - 1
    switch (block.type) {
      case "text":
        return (
          <MarkdownContent
            id={`${message.id}-text-${i}`}
            text={capitalizeProseStart(block.text)}
            streaming={message.streaming && isLast}
          />
        )
      case "thinking":
        return (
          <ThinkingBlock
            id={`${message.id}-thinking-${i}`}
            text={block.text}
            streaming={message.streaming && isLast}
          />
        )
      case "tool":
        return <ToolBlock block={block} />
      case "todo":
        return <TodoBlock items={block.items} />
      default:
        return null
    }
  }

  const first = blocks[0]

  return (
    <div className="space-y-2">
      <div className="flex gap-3">
        <div className="flex h-[1.625em] w-2 shrink-0 items-center text-[14px]">
          <div className="size-2 rounded-full bg-white" />
        </div>
        <div className="min-w-0 flex-1">{renderBlock(first, 0)}</div>
      </div>
      {blocks.slice(1).map((block, i) => (
        <div key={i + 1} className="pl-5">
          {renderBlock(block, i + 1)}
        </div>
      ))}
    </div>
  )
})
