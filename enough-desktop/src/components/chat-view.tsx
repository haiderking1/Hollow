import type { Message } from "../types"
import { ToolBlock } from "./tool-block"
import { ThinkingBlock } from "./thinking-block"
import { TodoBlock } from "./todo-block"

export function ChatView({ messages }: { messages: Message[] }) {
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
}

function MessageRow({ message }: { message: Message }) {
  if (message.role === "user") {
    return (
      <div className="flex justify-end">
        <div className="max-w-[85%] rounded-2xl rounded-br-md bg-elevated px-4 py-2.5 text-[14px] leading-relaxed text-foreground">
          {message.text}
        </div>
      </div>
    )
  }

  return (
    <div className="flex gap-3">
      <div className="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-white" />
      <div className="min-w-0 flex-1 space-y-3">
        {message.blocks.map((block, i) => {
          switch (block.type) {
            case "text":
              return (
                <p key={i} className="text-[14px] leading-relaxed text-foreground/90">
                  {block.text}
                  {message.streaming && i === message.blocks.length - 1 && (
                    <span className="ml-0.5 inline-block h-[1.05em] w-[2px] translate-y-[2px] bg-accent align-text-bottom animate-caret" />
                  )}
                </p>
              )
            case "thinking":
              return <ThinkingBlock key={i} text={block.text} />
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
}
