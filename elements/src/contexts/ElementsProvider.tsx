import { FrontendTools } from '@/components/FrontendTools'
import { ROOT_SELECTOR } from '@/constants/tailwind'
import {
  isLocalThreadId,
  useGramThreadListAdapter,
} from '@/hooks/useGramThreadListAdapter'
import { useMCPTools } from '@/hooks/useMCPTools'
import { useToolApproval } from '@/hooks/useToolApproval'
import { getApiUrl } from '@/lib/api'
import { initErrorTracking, trackError } from '@/lib/errorTracking'
import { MODELS } from '@/lib/models'
import {
  clearFrontendToolApprovalConfig,
  getEnabledTools,
  setFrontendToolApprovalConfig,
  toAISDKTools,
  wrapToolsWithApproval,
  type ApprovalHelpers,
  type FrontendTool,
} from '@/lib/tools'
import { cn } from '@/lib/utils'
import { recommended } from '@/plugins'
import { ElementsConfig, Model } from '@/types'
import { Plugin } from '@/types/plugins'
import {
  AssistantRuntimeProvider,
  AssistantTool,
  useAssistantState,
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
import {
  ConnectionStatusProvider,
  useConnectionStatusOptional,
} from './ConnectionStatusContext'
import { ElementsContext } from './contexts'
import { ToolApprovalProvider } from './ToolApprovalContext'
import { ToolExecutionProvider } from './ToolExecutionContext'

/**
 * Extracts executable tools from frontend tool definitions.
 * Frontend tools created via defineFrontendTool have an unstable_tool property
 * that contains the tool definition with execute function.
 */
function extractExecutableTools(
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  frontendTools: Record<string, FrontendTool<any, any>> | undefined
): Record<
  string,
  { execute?: (args: unknown, options?: unknown) => Promise<unknown> }
> {
  if (!frontendTools) return {}

  return Object.fromEntries(
    Object.entries(frontendTools).map(([name, tool]) => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const toolDef = (tool as any).unstable_tool
      return [
        name,
        {
          execute: toolDef?.execute,
        },
      ]
    })
  )
}

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
const ElementsProviderInner = ({ children, config }: ElementsProviderProps) => {
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

  // Initialize error tracking on mount
  useEffect(() => {
    initErrorTracking({
      enabled: config.errorTracking?.enabled,
      projectSlug: config.projectSlug,
      variant: config.variant,
    })
  }, [])

  // Generate a stable chat ID for server-side persistence (when history is disabled)
  // When history is enabled, the thread adapter manages chat IDs instead
  const chatIdRef = useRef<string | null>(null)

  const { data: mcpTools, mcpHeaders } = useMCPTools({
    auth,
    mcp: config.mcp,
    environment: config.environment ?? {},
    toolsToInclude: config.tools?.toolsToInclude,
    gramEnvironment: config.gramEnvironment,
  })

  // Store approval helpers in ref so they can be used in async contexts
  const approvalHelpersRef = useRef<ApprovalHelpers>({
    requestApproval: toolApproval.requestApproval,
    isToolApproved: toolApproval.isToolApproved,
    whitelistTool: toolApproval.whitelistTool,
  })

  // Connection status for tracking network failures
  const connectionStatus = useConnectionStatusOptional()

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
    if (config.tools?.toolsRequiringApproval) {
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

  // Map to share local thread IDs to UUIDs between adapter and transport (for history mode)
  const localIdToUuidMapRef = useRef(new Map<string, string>())

  // Ref to store the current thread's remoteId, synced from assistant-ui state.
  // This is needed because the runtime object doesn't expose threadListItem.remoteId
  // in a way that's accessible from the transport's sendMessages function.
  const currentRemoteIdRef = useRef<string | null>(null)

  // Create chat transport configuration
  const transport = useMemo<ChatTransport<UIMessage>>(
    () => ({
      sendMessages: async ({ messages, abortSignal }) => {
        const usingCustomModel = !!config.languageModel

        if (auth.isLoading) {
          throw new Error('Session is loading')
        }

        // Get chat ID - use the synced remoteId ref first (history mode),
        // fall back to generated ID (non-history mode)
        let chatId = currentRemoteIdRef.current

        // If we have a valid remoteId (not a local ID), use it directly
        if (chatId && !isLocalThreadId(chatId)) {
          // chatId is already set correctly from the synced ref
        } else if (isLocalThreadId(chatId) || !chatId) {
          // For local thread IDs or no ID, check/generate UUID mapping
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          const runtimeAny = runtimeRef.current as any
          const threadsState = runtimeAny?.threads?.getState?.()
          const localThreadId = (threadsState?.mainThreadId ??
            threadsState?.threadIds?.[0]) as string | undefined

          const lookupKey = chatId ?? localThreadId
          if (lookupKey) {
            const existingUuid = localIdToUuidMapRef.current.get(lookupKey)
            if (existingUuid) {
              chatId = existingUuid
            } else {
              // Generate a new UUID and store the mapping
              const newUuid = crypto.randomUUID()
              localIdToUuidMapRef.current.set(lookupKey, newUuid)
              chatId = newUuid
            }
          }
        }

        if (!chatId) {
          // Non-history mode fallback - use stable chatIdRef
          if (!chatIdRef.current) {
            chatIdRef.current = crypto.randomUUID()
          }
          chatId = chatIdRef.current
        }

        // Mutate the shared headers object so the MCP transport picks up the
        // chat ID on subsequent tool call requests.
        if (chatId) {
          mcpHeaders['Gram-Chat-ID'] = chatId
        }

        const context = runtimeRef.current?.thread.getModelContext()
        const frontendTools = toAISDKTools(
          getEnabledTools(context?.tools ?? {})
        )

        // Include Gram-Chat-ID header for chat persistence and Gram-Environment for environment selection
        const headersWithChatId = {
          ...auth.headers,
          'Gram-Chat-ID': chatId,
          'X-Gram-Source': 'elements',
          ...config.api?.headers, // We do this after X-Gram-Source so the playground can override it
          ...(config.gramEnvironment && {
            'Gram-Environment': config.gramEnvironment,
          }),
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
              trackError(error, { source: 'streaming' })

              // Check if this is a network/connection error
              const isNetworkError =
                error instanceof TypeError ||
                (error instanceof Error &&
                  (error.message.includes('fetch') ||
                    error.message.includes('network') ||
                    error.message.includes('Failed to fetch') ||
                    error.message.includes('NetworkError') ||
                    error.message.includes('ECONNREFUSED') ||
                    error.message.includes('ETIMEDOUT')))

              if (isNetworkError) {
                connectionStatus?.markDisconnected()
              }
            },
          })

          // Mark as connected when stream starts successfully
          connectionStatus?.markConnected()

          return result.toUIMessageStream()
        } catch (error) {
          console.error('Error creating stream:', error)
          trackError(error, { source: 'stream-creation' })

          // Check if this is a network/connection error
          const isNetworkError =
            error instanceof TypeError ||
            (error instanceof Error &&
              (error.message.includes('fetch') ||
                error.message.includes('network') ||
                error.message.includes('Failed to fetch') ||
                error.message.includes('NetworkError') ||
                error.message.includes('ECONNREFUSED') ||
                error.message.includes('ETIMEDOUT')))

          if (isNetworkError) {
            connectionStatus?.markDisconnected()
          }

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
      connectionStatus,
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
      mcpTools,
    }),
    [config, model, isExpanded, isOpen, plugins, mcpTools]
  )

  const frontendTools = config.tools?.frontendTools ?? {}

  // Create combined executable tools for direct tool execution (ActionButton)
  // Uses a simplified type that focuses on the execute function
  type ExecutableToolSet = Record<
    string,
    | { execute?: (args: unknown, options?: unknown) => Promise<unknown> }
    | undefined
  >
  const executableTools = useMemo<ExecutableToolSet>(() => {
    const extractedFrontendTools = extractExecutableTools(
      config.tools?.frontendTools
    )
    // MCP tools and extracted frontend tools both have execute functions
    return {
      ...mcpTools,
      ...extractedFrontendTools,
    } as ExecutableToolSet
  }, [mcpTools, config.tools?.frontendTools])

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
        localIdToUuidMap={localIdToUuidMapRef.current}
        currentRemoteIdRef={currentRemoteIdRef}
        executableTools={executableTools}
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
      executableTools={executableTools}
    >
      {children}
    </ElementsProviderWithoutHistory>
  )
}

