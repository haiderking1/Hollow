import ReactMarkdown from "react-markdown"
import remarkGfm from "remark-gfm"
import remarkBreaks from "remark-breaks"
import { cn } from "../lib/utils"

interface MarkdownContentProps {
  text: string
  className?: string
  streaming?: boolean
}

export function MarkdownContent({ text, className, streaming }: MarkdownContentProps) {
  return (
    <div className={cn("markdown-content text-[14px] leading-relaxed text-foreground/90", className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkBreaks]}
        components={{
          p: ({ children }) => <p className="mb-3 last:mb-0">{children}</p>,
          strong: ({ children }) => <strong className="font-semibold text-foreground">{children}</strong>,
          em: ({ children }) => <em className="italic text-foreground/80">{children}</em>,
          a: ({ href, children }) => (
            <a
              href={href}
              target="_blank"
              rel="noopener noreferrer"
              className="text-accent underline decoration-accent/40 underline-offset-2 hover:decoration-accent"
            >
              {children}
            </a>
          ),
          ul: ({ children }) => <ul className="mb-3 list-disc space-y-1 pl-5 last:mb-0">{children}</ul>,
          ol: ({ children }) => <ol className="mb-3 list-decimal space-y-1 pl-5 last:mb-0">{children}</ol>,
          li: ({ children }) => <li className="pl-0.5">{children}</li>,
          blockquote: ({ children }) => (
            <blockquote className="mb-3 border-l-2 border-border-strong pl-3 text-muted-foreground last:mb-0">
              {children}
            </blockquote>
          ),
          hr: () => <hr className="my-4 border-border" />,
          h1: ({ children }) => <h1 className="mb-2 mt-4 text-xl font-semibold text-foreground first:mt-0">{children}</h1>,
          h2: ({ children }) => <h2 className="mb-2 mt-4 text-lg font-semibold text-foreground first:mt-0">{children}</h2>,
          h3: ({ children }) => <h3 className="mb-2 mt-3 text-base font-semibold text-foreground first:mt-0">{children}</h3>,
          h4: ({ children }) => <h4 className="mb-2 mt-3 text-sm font-semibold text-foreground first:mt-0">{children}</h4>,
          pre: ({ children }) => (
            <pre className="mb-3 overflow-x-auto rounded-lg border border-border bg-surface px-3 py-2.5 last:mb-0">
              {children}
            </pre>
          ),
          code: ({ className: codeClass, children }) => {
            const isBlock = Boolean(codeClass?.includes("language-"))
            if (isBlock) {
              return (
                <code className={cn("block font-mono text-[13px] leading-relaxed text-foreground/90", codeClass)}>
                  {children}
                </code>
              )
            }
            return (
              <code className="rounded bg-surface px-1.5 py-0.5 font-mono text-[13px] text-foreground/90">
                {children}
              </code>
            )
          },
          table: ({ children }) => (
            <div className="mb-3 overflow-x-auto last:mb-0">
              <table className="w-full border-collapse text-[13px]">{children}</table>
            </div>
          ),
          thead: ({ children }) => <thead className="border-b border-border-strong">{children}</thead>,
          th: ({ children }) => (
            <th className="px-3 py-2 text-left font-semibold text-foreground">{children}</th>
          ),
          td: ({ children }) => <td className="border-t border-border px-3 py-2 text-foreground/85">{children}</td>,
        }}
      >
        {text}
      </ReactMarkdown>
      {streaming && (
        <span className="ml-0.5 inline-block h-[1.05em] w-[2px] translate-y-[2px] bg-accent align-text-bottom animate-caret" />
      )}
    </div>
  )
}
