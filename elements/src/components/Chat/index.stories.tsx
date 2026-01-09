import React, { useState } from 'react'
import { Chat } from '.'
import { ElementsProvider } from '../../contexts/ElementsProvider'
import type { Meta, StoryFn } from '@storybook/react-vite'
import { ZapIcon } from 'lucide-react'
import { google } from '@ai-sdk/google'
import {
  ToolCallMessagePartProps,
  useAssistantState,
} from '@assistant-ui/react'
import {
  COLOR_SCHEMES,
  ColorScheme,
  ComponentOverrides,
  DENSITIES,
  Density,
  ElementsConfig,
  RADII,
  Radius,
  Variant,
  VARIANTS,
} from '../../types'
import { recommended } from '../../plugins'
import { defineFrontendTool, FrontendTool } from '../../lib/tools'
import z from 'zod'

const meta: Meta<typeof Chat> = {
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
  args: {
    projectSlug:
      import.meta.env.VITE_GRAM_ELEMENTS_STORYBOOK_PROJECT_SLUG ?? '',
    mcpUrl: import.meta.env.VITE_GRAM_ELEMENTS_STORYBOOK_MCP_URL ?? '',
  },
  argTypes: {
    projectSlug: {
      control: 'text',
    },
    mcpUrl: {
      control: 'text',
    },
  },
} satisfies Meta<typeof Chat>

export default meta

type Story = StoryFn<typeof Chat>

export const Default: Story = () => (
  <div className="flex h-full w-full flex-col gap-4 p-10">
    <h1 className="text-2xl font-bold">Modal example</h1>

    <p>Click the button in the bottom right corner to open the chat.</p>
    <Chat />
  </div>
)

const baseConfig: ElementsConfig = {
  projectSlug: '', // will come from story controls
  mcp: '', // will come from story controls
  welcome: {
    title: 'Hello!',
    subtitle: 'How can I help you today?',
    suggestions: [
      {
        title: 'Search',
        label: 'in your data',
        action: 'Search for recent activity',
      },
      {
        title: 'Write a report',
        label: 'from your metrics',
        action: 'Write a summary report of this month',
      },
      {
        title: 'Analyze trends',
        label: 'in your business',
        action: 'Analyze recent trends',
      },
      {
        title: 'Get recommendations',
        label: 'for improvement',
        action: 'Give me recommendations',
      },
    ],
  },
  modal: {
    defaultOpen: true,
    title: 'Gram Elements Demo',
    expandable: true,
  },
  plugins: recommended,
}

// Playground with Storybook controls for all theme options
interface PlaygroundArgs {
  // Theme
  colorScheme: ColorScheme
  density: Density
  radius: Radius
  // Layout
  variant: Variant
  // Start screen
  greeting: string
  subtitle: string
  starterPrompts: 'none' | 'some' | 'many'
  // Composer
  placeholder: string
  attachments: boolean
  // Modal/Sidecar
  headerTitle: string
}

const starterPromptOptions = {
  none: [],
  some: [
    {
      title: 'Search for anything',
      label: 'in your data',
      action: 'Search for recent activity',
    },
    {
      title: 'Write a report',
      label: 'from your metrics',
      action: 'Write a summary report of this month',
    },
  ],
  many: [
    {
      title: 'Search for anything',
      label: 'in your data',
      action: 'Search for recent activity',
    },
    {
      title: 'Write a report',
      label: 'from your metrics',
      action: 'Write a summary report of this month',
    },
    {
      title: 'Analyze trends',
      label: 'in your business',
      action: 'Analyze recent trends',
    },
    {
      title: 'Get recommendations',
      label: 'for improvement',
      action: 'Give me recommendations',
    },
  ],
}