// Shared type for executable tools
type ExecutableToolSet = Record<
  string,
  | { execute?: (args: unknown, options?: unknown) => Promise<unknown> }
  | undefined
>

// Separate component for history-enabled mode to avoid conditional hook calls
interface ElementsProviderWithHistoryProps {
  children: ReactNode
  transport: ChatTransport<UIMessage>
  apiUrl: string
  headers: Record<string, string>
  contextValue: React.ContextType<typeof ElementsContext>
  runtimeRef: React.RefObject<ReturnType<typeof useChatRuntime> | null>
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  frontendTools: Record<string, AssistantTool | FrontendTool<any, any>>
  localIdToUuidMap: Map<string, string>
  currentRemoteIdRef: React.RefObject<string | null>
  executableTools: ExecutableToolSet
}

/**
 * Component that syncs the current thread's remoteId to a ref.
 * Must be rendered inside AssistantRuntimeProvider to access the state.
 */
const ThreadIdSync = ({
  remoteIdRef,
}: {
  remoteIdRef: React.RefObject<string | null>
}) => {
  const remoteId = useAssistantState(
    ({ threadListItem }) => threadListItem.remoteId ?? null
  )
  useEffect(() => {
    remoteIdRef.current = remoteId
  }, [remoteId, remoteIdRef])
  return null
}

