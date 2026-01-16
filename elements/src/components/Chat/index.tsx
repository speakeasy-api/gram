'use client'

import { useElements } from '@/hooks/useElements'
import { AssistantModal } from '../assistant-ui/assistant-modal'
import { AssistantSidecar } from '../assistant-ui/assistant-sidecar'
import { ErrorBoundary } from '../assistant-ui/error-boundary'
import { Thread } from '../assistant-ui/thread'
import { ROOT_SELECTOR } from '@/constants/tailwind'

interface ChatProps {
  className?: string
}

function wrapWithRootSelector<T extends React.ReactNode>(children: T) {
  return (
    <div className={ROOT_SELECTOR} style={{ height: 'inherit' }}>
      {children}
    </div>
  )
}

export const Chat = ({ className }: ChatProps) => {
  const { config } = useElements()

  switch (config.variant) {
    case 'standalone':
      // Standalone variant wraps Thread with ErrorBoundary at this level
      return <ErrorBoundary>{wrapWithRootSelector(<Thread />)}</ErrorBoundary>
    case 'sidecar':
      // Sidecar has its own internal ErrorBoundary around Thread
      return wrapWithRootSelector(<AssistantSidecar />)

    // If no variant is provided then fallback to the modal
    // Modal has its own internal ErrorBoundary around Thread
    default:
      return wrapWithRootSelector(<AssistantModal className={className} />)
  }
}
