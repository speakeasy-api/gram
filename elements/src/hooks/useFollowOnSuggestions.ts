import { useReplayContext } from '@/contexts/ReplayContext'
import { getApiUrl } from '@/lib/api'
import { useAssistantState } from '@assistant-ui/react'
import { generateObject } from 'ai'
import { useCallback, useEffect, useRef, useState } from 'react'
import { z } from 'zod'
import { useAuth } from './useAuth'
import { useElements } from './useElements'
import { useModel } from './useModel'

export interface FollowOnSuggestion {
  id: string
  prompt: string
}

const suggestionsSchema = z.object({
  suggestions: z.array(z.string()).describe('Array of follow-up questions'),
})

const questionCheckSchema = z.object({
  isQuestion: z
    .boolean()
    .describe(
      'Whether the message ends by asking the user a question that requires their input or response'
    ),
})

const SUGGESTIONS_MODEL = 'openai/gpt-4o-mini'

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
  const replayCtx = useReplayContext()
  const isReplay = replayCtx?.isReplay ?? false

  const auth = useAuth({
    auth: config.api,
    projectSlug: config.projectSlug,
  })

  const model = useModel(SUGGESTIONS_MODEL)

  // Check if follow-up suggestions are enabled (default: true)
  // Disable in replay mode since we don't need AI-generated suggestions
  const isEnabled = !isReplay && config.thread?.followUpSuggestions !== false

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

    // Find the last assistant message
    let lastAssistantMessage = ''
    for (let i = recentMessages.length - 1; i >= 0; i--) {
      const msg = recentMessages[i]
      if (msg.role === 'assistant') {
        lastAssistantMessage = msg.content
        break
      }
    }

    // Cancel any in-flight request
    if (abortControllerRef.current) {
      abortControllerRef.current.abort()
    }

    const controller = new AbortController()
    abortControllerRef.current = controller

    setIsLoading(true)

    try {
      // Check if the assistant is asking a question
      if (lastAssistantMessage) {
        try {
          const checkResult = await generateObject({
            model,
            schema: questionCheckSchema,
            prompt: `Does this message end by asking the user a question that requires their input or response?

Message:
${lastAssistantMessage}`,
            abortSignal: controller.signal,
          })

          if (checkResult.object.isQuestion) {
            // Don't generate suggestions if assistant is asking a question
            if (abortControllerRef.current === controller) {
              setSuggestions([])
              setIsLoading(false)
              abortControllerRef.current = null
            }
            return
          }
        } catch (error) {
          // If check fails, continue with generating suggestions
          console.warn('Failed to check if message is a question:', error)
        }
      }

      // Build conversation context
      const conversation = recentMessages
        .map((msg) => `${msg.role}: ${msg.content}`)
        .join('\n')

      const count = 3
      const systemPrompt = `Generate exactly ${count} follow-up questions the user could ask to learn MORE from the assistant.

The user wants to dig deeper into what the assistant just explained. Generate questions that ask the assistant to elaborate, compare, or provide more details.

Good examples:
- "How does X compare to Y?"
- "Can you explain more about Z?"
- "What are the pros and cons of X?"

Rules:
- Focus on the informational content the assistant provided
- Ask for elaboration, comparisons, or deeper explanations
- Keep each question concise (under 12 words)
- No numbering or bullet points
- One question per line, nothing else`

      const result = await generateObject({
        model,
        schema: suggestionsSchema,
        prompt: `${systemPrompt}

Conversation:
${conversation}`,
        abortSignal: controller.signal,
      })

      // Only update state if this request is still the current one
      if (abortControllerRef.current === controller) {
        setSuggestions(
          result.object.suggestions.slice(0, count).map((prompt) => ({
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
      console.error('Error generating follow-on suggestions:', error)
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
