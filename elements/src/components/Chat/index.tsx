'use client'

import { useElements } from '@/hooks/useElements'
import { AssistantModal } from '../assistant-ui/assistant-modal'
import { AssistantSidecar } from '../assistant-ui/assistant-sidecar'
import { ErrorBoundary } from '../assistant-ui/error-boundary'
import { Thread } from '../assistant-ui/thread'
import { ShadowRoot } from '@/components/ShadowRoot'

interface ChatProps {
  className?: string
}

function RootWrapper({ children }: { children: React.ReactNode }) {
  return (
    <ShadowRoot hostStyle={{ height: 'inherit', width: 'inherit' }}>
      {children}
    </ShadowRoot>
  )
}

export const Chat = ({ className }: ChatProps) => {
  const { config } = useElements()

  switch (config.variant) {
    case 'standalone':
      // Standalone variant wraps Thread with ErrorBoundary at this level
      return (
        <ErrorBoundary>
          <RootWrapper>
            <Thread />
          </RootWrapper>
        </ErrorBoundary>
      )
    case 'sidecar':
      // Sidecar has its own internal ErrorBoundary around Thread
      return (
        <RootWrapper>
          <AssistantSidecar />
        </RootWrapper>
      )

    // If no variant is provided then fallback to the modal
    // Modal has its own internal ErrorBoundary around Thread
    default:
      return (
        <RootWrapper>
          <AssistantModal className={className} />
        </RootWrapper>
      )
  }
}
