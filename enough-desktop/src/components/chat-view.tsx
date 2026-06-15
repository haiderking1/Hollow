import { memo } from "react"
import type { Message } from "../types"
import { MarkdownContent } from "./markdown-content"
import { ToolBlock } from "./tool-block"
import { ThinkingBlock } from "./thinking-block"
import { TodoBlock } from "./todo-block"

export const ChatView = memo(function ChatView({ messages }: { messages: Message[] }) {
  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <div className="w-full px-6 pt-6 pb-36">
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

  return (
    <div className="flex gap-3">
      <div className="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-white" />
      <div className="min-w-0 flex-1 space-y-2">
        {message.blocks.map((block, i) => {
          const isLast = i === message.blocks.length - 1
          switch (block.type) {
            case "text":
              return (
                <MarkdownContent
                  key={i}
                  id={`${message.id}-text-${i}`}
                  text={block.text}
                  streaming={message.streaming && isLast && block.type === "text"}
                />
              )
            case "thinking":
              return (
                <ThinkingBlock
                  key={i}
                  id={`${message.id}-thinking-${i}`}
                  text={block.text}
                  streaming={message.streaming && isLast}
                />
              )
            case "tool":
              return <ToolBlock key={i} block={block} />
            case "todo":
              return <TodoBlock key={i} items={block.items} />
            default:
              return null
          }
        })}
      </div>
    </div>
  )
})
