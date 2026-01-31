'use client'

import * as React from 'react'
import { cn } from '@/lib/utils'
import type { CommandBarToolCallEvent } from '@/types'
import { Loader, CheckCircle2, XCircle } from 'lucide-react'

interface CommandBarResultProps {
  toolCall: CommandBarToolCallEvent
  className?: string
}

const CommandBarResult = React.forwardRef<HTMLDivElement, CommandBarResultProps>(
  ({ toolCall, className, ...props }, ref) => {
    const isComplete = toolCall.result !== undefined
    const isError =
      typeof toolCall.result === 'string' &&
      toolCall.result.startsWith('Error:')

    return (
      <div
        ref={ref}
        data-slot="command-bar-result"
        className={cn(
          'flex items-center gap-2 py-1.5 text-xs',
          className
        )}
        {...props}
      >
        {isComplete ? (
          isError ? (
            <XCircle className="text-destructive size-3.5 shrink-0" />
          ) : (
            <CheckCircle2 className="text-emerald-500 size-3.5 shrink-0" />
          )
        ) : (
          <Loader className="text-muted-foreground size-3.5 shrink-0 animate-spin" />
        )}
        <span className="text-muted-foreground font-medium">
          {toolCall.toolName.replace(/_/g, ' ')}
        </span>
        {isComplete && (
          <span className="text-muted-foreground max-w-[200px] truncate">
            {typeof toolCall.result === 'string'
              ? toolCall.result
              : JSON.stringify(toolCall.result).slice(0, 100)}
          </span>
        )}
      </div>
    )
  }
)
CommandBarResult.displayName = 'CommandBarResult'

export { CommandBarResult }
