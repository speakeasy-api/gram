import { FrontendTools } from '@/components/FrontendTools'
import { MODELS } from '@/lib/models'
import { recommended } from '@/plugins'
import { ElementsProviderProps, Model } from '@/types'
import { Plugin } from '@/types/plugins'
import { experimental_createMCPClient as createMCPClient } from '@ai-sdk/mcp'
import { AssistantRuntimeProvider } from '@assistant-ui/react'
import { useChatRuntime } from '@assistant-ui/react-ai-sdk'
import { createOpenRouter } from '@openrouter/ai-sdk-provider'
import {
  convertToModelMessages,
  smoothStream,
  stepCountIs,
  streamText,
  type ChatTransport,
  type UIMessage,
} from 'ai'
import { useMemo, useState } from 'react'
import { ElementsContext } from './elementsContextType'

const GRAM_API_URL = 'https://app.getgram.ai'
const HEADER_PREFIX = 'MCP-'

const BASE_SYSTEM_PROMPT = `You are a helpful assistant that can answer questions and help with tasks.`

function mergeInternalSystemPromptWith(
  userSystemPrompt: string | undefined,
  plugins: Plugin[]
) {
  return `
  ${BASE_SYSTEM_PROMPT}

  User-provided System Prompt:
  ${userSystemPrompt ?? 'None provided'}

  Utilities:
  ${plugins.map((plugin) => `- ${plugin.language}: ${plugin.prompt}`).join('\n')}`
}

function transformEnvironmentToHeaders(environment: Record<string, unknown>) {
  if (typeof environment !== 'object' || environment === null) {
    return {}
  }
  return Object.entries(environment).reduce(
    (acc, [key, value]) => {
      // Normalize key: replace underscores with dashes
      const normalizedKey = key.replace(/_/g, '-')

      // Add MCP- prefix if it doesn't already have it
      const headerKey = normalizedKey.startsWith(HEADER_PREFIX)
        ? normalizedKey
        : `${HEADER_PREFIX}${normalizedKey}`

      acc[headerKey] = value as string
      return acc
    },
    {} as Record<string, string>
  )
}

export const ElementsProvider = ({
  children,
  config,
}: ElementsProviderProps) => {
  const session = config.clientToken
  
  const [model, setModel] = useState<Model>(
    config.model?.defaultModel ?? MODELS[0]
  )
  const [isExpanded, setIsExpanded] = useState(
    config.modal?.defaultExpanded ?? false
  )
  const [isOpen, setIsOpen] = useState(config.modal?.defaultOpen)

  // If there are any user provided plugins, use them, otherwise use the recommended plugins
  const plugins = config.plugins ?? recommended

  const systemPrompt = mergeInternalSystemPromptWith(
    config.systemPrompt,
    plugins
  )

  // Create custom transport
  const transport = useMemo<ChatTransport<UIMessage>>(
    () => ({
      sendMessages: async ({ messages, abortSignal }) => {
        if (!session) {
          throw new Error('No session found')
        }

        // TODO: FIX ME
        // const clientTools = getEnabledTools(
        //   frontendTools(config.tools?.frontendTools ?? {})
        // ) as any

        // Create MCP client
        // TODO: Don't do this every time we send a message
        const mcpClient = await createMCPClient({
          transport: {
            type: 'http',
            url: config.mcp,
            headers: {
              ...transformEnvironmentToHeaders(config.environment ?? {}),
              'Gram-Chat-Session': session,
            },
          },
        })

        const mcpTools = await mcpClient.tools()

        // Create OpenRouter model
        const openRouterModel = createOpenRouter({
          baseURL: GRAM_API_URL,
          apiKey: 'unused, but must be set',
          headers: {
            'Gram-Project': config.projectSlug,
            'Gram-Chat-Session': session,
          },
        })

        // Stream the response
        const result = streamText({
          system: systemPrompt,
          model: openRouterModel.chat(model),
          messages: convertToModelMessages(messages),
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          tools: {
            ...mcpTools,
            // ...frontendTools(clientTools),
          } as any,
          stopWhen: stepCountIs(10),
          experimental_transform: smoothStream({ delayInMs: 15 }),
          abortSignal,
        })

        return result.toUIMessageStream()
      },
      reconnectToStream: async () => {
        // Not implemented for client-side streaming
        throw new Error('Stream reconnection not supported')
      },
    }),
    [config, model, systemPrompt, session]
  )

  const runtime = useChatRuntime({
    transport,
  })

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <ElementsContext.Provider
        value={{
          config,
          setModel,
          model,
          isExpanded,
          setIsExpanded,
          isOpen: isOpen ?? false,
          setIsOpen,
          plugins,
        }}
      >
        {children}

        {/* Doesn't render anything, but is used to register frontend tools */}
        <FrontendTools tools={config.tools?.frontendTools ?? {}} />
      </ElementsContext.Provider>
    </AssistantRuntimeProvider>
  )
}
