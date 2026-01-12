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
import { useMemo, useState, useRef, useCallback, useEffect } from 'react'
import { ElementsContext } from './contexts'
import {
  clearFrontendToolApprovalConfig,
  getEnabledTools,
  setFrontendToolApprovalConfig,
  toAISDKTools,
  wrapToolsWithApproval,
  type ApprovalHelpers,
} from '@/lib/tools'
import { useSession } from '@/hooks/useSession'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useMCPTools } from '@/hooks/useMCPTools'
import { ToolApprovalProvider } from './ToolApprovalContext'
import { useToolApproval } from '@/hooks/useToolApproval'

const GRAM_API_URL = 'https://localhost:8080'

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

async function defaultGetSession(init: {
  projectSlug: string
}): Promise<string> {
  const response = await fetch('/chat/session', {
    method: 'POST',
    headers: {
      'Gram-Project': init.projectSlug,
    },
  })
  const data = await response.json()
  return data.client_token
}

// Internal state for chat history management, shared via context
interface ChatHistoryState {
  chatId: string | null
  setChatId: (id: string | null) => void
  isLoadingChat: boolean
  setIsLoadingChat: (loading: boolean) => void
  initialMessages: UIMessage[]
  setInitialMessages: (messages: UIMessage[]) => void
  runtimeKey: number
  setRuntimeKey: (updater: (k: number) => number) => void
}

// Props for the inner runtime component
interface ChatRuntimeProviderProps {
  children: React.ReactNode
  config: ElementsProviderProps['config']
  session: string | null
  toolApproval: ReturnType<typeof useToolApproval>
  chatHistoryState: ChatHistoryState
  getSession: ElementsProviderProps['getSession']
}

// Inner component that creates the runtime - keyed to force re-creation
const ChatRuntimeProvider = ({
  children,
  config,
  session,
  toolApproval,
  chatHistoryState,
  getSession = defaultGetSession,
}: ChatRuntimeProviderProps) => {
  const {
    chatId,
    setChatId,
    isLoadingChat,
    setIsLoadingChat,
    initialMessages,
    setInitialMessages,
    setRuntimeKey,
  } = chatHistoryState

  // Use ref to access current chatId in transport without re-creating it
  const chatIdRef = useRef<string | null>(null)
  chatIdRef.current = chatId

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

        // Generate a chat ID if we don't have one yet (first message of a new conversation)
        let currentChatId = chatIdRef.current
        if (!currentChatId) {
          currentChatId = crypto.randomUUID()
          setChatId(currentChatId)
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
                'Gram-Chat-ID': currentChatId,
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

  // Start a new chat by clearing the chat ID and resetting the thread
  const startNewChat = useCallback(() => {
    setChatId(null)
    setInitialMessages([])
    setRuntimeKey((k) => k + 1)
  }, [setChatId, setInitialMessages, setRuntimeKey])

  // Load a chat by ID, fetching its messages and populating the thread
  const loadChat = useCallback(
    async (chatIdToLoad: string) => {
      if (!session) {
        throw new Error('No session found')
      }

      setIsLoadingChat(true)
      try {
        const response = await fetch(
          `${GRAM_API_URL}/rpc/chat.load?id=${encodeURIComponent(chatIdToLoad)}`,
          {
            headers: {
              'Gram-Project': config.projectSlug,
              'Gram-Chat-Session': session,
            },
          }
        )

        if (!response.ok) {
          throw new Error(`Failed to load chat: ${response.statusText}`)
        }

        const chat = await response.json()

        // Convert server messages to ThreadMessageLike format
        // ThreadMessageLike requires: role, content, and optionally id, createdAt, status
        // Note: Server uses snake_case (created_at), we need camelCase (createdAt)
        const messages = (chat.messages ?? [])
          .filter(
            (msg: { role: string }) =>
              msg.role === 'user' || msg.role === 'assistant'
          )
          .map(
            (msg: {
              id: string
              role: 'user' | 'assistant'
              content?: string
              created_at: string
            }) => ({
              id: msg.id,
              role: msg.role,
              content: msg.content ?? '',
              createdAt: msg.created_at ? new Date(msg.created_at) : undefined,
              // Assistant messages need a status to indicate they're complete
              status: msg.role === 'assistant' ? { type: 'complete' } : undefined,
            })
          )

        console.log('Loaded chat with messages:', messages.length)

        // Convert to UIMessage format for the runtime
        type LoadedMessage = (typeof messages)[number]
        const uiMessages: UIMessage[] = messages.map((msg: LoadedMessage) => ({
          id: msg.id,
          role: msg.role as 'user' | 'assistant',
          content: msg.content,
          createdAt: msg.createdAt ?? new Date(),
          parts: [{ type: 'text' as const, text: msg.content }],
        }))

        // Set initial messages and increment key to force runtime re-creation
        setInitialMessages(uiMessages)
        setRuntimeKey((k) => k + 1)

        // Set the chat ID so future messages are added to this chat
        setChatId(chatIdToLoad)
      } finally {
        setIsLoadingChat(false)
      }
    },
    [session, config.projectSlug, setChatId, setIsLoadingChat, setInitialMessages, setRuntimeKey]
  )

  const runtime = useChatRuntime({
    transport,
    messages: initialMessages.length > 0 ? initialMessages : undefined,
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
          chatId,
          startNewChat,
          loadChat,
          isLoadingChat,
        }}
      >
        {children}

        {/* Doesn't render anything, but is used to register frontend tools */}
        <FrontendTools tools={config.tools?.frontendTools ?? {}} />
      </ElementsContext.Provider>
    </AssistantRuntimeProvider>
  )
}

// Outer wrapper that manages chat history state and keys the inner component
const ElementsProviderWithApproval = ({
  children,
  config,
  getSession = defaultGetSession,
}: ElementsProviderProps) => {
  const session = useSession({ getSession, projectSlug: config.projectSlug })
  const toolApproval = useToolApproval()

  // Track the current chat ID for persistence
  const [chatId, setChatId] = useState<string | null>(null)

  // Track loading state for chat history
  const [isLoadingChat, setIsLoadingChat] = useState(false)

  // Initial messages for loading historical chats
  const [initialMessages, setInitialMessages] = useState<UIMessage[]>([])

  // Key to force runtime component re-creation when loading a different chat
  const [runtimeKey, setRuntimeKey] = useState(0)

  const chatHistoryState: ChatHistoryState = {
    chatId,
    setChatId,
    isLoadingChat,
    setIsLoadingChat,
    initialMessages,
    setInitialMessages,
    runtimeKey,
    setRuntimeKey,
  }

  return (
    <ChatRuntimeProvider
      key={runtimeKey}
      config={config}
      session={session}
      toolApproval={toolApproval}
      chatHistoryState={chatHistoryState}
      getSession={getSession}
    >
      {children}
    </ChatRuntimeProvider>
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
