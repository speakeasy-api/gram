'use client'

import * as React from 'react'
import * as TooltipPrimitive from '@radix-ui/react-tooltip'

import { cn } from '@/lib/utils'

function TooltipProvider({
  delayDuration = 0,
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Provider>) {
  return (
    <TooltipPrimitive.Provider
      data-slot="tooltip-provider"
      delayDuration={delayDuration}
      {...props}
    />
  )
}

function Tooltip({
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Root>) {
  return (
    <TooltipProvider>
      <TooltipPrimitive.Root data-slot="tooltip" {...props} />
    </TooltipProvider>
  )
}

function TooltipTrigger({
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Trigger>) {
  return <TooltipPrimitive.Trigger data-slot="tooltip-trigger" {...props} />
}

function TooltipContent({
  className,
  sideOffset = 0,
  children,
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Content>) {
  return (
    <TooltipPrimitive.Portal>
      <TooltipPrimitive.Content
        data-slot="tooltip-content"
        sideOffset={sideOffset}
        className={cn('gramel:bg-foreground gramel:text-background gramel:animate-in gramel:fade-in-0 gramel:zoom-in-95 gramel:data-[state=closed]:animate-out gramel:data-[state=closed]:fade-out-0 gramel:data-[state=closed]:zoom-out-95 gramel:data-[side=bottom]:slide-in-from-top-2 gramel:data-[side=left]:slide-in-from-right-2 gramel:data-[side=right]:slide-in-from-left-2 gramel:data-[side=top]:slide-in-from-bottom-2 gramel:z-20 gramel:w-fit gramel:origin-(--radix-tooltip-content-transform-origin) gramel:rounded-md gramel:px-3 gramel:py-1.5 gramel:text-xs gramel:text-balance',
          className
        )}
        {...props}
      >
        {children}
        <TooltipPrimitive.Arrow className="gramel:bg-foreground gramel:fill-foreground gramel:z-20 gramel:size-2.5 gramel:translate-y-[calc(-50%-2px)] gramel:rotate-45 gramel:rounded-[2px]" />
      </TooltipPrimitive.Content>
    </TooltipPrimitive.Portal>
  )
}

export { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider }
