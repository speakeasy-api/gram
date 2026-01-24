'use client'

import { GenerativeUI } from '@/components/ui/generative-ui'
import { useRadius } from '@/hooks/useRadius'
import { cn } from '@/lib/utils'
import { SyntaxHighlighterProps } from '@assistant-ui/react-markdown'
import { FC, useMemo } from 'react'

const loadingMessages = [
  'Crafting your dashboard...',
  'Arranging the widgets...',
  'Painting pixels...',
  'Summoning components...',
  'Assembling the view...',
  'Weaving the interface...',
  'Brewing your UI...',
  'Conjuring layouts...',
  'Materializing widgets...',
  'Composing the experience...',
]

function getRandomLoadingMessage() {
  return loadingMessages[Math.floor(Math.random() * loadingMessages.length)]
}

export const GenerativeUIRenderer: FC<SyntaxHighlighterProps> = ({ code }) => {
  const r = useRadius()

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

  // Memoize the loading message so it doesn't change on every render
  const loadingMessage = useMemo(() => getRandomLoadingMessage(), [])

  // Show loading shimmer while JSON is incomplete/streaming
  if (!content) {
    return (
      <div
        className={cn(
          'border-border bg-card relative h-[450px] w-[600px] overflow-hidden border after:hidden',
          r('lg')
        )}
      >
        <div className="shimmer text-muted-foreground absolute inset-0 flex items-center justify-center">
          {loadingMessage}
        </div>
      </div>
    )
  }

  // Render without outer border - the Card component inside provides the border
  return (
    <div className="overflow-hidden after:hidden">
      <GenerativeUI content={content} />
    </div>
  )
}
