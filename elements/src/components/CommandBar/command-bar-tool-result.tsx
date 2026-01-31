'use client'

import * as React from 'react'
import { useState } from 'react'
import {
  CheckCircle2Icon,
  XCircleIcon,
  RotateCcwIcon,
  XIcon,
} from 'lucide-react'
import { cn } from '@/lib/utils'

interface CommandBarToolResultProps {
  toolName: string
  result?: unknown
  error?: string | null
  onClose: () => void
  onRetry: () => void
  className?: string
}

function humanizeFieldName(name: string): string {
  return name
    .replace(/_/g, ' ')
    .replace(/([a-z])([A-Z])/g, '$1 $2')
    .replace(/\b\w/g, (c) => c.toUpperCase())
}

function formatResult(result: unknown): string {
  if (result === undefined || result === null) return 'No output'
  if (typeof result === 'string') return result
  try {
    return JSON.stringify(result, null, 2)
  } catch {
    return String(result)
  }
}

const MAX_DISPLAY_LENGTH = 500

export function CommandBarToolResult({
  toolName,
  result,
  error,
  onClose,
  onRetry,
  className,
}: CommandBarToolResultProps) {
  const [expanded, setExpanded] = useState(false)
  const isError = !!error
  const formatted = isError ? error : formatResult(result)
  const isTruncated = formatted.length > MAX_DISPLAY_LENGTH && !expanded

  return (
    <div
      data-slot="command-bar-tool-result"
      className={cn('flex flex-col', className)}
      onKeyDown={(e) => {
        e.stopPropagation()
        if (e.key === 'Escape') {
          e.preventDefault()
          onClose()
        }
      }}
    >
      {/* Header */}
      <div className="flex items-center gap-2 border-b px-3 py-2.5">
        {isError ? (
          <XCircleIcon className="text-destructive size-4 shrink-0" />
        ) : (
          <CheckCircle2Icon className="size-4 shrink-0 text-green-500" />
        )}
        <div className="text-foreground min-w-0 flex-1 text-sm font-medium">
          {humanizeFieldName(toolName)}
        </div>
        <span
          className={cn(
            'rounded-full px-2 py-0.5 text-[10px] font-medium',
            isError
              ? 'bg-destructive/10 text-destructive'
              : 'bg-green-500/10 text-green-600'
          )}
        >
          {isError ? 'Error' : 'Success'}
        </span>
      </div>

      {/* Result body */}
      <div className="max-h-[250px] overflow-y-auto px-3 py-2">
        <pre
          className={cn(
            'bg-muted overflow-x-auto rounded-md p-2.5 font-mono text-xs leading-relaxed',
            isError ? 'text-destructive' : 'text-foreground'
          )}
        >
          {isTruncated ? formatted.slice(0, MAX_DISPLAY_LENGTH) + '…' : formatted}
        </pre>
        {formatted.length > MAX_DISPLAY_LENGTH && (
          <button
            type="button"
            onClick={() => setExpanded(!expanded)}
            className="text-primary mt-1 text-xs hover:underline"
          >
            {expanded ? 'Show less' : 'Show more'}
          </button>
        )}
      </div>

      {/* Actions */}
      <div className="flex items-center gap-2 border-t px-3 py-2">
        <button
          type="button"
          onClick={onRetry}
          className={cn(
            'flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium transition-colors',
            'bg-muted text-foreground hover:bg-muted/80'
          )}
        >
          <RotateCcwIcon className="size-3" />
          Run again
        </button>
        <button
          type="button"
          onClick={onClose}
          className={cn(
            'flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium transition-colors',
            'bg-primary text-primary-foreground hover:bg-primary/90'
          )}
        >
          <XIcon className="size-3" />
          Done
        </button>
      </div>
    </div>
  )
}
