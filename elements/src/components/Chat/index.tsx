'use client'

import { useElements } from '@/hooks/useElements'
import { AssistantModal } from '../assistant-ui/assistant-modal'
import { AssistantSidecar } from '../assistant-ui/assistant-sidecar'
import { ErrorBoundary } from '../assistant-ui/error-boundary'
import { Thread } from '../assistant-ui/thread'

interface ChatProps {
  className?: string
}

export const Chat = ({ className }: ChatProps) => {
  const { config } = useElements()

  switch (config.variant) {
    case 'standalone':
      // Standalone variant wraps Thread with ErrorBoundary at this level
      return (
        <ErrorBoundary>
          <Thread />
        </ErrorBoundary>
      )
    case 'sidecar':
      // Sidecar has its own internal ErrorBoundary around Thread
      return <AssistantSidecar />

    // If no variant is provided then fallback to the modal
    // Modal has its own internal ErrorBoundary around Thread
    default:
      return <AssistantModal className={className} />
  }
}