export const ThemePlayground: StoryFn<PlaygroundArgs> = (args) => {
  const config: ElementsConfig = {
    projectSlug: 'demo',
    mcp: 'https://chat.speakeasy.com/mcp/speakeasy-team-my_api',
    variant: args.variant,
    theme: {
      colorScheme: args.colorScheme,
      density: args.density,
      radius: args.radius,
    },
    welcome: {
      title: args.greeting,
      subtitle: args.subtitle,
      suggestions: starterPromptOptions[args.starterPrompts],
    },
    composer: {
      placeholder: args.placeholder,
      attachments: args.attachments,
    },
    modal: {
      title: args.headerTitle,
      defaultOpen: args.variant === 'widget',
    },
    sidecar: {
      title: args.headerTitle,
    },
  }

  // Determine if dark mode should be applied
  const isDark =
    args.colorScheme === 'dark' ||
    (args.colorScheme === 'system' &&
      typeof window !== 'undefined' &&
      window.matchMedia('(prefers-color-scheme: dark)').matches)

  return (
    <ElementsProvider key={JSON.stringify(config)} config={config}>
      <div
        className={`min-h-screen ${args.variant === 'sidecar' ? 'mr-[400px]' : ''} ${isDark ? 'dark bg-background text-foreground' : ''}`}
      >
        <div className="p-10">
          <h1 className="text-2xl font-bold">Theme Playground</h1>
          <p className="text-muted-foreground mt-2">
            Use the controls panel to customize the chat appearance.
          </p>
        </div>
        <Chat />
      </div>
    </ElementsProvider>
  )
}
ThemePlayground.args = {
  // Theme defaults
  colorScheme: 'light',
  density: 'normal',
  radius: 'soft',
  // Layout
  variant: 'widget',
  // Start screen
  greeting: 'Hello!',
  subtitle: 'How can I help you today?',
  starterPrompts: 'some',
  // Composer
  placeholder: 'Send a message...',
  attachments: true,
}
ThemePlayground.argTypes = {
  colorScheme: {
    control: 'inline-radio',
    options: COLOR_SCHEMES,
    description: 'The color scheme for the UI',
    table: { category: 'Theme' },
  },
  density: {
    control: 'select',
    options: DENSITIES,
    description: 'Controls the overall spacing of the UI',
    table: { category: 'Theme' },
  },
  radius: {
    control: 'select',
    options: RADII,
    description: 'Controls the roundness of UI elements',
    table: { category: 'Theme' },
  },
  variant: {
    control: 'inline-radio',
    options: VARIANTS,
    description: 'The layout variant',
    table: { category: 'Layout' },
  },
  greeting: {
    control: 'text',
    description: 'The main welcome message',
    table: { category: 'Start Screen' },
  },
  subtitle: {
    control: 'text',
    description: 'Secondary welcome text',
    table: { category: 'Start Screen' },
  },
  starterPrompts: {
    control: 'select',
    options: ['none', 'some', 'many'],
    description: 'Suggested prompts shown on the start screen',
    table: { category: 'Start Screen' },
  },
  placeholder: {
    control: 'text',
    description: 'Placeholder text in the composer input',
    table: { category: 'Composer' },
  },
  attachments: {
    control: 'boolean',
    description: 'Enable file attachments',
    table: { category: 'Composer' },
  },
  headerTitle: {
    control: 'text',
    description: 'Title shown in the modal/sidecar header',
    table: { category: 'Header' },
  },
}
ThemePlayground.parameters = {
  layout: 'fullscreen',
  elements: false, // Disable the global decorator for this story
}

export const VariantPlayground: Story = () => {
  const [variant, setVariant] = useState<Variant>('widget')

  return (
    <ElementsProvider
      key={variant}
      config={{
        ...baseConfig,
        variant,
      }}
    >
      <div className="min-h-screen">
        {/* Variant switcher */}
        <div className="fixed top-4 left-4 z-9999 flex gap-2">
          {VARIANTS.map((v) => (
            <button
              key={v}
              onClick={() => setVariant(v)}
              className={`rounded px-3 py-1.5 text-sm font-medium transition-colors ${
                variant === v
                  ? 'bg-blue-600 text-white'
                  : 'border bg-white text-gray-700 shadow-sm hover:bg-gray-50'
              }`}
            >
              {v}
            </button>
          ))}
        </div>

        <Chat />
      </div>
    </ElementsProvider>
  )
}
VariantPlayground.parameters = {
  layout: 'fullscreen',
  elements: false, // Disable the global decorator for this story
}

