import { MODELS } from '@/lib/models'
import {
  AssistantTool,
  ImageMessagePartComponent,
  ReasoningGroupComponent,
  ReasoningMessagePartComponent,
  TextMessagePartComponent,
  ToolCallMessagePartComponent,
} from '@assistant-ui/react'
import { LanguageModel } from 'ai'
import {
  ComponentType,
  Dispatch,
  PropsWithChildren,
  SetStateAction,
  type ReactNode,
} from 'react'
import type { Plugin } from './plugins'

/**
 * Function to retrieve the session token from the backend endpoint.
 * Override this if you have mounted your session endpoint at a different path.
 */
export type GetSessionFn = (init: { projectSlug: string }) => Promise<string>

type ServerUrl = string

export const VARIANTS = ['widget', 'sidecar', 'standalone'] as const
export type Variant = (typeof VARIANTS)[number]

/**
 * The top level configuration object for the Elements library.
 *
 * @example
 * const config: ElementsConfig = {
 *   mcp: 'https://app.getgram.ai/mcp/your-mcp-slug',
 *   projectSlug: 'my-project',
 *   systemPrompt: 'You are a helpful assistant.',
 * }
 */
export interface ElementsConfig {
  /**
   * The system prompt to use for the Elements library.
   */
  systemPrompt?: string

  /**
   * Any plugins to use for the Elements library.
   *
   * @default import { recommended } from '@gram-ai/elements/plugins'
   */
  plugins?: Plugin[]

  /**
   * Override the default components used by the Elements library.
   *
   * The available components are:
   * - Composer
   * - UserMessage
   * - EditComposer
   * - AssistantMessage
   * - ThreadWelcome
   * - Text
   * - Image
   * - ToolFallback
   * - Reasoning
   * - ReasoningGroup
   * - ToolGroup
   *
   * To understand how to override these components, please consult the [assistant-ui documentation](https://www.assistant-ui.com/docs).
   *
   * @example
   * const config: ElementsConfig = {
   *   components: {
   *     Composer: CustomComposerComponent,
   *   },
   * }
   */
  components?: ComponentOverrides

  /**
   * The project slug to use for the Elements library.
   *
   * Your project slug can be found within the Gram dashboard.
   *
   * @example
   * const config: ElementsConfig = {
   *   projectSlug: 'your-project-slug',
   * }
   */
  projectSlug: string

  /**
   * The Gram Server URL to use for the Elements library.
   * Can be retrieved from https://app.getgram.ai/{team}/{project}/mcp/{mcp_slug}
   *
   * Note: This config option will likely change in the future
   *
   * @example
   * const config: ElementsConfig = {
   *   mcp: 'https://app.getgram.ai/mcp/your-mcp-slug',
   * }
   */
  mcp?: ServerUrl

  /**
   * Custom environment variable overrides for the Elements library.
   * Will be used to override the environment variables for the MCP server.
   *
   * For more documentation on passing through different kinds of environment variables, including bearer tokens, see the [Gram documentation](https://www.speakeasy.com/docs/gram/host-mcp/public-private-servers#pass-through-authentication).
   */
  environment?: Record<string, unknown>

  /**
   * The layout variant for the chat interface.
   *
   * - `widget`: A popup modal anchored to the bottom-right corner (default)
   * - `sidecar`: A side panel that slides in from the right edge of the screen
   * - `standalone`: A full-page chat experience
   *
   * @default 'widget'
   */
  variant?: Variant

  /**
   * LLM model configuration.
   *
   * @example
   * const config: ElementsConfig = {
   *   model: {
   *     defaultModel: 'openai/gpt-4o',
   *     showModelPicker: true,
   *   },
   * }
   */
  model?: ModelConfig

  /**
   * Visual appearance configuration options.
   * Similar to OpenAI ChatKit's ThemeOption.\
   *
   * @example
   * const config: ElementsConfig = {
   *   theme: {
   *     colorScheme: 'dark',
   *     density: 'compact',
   *     radius: 'round',
   *   },
   * }
   */
  theme?: ThemeConfig

  /**
   * The configuration for the welcome message and initial suggestions.
   *
   * @example
   * const config: ElementsConfig = {
   *   welcome: {
   *     title: 'Welcome to the chat',
   *     subtitle: 'This is a chat with a bot',
   *     suggestions: [
   *       { title: 'Suggestion 1', label: 'Suggestion 1', action: 'action1' },
   *     ],
   *   },
   * }
   */
  welcome?: WelcomeConfig

