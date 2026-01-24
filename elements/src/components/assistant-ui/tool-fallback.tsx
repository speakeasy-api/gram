import { cn } from '@/lib/utils'
import {
  useAssistantState,
  type ToolCallMessagePartComponent,
} from '@assistant-ui/react'
import { useToolApproval } from '@/hooks/useToolApproval'
import { useElements } from '@/hooks/useElements'
import {
  ToolUI,
  type ToolStatus,
  type ContentItem,
} from '@/components/ui/tool-ui'
import { GenerativeUI } from '@/components/ui/generative-ui'
import { isJsonRenderTree } from '@/lib/generative-ui'

export const ToolFallback: ToolCallMessagePartComponent = ({
  toolName,
  toolCallId,
  status,
  result,
  args,
}) => {
  const { config } = useElements()
  const {
    pendingApprovals,
    whitelistTool,
    confirmPendingApproval,
    rejectPendingApproval,
  } = useToolApproval()

  // Check if this specific tool call has a pending approval
  const pendingApproval = pendingApprovals.get(toolCallId)
  const message = useAssistantState(({ message }) => message)
  const toolParts = message.parts.filter((part) => part.type === 'tool-call')
  const matchingMessagePartIndex = toolParts.findIndex(
    (part) => part.toolName === toolName
  )

  const handleApproveOnce = () => {
    confirmPendingApproval(toolCallId)
  }

  const handleApproveForSession = () => {
    whitelistTool(toolName)
    confirmPendingApproval(toolCallId)
  }

  const handleDeny = () => {
    rejectPendingApproval(toolCallId)
  }

  // Map assistant-ui status to ToolUI status
  const getToolStatus = (): ToolStatus => {
    if (pendingApproval) return 'approval'
    if (status.type === 'incomplete') return 'error'
    if (status.type === 'complete') {
      // Check if the result indicates an error (e.g., tool was denied)
      if (
        result &&
        typeof result === 'object' &&
        'isError' in result &&
        result.isError
      ) {
        return 'error'
      }
      return 'complete'
    }
    return 'running'
  }

  // Parse result to structured content if possible
  const getResult = ():
    | string
    | Record<string, unknown>
    | { content: ContentItem[] }
    | undefined => {
    if (result === undefined) return undefined
    // Check if it's structured content with a content array
    if (
      typeof result === 'object' &&
      result !== null &&
      'content' in result &&
      Array.isArray((result as { content: unknown }).content)
    ) {
      return result as { content: ContentItem[] }
    }
    // Otherwise return as-is (string or object)
    if (typeof result === 'string') return result
    return result as Record<string, unknown>
  }

  // Check if generativeUI is enabled and result is a json-render tree
  const useGenerativeUI =
    config.tools?.generativeUI?.enabled &&
    status.type === 'complete' &&
    isJsonRenderTree(result)

  // When generativeUI is enabled and result is a json-render tree,
  // render the result directly using GenerativeUI instead of ToolUI
  if (useGenerativeUI) {
    return (
      <div
        className={cn(
          'aui-tool-fallback-root flex w-full flex-col',
          matchingMessagePartIndex !== -1 &&
            matchingMessagePartIndex !== toolParts.length - 1 &&
            'border-b'
        )}
      >
        <GenerativeUI content={result} />
      </div>
    )
  }

  return (
    <div
      className={cn(
        'aui-tool-fallback-root flex w-full flex-col',
        matchingMessagePartIndex !== -1 &&
          matchingMessagePartIndex !== toolParts.length - 1 &&
          'border-b'
      )}
    >
      <ToolUI
        name={toolName}
        status={getToolStatus()}
        request={args as Record<string, unknown>}
        result={getResult()}
        onApproveOnce={pendingApproval ? handleApproveOnce : undefined}
        onApproveForSession={
          pendingApproval ? handleApproveForSession : undefined
        }
        onDeny={pendingApproval ? handleDeny : undefined}
        className="rounded-none border-0"
      />
    </div>
  )
}
