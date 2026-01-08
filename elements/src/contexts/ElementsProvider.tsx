import { FrontendTools } from '@/components/FrontendTools'
import { MODELS } from '@/lib/models'
import { recommended } from '@/plugins'
import { ApprovalType, ElementsProviderProps, Model } from '@/types'
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
import { useMemo, useState, useRef, useCallback } from 'react'
import { ElementsContext } from './elementsContextType'
import { getEnabledTools, toAISDKTools } from '@/lib/tools'
import { useSession } from '@/hooks/useSession'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useMCPTools } from '@/hooks/useMCPTools'
import { ToolApprovalProvider } from './ToolApprovalContext'
import { useToolApproval } from '@/hooks/useToolApproval'

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

interface ApprovalHelpers {
  requestApproval: (
    toolName: string,
    toolCallId: string,
    args: unknown
  ) => Promise<boolean>
  isToolApproved: (toolName: string) => boolean
  markToolApproved: (toolName: string) => void
}

interface ToolExecuteContext {
  toolCallId: string
  abortSignal?: AbortSignal
}

function wrapToolsWithApproval(
  tools: ToolSet,
  approvalConfig: Record<string, ApprovalType> | undefined,
  approvalHelpers: ApprovalHelpers
): ToolSet {
  if (!approvalConfig) {
    return tools
  }

  return Object.fromEntries(
    Object.entries(tools).map(([name, tool]) => {
      const approvalType = approvalConfig[name]

      if (!approvalType || approvalType === 'never') {
        return [name, tool]
      }

      const originalExecute = tool.execute
      if (!originalExecute) {
        return [name, tool]
      }

      return [
        name,
        {
          ...tool,
          execute: async (args: unknown, context: ToolExecuteContext) => {
            // Check if already approved (for "once" type)
            if (
              approvalType === 'once' &&
              approvalHelpers.isToolApproved(name)
            ) {
              return originalExecute(args, context)
            }

            // Request approval using the actual toolCallId from the stream
            const approved = await approvalHelpers.requestApproval(
              name,
              context.toolCallId,
              args
            )

            if (!approved) {
              return {
                content: [
                  {
                    type: 'text',
                    text: `Tool "${name}" execution was denied by the user. Please acknowledge this and continue without using this tool's result.`,
                  },
                ],
                isError: true,
              }
            }

            // Mark as approved for "once" type
            if (approvalType === 'once') {
              approvalHelpers.markToolApproved(name)
            }

            return originalExecute(args, context)
          },
        },
      ]
    })
  ) as ToolSet
}

const ElementsProviderWithApproval = ({
  children,
  config,
  getSession = defaultGetSession,
}: ElementsProviderProps) => {
  const session = useSession({ getSession, projectSlug: config.projectSlug })
  const toolApproval = useToolApproval()

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

  // Store approval helpers in ref so they can be used in async contexts
  const approvalHelpersRef = useRef<ApprovalHelpers>({
    requestApproval: toolApproval.requestApproval,
    isToolApproved: toolApproval.isToolApproved,
    markToolApproved: toolApproval.markToolApproved,
  })

  // Keep ref updated
  approvalHelpersRef.current = {
    requestApproval: toolApproval.requestApproval,
    isToolApproved: toolApproval.isToolApproved,
    markToolApproved: toolApproval.markToolApproved,
  }

  // Stable wrapper function that uses the ref
  const getApprovalHelpers = useCallback((): ApprovalHelpers => {
    return {
      requestApproval: (...args) =>
        approvalHelpersRef.current.requestApproval(...args),
      isToolApproved: (...args) =>
        approvalHelpersRef.current.isToolApproved(...args),
      markToolApproved: (...args) =>
        approvalHelpersRef.current.markToolApproved(...args),
    }
  }, [])

  // Create custom transport
  const transport = useMemo<ChatTransport<UIMessage> | undefined>(
    () => ({
      sendMessages: async ({ messages, abortSignal }) => {
        const usingCustomModel = !!config.languageModel

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

        // Create OpenRouter model (only needed when not using custom model)
        const openRouterModel = usingCustomModel
          ? null
          : createOpenRouter({
              baseURL: GRAM_API_URL,
              apiKey: 'unused, but must be set',
              headers: {
                'Gram-Project': config.projectSlug,
                'Gram-Chat-Session': session!,
              },
            })

        if (config.languageModel) {
          console.log('Using custom language model', config.languageModel)
        }

        // Combine tools - MCP tools only available when not using custom model
        const combinedTools: ToolSet = {
          ...mcpTools,
          ...convertFrontendToolsToAISDKTools(frontendTools),
        } as ToolSet

        // Wrap tools that require approval
        const tools = wrapToolsWithApproval(
          combinedTools,
          config.tools?.toolsRequiringApproval,
          getApprovalHelpers()
        )

        // Stream the response
        const modelToUse = config.languageModel
          ? config.languageModel
          : openRouterModel!.chat(model)

        try {
          const result = streamText({
            system: systemPrompt,
            model: modelToUse,
            messages: convertToModelMessages(messages),
            tools,
            stopWhen: stepCountIs(10),
            experimental_transform: smoothStream({ delayInMs: 15 }),
            abortSignal,
            onError: ({ error }) => {
              console.error('Stream error in onError callback:', error)
            },
          })

          return result.toUIMessageStream()
        } catch (error) {
          console.error('Error creating stream:', error)
          throw error
        }
      },
      reconnectToStream: async () => {
        // Not implemented for client-side streaming
        throw new Error('Stream reconnection not supported')
      },
    }),
    [
      config,
      config.languageModel,
      model,
      systemPrompt,
      session,
      mcpTools,
      mcpToolsLoading,
      getApprovalHelpers,
    ]
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
      <ToolApprovalProvider>
        <ElementsProviderWithApproval {...props} />
      </ToolApprovalProvider>
    </QueryClientProvider>
  )
}
