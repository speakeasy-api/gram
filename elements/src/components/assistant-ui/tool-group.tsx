import { cn } from '@/lib/utils'
import { useAssistantState } from '@assistant-ui/react'
import { useMemo, type FC, type PropsWithChildren } from 'react'
import { useElements } from '@/hooks/useElements'
import { humanizeToolName } from '@/lib/humanize'
import { ToolUIGroup } from '@/components/ui/tool-ui'

export const ToolGroup: FC<
  PropsWithChildren<{ startIndex: number; endIndex: number }>
> = ({ children }) => {
  const parts = useAssistantState(({ message }) => message).parts
  const toolCallParts = parts.filter((part) => part.type === 'tool-call')
  const anyMessagePartsAreRunning = toolCallParts.some(
    (part) => part.status?.type === 'running'
  )

  const { config } = useElements()
  const defaultExpanded = config.tools?.expandToolGroupsByDefault ?? false

  const groupTitle = useMemo(() => {
    const toolParts = parts.filter((part) => part.type === 'tool-call')

    if (toolParts.length === 0) return 'No tools called'
    if (toolParts.length === 1)
      return `Calling ${humanizeToolName(toolParts[0].toolName)}...`
    return anyMessagePartsAreRunning
      ? `Calling ${toolParts.length} tools...`
      : `Executed ${toolParts.length} tools`
  }, [parts, anyMessagePartsAreRunning])

  // If there's a custom component for the single tool, render children directly
  if (config.tools?.components?.[toolCallParts[0]?.toolName]) {
    return children
  }

  // For single tool calls, render without the group wrapper
  if (toolCallParts.length === 1) {
    return (
      <div className={cn('my-4 w-full max-w-xl')}>
        <div className="border-border bg-card overflow-hidden rounded-lg border">
          {children}
        </div>
      </div>
    )
  }

  // For multiple tool calls, use the group component
  return (
    <div className="my-4 w-full max-w-xl">
      <ToolUIGroup
        title={groupTitle}
        status={anyMessagePartsAreRunning ? 'running' : 'complete'}
        defaultExpanded={defaultExpanded}
      >
        {children}
      </ToolUIGroup>
    </div>
  )
}
