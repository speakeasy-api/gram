import { useCallback, useContext, useRef, useState } from 'react'
import { streamText, stepCountIs, ToolSet } from 'ai'
import { createOpenRouter } from '@openrouter/ai-sdk-provider'
import { ElementsContext } from '@/contexts/contexts'
import { getApiUrl } from '@/lib/api'
import { useAuth } from '@/hooks/useAuth'
import type {
  CommandBarMessageEvent,
  CommandBarToolCallEvent,
} from '@/types'

export interface UseCommandBarAIOptions {
  onToolCall?: (event: CommandBarToolCallEvent) => void
  onMessage?: (event: CommandBarMessageEvent) => void
}

export interface UseCommandBarAIReturn {
  submit: (query: string) => void
  text: string
  isStreaming: boolean
  error: string | null
  toolCalls: CommandBarToolCallEvent[]
  reset: () => void
}

const COMMAND_BAR_SYSTEM_PROMPT = `You are a concise assistant embedded in a command bar. Respond in 1-3 sentences unless more detail is explicitly requested. When using tools, summarize the result briefly.`

/**
 * Hook that manages AI streaming for the command bar fallback.
 *
 * Uses the same auth, model, API URL, and MCP tools as the main Chat
 * when inside an ElementsProvider. When standalone, AI is unavailable
 * and `submit` will set an error.
 */
export function useCommandBarAI(
  options: UseCommandBarAIOptions = {}
): UseCommandBarAIReturn {
  const elements = useContext(ElementsContext)

  // Auth mirrors the same setup used by ElementsProvider's transport
  const auth = useAuth({
    auth: elements?.config.api,
    projectSlug: elements?.config.projectSlug ?? '',
  })

  const [text, setText] = useState('')
  const [isStreaming, setIsStreaming] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [toolCalls, setToolCalls] = useState<CommandBarToolCallEvent[]>([])
  const abortRef = useRef<AbortController | null>(null)

  const reset = useCallback(() => {
    abortRef.current?.abort()
    abortRef.current = null
    setText('')
    setIsStreaming(false)
    setError(null)
    setToolCalls([])
  }, [])

  const submit = useCallback(
    async (query: string) => {
      if (!elements) {
        setError('AI requires an ElementsProvider')
        return
      }

      if (auth.isLoading) {
        setError('Session is loading')
        return
      }

      // Abort any in-flight request
      abortRef.current?.abort()

      const abortController = new AbortController()
      abortRef.current = abortController

      setText('')
      setError(null)
      setToolCalls([])
      setIsStreaming(true)

      options.onMessage?.({ role: 'user', content: query })

      try {
        const { config, model, mcpTools } = elements
        const apiUrl = getApiUrl(config)

        const chatId = crypto.randomUUID()

        const headers = {
          ...auth.headers,
          'Gram-Chat-ID': chatId,
          'X-Gram-Source': 'elements',
          ...config.api?.headers,
          ...(config.gramEnvironment && {
            'Gram-Environment': config.gramEnvironment,
          }),
        }

        const modelToUse = config.languageModel
          ? config.languageModel
          : createOpenRouter({
              baseURL: apiUrl,
              apiKey: 'unused, but must be set',
              headers,
            }).chat(model)

        const tools = (mcpTools ?? {}) as ToolSet

        const result = streamText({
          model: modelToUse,
          system: COMMAND_BAR_SYSTEM_PROMPT,
          messages: [{ role: 'user', content: query }],
          tools,
          stopWhen: stepCountIs(5),
          abortSignal: abortController.signal,
        })

        for await (const chunk of (await result).textStream) {
          if (abortController.signal.aborted) break
          setText((prev) => prev + chunk)
        }

        const finalResult = await result
        const responseText =
          typeof finalResult === 'object' && 'text' in finalResult
            ? String(finalResult.text)
            : ''

        if (responseText) {
          options.onMessage?.({ role: 'assistant', content: responseText })
        }
      } catch (err) {
        if (abortController.signal.aborted) return
        const message = err instanceof Error ? err.message : 'Unknown error'
        setError(message)
      } finally {
        if (!abortController.signal.aborted) {
          setIsStreaming(false)
        }
      }
    },
    [elements, auth, options]
  )

  return { submit, text, isStreaming, error, toolCalls, reset }
}
