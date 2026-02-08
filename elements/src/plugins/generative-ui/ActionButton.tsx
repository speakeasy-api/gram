'use client'

import { useToolExecution } from '@/contexts/ToolExecutionContext'
import { useRadius } from '@/hooks/useRadius'
import { cn } from '@/lib/utils'
import { useThreadRuntime } from '@assistant-ui/react'
import { CheckCircleIcon, Loader2Icon, XCircleIcon } from 'lucide-react'
import type { FC } from 'react'
import { useCallback, useState } from 'react'

export interface ActionButtonProps {
  /** Button label text */
  label: string
  /** Tool action name to execute */
  action: string
  /** Arguments to pass to the tool */
  args?: Record<string, unknown>
  /** Button style variant */
  variant?: 'default' | 'secondary' | 'outline' | 'destructive'
  /** Additional class names */
  className?: string
}

const variantClasses: Record<string, string> = {
  default: 'bg-primary text-primary-foreground hover:bg-primary/90',
  secondary: 'bg-secondary text-secondary-foreground hover:bg-secondary/80',
  outline:
    'border border-input bg-background hover:bg-accent hover:text-accent-foreground',
  destructive:
    'bg-destructive text-destructive-foreground hover:bg-destructive/90',
}

/**
 * ActionButton component - Interactive button triggering tool calls.
 * Use for executing actions from generative UI.
 */
export const ActionButton: FC<ActionButtonProps> = ({
  label,
  action,
  args,
  variant = 'default',
  className,
}) => {
  const r = useRadius()
  const { executeTool, isToolAvailable } = useToolExecution()
  const runtime = useThreadRuntime({ optional: true })
  const [isLoading, setIsLoading] = useState(false)
  const [result, setResult] = useState<{
    success: boolean
    message?: string
  } | null>(null)

  const toolAvailable = action ? isToolAvailable(action) : false

  const handleClick = useCallback(async () => {
    if (!action) return

    setIsLoading(true)
    setResult(null)

    try {
      const toolResult = await executeTool(action, args ?? {})

      if (toolResult.success) {
        // Format the result message
        let message = 'Done'
        if (toolResult.result) {
          if (typeof toolResult.result === 'string') {
            message = toolResult.result
          } else if (
            typeof toolResult.result === 'object' &&
            toolResult.result !== null &&
            'content' in toolResult.result
          ) {
            // Handle MCP tool result format
            const content = (
              toolResult.result as { content: Array<{ text?: string }> }
            ).content
            if (Array.isArray(content) && content[0]?.text) {
              message = content[0].text
            }
          }
        }
        setResult({ success: true, message })

        // Notify the LLM of the action result so it can respond
        if (runtime) {
          await runtime.append({
            role: 'user',
            content: [
              {
                type: 'text',
                text: `[Action completed] ${action}: ${message}`,
              },
            ],
          })
        }
      } else {
        setResult({ success: false, message: toolResult.error })

        // Also notify on failure so LLM can help
        if (runtime) {
          await runtime.append({
            role: 'user',
            content: [
              {
                type: 'text',
                text: `[Action failed] ${action}: ${toolResult.error}`,
              },
            ],
          })
        }
      }
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : 'Unknown error'
      setResult({
        success: false,
        message: errorMessage,
      })
    } finally {
      setIsLoading(false)
    }
  }, [action, args, executeTool, runtime])

  // Show result state if we have one
  if (result) {
    return (
      <div
        className={cn(
          'inline-flex items-center gap-2 px-4 py-2 text-sm',
          r('md'),
          result.success
            ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-100'
            : 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-100',
          className
        )}
      >
        {result.success ? (
          <CheckCircleIcon className="size-4" />
        ) : (
          <XCircleIcon className="size-4" />
        )}
        <span className="max-w-[200px] truncate">
          {result.message ?? (result.success ? 'Success' : 'Failed')}
        </span>
      </div>
    )
  }

  return (
    <button
      onClick={handleClick}
      disabled={isLoading || !toolAvailable}
      title={!toolAvailable ? `Tool "${action}" not available` : undefined}
      className={cn(
        'inline-flex items-center justify-center gap-2 px-4 py-2 text-sm font-medium transition-colors',
        'disabled:pointer-events-none disabled:opacity-50',
        r('md'),
        variantClasses[variant] ?? variantClasses.default,
        className
      )}
    >
      {isLoading && <Loader2Icon className="size-4 animate-spin" />}
      {label}
    </button>
  )
}
