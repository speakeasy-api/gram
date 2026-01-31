'use client'

import * as React from 'react'
import { useMemo } from 'react'
import { Command as CommandPrimitive } from 'cmdk'
import { cn } from '@/lib/utils'
import { CommandBarActionItem } from './command-bar-action-item'
import type { CommandBarAction } from '@/types'

interface CommandBarListProps {
  actions: CommandBarAction[]
  onSelect: (action: CommandBarAction) => void
  maxVisible?: number
  className?: string
}

const CommandBarList = React.forwardRef<HTMLDivElement, CommandBarListProps>(
  ({ actions, onSelect, maxVisible = 8, className }, ref) => {
    const groupedActions = useMemo(() => {
      const groups = new Map<string, CommandBarAction[]>()
      for (const action of actions) {
        const group = action.group ?? 'Actions'
        const existing = groups.get(group) ?? []
        existing.push(action)
        groups.set(group, existing)
      }
      return groups
    }, [actions])

    const itemHeight = 40 // approximate px per item
    const maxHeight = maxVisible * itemHeight

    return (
      <CommandPrimitive.List
        ref={ref}
        data-slot="command-bar-list"
        className={cn('overflow-y-auto overflow-x-hidden', className)}
        style={{ maxHeight }}
      >
        {Array.from(groupedActions.entries()).map(
          ([groupName, groupActions]) => (
            <CommandPrimitive.Group
              key={groupName}
              heading={groupName}
              className="text-muted-foreground [&_[cmdk-group-heading]]:text-muted-foreground [&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:font-medium"
            >
              {groupActions.map((action) => (
                <CommandPrimitive.Item
                  key={action.id}
                  value={[
                    action.id,
                    action.label,
                    action.description,
                    ...(action.keywords ?? []),
                  ]
                    .filter(Boolean)
                    .join(' ')}
                  onSelect={() => onSelect(action)}
                  disabled={action.disabled}
                  className="data-[selected=true]:bg-accent data-[selected=true]:text-accent-foreground flex cursor-pointer select-none items-center rounded-sm px-3 py-2 text-sm outline-none data-[disabled=true]:pointer-events-none data-[disabled=true]:opacity-50"
                >
                  <CommandBarActionItem action={action} />
                </CommandPrimitive.Item>
              ))}
            </CommandPrimitive.Group>
          )
        )}
      </CommandPrimitive.List>
    )
  }
)
CommandBarList.displayName = 'CommandBarList'

export { CommandBarList }