const ElementsProviderWithHistory = ({
  children,
  transport,
  apiUrl,
  headers,
  contextValue,
  runtimeRef,
  frontendTools,
  localIdToUuidMap,
  currentRemoteIdRef,
  executableTools,
}: ElementsProviderWithHistoryProps) => {
  const threadListAdapter = useGramThreadListAdapter({
    apiUrl,
    headers,
    localIdToUuidMap,
  })
  const initialThreadId = contextValue?.config.history?.initialThreadId

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

  // Switch to initial thread if provided (for shared chat URLs)
  const initialThreadSwitched = useRef(false)
  useEffect(() => {
    if (initialThreadId && !initialThreadSwitched.current) {
      initialThreadSwitched.current = true
      // Use setTimeout to ensure runtime is fully initialized
      const timeoutId = setTimeout(() => {
        runtime.threads.switchToThread(initialThreadId).catch((error) => {
          console.error('Failed to switch to initial thread:', error)
        })
      }, 100)
      return () => clearTimeout(timeoutId)
    }
  }, [initialThreadId, runtime])

  // Get the Provider from our adapter to wrap the content
  const HistoryProvider =
    threadListAdapter.unstable_Provider ??
    (({ children }: { children: React.ReactNode }) => <>{children}</>)

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <ThreadIdSync remoteIdRef={currentRemoteIdRef} />
      <HistoryProvider>
        <ElementsContext.Provider value={contextValue}>
          <ToolExecutionProvider tools={executableTools}>
            <div
              className={cn(
                ROOT_SELECTOR,
                (contextValue?.config.variant === 'standalone' ||
                  contextValue?.config.variant === 'sidecar') &&
                  'h-full'
              )}
            >
              {children}
            </div>
            <FrontendTools tools={frontendTools} />
          </ToolExecutionProvider>
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
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  frontendTools: Record<string, AssistantTool | FrontendTool<any, any>>
  executableTools: ExecutableToolSet
}

const ElementsProviderWithoutHistory = ({
  children,
  transport,
  contextValue,
  runtimeRef,
  frontendTools,
  executableTools,
}: ElementsProviderWithoutHistoryProps) => {
  const runtime = useChatRuntime({ transport })

  // Populate runtimeRef so transport can access thread context
  useEffect(() => {
    runtimeRef.current = runtime
  }, [runtime, runtimeRef])

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <ElementsContext.Provider value={contextValue}>
        <ToolExecutionProvider tools={executableTools}>
          <div
            className={cn(
              ROOT_SELECTOR,
              (contextValue?.config.variant === 'standalone' ||
                contextValue?.config.variant === 'sidecar') &&
                'h-full'
            )}
          >
            {children}
          </div>
          <FrontendTools tools={frontendTools} />
        </ToolExecutionProvider>
      </ElementsContext.Provider>
    </AssistantRuntimeProvider>
  )
}

const queryClient = new QueryClient()

export const ElementsProvider = (props: ElementsProviderProps) => {
  return (
    <QueryClientProvider client={queryClient}>
      <ConnectionStatusProvider>
        <ToolApprovalProvider>
          <ElementsProviderInner {...props} />
        </ToolApprovalProvider>
      </ConnectionStatusProvider>
    </QueryClientProvider>
  )
}
