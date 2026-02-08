'use client'

import { GenerativeUI } from '@/components/ui/generative-ui'
import { SyntaxHighlighterProps } from '@assistant-ui/react-markdown'
import { FC, useMemo } from 'react'
import { MacOSWindowFrame } from '../components/MacOSWindowFrame'
import { PluginLoadingState } from '../components/PluginLoadingState'

const loadingMessages = [
  'Preparing your data...',
  'Building your view...',
  'Generating results...',
  'Loading content...',
  'Fetching information...',
  'Processing your request...',
  'Almost ready...',
  'Setting things up...',
]

function getRandomLoadingMessage() {
  return loadingMessages[Math.floor(Math.random() * loadingMessages.length)]
}

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

  // Memoize the loading message so it doesn't change on every render
  const loadingMessage = useMemo(() => getRandomLoadingMessage(), [])

  // Show loading shimmer while JSON is incomplete/streaming
  if (!content) {
    return <PluginLoadingState text={loadingMessage} />
  }

  // Render with macOS-style window frame
  return (
    <MacOSWindowFrame>
      <GenerativeUI content={content} />
    </MacOSWindowFrame>
  )
}
