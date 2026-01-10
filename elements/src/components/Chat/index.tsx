'use client'

import { useElements } from '@/hooks/useElements'
import { AssistantModal } from '../assistant-ui/assistant-modal'
import { AssistantSidecar } from '../assistant-ui/assistant-sidecar'
import { Thread } from '../assistant-ui/thread'

export const Chat = () => {
  const { config } = useElements()

  switch (config.variant) {
    case 'standalone':
      return <Thread />
    case 'sidecar':
      return <AssistantSidecar />

    // If no variant is provided then fallback to the modal
    default:
      return <AssistantModal />
  }
}
