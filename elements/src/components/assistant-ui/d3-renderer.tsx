'use client'

import { useMessage } from '@assistant-ui/react'
import * as d3 from 'd3'
import { memo, useEffect, useRef, useState } from 'react'

import { cn } from '@/lib/utils'

export interface D3RendererProps {
  code: string
  className?: string
}

/** Creates a d3 instance with select/selectAll scoped to a container */
const createScopedD3 = (container: HTMLElement) => ({
  ...d3,
  select: (selector: string) => {
    if (typeof selector === 'string') {
      const el = container.querySelector(selector)
      if (el) return d3.select(el)
    }
    return d3.select(selector)
  },
  selectAll: (selector: string) => {
    if (typeof selector === 'string') {
      return d3.selectAll(container.querySelectorAll(selector))
    }
    return d3.selectAll(selector)
  },
})

/** Execute code with d3 and container in scope */
const executeScript = (
  code: string,
  scopedD3: ReturnType<typeof createScopedD3>,
  container: HTMLElement
) => {
  const fn = new Function('d3', 'container', `"use strict";\n${code}`)
  fn(scopedD3, container)
}

const ErrorDisplay = ({
  message,
  className,
}: {
  message: string
  className?: string
}) => (
  <div
    className={cn(
      'mt-4 rounded-lg border border-red-300 bg-red-50 p-4 dark:border-red-800 dark:bg-red-950',
      className
    )}
  >
    <p className="mb-2 text-sm font-semibold text-red-700 dark:text-red-400">
      Rendering Error
    </p>
    <pre className="overflow-x-auto text-xs text-red-600 dark:text-red-300">
      {message}
    </pre>
  </div>
)

const containerClassName =
  'mt-4 overflow-x-auto rounded-lg border bg-white p-4 dark:bg-neutral-900'

const D3RendererImpl = ({ code, className }: D3RendererProps) => {
  const containerRef = useRef<HTMLDivElement>(null)
  const [error, setError] = useState<string | null>(null)
  const message = useMessage()
  const isComplete = message.status?.type === 'complete'

  useEffect(() => {
    // Don't execute until streaming is complete
    if (!isComplete || !containerRef.current || !code) return

    // Debounce: wait 100ms after code stops changing
    const timeoutId = setTimeout(() => {
      const container = containerRef.current
      if (!container) return

      container.innerHTML = ''
      setError(null)

      try {
        const scopedD3 = createScopedD3(container)
        executeScript(code, scopedD3, container)
      } catch (err) {
        const msg = err instanceof Error ? err.message : 'Unknown error'
        setError(msg)
        console.error('[D3Renderer] error:', err)
      }
    }, 100)

    return () => clearTimeout(timeoutId)
  }, [code, isComplete])

  return (
    <>
      <div ref={containerRef} className={cn(containerClassName, className)}>
        {!isComplete && (
          <div className="text-muted-foreground flex items-center gap-2 text-sm">
            Generating chart...
          </div>
        )}
      </div>
      {error && <ErrorDisplay message={error} className={className} />}
    </>
  )
}

export const D3Renderer = memo(D3RendererImpl)
