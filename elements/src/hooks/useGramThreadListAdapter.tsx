import {
  unstable_RemoteThreadListAdapter as RemoteThreadListAdapter,
  ThreadMessage,
  RuntimeAdapterProvider,
  ThreadHistoryAdapter,
  useAssistantApi,
  type AssistantApi,
} from '@assistant-ui/react'
import type { AssistantStream } from 'assistant-stream'
import {
  GramChatOverview,
  GramChat,
  convertGramMessagesToExported,
} from '@/lib/messageConverter'
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type PropsWithChildren,
} from 'react'

export interface ThreadListAdapterOptions {
  apiUrl: string
  headers: Record<string, string>
}

interface ListChatsResponse {
  chats: GramChatOverview[]
}

/**
 * Thread history adapter that loads messages from Gram API.
 * Note: We use `as ThreadHistoryAdapter` cast because the withFormat generic
 * signature doesn't match our concrete implementation, but it works at runtime.
 */
class GramThreadHistoryAdapter {
  private apiUrl: string
  private headers: Record<string, string>
  private store: AssistantApi

  constructor(
    apiUrl: string,
    headers: Record<string, string>,
    store: AssistantApi
  ) {
    this.apiUrl = apiUrl
    this.headers = headers
    this.store = store
  }

  async load() {
    const remoteId = this.store.threadListItem().getState().remoteId
    if (!remoteId) {
      return { messages: [], headId: null }
    }

    try {
      const response = await fetch(
        `${this.apiUrl}/rpc/chat.load?id=${encodeURIComponent(remoteId)}`,
        { headers: this.headers }
      )

      if (!response.ok) {
        console.error('Failed to load chat:', response.status)
        return { messages: [], headId: null }
      }

      const chat = (await response.json()) as GramChat
      return convertGramMessagesToExported(chat.messages)
    } catch (error) {
      console.error('Error loading chat:', error)
      return { messages: [], headId: null }
    }
  }

  async append() {
    // No-op: Gram persists messages server-side during streaming.
  }

  // Required by ThreadHistoryAdapter - wraps adapter with format conversion.
  // The _formatAdapter param is part of the interface but unused since we handle conversion ourselves.
  // Using arrow functions to capture `this` lexically.
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  withFormat(_formatAdapter: unknown) {
    return {
      load: async () => {
        const remoteId = this.store.threadListItem().getState().remoteId
        if (!remoteId) {
          return { messages: [], headId: null }
        }

        try {
          const response = await fetch(
            `${this.apiUrl}/rpc/chat.load?id=${encodeURIComponent(remoteId)}`,
            { headers: this.headers }
          )

          if (!response.ok) {
            console.error('Failed to load chat (withFormat):', response.status)
            return { messages: [], headId: null }
          }

          const chat = (await response.json()) as GramChat

          // Filter out system messages (assistant-ui doesn't support them in the import path)
          const filteredMessages = chat.messages.filter(
            (msg) => msg.role !== 'system'
          )

          if (filteredMessages.length === 0) {
            return { messages: [], headId: null }
          }

          // Convert to the format expected by useExternalHistory
          // It expects UIMessage format with role and parts array
          let prevId: string | null = null
          const messages = filteredMessages.map((msg, index) => {
            // Generate a fallback ID if missing (required by assistant-ui's MessageRepository)
            const messageId = msg.id || `fallback-${index}-${Date.now()}`
            const uiMessage = {
              parentId: prevId,
              message: {
                id: messageId,
                role: msg.role as 'user' | 'assistant',
                parts: [{ type: 'text' as const, text: msg.content || '' }],
                createdAt: msg.createdAt ? new Date(msg.createdAt) : new Date(),
              },
            }
            prevId = messageId
            return uiMessage
          })

          return {
            headId: prevId,
            messages,
          }
        } catch (error) {
          console.error('Error loading chat (withFormat):', error)
          return { messages: [], headId: null }
        }
      },
      append: async () => {
        // No-op
      },
    }
  }
}

/**
 * Hook to create a Gram thread history adapter.
 */
function useGramThreadHistoryAdapter(
  optionsRef: React.RefObject<ThreadListAdapterOptions>
): ThreadHistoryAdapter {
  const store = useAssistantApi()
  const [adapter] = useState(
    () =>
      new GramThreadHistoryAdapter(
        optionsRef.current.apiUrl,
        optionsRef.current.headers,
        store
      )
  )
  // Cast to ThreadHistoryAdapter - the withFormat generic doesn't match but works at runtime
  return adapter as unknown as ThreadHistoryAdapter
}

/**
 * Hook that creates a RemoteThreadListAdapter for the Gram API.
 * This properly handles React component identity for the Provider.
 */
export function useGramThreadListAdapter(
  options: ThreadListAdapterOptions
): RemoteThreadListAdapter {
  const optionsRef = useRef(options)
  useEffect(() => {
    optionsRef.current = options
  }, [options])

  // Create stable Provider component using useCallback
  const unstable_Provider = useCallback(function GramHistoryProvider({
    children,
  }: PropsWithChildren) {
    const history = useGramThreadHistoryAdapter(optionsRef)
    const adapters = useMemo(() => ({ history }), [history])
    return (
      <RuntimeAdapterProvider adapters={adapters}>
        {children}
      </RuntimeAdapterProvider>
    )
  }, [])

  // Return adapter with stable methods
  return useMemo(
    () => ({
      unstable_Provider,

      async list() {
        try {
          const response = await fetch(
            `${optionsRef.current.apiUrl}/rpc/chat.list`,
            {
              headers: optionsRef.current.headers,
            }
          )

          if (!response.ok) {
            console.error('Failed to list chats:', response.status)
            return { threads: [] }
          }

          const data = (await response.json()) as ListChatsResponse
          return {
            threads: data.chats.map((chat) => ({
              remoteId: chat.id,
              externalId: chat.id,
              status: 'regular' as const,
              title: chat.title || 'New Chat',
            })),
          }
        } catch (error) {
          console.error('Error listing chats:', error)
          return { threads: [] }
        }
      },

      async initialize(threadId: string) {
        return {
          remoteId: threadId,
          externalId: threadId,
        }
      },

      async rename() {
        // No-op
      },

      async archive() {
        // No-op
      },

      async unarchive() {
        // No-op
      },

      async delete() {
        // No-op
      },

      async generateTitle(
        // eslint-disable-next-line @typescript-eslint/no-unused-vars
        _remoteId: string,
        // eslint-disable-next-line @typescript-eslint/no-unused-vars
        _messages: readonly ThreadMessage[]
      ): Promise<AssistantStream> {
        // Return an empty stream that immediately completes
        // Server generates titles automatically, so we just provide a placeholder
        return new ReadableStream({
          start(controller) {
            controller.close()
          },
        }) as AssistantStream
      },

      async fetch(threadId: string) {
        try {
          const response = await fetch(
            `${optionsRef.current.apiUrl}/rpc/chat.load?id=${encodeURIComponent(threadId)}`,
            {
              headers: optionsRef.current.headers,
            }
          )

          if (!response.ok) {
            console.error('Failed to fetch thread:', response.status)
            return {
              remoteId: threadId,
              status: 'regular' as const,
            }
          }

          const chat = await response.json()
          return {
            remoteId: chat.id,
            externalId: chat.id,
            status: 'regular' as const,
            title: chat.title || 'New Chat',
          }
        } catch (error) {
          console.error('Error fetching thread:', error)
          return {
            remoteId: threadId,
            status: 'regular' as const,
          }
        }
      },
    }),
    [unstable_Provider]
  )
}
