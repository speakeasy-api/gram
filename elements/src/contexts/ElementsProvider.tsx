import { FrontendTools } from '@/components/FrontendTools'
import { MODELS } from '@/lib/models'
import { recommended } from '@/plugins'
import { ElementsProviderProps, Model } from '@/types'
import { Plugin } from '@/types/plugins'
import { AssistantRuntimeProvider } from '@assistant-ui/react'
import {
  frontendTools as convertFrontendToolsToAISDKTools,
  useChatRuntime,
} from '@assistant-ui/react-ai-sdk'
import { createOpenRouter } from '@openrouter/ai-sdk-provider'
import {
  convertToModelMessages,
  smoothStream,
  stepCountIs,
  streamText,
  ToolSet,
  type ChatTransport,
  type UIMessage,
} from 'ai'
import { useMemo, useState } from 'react'
import { ElementsContext } from './elementsContextType'
import { getEnabledTools, toAISDKTools } from '@/lib/tools'
import { useSession } from '@/hooks/useSession'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useMCPTools } from '@/hooks/useMCPTools'

const GRAM_API_URL = 'https://app.getgram.ai'

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

async function defaultGetSession(): Promise<string> {
  const response = await fetch('/chat/session', { method: 'POST' })
  const data = await response.json()
  return data.client_token
}

const ElementsProviderInner = ({
  children,
  config,
  getSession = defaultGetSession,
}: ElementsProviderProps) => {
  const session = useSession({ getSession, projectSlug: config.projectSlug })

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

  const { data: mcpTools, isLoading: mcpToolsLoading } = useMCPTools({
    getSession,
    projectSlug: config.projectSlug,
    mcp: config.mcp,
    environment: config.environment ?? {},
  })

  // Show loading if we don't have tools yet or they're actively loading
  const isLoadingMCPTools = !mcpTools || mcpToolsLoading

  // Create custom transport
  const transport = useMemo<ChatTransport<UIMessage> | undefined>(
    () => ({
      sendMessages: async ({ messages, abortSignal }) => {
        if (!session) {
          throw new Error('No session found')
        }

        if (mcpToolsLoading || !mcpTools) {
          throw new Error('MCP tools are still being discovered')
        }

        const context = runtime.thread.getModelContext()
        const frontendTools = toAISDKTools(
          getEnabledTools(context?.tools ?? {})
        )

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
          tools: {
            ...mcpTools,
            ...convertFrontendToolsToAISDKTools(frontendTools),
          } as ToolSet,
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
    [config, model, systemPrompt, session, mcpTools, mcpToolsLoading]
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
          isLoadingMCPTools,
        }}
      >
        {children}

        {/* Doesn't render anything, but is used to register frontend tools */}
        <FrontendTools tools={config.tools?.frontendTools ?? {}} />
      </ElementsContext.Provider>
    </AssistantRuntimeProvider>
  )
}

export const ElementsProvider = (props: ElementsProviderProps) => {
  const queryClient = new QueryClient()
  return (
    <QueryClientProvider client={queryClient}>
      <ElementsProviderInner {...props} />
    </QueryClientProvider>
  )
}
