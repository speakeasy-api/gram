'use client'

import * as React from 'react'
import { Button, buttonVariants } from './button'
import type { VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

export interface ActionButtonProps
  extends
    Omit<React.ComponentProps<'button'>, 'onClick'>,
    VariantProps<typeof buttonVariants> {
  label: string
  toolName: string
  args?: Record<string, unknown>
}

export function ActionButton({
  label,
  toolName,
  args,
  variant = 'default',
  size = 'default',
  className,
  ...props
}: ActionButtonProps) {
  const handleClick = React.useCallback(() => {
    // Dispatch a custom event that the chat system can listen to
    const event = new CustomEvent('generative-ui:action', {
      bubbles: true,
      detail: { toolName, args },
    })
    document.dispatchEvent(event)
  }, [toolName, args])

  return (
    <Button
      variant={variant}
      size={size}
      className={cn(className)}
      onClick={handleClick}
      {...props}
    >
      {label}
    </Button>
  )
}
