import { useQuery, type UseQueryResult } from '@tanstack/react-query'
import { useSession } from './useSession'
import { GetSessionFn } from '@/types'

const GRAM_API_URL = 'https://localhost:8080'

/**
 * Chat overview from the server API
 */
export interface ChatOverview {
  id: string
  title: string
  userId: string
  numMessages: number
  createdAt: string
  updatedAt: string
}

/**
 * Response from the chat list endpoint
 */
interface ListChatsResponse {
  chats: ChatOverview[]
}

/**
 * Hook to fetch the list of chats for the current user/project.
 */
export function useChatList({
  getSession,
  projectSlug,
  enabled = true,
}: {
  getSession: GetSessionFn
  projectSlug: string
  enabled?: boolean
}): UseQueryResult<ChatOverview[], Error> {
  const session = useSession({
    getSession,
    projectSlug,
  })

  return useQuery({
    queryKey: ['chatList', projectSlug, session],
    queryFn: async (): Promise<ChatOverview[]> => {
      if (!session) {
        return []
      }

      const response = await fetch(`${GRAM_API_URL}/rpc/chat.list`, {
        headers: {
          'Gram-Project': projectSlug,
          'Gram-Chat-Session': session,
        },
      })

      if (!response.ok) {
        throw new Error(`Failed to fetch chat list: ${response.statusText}`)
      }

      const data: ListChatsResponse = await response.json()
      return data.chats ?? []
    },
    enabled: enabled && !!session,
    staleTime: 30_000, // Refetch after 30 seconds
  })
}