  /**
   * The configuration for the composer.
   *
   * @example
   * const config: ElementsConfig = {
   *   composer: {
   *     placeholder: 'Enter your message...',
   *   },
   * }
   */
  composer?: ComposerConfig

  /**
   * Optional property to override the LLM provider. If you override the model,
   * then logs & usage metrics will not be tracked directly via Gram.
   *
   * Please ensure that you are using an AI SDK v2 compatible model (e.g a
   * Vercel AI sdk provider in the v2 semver range), as this is the only variant
   * compatible with AI SDK V5
   *
   * Example with Google Gemini:
   * ```ts
   * import { google } from '@ai-sdk/google';
   *
   * const googleGemini = google('gemini-3-pro-preview');
   *
   * const config: ElementsConfig = {
   *   {other options}
   *   languageModel: googleGemini,
   * }
   * ```
   */
  languageModel?: LanguageModel

  /**
   * The configuration for the modal window.
   * Only applicable if variant is 'widget'.
   *
   * @example
   * const config: ElementsConfig = {
   *   modal: {
   *     title: 'Chat',
   *     position: 'bottom-right',
   *     expandable: true,
   *     defaultExpanded: false,
   *     dimensions: {
   *       default: {
   *         width: 400,
   *         height: 600,
   *       },
   *     },
   *   },
   * }
   */
  modal?: ModalConfig

  /**
   * The configuration for the sidecar panel.
   * Only applies if variant is 'sidecar'.
   *
   * @example
   * const config: ElementsConfig = {
   *   sidecar: {
   *     title: 'Chat',
   *     expandable: true,
   *     defaultExpanded: false,
   *     dimensions: {
   *       default: {
   *         width: 400,
   *         height: 600,
   *       },
   *     },
   *   },
   * }
   */
  sidecar?: SidecarConfig

  /**
   * The configuration for the tools.
   *
   * @example
   * const config: ElementsConfig = {
   *   tools: {
   *     expandToolGroupsByDefault: true,
   *     frontendTools: {
   *       fetchUrl: FetchTool,
   *     },
   *     components: {
   *       fetchUrl: FetchToolComponent,
   *     },
   *   },
   * }
   */
  tools?: ToolsConfig

  api?: {
    /**
     * The Gram API URL to use for the Elements library.
     *
     * @example
     * const config: ElementsConfig = {
     *   apiURL: 'https://api.getgram.ai',
     * }
     */
    url?: string
  } & AuthConfig
}

export type AuthConfig =
  | {
      /**
       * The function to use to retrieve the session token from the backend endpoint.
       * By default, this will attempt to fetch the session token from `/chat/session`.
       *
       * @example
       * const config: ElementsConfig = {
       *   api: {
       *     sessionFn: async () => {
       *       return fetch('/chat/session').then(res => res.json()).then(data => data.client_token)
       *     },
       *   },
       * }
       */
      sessionFn?: GetSessionFn
    }
  | {
      /**
       * The API key to use if you haven't yet configured a session endpoint.
       * Do not use this in production.
       *
       * @example
       * const config: ElementsConfig = {
       *   api: {
       *     UNSAFE_apiKey: 'your-api-key',
       *   },
       * }
       */
      UNSAFE_apiKey: string
    }

/**
 * The LLM model to use for the Elements library.
 *
 * @example
 * const config: ElementsConfig = {
 *   model: {
 *     defaultModel: 'openai/gpt-4o',
 *   },
 * }
 */
export type Model = (typeof MODELS)[number]

/**
 * ModelConfig is used to configure model support in the Elements library.
 *
 */
export interface ModelConfig {
  /**
   * Whether to show the model picker in the composer.
   */
  showModelPicker?: boolean

  /**
   * The default model to use for the Elements library.
   */
  defaultModel?: Model
}

export const DENSITIES = ['compact', 'normal', 'spacious'] as const
export type Density = (typeof DENSITIES)[number]
export const COLOR_SCHEMES = ['light', 'dark', 'system'] as const
export type ColorScheme = (typeof COLOR_SCHEMES)[number]

export const RADII = ['round', 'soft', 'sharp'] as const
export type Radius = (typeof RADII)[number]

