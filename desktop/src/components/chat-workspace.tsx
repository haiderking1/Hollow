import { memo } from "react"
import type { Message, RepoStatus } from "../types"
import type { ModelCatalog } from "../agent/rpc"
import { ChatView } from "./chat-view"
import { EmptyState } from "./empty-state"
import { ModelPicker } from "./model-picker"
import { PromptInput } from "./prompt-input"

interface ChatWorkspaceProps {
  loadingThread: boolean
  messages: Message[]
  sessionId: string | null
  currentCwd: string
  modelCatalog: ModelCatalog | null
  isStreaming: boolean
  syncingThread: boolean
  repoStatus: RepoStatus | null
  loopStatus: { active: boolean; iteration: number; maxIterations: number; task: string } | null
  onSend: (text: string) => void
  onAbort: () => void
  onSelectModel: (provider: string, modelId: string, thinkingLevel: string) => void
  onToggleModelEnabled?: (modelId: string) => void
  onRefreshCatalog?: () => void
  onOpenSettingsModels?: () => void
}

export const ChatWorkspace = memo(function ChatWorkspace({
  loadingThread,
  messages,
  sessionId,
  modelCatalog,
  isStreaming,
  syncingThread,
  repoStatus,
  loopStatus,
  onSend,
  onAbort,
  onSelectModel,
  onToggleModelEnabled,
  onRefreshCatalog,
  onOpenSettingsModels,
}: ChatWorkspaceProps) {
  const showEmpty = messages.length === 0
  const streaming = isStreaming || syncingThread

  const composer = (
    <PromptInput
      onSend={onSend}
      isStreaming={isStreaming}
      onAbort={onAbort}
      repoStatus={repoStatus}
      loopStatus={loopStatus}
      onOpenSettingsModels={onOpenSettingsModels}
      footer={
        <ModelPicker
          catalog={modelCatalog}
          disabled={isStreaming}
          onSelect={onSelectModel}
          onToggleEnabled={onToggleModelEnabled}
          onRefreshCatalog={onRefreshCatalog}
          onOpenSettingsModels={onOpenSettingsModels}
        />
      }
    />
  )

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      {loadingThread ? (
        <div className="flex min-h-0 flex-1 items-center justify-center">
          <span className="block h-5 w-5 rounded-full border-2 border-muted-foreground/30 border-t-foreground animate-spin [animation-duration:0.9s]" />
        </div>
      ) : showEmpty ? (
        <EmptyState composer={composer} />
      ) : (
        <>
          <ChatView messages={messages} sessionId={sessionId} isStreaming={streaming} />
          <div className="absolute bottom-0 left-0 right-0 pointer-events-none bg-gradient-to-t from-background via-background/95 to-transparent pt-10 pb-4">
            <div className="mx-auto w-full max-w-[720px] px-6 pointer-events-auto">{composer}</div>
          </div>
        </>
      )}
    </div>
  )
})
