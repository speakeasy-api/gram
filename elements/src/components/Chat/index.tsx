'use client'

import { useElements } from '@/hooks/useElements'
import { AssistantModal } from '../assistant-ui/assistant-modal'
import { AssistantSidecar } from '../assistant-ui/assistant-sidecar'
import { Thread } from '../assistant-ui/thread'

interface ChatProps {
  className?: string
}

export const Chat = ({ className }: ChatProps) => {
  const { config } = useElements()

  switch (config.variant) {
    case 'standalone':
      return <Thread className={className} />
    case 'sidecar':
      return <AssistantSidecar className={className} />

    // If no variant is provided then fallback to the modal
    default:
      return <AssistantModal className={className} />
  }
}