/**
 * ThemeConfig provides visual appearance customization options.
 * Inspired by OpenAI ChatKit's ThemeOption.
 *
 * @example
 * const config: ElementsConfig = {
 *   theme: {
 *     colorScheme: 'dark',
 *     density: 'compact',
 *     radius: 'round',
 *   },
 * }
 */
export interface ThemeConfig {
  /**
   * The color scheme to use for the UI.
   * @default 'light'
   */
  colorScheme?: ColorScheme

  /**
   * Determines the overall spacing of the UI.
   * - `compact`: Reduced padding and margins for dense layouts
   * - `normal`: Standard spacing (default)
   * - `spacious`: Increased padding and margins for airy layouts
   * @default 'normal'
   */
  density?: Density

  /**
   * Determines the overall roundness of the UI.
   * - `round`: Large border radius
   * - `soft`: Moderate border radius (default)
   * - `sharp`: Minimal border radius
   * @default 'soft'
   */
  radius?: Radius
}

export interface ComponentOverrides {
  /**
   * The component to use for the composer (the input area where users type messages)
   */
  Composer?: ComponentType
  /**
   * The component to use for the user message.
   */
  UserMessage?: ComponentType
  /**
   * The component to use for the edit composer (inline message editor)
   */
  EditComposer?: ComponentType
  /**
   * The component to use for the assistant message (messages generated by the LLM).
   *
   * Note: if you override this, the Text component will not be used.
   */
  AssistantMessage?: ComponentType
  /**
   * The component to use for the thread welcome.
   */
  ThreadWelcome?: ComponentType

  // MessagePrimitive.Parts components
  /**
   * The component to use for the text message.
   */
  Text?: TextMessagePartComponent
  /**
   * The component to use for the image message.
   */
  Image?: ImageMessagePartComponent
  /**
   * The component to use for the tool fallback (default UI shown when a tool returns a result).
   */
  ToolFallback?: ToolCallMessagePartComponent
  /**
   * The component to use for the reasoning message.
   */
  Reasoning?: ReasoningMessagePartComponent
  /**
   * The component to use for the reasoning group.
   */
  ReasoningGroup?: ReasoningGroupComponent

  /**
   * The component to use for the tool group (a group of tool calls returned by the LLM in a single message).
   */
  ToolGroup?: ComponentType<
    PropsWithChildren<{ startIndex: number; endIndex: number }>
  >
}

/**
 * ToolsConfig is used to configure tool support in the Elements library.
 * At the moment, you can override the default React components used by
 * individual tool results.
 *
 * @example
 * const config: ElementsConfig = {
 *   tools: {
 *     components: {
 *       "get_current_weather": WeatherComponent,
 *     },
 *   },
 * }
 */

export interface ToolsConfig {
  /**
   * Whether individual tool calls within a group should be expanded by default.
   * @default false
   */
  expandToolGroupsByDefault?: boolean

  /**
   * `components` can be used to override the default components used by the
   * Elements library for a given tool result.
   *
   * Please ensure that the tool name directly matches the tool name in your Gram toolset.
   *
   * @example
   * const config: ElementsConfig = {
   *   tools: {
   *     components: {
   *       "get_current_weather": WeatherComponent,
   *     },
   *   },
   * }
   */
  components?:
    | Record<string, ToolCallMessagePartComponent | undefined>
    | undefined

  /**
   * The frontend tools to use for the Elements library.
   *
   * @example
   * ```ts
   * import { defineFrontendTool } from '@gram-ai/elements'
   *
   * const FetchTool = defineFrontendTool<{ url: string }, string>(
   *   {
   *     description: 'Fetch a URL (supports CORS-enabled URLs like httpbin.org)',
   *     parameters: z.object({
   *       url: z.string().describe('URL to fetch (must support CORS)'),
   *     }),
   *     execute: async ({ url }) => {
   *       const response = await fetch(url as string)
   *       const text = await response.text()
   *       return text
   *     },
   *   },
   *   'fetchUrl'
   * )
   * const config: ElementsConfig = {
   *   tools: {
   *     frontendTools: {
   *       fetchUrl: FetchTool,
   *     },
   *   },
   * }
   * ```
   *
   * You can also override the default components used by the
   * Elements library for a given tool result.
   *
   * @example
   * ```ts
   * import { FetchToolComponent } from './components/FetchToolComponent'
   *
   * const config: ElementsConfig = {
   *   tools: {
   *     frontendTools: {
   *       fetchUrl: FetchTool,
   *     },
   *     components: {
   *       'fetchUrl': FetchToolComponent, // will override the default component used by the Elements library for the 'fetchUrl' tool
   *     },
   *   },
   * }
   * ```
   */
  frontendTools?: Record<string, AssistantTool>

