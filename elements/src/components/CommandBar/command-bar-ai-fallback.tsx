'use client'

import * as React from 'react'
import { useCallback, useState } from 'react'
import { SparklesIcon, Loader2Icon } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useCommandBarAI } from '@/hooks/useCommandBarAI'
import { CommandBarResult } from './command-bar-result'
import type { CommandBarToolCallEvent, CommandBarMessageEvent } from '@/types'

interface CommandBarAIFallbackProps {
  query: string
  onToolCall?: (event: CommandBarToolCallEvent) => void
  onMessage?: (event: CommandBarMessageEvent) => void
  submitRef?: React.RefObject<(() => void) | null>
  className?: string
}

export function CommandBarAIFallback({
  query,
  onToolCall,
  onMessage,
  submitRef,
  className,
}: CommandBarAIFallbackProps) {
  const [submitted, setSubmitted] = useState(false)
  const { submit, text, isStreaming, error, toolCalls, reset } =
    useCommandBarAI({
      onToolCall,
      onMessage,
    })

  const handleSubmit = useCallback(() => {
    if (!query.trim() || isStreaming) return
    setSubmitted(true)
    submit(query)
  }, [query, isStreaming, submit])

  // Expose submit to parent via ref so Enter key can trigger it
  React.useEffect(() => {
    if (submitRef) {
      submitRef.current = handleSubmit
    }
    return () => {
      if (submitRef) {
        submitRef.current = null
      }
    }
  }, [submitRef, handleSubmit])

  // Reset when query changes
  React.useEffect(() => {
    setSubmitted(false)
    reset()
  }, [query, reset])

  // Pre-submit hint
  if (!submitted && !text) {
    return (
      <div
        data-slot="command-bar-ai-hint"
        className={cn('flex items-center gap-2 px-3 py-4 text-sm', className)}
      >
        <SparklesIcon className="text-muted-foreground size-4 shrink-0" />
        <span className="text-muted-foreground">
          Press{' '}
          <kbd className="bg-muted rounded px-1 py-0.5 text-[10px] font-medium">
            Enter
          </kbd>{' '}
          to ask AI
        </span>
        <button
          type="button"
          onClick={handleSubmit}
          className="text-primary ml-auto text-xs font-medium hover:underline"
        >
          Ask
        </button>
      </div>
    )
  }

  const isLoading = isStreaming && !text && toolCalls.length === 0

  return (
    <div
      data-slot="command-bar-ai-result"
      className={cn('max-h-[300px] overflow-y-auto px-3 py-3', className)}
    >
      {/* Loading state before any content arrives */}
      {isLoading && (
        <div className="flex items-center gap-2 py-2">
          <Loader2Icon className="text-muted-foreground size-4 shrink-0 animate-spin" />
          <span className="text-muted-foreground text-sm">Thinking...</span>
        </div>
      )}

      {/* Tool call results */}
      {toolCalls.length > 0 && (
        <div className="space-y-1">
          {toolCalls.map((tc, i) => (
            <CommandBarResult key={i} toolCall={tc} />
          ))}
        </div>
      )}

      {/* Streamed text */}
      {text && (
        <div
          className={cn(
            'text-foreground text-sm leading-relaxed whitespace-pre-wrap',
            toolCalls.length > 0 && 'mt-2 border-t pt-2'
          )}
        >
          {text}
          {isStreaming && (
            <span className="bg-foreground ml-0.5 inline-block h-4 w-0.5 animate-pulse" />
          )}
        </div>
      )}

      {/* Error state */}
      {error && (
        <div className="flex items-center gap-2 py-2">
          <span className="text-destructive text-sm">{error}</span>
        </div>
      )}
    </div>
  )
}
