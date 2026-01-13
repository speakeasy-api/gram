'use client'

import { useDensity } from '@/hooks/useDensity'
import { useRadius } from '@/hooks/useRadius'
import { cn } from '@/lib/utils'
import { useAssistantState } from '@assistant-ui/react'
import { SyntaxHighlighterProps } from '@assistant-ui/react-markdown'
import { AlertCircleIcon } from 'lucide-react'
import { FC, useEffect, useMemo, useRef, useState } from 'react'
import { parse, View, Warn } from 'vega'

export const ChartRenderer: FC<SyntaxHighlighterProps> = ({ code }) => {
  const message = useAssistantState(({ message }) => message)
  const containerRef = useRef<HTMLDivElement>(null)
  const viewRef = useRef<View | null>(null)
  const [error, setError] = useState<string | null>(null)
  const messageIsComplete = message.status?.type === 'complete'
  const r = useRadius()
  const d = useDensity()

  // Parse and validate JSON in useMemo - only recomputes when code changes
  const parsedSpec = useMemo(() => {
    const trimmedCode = code.trim()
    if (!trimmedCode) return null

    try {
      return JSON.parse(trimmedCode) as Record<string, unknown>
    } catch {
      return null
    }
  }, [code])

  // Only render when we have valid JSON AND message is complete
  const shouldRender = messageIsComplete && parsedSpec !== null

  useEffect(() => {
    if (!containerRef.current || !shouldRender) {
      return
    }

    setError(null)

    const runChart = async () => {
      try {
        // Clean up any existing view
        if (viewRef.current) {
          viewRef.current.finalize()
          viewRef.current = null
        }

        const chart = parse(parsedSpec)
        const view = new View(chart, {
          container: containerRef.current ?? undefined,
          renderer: 'svg',
          hover: true,
          logLevel: Warn,
        })
        viewRef.current = view

        await view.runAsync()
      } catch (err) {
        console.error('Failed to render chart:', err)
        setError(err instanceof Error ? err.message : 'Failed to render chart')
      }
    }

    runChart()

    return () => {
      if (viewRef.current) {
        viewRef.current.finalize()
        viewRef.current = null
      }
    }
  }, [shouldRender, parsedSpec])

  return (
    <div
      className={cn(
        // the after:hidden is to prevent assistant-ui from showing its default code block loading indicator
        'relative flex min-h-[400px] w-fit max-w-full min-w-[400px] items-center justify-center border p-6 after:hidden',
        r('lg'),
        d('p-lg')
      )}
    >
      {!shouldRender && !error && (
        <div className="shimmer text-muted-foreground bg-background/80 absolute inset-0 z-10 flex items-center justify-center">
          Rendering chart...
        </div>
      )}

      {error && (
        <div className="bg-background absolute inset-0 z-10 flex items-center justify-center gap-2 text-rose-500">
          <AlertCircleIcon name="alert-circle" className="h-4 w-4" />
          {error}
        </div>
      )}

      <div ref={containerRef} className={!shouldRender ? 'hidden' : 'block'} />
    </div>
  )
}