export const Standalone: Story = () => <Chat />
Standalone.parameters = {
  elements: { config: { variant: 'standalone' } },
}
Standalone.decorators = [
  (Story) => (
    <div className="m-auto flex h-screen w-full max-w-3xl flex-col">
      <Story />
    </div>
  ),
]

export const Sidecar: Story = () => (
  <div className="mr-[400px] p-10">
    <h1 className="text-2xl font-bold">Sidecar Variant</h1>
    <p>The sidebar is always visible on the right.</p>
    <Chat />
  </div>
)
Sidecar.parameters = {
  elements: { config: { variant: 'sidecar' } },
}

export const WithCustomModalIcon: Story = () => <Chat />
WithCustomModalIcon.parameters = {
  elements: {
    config: {
      modal: {
        defaultOpen: false,
        icon: (state: 'open' | 'closed' | undefined) => (
          <ZapIcon
            data-state={state}
            className="aui-modal-button-closed-icon absolute transition-all data-[state=closed]:scale-100 data-[state=closed]:rotate-0 data-[state=open]:scale-0 data-[state=open]:rotate-90"
          />
        ),
      },
    },
  },
}

export const WithCustomComposerPlaceholder: Story = () => <Chat />
WithCustomComposerPlaceholder.parameters = {
  elements: {
    config: {
      composer: { placeholder: 'What would you like to know?' },
    },
  },
}

export const WithAttachmentsDisabled: Story = () => <Chat />
WithAttachmentsDisabled.parameters = {
  elements: {
    config: {
      composer: { attachments: false },
    },
  },
}

export const WithCustomWelcomeMessage: Story = () => <Chat />
WithCustomWelcomeMessage.parameters = {
  elements: {
    config: {
      welcome: {
        title: 'Hello there!',
        subtitle: "How can I serve your organization's needs today?",
        suggestions: [
          {
            title: 'Write a SQL query',
            label: 'to find top customers',
            action: 'Write a SQL query to find top customers',
          },
        ],
      },
    },
  },
}

