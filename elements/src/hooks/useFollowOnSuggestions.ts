import { useAssistantState } from '@assistant-ui/react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useElements } from './useElements'
import { getApiUrl } from '@/lib/api'
import { useAuth } from './useAuth'

export interface FollowOnSuggestion {
  id: string
  prompt: string
}

interface GenerateFollowOnSuggestionsResponse {
  suggestions: string[]
}

/**
 * Hook to fetch follow-on suggestions after the assistant finishes responding.
 * Suggestions are generated based on the conversation context.
 *
 * Can be disabled via `config.thread.followUpSuggestions: false`
 */
export function useFollowOnSuggestions(): {
  suggestions: FollowOnSuggestion[]
  isLoading: boolean
} {
  const { config } = useElements()
  const auth = useAuth({
    auth: config.api,
    projectSlug: config.projectSlug,
  })

  // Check if follow-up suggestions are enabled (default: true)
  const isEnabled = config.thread?.followUpSuggestions !== false

  const [suggestions, setSuggestions] = useState<FollowOnSuggestion[]>([])
  const [isLoading, setIsLoading] = useState(false)

  // Track the last message ID we generated suggestions for to avoid duplicates
  const lastProcessedMessageIdRef = useRef<string | null>(null)
  // Track abort controller for in-flight requests
  const abortControllerRef = useRef<AbortController | null>(null)

  // Get thread state from assistant-ui
  const isRunning = useAssistantState(({ thread }) => thread.isRunning)
  const messages = useAssistantState(({ thread }) => thread.messages)

  const apiUrl = getApiUrl(config)

  const fetchSuggestions = useCallback(async () => {
    if (!isEnabled || auth.isLoading || !auth.headers) return

    // Get the last few messages for context
    const recentMessages = messages.slice(-10).map((msg) => {
      // Extract text content from message parts
      const textContent = msg.parts
        .filter((part) => part.type === 'text')
        .map((part) => ('text' in part ? part.text : ''))
        .join('\n')

      return {
        role: msg.role,
        content: textContent,
      }
    })

    if (recentMessages.length === 0) return

    // Cancel any in-flight request
    if (abortControllerRef.current) {
      abortControllerRef.current.abort()
    }

    const controller = new AbortController()
    abortControllerRef.current = controller

    setIsLoading(true)

    try {
      const response = await fetch(
        `${apiUrl}/rpc/chat.generateFollowOnSuggestions`,
        {
          method: 'POST',
          headers: {
            ...auth.headers,
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({
            messages: recentMessages,
            count: 3,
          }),
          signal: controller.signal,
        }
      )

      if (!response.ok) {
        console.error('Failed to fetch follow-on suggestions:', response.status)
        if (abortControllerRef.current === controller) {
          setSuggestions([])
        }
        return
      }

      const data =
        (await response.json()) as GenerateFollowOnSuggestionsResponse

      // Only update state if this request is still the current one
      if (abortControllerRef.current === controller) {
        setSuggestions(
          data.suggestions.map((prompt) => ({
            id: crypto.randomUUID(),
            prompt,
          }))
        )
      }
    } catch (error) {
      if (error instanceof Error && error.name === 'AbortError') {
        // Request was aborted, ignore
        return
      }
      console.error('Error fetching follow-on suggestions:', error)
      if (abortControllerRef.current === controller) {
        setSuggestions([])
      }
    } finally {
      // Only clear state if this request is still the current one
      if (abortControllerRef.current === controller) {
        setIsLoading(false)
        abortControllerRef.current = null
      }
    }
  }, [isEnabled, apiUrl, auth.headers, auth.isLoading, messages])

  // Fetch suggestions when:
  // 1. The thread stops running (assistant finished responding)
  // 2. There are messages in the thread
  // 3. The last message is from the assistant
  // 4. We haven't already processed this message
  useEffect(() => {
    if (isRunning) {
      // Abort any in-flight request and clear suggestions when a new run starts
      if (abortControllerRef.current) {
        abortControllerRef.current.abort()
        abortControllerRef.current = null
      }
      setSuggestions([])
      setIsLoading(false)
      return
    }

    if (messages.length === 0) return

    const lastMessage = messages[messages.length - 1]
    if (!lastMessage || lastMessage.role !== 'assistant') return

    // Check if we've already processed this message
    if (lastProcessedMessageIdRef.current === lastMessage.id) return

    lastProcessedMessageIdRef.current = lastMessage.id
    fetchSuggestions()
  }, [isRunning, messages, fetchSuggestions])

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (abortControllerRef.current) {
        abortControllerRef.current.abort()
      }
    }
  }, [])

  return { suggestions, isLoading }
}
