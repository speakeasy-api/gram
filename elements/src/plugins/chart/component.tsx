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
  const [processingChart, setProcessingChart] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const messageIsComplete = message.status?.type === 'complete'
  const r = useRadius()
  const d = useDensity()

  useEffect(() => {
    if (!messageIsComplete) {
      return
    }
    if (!containerRef.current) {
      return
    }

    let view: View | null = null

    const runChart = async () => {
      try {
        let json
        try {
          json = JSON.parse(code)
        } catch (parseErr) {
          throw new Error(
            `Invalid JSON syntax: ${parseErr instanceof Error ? parseErr.message : 'Unable to parse'}`
          )
        }
        const chart = parse(json)
        view = new View(chart, {
          container: containerRef.current ?? undefined,
          renderer: 'svg',
          hover: true,
          logLevel: Warn,
        })

        await view.runAsync()
        setProcessingChart(false)
      } catch (err) {
        console.error('Failed to render chart:', err)
        const errorMessage =
          err instanceof Error ? err.message : 'Failed to render chart'
        setError(errorMessage)
        setProcessingChart(false)
      }
    }
    runChart()

    // Cleanup function to destroy the view when component unmounts or re-renders
    return () => {
      if (view) {
        view.finalize()
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