const CardPinRevealComponent = ({
  result,
  argsText,
}: ToolCallMessagePartProps) => {
  const [isFlipped, setIsFlipped] = React.useState(false)

  // Parse the result to get the pin
  let pin = '****'
  try {
    if (result) {
      const parsed = typeof result === 'string' ? JSON.parse(result) : result
      if (parsed?.content?.[0]?.text) {
        const content = JSON.parse(parsed.content[0].text)
        pin = content.pin || '****'
      } else if (parsed?.pin) {
        pin = parsed.pin
      }
    }
  } catch {
    // Fallback to default
  }

  const args = JSON.parse(argsText || '{}')
  const cardNumber = args?.queryParameters?.cardNumber || '4532 •••• •••• 1234'
  const cardHolder = 'JOHN DOE'
  const expiry = '12/25'
  const cvv = '123'

  if (!cardNumber) {
    return null
  }

  return (
    <div className="my-4 perspective-[1000px]">
      <div
        className={`relative h-48 w-80 cursor-pointer transition-transform duration-700 [transform-style:preserve-3d] ${
          isFlipped ? 'transform-[rotateY(180deg)]' : ''
        }`}
        onClick={() => setIsFlipped(!isFlipped)}
      >
        {/* Front of card */}
        <div className="absolute inset-0 backface-hidden">
          <div className="relative h-full w-full overflow-hidden rounded-xl bg-gradient-to-br from-indigo-600 via-purple-600 to-pink-500 p-6 text-white shadow-2xl">
            {/* Card pattern overlay */}
            <div className="absolute inset-0 opacity-10">
              <div className="absolute -top-10 -right-10 h-40 w-40 rounded-full bg-white"></div>
              <div className="absolute -bottom-10 -left-10 h-32 w-32 rounded-full bg-white"></div>
            </div>

            {/* Card content */}
            <div className="relative z-10 flex h-full flex-col justify-between">
              <div className="flex items-center justify-between">
                <div className="text-2xl font-bold">VISA</div>
                <div className="h-8 w-12 rounded bg-white/20"></div>
              </div>

              <div className="space-y-2">
                <div className="font-mono text-2xl tracking-wider">
                  {cardNumber}
                </div>
                <div className="flex items-center justify-between text-sm">
                  <div>
                    <div className="text-xs opacity-70">CARDHOLDER</div>
                    <div className="font-semibold">{cardHolder}</div>
                  </div>
                  <div>
                    <div className="text-xs opacity-70">EXPIRES</div>
                    <div className="font-semibold">{expiry}</div>
                  </div>
                </div>
              </div>
            </div>

            {/* Click hint */}
            <div className="absolute right-2 bottom-2 text-xs opacity-50">
              Click to flip
            </div>
          </div>
        </div>

        {/* Back of card */}
        <div className="absolute inset-0 transform-[rotateY(180deg)] backface-hidden">
          <div className="relative h-full w-full overflow-hidden rounded-xl bg-gradient-to-br from-slate-800 via-slate-700 to-slate-900 p-6 text-white shadow-2xl">
            {/* Magnetic strip */}
            <div className="absolute top-8 right-0 left-0 h-12 bg-black"></div>

            {/* Card content */}
            <div className="relative z-10 flex h-full flex-col justify-between">
              <div className="mt-16 space-y-4">
                <div className="flex items-center gap-2">
                  <div className="h-8 flex-1 rounded bg-white/10 px-3 py-2 text-right font-mono text-sm">
                    {cvv}
                  </div>
                  <div className="text-xs opacity-70">CVV</div>
                </div>

                {/* PIN Display */}
                <div className="mt-6 space-y-2">
                  <div className="text-xs opacity-70">PIN</div>
                  <div className="flex items-center gap-3">
                    <div className="flex h-16 w-16 items-center justify-center rounded-lg bg-gradient-to-br from-yellow-400 to-orange-500 shadow-lg">
                      <span className="text-2xl font-bold text-white">
                        {pin}
                      </span>
                    </div>
                    <div className="text-xs opacity-60">
                      Keep this PIN secure
                    </div>
                  </div>
                </div>
              </div>

              <div className="flex items-center justify-between text-xs opacity-50">
                <div>VISA</div>
                <div>{cardNumber}</div>
              </div>
            </div>

            {/* Click hint */}
            <div className="absolute bottom-2 left-2 text-xs opacity-50">
              Click to flip back
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

export const WithCustomTools: Story = () => <Chat />
WithCustomTools.parameters = {
  elements: {
    config: {
      welcome: {
        suggestions: [
          {
            title: 'Get card details',
            label: 'for your card',
            action: 'Get card details for your card number 4532 •••• •••• 1234',
          },
        ],
      },
      tools: {
        components: {
          my_api_get_get_card_details: CardPinRevealComponent,
        },
      },
    },
  },
}

export const WithModelPicker: Story = () => <Chat />
WithModelPicker.parameters = {
  elements: {
    config: {
      model: { showModelPicker: true },
    },
  },
}

export const WithExpandableModal: Story = () => <Chat />
WithExpandableModal.parameters = {
  elements: {
    config: {
      modal: {
        expandable: true,
        dimensions: {
          default: { width: '500px', height: '600px', maxHeight: '100vh' },
          expanded: { width: '80vw', height: '90vh' },
        },
      },
    },
  },
}

export const WithCustomModalTriggerPositionTopRight: Story = () => <Chat />
WithCustomModalTriggerPositionTopRight.parameters = {
  elements: {
    config: {
      modal: { position: 'top-right' },
    },
  },
}

export const WithCustomModalTriggerPositionBottomRight: Story = () => <Chat />
WithCustomModalTriggerPositionBottomRight.parameters = {
  elements: {
    config: {
      modal: { position: 'bottom-right' },
    },
  },
}

export const WithCustomModalTriggerPositionBottomLeft: Story = () => <Chat />
WithCustomModalTriggerPositionBottomLeft.parameters = {
  elements: {
    config: {
      modal: { position: 'bottom-left' },
    },
  },
}

export const WithCustomModalTriggerPositionTopLeft: Story = () => <Chat />
WithCustomModalTriggerPositionTopLeft.parameters = {
  elements: {
    config: {
      modal: { position: 'top-left' },
    },
  },
}

export const WithCustomSystemPrompt: Story = () => <Chat />
WithCustomSystemPrompt.parameters = {
  elements: {
    config: {
      systemPrompt: 'Please speak like a pirate',
    },
  },
}

const countryData = JSON.stringify({
  countries: [
    { name: 'USA', gdp: 22000 },
    { name: 'Canada', gdp: 16000 },
    { name: 'Mexico', gdp: 10000 },
  ],
})

export const WithChartPlugin: Story = () => <Chat />
WithChartPlugin.parameters = {
  elements: {
    config: {
      welcome: {
        suggestions: [
          {
            title: 'Create a chart',
            label: 'Visualize your data',
            action: `Create a bar chart for the following country + GDP data:
            ${countryData}
            `,
          },
        ],
      },
    },
  },
}

const customComponents: ComponentOverrides = {
  Text: () => {
    const message = useAssistantState(({ message }) => message)
    return (
      <div className="text-red-500">
        {message.parts
          .map((part) => (part.type === 'text' ? part.text : ''))
          .join('')}
      </div>
    )
  },
}
export const WithCustomComponentOverrides: Story = () => <Chat />
WithCustomComponentOverrides.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      components: customComponents,
    },
  },
}

