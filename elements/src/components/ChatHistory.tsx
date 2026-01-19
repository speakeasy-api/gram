'use client'

import { ThreadList } from '@/components/assistant-ui/thread-list'
import { ShadowRoot } from '@/components/ShadowRoot'

interface ChatHistoryProps {
  className?: string
}

export const ChatHistory = ({ className }: ChatHistoryProps) => {
  return (
    <ShadowRoot hostStyle={{ height: 'inherit', width: 'inherit' }}>
      <ThreadList className={className} />
    </ShadowRoot>
  )
}
