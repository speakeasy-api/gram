'use client'

import { useDensity } from '@/hooks/useDensity'
import { useRadius } from '@/hooks/useRadius'
import { cn } from '@/lib/utils'
import { useAssistantState } from '@assistant-ui/react'
import { SyntaxHighlighterProps } from '@assistant-ui/react-markdown'
import { AlertCircleIcon } from 'lucide-react'
import { FC, useEffect, useRef, useState } from 'react'
import { parse, View, Warn } from 'vega'

export const ChartRenderer: FC<SyntaxHighlighterProps> = ({ code }) => {
  const message = useAssistantState(({ message }) => message)
  const containerRef = useRef<HTMLDivElement>(null)
  const viewRef = useRef<View | null>(null)
  const errorTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const [processingChart, setProcessingChart] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const messageIsComplete = message.status?.type === 'complete'
  const r = useRadius()
  const d = useDensity()

  useEffect(() => {
    if (!containerRef.current) {
      return
    }

    // Clear any pending error timeout
    if (errorTimeoutRef.current) {
      clearTimeout(errorTimeoutRef.current)
      errorTimeoutRef.current = null
    }

    const trimmedCode = code.trim()

    // Try to parse JSON
    let json: Record<string, unknown>
    try {
      json = JSON.parse(trimmedCode)
      // Parsing succeeded - clear error
      setError(null)
    } catch (parseErr) {
      // If message is complete, delay showing error to allow code prop to catch up
      // This handles race condition where messageIsComplete becomes true before code finishes
      if (messageIsComplete) {
        errorTimeoutRef.current = setTimeout(() => {
          // Re-check parsing with current code value
          try {
            JSON.parse(code.trim())
            setError(null)
          } catch {
            console.error('Invalid JSON code:', code.trim())
            setError(
              `Invalid JSON syntax: ${parseErr instanceof Error ? parseErr.message : 'Unable to parse'}`
            )
            setProcessingChart(false)
          }
        }, 200)
      }
      return
    }

    // Only render if message is complete
    if (!messageIsComplete) {
      return
    }

    // Clear error and render chart
    setError(null)
    setProcessingChart(true)

    const runChart = async () => {
      try {
        // Clean up any existing view
        if (viewRef.current) {
          viewRef.current.finalize()
          viewRef.current = null
        }

        const chart = parse(json)
        const view = new View(chart, {
          container: containerRef.current ?? undefined,
          renderer: 'svg',
          hover: true,
          logLevel: Warn,
        })
        viewRef.current = view

        await view.runAsync()
        setProcessingChart(false)
      } catch (err) {
        console.error('Failed to render chart:', err)
        setError(err instanceof Error ? err.message : 'Failed to render chart')
        setProcessingChart(false)
      }
    }
    runChart()

    // Cleanup function to destroy the view when component unmounts or re-renders
    return () => {
      if (viewRef.current) {
        viewRef.current.finalize()
        viewRef.current = null
      }
      if (errorTimeoutRef.current) {
        clearTimeout(errorTimeoutRef.current)
        errorTimeoutRef.current = null
      }
    }
  }, [messageIsComplete, code])

  return (
    <div
      className={cn(
        // the after:hidden is to prevent assistant-ui from showing its default code block loading indicator
        'relative flex min-h-[400px] w-fit max-w-full min-w-[400px] items-center justify-center border p-6 after:hidden',
        r('lg'),
        d('p-lg')
      )}
    >
      {(processingChart || !messageIsComplete) && (
        <div className="shimmer text-muted-foreground bg-background/80 absolute inset-0 z-10 flex items-center justify-center">
          Rendering chart...
        </div>
      )}

      {error && (
        <div className="bg-background absolute inset-0 z-10 flex items-center justify-center gap-2 text-rose-500">
          <AlertCircleIcon name="alert-circle" className="h-4 w-4" />
          Error rendering chart
        </div>
      )}

      <div
        ref={containerRef}
        className={processingChart ? 'hidden' : 'block'}
      />
    </div>
  )
}
