import { cn } from '@/lib/utils'
import { useAssistantState } from '@assistant-ui/react'
import { useMemo, type FC, type PropsWithChildren } from 'react'
import { useElements } from '@/hooks/useElements'
import { humanizeToolName } from '@/lib/humanize'
import { ToolUIGroup } from '@/components/ui/tool-ui'

export const ToolGroup: FC<
  PropsWithChildren<{ startIndex: number; endIndex: number }>
> = ({ children, startIndex, endIndex }) => {
  // startIndex/endIndex are inclusive indices into message.parts.
  // assistant-ui only groups consecutive tool-call parts, so every part
  // in the range is a tool-call — the count is simply the range size.
  const toolCount = endIndex - startIndex + 1

  const firstToolName = useAssistantState(({ message }) => {
    const part = message.parts[startIndex]
    return part?.type === 'tool-call' ? part.toolName : undefined
  })
  const anyMessagePartsAreRunning = useAssistantState(({ message }) => {
    for (let i = startIndex; i <= endIndex; i++) {
      if (message.parts[i]?.status?.type === 'running') return true
    }
    return false
  })

  const { config } = useElements()
  const defaultExpanded = config.tools?.expandToolGroupsByDefault ?? false

  const groupTitle = useMemo(() => {
    if (toolCount === 0) return 'No tools called'
    if (toolCount === 1) {
      return firstToolName
        ? `Calling ${humanizeToolName(firstToolName)}...`
        : 'Calling tool...'
    }
    return anyMessagePartsAreRunning
      ? `Calling ${toolCount} tools...`
      : `Executed ${toolCount} tools`
  }, [toolCount, firstToolName, anyMessagePartsAreRunning])

  // If there's a custom component for the single tool, render children directly
  if (firstToolName && config.tools?.components?.[firstToolName]) {
    return children
  }

  // For single tool calls, render without the group wrapper
  if (toolCount === 1) {
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
