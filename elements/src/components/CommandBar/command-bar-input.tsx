'use client'

import * as React from 'react'
import { Command as CommandPrimitive } from 'cmdk'
import { SearchIcon } from 'lucide-react'
import { cn } from '@/lib/utils'

const CommandBarInput = React.forwardRef<
  React.ComponentRef<typeof CommandPrimitive.Input>,
  React.ComponentPropsWithoutRef<typeof CommandPrimitive.Input>
>(({ className, ...props }, ref) => {
  return (
    <div
      data-slot="command-bar-input-wrapper"
      className="flex items-center gap-2 border-b px-3"
    >
      <SearchIcon className="text-muted-foreground size-4 shrink-0" />
      <CommandPrimitive.Input
        ref={ref}
        data-slot="command-bar-input"
        className={cn(
          'placeholder:text-muted-foreground flex h-11 w-full bg-transparent py-3 text-sm outline-none disabled:cursor-not-allowed disabled:opacity-50',
          className
        )}
        {...props}
      />
    </div>
  )
})
CommandBarInput.displayName = 'CommandBarInput'

export { CommandBarInput }
