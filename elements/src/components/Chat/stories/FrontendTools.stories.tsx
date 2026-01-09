import React from 'react'
import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'
import { ToolCallMessagePartProps } from '@assistant-ui/react'
import { defineFrontendTool, FrontendTool } from '../../../lib/tools'
import z from 'zod'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Frontend Tools',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
  args: {
    projectSlug:
      import.meta.env.VITE_GRAM_ELEMENTS_STORYBOOK_PROJECT_SLUG ?? '',
    mcpUrl: import.meta.env.VITE_GRAM_ELEMENTS_STORYBOOK_MCP_URL ?? '',
  },
  decorators: [
    (Story) => (
      <div className="m-auto flex h-screen w-full max-w-3xl flex-col">
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof Chat>

export default meta

type Story = StoryFn<typeof Chat>

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

export const FetchUrl: Story = () => <Chat />
FetchUrl.storyName = 'Fetch URL Tool'
FetchUrl.parameters = {
  elements: {
    config: {
      variant: 'standalone',
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
