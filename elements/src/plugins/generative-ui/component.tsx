'use client'

import { GenerativeUI } from '@/components/ui/generative-ui'
import { SyntaxHighlighterProps } from '@assistant-ui/react-markdown'
import { FC, useMemo } from 'react'
import { MacOSWindowFrame } from '../components/MacOSWindowFrame'

// Debug mode - set to true to see streaming output
const DEBUG_STREAMING = true

export const GenerativeUIRenderer: FC<SyntaxHighlighterProps> = ({ code }) => {
  // Parse JSON - returns null if invalid (still streaming)
  const content = useMemo(() => {
    const trimmedCode = code.trim()
    if (!trimmedCode) return null

    try {
      const parsed = JSON.parse(trimmedCode)
      // Validate it has a type field (basic json-render structure)
      if (!parsed || typeof parsed !== 'object' || !('type' in parsed)) {
        return null
      }
      return parsed
    } catch {
      // JSON is incomplete (still streaming) - return null to show loading state
      return null
    }
  }, [code])

  // Show debug streaming view while JSON is incomplete
  if (!content) {
    return (
      <MacOSWindowFrame>
        <div className="bg-card min-h-[200px] p-4">
          {DEBUG_STREAMING ? (
            <div className="space-y-2">
              <div className="text-muted-foreground flex items-center gap-2 text-xs">
                <span className="inline-block size-2 animate-pulse rounded-full bg-amber-500" />
                Streaming JSON ({code.length} chars)
              </div>
              <pre className="bg-muted text-foreground max-h-[300px] overflow-auto rounded p-2 font-mono text-xs">
                {code || '(waiting for content...)'}
              </pre>
            </div>
          ) : (
            <div className="flex h-full min-h-[200px] items-center justify-center">
              <span className="shimmer text-muted-foreground text-sm">
                Building UI...
              </span>
            </div>
          )}
        </div>
      </MacOSWindowFrame>
    )
  }

  // Render with macOS-style window frame
  return (
    <MacOSWindowFrame>
      <GenerativeUI content={content} />
    </MacOSWindowFrame>
  )
}