  /**
   * List of tool names that require confirmation from the end user before
   * being executed. The user can choose to approve once or approve for the
   * entire session via the UI.
   *
   * @example
   * ```ts
   * tools: {
   *   toolsRequiringApproval: ['delete_file', 'send_email'],
   * }
   * ```
   */
  toolsRequiringApproval?: string[]
}

export interface WelcomeConfig {
  /**
   * The welcome message to display when the thread is empty.
   */
  title: string

  /**
   * The subtitle to display when the thread is empty.
   */
  subtitle: string

  /**
   * The suggestions to display when the thread is empty.
   */
  suggestions?: Suggestion[]
}

export interface Suggestion {
  title: string
  label: string
  action: string
}

export interface Dimensions {
  default: Dimension
  expanded?: {
    width: number | string
    height: number | string
  }
}

export interface Dimension {
  width: number | string
  height: number | string
  maxHeight?: number | string
}

interface ExpandableConfig {
  /**
   * Whether the modal or sidecar can be expanded
   */
  expandable?: boolean

  /**
   * Whether the modal or sidecar should be expanded by default.
   * @default false
   */
  defaultExpanded?: boolean

  /**
   * The dimensions for the modal or sidecar window.
   */
  dimensions?: Dimensions
}

export type ModalTriggerPosition =
  | 'bottom-right'
  | 'bottom-left'
  | 'top-right'
  | 'top-left'

export interface ModalConfig extends ExpandableConfig {
  /**
   * Whether to open the modal window by default.
   */
  defaultOpen?: boolean

  /**
   * The title displayed in the modal header.
   * @default 'Chat'
   */
  title?: string

  /**
   * The position of the modal trigger
   *
   * @default 'bottom-right'
   */
  position?: ModalTriggerPosition

  /**
   * The icon to use for the modal window.
   * Receives the current state of the modal window.
   *
   * @example
   * ```ts
   * import { MessageCircleIcon } from 'lucide-react'
   * import { cn } from '@/lib/utils'
   *
   * const config: ElementsConfig = {
   *   modal: {
   *     icon: (state) => {
   *       return <div className={cn('size-6', state === 'open' ? 'rotate-90' : 'rotate-0')}>
   *         <MessageCircleIcon className="size-6" />
   *       </div>
   *     },
   *   },
   * }
   * ```
   */
  icon?: (state: 'open' | 'closed' | undefined) => ReactNode
}

export interface ComposerConfig {
  /**
   * The placeholder text for the composer input.
   * @default 'Send a message...'
   */
  placeholder?: string

  /**
   * Configuration for file attachments in the composer.
   * Set to `false` to disable attachments entirely.
   * Set to `true` for default attachment behavior.
   * Or provide an object for fine-grained control.
   * @default true
   */
  attachments?: boolean | AttachmentsConfig
}

/**
 * AttachmentsConfig provides fine-grained control over file attachments.
 *
 * Note: not yet implemented. Attachments are not supported yet.
 */
export interface AttachmentsConfig {
  /**
   * Accepted file types. Can be MIME types or file extensions.
   * @example ['image/*', '.pdf', '.docx']
   */
  accept?: string[]

  /**
   * Maximum number of files that can be attached at once.
   * @default 10
   */
  maxCount?: number

  /**
   * Maximum file size in bytes.
   * @default 104857600 (100MB)
   */
  maxSize?: number
}

export interface SidecarConfig extends ExpandableConfig {
  /**
   * The title displayed in the sidecar header.
   * @default 'Chat'
   */
  title?: string
}

/**
 * @internal
 */
export type ElementsContextType = {
  config: ElementsConfig
  setModel: (model: Model) => void
  model: Model
  isExpanded: boolean
  setIsExpanded: Dispatch<SetStateAction<boolean>>
  isOpen: boolean
  setIsOpen: (isOpen: boolean) => void
  plugins: Plugin[]
}
