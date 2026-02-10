'use client'

import * as React from 'react'
import { Button, buttonVariants } from './button'
import type { VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'
import { useToolExecution } from '@/contexts/ToolExecutionContext'

export interface ActionButtonProps
  extends
    Omit<React.ComponentProps<'button'>, 'onClick'>,
    VariantProps<typeof buttonVariants> {
  label: string
  /** Tool name to invoke when clicked (matches LLM prompt) */
  action: string
  args?: Record<string, unknown>
}

export function ActionButton({
  label,
  action,
  args,
  variant = 'default',
  size = 'default',
  className,
  disabled,
  ...props
}: ActionButtonProps) {
  const { executeTool, isToolAvailable } = useToolExecution()
  const [isLoading, setIsLoading] = React.useState(false)

  const toolAvailable = isToolAvailable(action)

  const [error, setError] = React.useState<string | null>(null)

  const handleClick = React.useCallback(async () => {
    if (!toolAvailable || isLoading) return

    setIsLoading(true)
    setError(null)
    try {
      const result = await executeTool(action, args ?? {})
      if (!result.success && result.error) {
        setError(result.error)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    } finally {
      setIsLoading(false)
    }
  }, [action, args, executeTool, isLoading, toolAvailable])

  return (
    <div className="inline-flex flex-col gap-1">
      <Button
        variant={error ? 'destructive' : variant}
        size={size}
        className={cn(className)}
        onClick={handleClick}
        disabled={disabled || isLoading || !toolAvailable}
        {...props}
      >
        {isLoading ? 'Loading...' : label}
      </Button>
      {error && <span className="text-destructive text-xs">{error}</span>}
    </div>
  )
}