const FetchTool = defineFrontendTool<{ url: string }, string>(
  {
    description: 'Fetch a URL (supports CORS-enabled URLs like httpbin.org)',
    parameters: z.object({
      url: z.string().describe('URL to fetch (must support CORS)'),
    }),
    execute: async ({ url }) => {
      try {
        const response = await fetch(url as string)
        const text = await response.text()
        return text
      } catch (error) {
        return `Error fetching ${url}: ${error instanceof Error ? error.message : 'Unknown error'}. Note: URL must support CORS for browser requests.`
      }
    },
  },
  'fetchUrl'
)
const frontendTools: Record<string, FrontendTool<{ url: string }, string>> = {
  fetchUrl: FetchTool,
}

// Render OS X style browser window with the fetched URL html rendered
const FetchToolComponent = ({ result, args }: ToolCallMessagePartProps) => {
  const url = (args as { url?: string })?.url || 'about:blank'
  const [isLoading, setIsLoading] = React.useState(true)

  return (
    <div className="my-5 flex w-full flex-col overflow-hidden rounded-lg border shadow-lg">
      {/* macOS Window Controls Bar */}
      <div className="bg-muted flex flex-col border-b">
        <div className="flex items-center gap-2 px-3 py-2">
          {/* Traffic lights */}
          <div className="flex gap-2">
            <div className="h-3 w-3 rounded-full bg-red-500" />
            <div className="h-3 w-3 rounded-full bg-yellow-500" />
            <div className="h-3 w-3 rounded-full bg-green-500" />
          </div>

          {/* Address bar */}
          <div className="bg-background mx-4 flex flex-1 items-center rounded-md px-3 py-1">
            <svg
              className="text-muted-foreground mr-2 h-4 w-4"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z"
              />
            </svg>
            <span className="text-muted-foreground truncate text-sm">
              {url}
            </span>
          </div>
        </div>

        {/* Loading bar */}
        {isLoading && (
          <div className="h-0.5 w-full overflow-hidden">
            <div
              className="h-full w-1/3 animate-pulse bg-blue-500"
              style={{
                animation: 'slide 1.5s ease-in-out infinite',
              }}
            />
          </div>
        )}
      </div>

      {/* Content */}
      <div className="bg-background h-96">
        <iframe
          srcDoc={result as string}
          className="h-full w-full"
          onLoad={() => setIsLoading(false)}
        />
      </div>

      <style>{`
        @keyframes slide {
          0% { transform: translateX(-100%); }
          100% { transform: translateX(400%); }
        }
      `}</style>
    </div>
  )
}

