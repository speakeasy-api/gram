import * as React from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'

import { cn } from '@/lib/utils'

function Popover({
  ...props
}: React.ComponentProps<typeof PopoverPrimitive.Root>) {
  return <PopoverPrimitive.Root data-slot="popover" {...props} />
}

function PopoverTrigger({
  ...props
}: React.ComponentProps<typeof PopoverPrimitive.Trigger>) {
  return <PopoverPrimitive.Trigger data-slot="popover-trigger" {...props} />
}

function PopoverContent({
  className,
  align = 'center',
  sideOffset = 4,
  ...props
}: React.ComponentProps<typeof PopoverPrimitive.Content>) {
  return (
    <PopoverPrimitive.Portal>
      <PopoverPrimitive.Content
        data-slot="popover-content"
        align={align}
        sideOffset={sideOffset}
        className={cn('gramel:bg-popover gramel:text-popover-foreground gramel:data-[state=open]:animate-in gramel:data-[state=closed]:animate-out gramel:data-[state=closed]:fade-out-0 gramel:data-[state=open]:fade-in-0 gramel:data-[state=closed]:zoom-out-95 gramel:data-[state=open]:zoom-in-95 gramel:data-[side=bottom]:slide-in-from-top-2 gramel:data-[side=left]:slide-in-from-right-2 gramel:data-[side=right]:slide-in-from-left-2 gramel:data-[side=top]:slide-in-from-bottom-2 gramel:z-20 gramel:w-72 gramel:origin-(--radix-popover-content-transform-origin) gramel:rounded-md gramel:border gramel:p-4 gramel:shadow-md gramel:outline-hidden',
          className
        )}
        {...props}
      />
    </PopoverPrimitive.Portal>
  )
}

function PopoverAnchor({
  ...props
}: React.ComponentProps<typeof PopoverPrimitive.Anchor>) {
  return <PopoverPrimitive.Anchor data-slot="popover-anchor" {...props} />
}

export { Popover, PopoverTrigger, PopoverContent, PopoverAnchor }
