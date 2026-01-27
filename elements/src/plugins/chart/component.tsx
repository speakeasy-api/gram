'use client'

import { useDensity } from '@/hooks/useDensity'
import { useRadius } from '@/hooks/useRadius'
import { cn } from '@/lib/utils'
import { SyntaxHighlighterProps } from '@assistant-ui/react-markdown'
import { AlertCircleIcon } from 'lucide-react'
import { FC, useEffect, useMemo, useRef, useState } from 'react'
import { parse, View, Warn } from 'vega'
import { expressionInterpreter } from 'vega-interpreter'
import { PluginLoadingState } from '../components/PluginLoadingState'

export const ChartRenderer: FC<SyntaxHighlighterProps> = ({ code }) => {
  const containerRef = useRef<HTMLDivElement>(null)
  const viewRef = useRef<View | null>(null)
  const [error, setError] = useState<string | null>(null)
  const r = useRadius()
  const d = useDensity()

  // Parse and validate JSON in useMemo - only recomputes when code changes
  const parsedSpec = useMemo(() => {
    const trimmedCode = code.trim()
    if (!trimmedCode) return null

    try {
      const spec = JSON.parse(trimmedCode) as Record<string, unknown>

      // Validate that data array exists and has at least one record with values
      const dataArray = spec.data as Array<{ values?: unknown[] }> | undefined
      if (!dataArray?.length) return null

      const hasValidData = dataArray.some(
        (d) => Array.isArray(d.values) && d.values.length > 0
      )
      if (!hasValidData) return null

      return spec
    } catch {
      return null
    }
  }, [code])

  // Only render when we have valid JSON
  const shouldRender = parsedSpec !== null

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

        const chart = parse(parsedSpec, undefined, { ast: true })
        const view = new View(chart, {
          container: containerRef.current ?? undefined,
          renderer: 'svg',
          hover: true,
          logLevel: Warn,
          expr: expressionInterpreter,
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

  // Show loading state while JSON is incomplete/streaming
  if (!shouldRender && !error) {
    return <PluginLoadingState text="Rendering chart..." />
  }

  return (
    <div
      className={cn(
        // the after:hidden is to prevent assistant-ui from showing its default code block loading indicator
        'border-border relative min-h-[400px] w-fit min-w-[400px] max-w-full overflow-auto border after:hidden',
        r('lg'),
        d('p-lg')
      )}
    >
      {error && (
        <div className="bg-background absolute inset-0 z-10 flex items-center justify-center gap-2 text-rose-500">
          <AlertCircleIcon name="alert-circle" className="h-4 w-4" />
          {error}
        </div>
      )}

      <div ref={containerRef} className={error ? 'hidden' : 'block'} />
    </div>
  )
}