export const WithFrontendTools: Story = () => <Chat />
WithFrontendTools.parameters = {
  elements: {
    config: {
      welcome: {
        title: '',
        subtitle: '',
        suggestions: [
          {
            title: 'Fetch a URL',
            label: 'Fetch a URL',
            action: 'Fetch https://httpbin.org/html',
          },
        ],
      },
      tools: {
        frontendTools,
        components: {
          fetchUrl: FetchToolComponent,
        },
      },
    },
  },
}

// NOTE: add Gemini API key to .env.local with the key VITE_GOOGLE_GENERATIVE_AI_API_KEY
export const WithCustomLanguageModel: Story = () => <Chat />
WithCustomLanguageModel.parameters = {
  elements: {
    config: {
      welcome: {
        title: 'Using Google Gemini',
        subtitle: 'Using Google Gemini 3 Flash Preview',
        suggestions: [
          {
            title: 'Generate a chart',
            label: 'Generate a chart',
            action: 'Generate a chart of these values: 1, 2, 3, 4, 5',
          },
          {
            title: 'Call all tools',
            label: 'Call all tools',
            action: 'Call all tools',
          },
        ],
      },
      languageModel: google('gemini-3-flash-preview'),
    },
  },
}

export const WithToolsRequiringApproval: Story = () => <Chat />
WithToolsRequiringApproval.parameters = {
  elements: {
    config: {
      welcome: {
        suggestions: [
          {
            title: 'Call a tool requiring approval',
            label: 'Get a salutation',
            action: 'Get a salutation',
          },
        ],
      },
      tools: {
        toolsRequiringApproval: ['my_api_get_salutation'],
      },
    },
  },
}

// This story demonstrates the tool approval UI when multiple tools are grouped together.
export const WithMultipleGroupedToolsRequiringApproval: Story = () => <Chat />
WithMultipleGroupedToolsRequiringApproval.parameters = {
  elements: {
    config: {
      welcome: {
        suggestions: [
          {
            title: 'Call both tools requiring approval',
            label: 'Call both tools requiring approval',
            action:
              'Call both my_api_get_salutation and my_api_get_get_card_details',
          },
        ],
      },
      tools: {
        toolsRequiringApproval: [
          'my_api_get_salutation',
          'my_api_get_get_card_details',
        ],
      },
    },
  },
}
const deleteFile = defineFrontendTool<{ fileId: string }, string>(
  {
    description: 'Delete a file',
    parameters: z.object({
      fileId: z.string().describe('The ID of the file to delete'),
    }),
    execute: async ({ fileId }) => {
      alert(`File ${fileId} deleted`)
      return `File ${fileId} deleted`
    },
  },
  'deleteFile'
)

export const WithFrontendToolRequiringApproval: Story = () => <Chat />
WithFrontendToolRequiringApproval.parameters = {
  elements: {
    config: {
      welcome: {
        suggestions: [
          {
            title: 'Delete a file',
            label: 'Delete a file',
            action: 'Delete file with ID 123',
          },
        ],
      },
      tools: {
        frontendTools: {
          deleteFile,
        },
        toolsRequiringApproval: ['deleteFile'],
      },
    },
  },
}
