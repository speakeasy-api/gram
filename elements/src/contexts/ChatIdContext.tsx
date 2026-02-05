import { createContext, useContext } from 'react'

export interface ChatIdContextValue {
  chatId: string | null
}

export const ChatIdContext = createContext<ChatIdContextValue | null>(null)

/**
 * Hook to access the current chat ID from the Elements context.
 * Works in both history-enabled and history-disabled modes.
 *
 * @returns The current chat ID, or null if not yet initialized
 */
export const useChatId = () => {
  const context = useContext(ChatIdContext)
  if (!context) {
    throw new Error('useChatId must be used within ElementsProvider')
  }
  return context.chatId
}
