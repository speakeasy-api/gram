'use client'

import { useElements } from '@/hooks/useElements'
import { AssistantModal } from '../assistant-ui/assistant-modal'
import { AssistantSidecar } from '../assistant-ui/assistant-sidecar'
import { Thread } from '../assistant-ui/thread'
import { assertNever } from '@/lib/utils'

export const Chat = () => {
  const { config } = useElements()

  switch (config.variant) {
    case 'standalone':
      return <Thread />
    case 'sidecar':
      return <AssistantSidecar />
    case 'widget':
      return <AssistantModal />
    default:
      assertNever(config.variant)
  }
}
