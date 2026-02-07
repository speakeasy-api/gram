'use client'

import * as React from 'react'
import { useCallback, useRef, useState } from 'react'
import { ArrowLeftIcon, Loader2Icon, SendIcon } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useCommandBarAI } from '@/hooks/useCommandBarAI'
import { CommandBarResult } from './command-bar-result'
import type { CommandBarToolMeta, CommandBarToolCallEvent } from '@/types'

interface SchemaProperty {
  type?: string
  description?: string
  enum?: unknown[]
  properties?: Record<string, SchemaProperty>
  required?: string[]
}

interface CommandBarToolPromptProps {
  toolMeta: CommandBarToolMeta
  onBack: () => void
  onToolCall?: (event: CommandBarToolCallEvent) => void
  className?: string
}

function humanizeFieldName(name: string): string {
  return name
    .replace(/_/g, ' ')
    .replace(/([a-z])([A-Z])/g, '$1 $2')
    .replace(/\b\w/g, (c) => c.toUpperCase())
}

/**
 * Unwraps the AI SDK `StandardSchemaV1` wrapper if present.
 */
function unwrapSchema(
  parameters: Record<string, unknown>
): Record<string, unknown> {
  let schema = parameters
  if (
    'jsonSchema' in schema &&
    typeof schema.jsonSchema === 'object' &&
    schema.jsonSchema !== null
  ) {
    schema = schema.jsonSchema as Record<string, unknown>
  }
  return schema
}

/**
 * Extracts a summary of parameters from the schema for display.
 */
function getParameterSummary(parameters: Record<string, unknown>): Array<{
  name: string
  description?: string
  required: boolean
  type?: string
}> {
  const schema = unwrapSchema(parameters)
  const props = (schema.properties ?? {}) as Record<string, SchemaProperty>
  const requiredKeys = (schema.required ?? []) as string[]

  return Object.entries(props).map(([name, prop]) => ({
    name,
    description: prop.description,
    required: requiredKeys.includes(name),
    type: prop.type,
  }))
}

/**
 * Builds a system prompt that instructs the AI to use a specific tool.
 */
function buildToolPrompt(toolMeta: CommandBarToolMeta): string {
  const params = getParameterSummary(toolMeta.parameters)
  const paramDescriptions = params
    .map((p) => {
      const req = p.required ? '(required)' : '(optional)'
      const desc = p.description ? `: ${p.description}` : ''
      return `- ${p.name} ${req}${desc}`
    })
    .join('\n')

  return `You are helping the user execute the "${toolMeta.toolName}" tool.

Tool description: ${toolMeta.description ?? 'No description available.'}

Available parameters:
${paramDescriptions || 'This tool has no parameters.'}

Based on the user's request, determine the appropriate parameter values and execute the tool. Be concise in your response.`
}

export function CommandBarToolPrompt({
  toolMeta,
  onBack,
  onToolCall,
  className,
}: CommandBarToolPromptProps) {
  const [input, setInput] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)
  const params = getParameterSummary(toolMeta.parameters)

  const { submit, text, isStreaming, error, toolCalls, reset } =
    useCommandBarAI({ onToolCall })

  // Focus input on mount
  React.useEffect(() => {
    const t = setTimeout(() => inputRef.current?.focus(), 50)
    return () => clearTimeout(t)
  }, [])

  // Reset AI state when tool changes
  React.useEffect(() => {
    reset()
    setInput('')
  }, [toolMeta.toolName, reset])

  const handleSubmit = useCallback(
    (e: React.FormEvent) => {
      e.preventDefault()
      if (!input.trim() || isStreaming) return

      // Build the prompt with tool context
      const systemPrompt = buildToolPrompt(toolMeta)
      const fullQuery = `${systemPrompt}\n\nUser request: ${input}`
      submit(fullQuery)
    },
    [input, isStreaming, toolMeta, submit]
  )

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      e.stopPropagation()
      if (e.key === 'Escape') {
        e.preventDefault()
        onBack()
      }
    },
    [onBack]
  )

  const isLoading = isStreaming && !text && toolCalls.length === 0
  const hasResult = text || toolCalls.length > 0 || error

  return (
    <div
      data-slot="command-bar-tool-prompt"
      className={cn('flex flex-col', className)}
      onKeyDown={handleKeyDown}
    >
      {/* Header */}
      <div className="flex items-center gap-2 border-b px-3 py-2.5">
        <button
          type="button"
          onClick={onBack}
          className="text-muted-foreground hover:text-foreground -ml-0.5 rounded p-0.5 transition-colors"
          aria-label="Back to command list"
        >
          <ArrowLeftIcon className="size-4" />
        </button>
        <div className="min-w-0 flex-1">
          <div className="text-foreground text-sm font-medium">
            {humanizeFieldName(toolMeta.toolName)}
          </div>
          {toolMeta.description && (
            <div className="text-muted-foreground truncate text-xs">
              {toolMeta.description}
            </div>
          )}
        </div>
      </div>

      {/* Parameters summary */}
      {params.length > 0 && !hasResult && (
        <div className="border-b px-3 py-2">
          <div className="text-muted-foreground mb-1.5 text-[10px] font-medium tracking-wider uppercase">
            Available Parameters
          </div>
          <div className="flex flex-wrap gap-1.5">
            {params.map((p) => (
              <span
                key={p.name}
                className={cn(
                  'rounded px-1.5 py-0.5 text-[11px]',
                  p.required
                    ? 'bg-primary/10 text-primary'
                    : 'bg-muted text-muted-foreground'
                )}
                title={p.description}
              >
                {humanizeFieldName(p.name)}
                {p.required && '*'}
              </span>
            ))}
          </div>
        </div>
      )}

      {/* Result area */}
      {hasResult && (
        <div className="max-h-[200px] overflow-y-auto px-3 py-2">
          {isLoading && (
            <div className="flex items-center gap-2 py-2">
              <Loader2Icon className="text-muted-foreground size-4 shrink-0 animate-spin" />
              <span className="text-muted-foreground text-sm">Thinking...</span>
            </div>
          )}

          {toolCalls.length > 0 && (
            <div className="space-y-1">
              {toolCalls.map((tc, i) => (
                <CommandBarResult key={i} toolCall={tc} />
              ))}
            </div>
          )}

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

          {error && (
            <div className="flex items-center gap-2 py-2">
              <span className="text-destructive text-sm">{error}</span>
            </div>
          )}
        </div>
      )}

      {/* Input */}
      <form onSubmit={handleSubmit} className="border-t px-3 py-2">
        <div className="flex items-center gap-2">
          <input
            ref={inputRef}
            type="text"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder={`Describe what you want to do...`}
            disabled={isStreaming}
            className={cn(
              'text-foreground placeholder:text-muted-foreground flex-1 bg-transparent text-sm outline-none',
              'disabled:opacity-50'
            )}
          />
          <button
            type="submit"
            disabled={!input.trim() || isStreaming}
            className={cn(
              'text-muted-foreground hover:text-foreground rounded p-1 transition-colors',
              'disabled:pointer-events-none disabled:opacity-50'
            )}
            aria-label="Submit"
          >
            {isStreaming ? (
              <Loader2Icon className="size-4 animate-spin" />
            ) : (
              <SendIcon className="size-4" />
            )}
          </button>
        </div>
      </form>
    </div>
  )
}
