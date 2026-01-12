import { useQuery, type UseQueryResult } from '@tanstack/react-query'
import { useSession } from './useSession'
import { GetSessionFn } from '@/types'

const GRAM_API_URL = 'https://localhost:8080'

/**
 * Chat message from the server API
 */
export interface ChatMessage {
  id: string
  role: 'user' | 'assistant' | 'system' | 'tool'
  content?: string
  model?: string
  toolCallId?: string
  toolCalls?: string
  finishReason?: string
  userId?: string
  createdAt: string
}

/**
 * Full chat with messages from the server API
 */
export interface Chat {
  id: string
  title: string
  userId: string
  numMessages: number
  messages: ChatMessage[]
  createdAt: string
  updatedAt: string
}

/**
 * Hook to load a specific chat with all its messages.
 */
export function useChatLoad({
  getSession,
  projectSlug,
  chatId,
  enabled = true,
}: {
  getSession: GetSessionFn
  projectSlug: string
  chatId: string | null
  enabled?: boolean
}): UseQueryResult<Chat | null, Error> {
  const session = useSession({
    getSession,
    projectSlug,
  })

  return useQuery({
    queryKey: ['chat', projectSlug, chatId, session],
    queryFn: async (): Promise<Chat | null> => {
      if (!session || !chatId) {
        return null
      }

      const response = await fetch(
        `${GRAM_API_URL}/rpc/chat.load?id=${encodeURIComponent(chatId)}`,
        {
          headers: {
            'Gram-Project': projectSlug,
            'Gram-Chat-Session': session,
          },
        }
      )

      if (!response.ok) {
        throw new Error(`Failed to load chat: ${response.statusText}`)
      }

      return response.json()
    },
    enabled: enabled && !!session && !!chatId,
    staleTime: 30_000,
  })
}
