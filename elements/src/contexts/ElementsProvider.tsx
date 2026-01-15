import { FrontendTools } from '@/components/FrontendTools'
import { useMCPTools } from '@/hooks/useMCPTools'
import { useToolApproval } from '@/hooks/useToolApproval'
import { getApiUrl } from '@/lib/api'
import { MODELS } from '@/lib/models'
import {
  clearFrontendToolApprovalConfig,
  getEnabledTools,
  setFrontendToolApprovalConfig,
  toAISDKTools,
  wrapToolsWithApproval,
  type ApprovalHelpers,
} from '@/lib/tools'
import { recommended } from '@/plugins'
import { ElementsConfig, Model } from '@/types'
import { Plugin } from '@/types/plugins'
import {
  AssistantRuntimeProvider,
  AssistantTool,
  unstable_useRemoteThreadListRuntime as useRemoteThreadListRuntime,
} from '@assistant-ui/react'
import {
  frontendTools as convertFrontendToolsToAISDKTools,
  useChatRuntime,
} from '@assistant-ui/react-ai-sdk'
import { createOpenRouter } from '@openrouter/ai-sdk-provider'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  convertToModelMessages,
  smoothStream,
  stepCountIs,
  streamText,
  ToolSet,
  type ChatTransport,
  type UIMessage,
} from 'ai'
import {
  ReactNode,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { useAuth } from '../hooks/useAuth'
import { ElementsContext } from './contexts'
import { ToolApprovalProvider } from './ToolApprovalContext'
import { useGramThreadListAdapter } from '@/hooks/useGramThreadListAdapter'

export interface ElementsProviderProps {
  children: ReactNode
  config: ElementsConfig
}

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

/**
 * Cleans messages before sending to the model to work around an AI SDK bug.
 * Strips callProviderMetadata from all parts (AI SDK bug #9731)
 */
function cleanMessagesForModel(messages: UIMessage[]): UIMessage[] {
  return messages.map((message) => {
    const partsArray = message.parts
    if (!Array.isArray(partsArray)) {
      return message
    }

    // Process each part: strip providerOptions/providerMetadata and filter reasoning
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const cleanedParts = partsArray.map((part: any) => {
      // Strip providerOptions and providerMetadata from all remaining parts
      // eslint-disable-next-line @typescript-eslint/no-unused-vars
      const { callProviderMetadata: _, ...cleanPart } = part
      return cleanPart
    })

    return {
      ...message,
      parts: cleanedParts,
    }
  })
}

/**
 * Main provider component that sets up auth, tools, and transport.
 * Delegates to either WithHistory or WithoutHistory based on config.
 */
const ElementsProviderWithApproval = ({
  children,
  config,
}: ElementsProviderProps) => {
  const apiUrl = getApiUrl(config)
  const auth = useAuth({
    auth: config.api,
    projectSlug: config.projectSlug,
  })
  const toolApproval = useToolApproval()

  const [model, setModel] = useState<Model>(
    config.model?.defaultModel ?? MODELS[0]
  )
  const [isExpanded, setIsExpanded] = useState(
    config.modal?.defaultExpanded ?? false
  )
  const [isOpen, setIsOpen] = useState(config.modal?.defaultOpen)

  const plugins = config.plugins ?? recommended

  const systemPrompt = mergeInternalSystemPromptWith(
    config.systemPrompt,
    plugins
  )

  const { data: mcpTools } = useMCPTools({
    auth,
    mcp: config.mcp,
    environment: config.environment ?? {},
  })

  // Store approval helpers in ref so they can be used in async contexts
  const approvalHelpersRef = useRef<ApprovalHelpers>({
    requestApproval: toolApproval.requestApproval,
    isToolApproved: toolApproval.isToolApproved,
    whitelistTool: toolApproval.whitelistTool,
  })

  approvalHelpersRef.current = {
    requestApproval: toolApproval.requestApproval,
    isToolApproved: toolApproval.isToolApproved,
    whitelistTool: toolApproval.whitelistTool,
  }

  const getApprovalHelpers = useCallback((): ApprovalHelpers => {
    return {
      requestApproval: (...args) =>
        approvalHelpersRef.current.requestApproval(...args),
      isToolApproved: (...args) =>
        approvalHelpersRef.current.isToolApproved(...args),
      whitelistTool: (...args) =>
        approvalHelpersRef.current.whitelistTool(...args),
    }
  }, [])

  // Set up frontend tool approval config for runtime checking
  useEffect(() => {
    if (config.tools?.toolsRequiringApproval?.length) {
      setFrontendToolApprovalConfig(
        getApprovalHelpers(),
        config.tools.toolsRequiringApproval
      )
    }
    return () => {
      clearFrontendToolApprovalConfig()
    }
  }, [config.tools?.toolsRequiringApproval, getApprovalHelpers])

  // Ref to access runtime from within transport's sendMessages.
  // This solves a circular dependency: transport needs runtime.thread.getModelContext(),
  // but runtime is created using transport. The ref gets populated after runtime creation.
  const runtimeRef = useRef<ReturnType<typeof useChatRuntime> | null>(null)

  // Generate a stable chat ID for server-side persistence (when history is disabled)
  // When history is enabled, the thread adapter manages chat IDs instead
  const chatIdRef = useRef<string | null>(null)

  // Map local thread IDs to real UUIDs for consistency between adapter and transport
  // This ensures the same UUID is used when initialize() creates an ID and when sendMessages() persists
  const localIdToUuidRef = useRef<Map<string, string>>(new Map())

  // Track the last chat ID used by sendMessages so the adapter can use the same UUID
  // This handles the case where sendMessages runs before initialize
  const lastUsedChatIdRef = useRef<string | null>(null)

  // Create chat transport configuration
  const transport = useMemo<ChatTransport<UIMessage>>(
    () => ({
      sendMessages: async ({ messages, abortSignal }) => {
        const usingCustomModel = !!config.languageModel

        if (auth.isLoading) {
          throw new Error('Session is loading')
        }

        const context = runtimeRef.current?.thread.getModelContext()
        const frontendTools = toAISDKTools(
          getEnabledTools(context?.tools ?? {})
        )

        // Get chat ID from thread list item (when history enabled) or generate one
        const threadListItemState = (
          runtimeRef.current as unknown as {
            threadListItem?: () => {
              getState: () => { id?: string; remoteId?: string }
            }
          }
        )
          ?.threadListItem?.()
          ?.getState()
        const remoteId = threadListItemState?.remoteId
        const threadId = threadListItemState?.id // This is the __LOCALID_ or actual ID

        // Determine chat ID for server-side persistence
        let chatId: string
        if (remoteId && !remoteId.startsWith('__LOCALID_')) {
          // We have a valid remote UUID from the adapter
          chatId = remoteId
        } else if (threadId && threadId.startsWith('__LOCALID_')) {
          // We have a local thread ID - use it to look up or create a UUID
          // This ensures consistency with the adapter's initialize() method
          const existingUuid = localIdToUuidRef.current.get(threadId)
          if (existingUuid) {
            chatId = existingUuid
          } else {
            // Generate a new UUID and store the mapping
            const newUuid = crypto.randomUUID()
            localIdToUuidRef.current.set(threadId, newUuid)
            chatId = newUuid
          }
        } else {
          // Fallback - generate a UUID and store it for the adapter to use
          // This handles both history-disabled mode and the race condition where
          // sendMessages runs before initialize (threadId is undefined)
          if (!chatIdRef.current) {
            chatIdRef.current = crypto.randomUUID()
          }
          chatId = chatIdRef.current
          // Store this so the adapter's initialize can use the same UUID
          lastUsedChatIdRef.current = chatId
        }

        // Include Gram-Chat-ID header for chat persistence
        const headersWithChatId = {
          ...auth.headers,
          'Gram-Chat-ID': chatId,
        }

        // Create OpenRouter model (only needed when not using custom model)
        const openRouterModel = usingCustomModel
          ? null
          : createOpenRouter({
              baseURL: apiUrl,
              apiKey: 'unused, but must be set',
              headers: headersWithChatId,
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
          // This works around AI SDK bug where these fields cause validation failures
          const cleanedMessages = cleanMessagesForModel(messages)
          const modelMessages = convertToModelMessages(cleanedMessages)

          const result = streamText({
            system: systemPrompt,
            model: modelToUse,
            messages: modelMessages,
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
        throw new Error('Stream reconnection not supported')
      },
    }),
    [
      config.languageModel,
      config.tools?.toolsRequiringApproval,
      model,
      systemPrompt,
      mcpTools,
      getApprovalHelpers,
      apiUrl,
      auth.headers,
      auth.isLoading,
    ]
  )

  const historyEnabled = config.history?.enabled ?? false

  // Shared context value for ElementsContext
  const contextValue = useMemo(
    () => ({
      config,
      setModel,
      model,
      isExpanded,
      setIsExpanded,
      isOpen: isOpen ?? false,
      setIsOpen,
      plugins,
    }),
    [config, model, isExpanded, isOpen, plugins]
  )

  const frontendTools = config.tools?.frontendTools ?? {}

  // Render the appropriate runtime provider based on history config.
  // We use separate components to avoid conditional hook calls.
  if (historyEnabled && !auth.isLoading) {
    return (
      <ElementsProviderWithHistory
        transport={transport}
        apiUrl={apiUrl}
        headers={auth.headers}
        contextValue={contextValue}
        runtimeRef={runtimeRef}
        frontendTools={frontendTools}
        localIdToUuidRef={localIdToUuidRef}
        lastUsedChatIdRef={lastUsedChatIdRef}
      >
        {children}
      </ElementsProviderWithHistory>
    )
  }

  return (
    <ElementsProviderWithoutHistory
      transport={transport}
      contextValue={contextValue}
      runtimeRef={runtimeRef}
      frontendTools={frontendTools}
    >
      {children}
    </ElementsProviderWithoutHistory>
  )
}

// Separate component for history-enabled mode to avoid conditional hook calls
interface ElementsProviderWithHistoryProps {
  children: ReactNode
  transport: ChatTransport<UIMessage>
  apiUrl: string
  headers: Record<string, string>
  contextValue: React.ContextType<typeof ElementsContext>
  runtimeRef: React.RefObject<ReturnType<typeof useChatRuntime> | null>
  frontendTools: Record<string, AssistantTool>
  localIdToUuidRef: React.RefObject<Map<string, string>>
  lastUsedChatIdRef: React.RefObject<string | null>
}

const ElementsProviderWithHistory = ({
  children,
  transport,
  apiUrl,
  headers,
  contextValue,
  runtimeRef,
  frontendTools,
  localIdToUuidRef,
  lastUsedChatIdRef,
}: ElementsProviderWithHistoryProps) => {
  const threadListAdapter = useGramThreadListAdapter({
    apiUrl,
    headers,
    localIdToUuidMap: localIdToUuidRef.current,
    lastUsedChatIdRef,
  })

  // Hook factory for creating the base chat runtime
  const useChatRuntimeHook = useCallback(() => {
    return useChatRuntime({ transport })
  }, [transport])

  const runtime = useRemoteThreadListRuntime({
    adapter: threadListAdapter,
    runtimeHook: useChatRuntimeHook,
  })

  // Populate runtimeRef so transport can access thread context
  useEffect(() => {
    runtimeRef.current = runtime as ReturnType<typeof useChatRuntime>
  }, [runtime, runtimeRef])

  // Get the Provider from our adapter to wrap the content
  const HistoryProvider =
    threadListAdapter.unstable_Provider ??
    (({ children }: { children: React.ReactNode }) => <>{children}</>)

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <HistoryProvider>
        <ElementsContext.Provider value={contextValue}>
          {children}
          <FrontendTools tools={frontendTools} />
        </ElementsContext.Provider>
      </HistoryProvider>
    </AssistantRuntimeProvider>
  )
}

// Separate component for non-history mode to avoid conditional hook calls
interface ElementsProviderWithoutHistoryProps {
  children: ReactNode
  transport: ChatTransport<UIMessage>
  contextValue: React.ContextType<typeof ElementsContext>
  runtimeRef: React.RefObject<ReturnType<typeof useChatRuntime> | null>
  frontendTools: Record<string, AssistantTool>
}

const ElementsProviderWithoutHistory = ({
  children,
  transport,
  contextValue,
  runtimeRef,
  frontendTools,
}: ElementsProviderWithoutHistoryProps) => {
  const runtime = useChatRuntime({ transport })

  // Populate runtimeRef so transport can access thread context
  useEffect(() => {
    runtimeRef.current = runtime
  }, [runtime, runtimeRef])

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <ElementsContext.Provider value={contextValue}>
        {children}
        <FrontendTools tools={frontendTools} />
      </ElementsContext.Provider>
    </AssistantRuntimeProvider>
  )
}

const queryClient = new QueryClient()

export const ElementsProvider = (props: ElementsProviderProps) => {
  return (
    <QueryClientProvider client={queryClient}>
      <ToolApprovalProvider>
        <ElementsProviderWithApproval {...props} />
      </ToolApprovalProvider>
    </QueryClientProvider>
  )
}
