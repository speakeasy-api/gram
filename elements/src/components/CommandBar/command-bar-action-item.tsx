'use client'

import * as React from 'react'
import { cn } from '@/lib/utils'
import type { CommandBarAction } from '@/types'
import { formatShortcut } from '@/hooks/useCommandBarShortcut'

interface CommandBarActionItemProps {
  action: CommandBarAction
}

const CommandBarActionItem = React.forwardRef<
  HTMLDivElement,
  CommandBarActionItemProps & React.HTMLAttributes<HTMLDivElement>
>(({ action, className, ...props }, ref) => {
  return (
    <div
      ref={ref}
      className={cn(
        'flex w-full items-center justify-between gap-2',
        action.disabled && 'opacity-50',
        className
      )}
      {...props}
    >
      <div className="flex min-w-0 items-center gap-3">
        {action.icon && (
          <span className="text-muted-foreground flex size-4 shrink-0 items-center justify-center">
            {action.icon}
          </span>
        )}
        <div className="flex min-w-0 flex-col">
          <span className="truncate text-sm">{action.label}</span>
          {action.description && (
            <span className="text-muted-foreground truncate text-xs">
              {action.description}
            </span>
          )}
        </div>
      </div>
      {action.shortcut && (
        <kbd className="bg-muted text-muted-foreground pointer-events-none ml-auto shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium">
          {formatShortcut(action.shortcut)}
        </kbd>
      )}
    </div>
  )
})
CommandBarActionItem.displayName = 'CommandBarActionItem'

export { CommandBarActionItem }
