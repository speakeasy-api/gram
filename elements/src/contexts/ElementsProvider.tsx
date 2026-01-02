import { MODELS } from '@/lib/models'
import { ElementsProviderProps, Model } from '@/types'
import { AssistantRuntimeProvider } from '@assistant-ui/react'
import {
  AssistantChatTransport,
  useChatRuntime,
} from '@assistant-ui/react-ai-sdk'
import { useState } from 'react'
import { ElementsContext } from './elementsContextType'
import { Plugin } from '@/types/plugins'
import { recommended } from '@/plugins'
import { getEnabledTools, toAISDKTools } from '@/lib/tools'
import { FrontendTools } from '@/components/FrontendTools'
import { lastAssistantMessageIsCompleteWithToolCalls } from 'ai'

const DEFAULT_CHAT_ENDPOINT = '/chat/completions'

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

export const ElementsProvider = ({
  children,
  config,
}: ElementsProviderProps) => {
  const [model, setModel] = useState<Model>(
    config.model?.defaultModel ?? MODELS[0]
  )
  const [isExpanded, setIsExpanded] = useState(
    config.modal?.defaultExpanded ?? false
  )
  const [isOpen, setIsOpen] = useState(config.modal?.defaultOpen)

  // If there are any user provided plugins, use them, otherwise use the recommended plugins
  const plugins = config.plugins ?? recommended
  const runtime = useChatRuntime({
    sendAutomaticallyWhen: lastAssistantMessageIsCompleteWithToolCalls,
    transport: new AssistantChatTransport({
      api: config.chatEndpoint ?? DEFAULT_CHAT_ENDPOINT,
      // Because we override prepareSendMessagesRequest, we need to manually
      // pass the system prompt to the server (usually this would work with
      // useAssistantInstructions but we need to pass custom config across which
      // clobbers the super implementation of prepareSendMessagesRequest)
      prepareSendMessagesRequest: ({ id, messages }) => {
        const context = runtime.thread.getModelContext()
        const tools = toAISDKTools(getEnabledTools(context?.tools ?? {}))
        return {
          body: {
            messages,
            system: mergeInternalSystemPromptWith(config.systemPrompt, plugins),
            id,
            tools,
            config: {
              mcp: config.mcp,
              environment: config.environment,
              projectSlug: config.projectSlug,
              model,
            },
          },
        }
      },
    }),
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
