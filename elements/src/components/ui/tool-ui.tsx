import * as React from 'react'
import { useState } from 'react'
import { cva } from 'class-variance-authority'
import {
  CheckIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  CopyIcon,
  LoaderIcon,
  AlertCircleIcon,
} from 'lucide-react'
import { cn } from '@/lib/utils'

/* -----------------------------------------------------------------------------
 * Status indicator styles
 * -------------------------------------------------------------------------- */

const statusVariants = cva(
  'flex size-5 items-center justify-center rounded-full',
  {
    variants: {
      status: {
        pending: 'border border-dashed border-muted-foreground/50',
        running: 'text-primary',
        complete: 'text-green-600 dark:text-green-500',
        error: 'text-destructive',
      },
    },
    defaultVariants: {
      status: 'pending',
    },
  }
)

/* -----------------------------------------------------------------------------
 * Types
 * -------------------------------------------------------------------------- */

type ToolStatus = 'pending' | 'running' | 'complete' | 'error'

interface ToolUIProps {
  /** Display name of the tool */
  name: string
  /** Optional icon to display (defaults to first letter of name) */
  icon?: React.ReactNode
  /** Provider/source name (e.g., "Notion", "GitHub") */
  provider?: string
  /** Current status of the tool execution */
  status?: ToolStatus
  /** Request/input data - can be string or object */
  request?: string | Record<string, unknown>
  /** Result/output data - can be string or object */
  result?: string | Record<string, unknown>
  /** Whether the tool card starts expanded */
  defaultExpanded?: boolean
  /** Additional class names */
  className?: string
}

interface ToolUISectionProps {
  /** Section title */
  title: string
  /** Content to display - string or object (will be JSON stringified) */
  content: string | Record<string, unknown>
  /** Whether section starts expanded */
  defaultExpanded?: boolean
}

/* -----------------------------------------------------------------------------
 * Helper Components
 * -------------------------------------------------------------------------- */

function StatusIndicator({ status }: { status: ToolStatus }) {
  return (
    <div className={cn(statusVariants({ status }))}>
      {status === 'pending' && null}
      {status === 'running' && <LoaderIcon className="size-4 animate-spin" />}
      {status === 'complete' && <CheckIcon className="size-4" />}
      {status === 'error' && <AlertCircleIcon className="size-4" />}
    </div>
  )
}

function CopyButton({ content }: { content: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = async (e: React.MouseEvent) => {
    e.stopPropagation()
    await navigator.clipboard.writeText(content)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <button
      onClick={handleCopy}
      className="text-muted-foreground hover:bg-accent hover:text-foreground rounded p-1 transition-colors"
      aria-label="Copy to clipboard"
    >
      {copied ? (
        <CheckIcon className="size-4" />
      ) : (
        <CopyIcon className="size-4" />
      )}
    </button>
  )
}

/* -----------------------------------------------------------------------------
 * ToolUISection - Expandable section for Request/Result
 * -------------------------------------------------------------------------- */

function ToolUISection({
  title,
  content,
  defaultExpanded = false,
}: ToolUISectionProps) {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded)
  const contentString =
    typeof content === 'string' ? content : JSON.stringify(content, null, 2)

  return (
    <div data-slot="tool-ui-section" className="border-border border-t">
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className="hover:bg-accent/50 flex w-full items-center justify-between px-4 py-2.5 text-left transition-colors"
      >
        <span className="text-muted-foreground text-sm">{title}</span>
        <div className="flex items-center gap-1">
          <CopyButton content={contentString} />
          <ChevronRightIcon
            className={cn(
              'text-muted-foreground size-4 transition-transform duration-200',
              isExpanded && 'rotate-90'
            )}
          />
        </div>
      </button>
      {isExpanded && (
        <div className="border-border bg-muted/30 border-t px-4 py-3">
          <pre className="text-foreground overflow-x-auto text-sm whitespace-pre-wrap">
            {contentString}
          </pre>
        </div>
      )}
    </div>
  )
}

/* -----------------------------------------------------------------------------
 * ToolUI - Main component
 * -------------------------------------------------------------------------- */

function ToolUI({
  name,
  icon,
  provider,
  status = 'complete',
  request,
  result,
  defaultExpanded = false,
  className,
}: ToolUIProps) {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded)
  const hasContent = request !== undefined || result !== undefined

  return (
    <div
      data-slot="tool-ui"
      className={cn(
        'border-border bg-card overflow-hidden rounded-lg border',
        className
      )}
    >
      {/* Header with provider */}
      {provider && (
        <div
          data-slot="tool-ui-provider"
          className="border-border flex items-center gap-2 border-b px-4 py-2.5"
        >
          {icon ? (
            <span className="flex size-5 items-center justify-center">
              {icon}
            </span>
          ) : (
            <span className="bg-muted flex size-5 items-center justify-center rounded text-xs font-medium">
              {provider.charAt(0).toUpperCase()}
            </span>
          )}
          <span className="text-sm font-medium">{provider}</span>
        </div>
      )}

      {/* Tool row */}
      <button
        onClick={() => hasContent && setIsExpanded(!isExpanded)}
        disabled={!hasContent}
        className={cn(
          'flex w-full items-center gap-3 px-4 py-3 text-left',
          hasContent && 'hover:bg-accent/50 cursor-pointer transition-colors'
        )}
      >
        <StatusIndicator status={status} />
        <span className="flex-1 text-sm">{name}</span>
        {hasContent && (
          <ChevronDownIcon
            className={cn(
              'text-muted-foreground size-4 transition-transform duration-200',
              isExpanded && 'rotate-180'
            )}
          />
        )}
      </button>

      {/* Expandable content */}
      {isExpanded && hasContent && (
        <div data-slot="tool-ui-content">
          {request !== undefined && (
            <ToolUISection title="Request" content={request} />
          )}
          {result !== undefined && (
            <ToolUISection title="Results" content={result} />
          )}
        </div>
      )}
    </div>
  )
}

/* -----------------------------------------------------------------------------
 * Exports
 * -------------------------------------------------------------------------- */

export { ToolUI, ToolUISection }
export type { ToolUIProps, ToolUISectionProps, ToolStatus }
