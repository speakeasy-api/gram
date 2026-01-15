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
  decorators: [
    (Story) => (
      <div className="gramel:m-auto gramel:flex gramel:h-screen gramel:w-full gramel:max-w-3xl gramel:flex-col">
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
    <div className="gramel:my-5 gramel:flex gramel:w-full gramel:flex-col gramel:overflow-hidden gramel:rounded-lg gramel:border gramel:shadow-lg">
      {/* macOS Window Controls Bar */}
      <div className="gramel:bg-muted gramel:flex gramel:flex-col gramel:border-b">
        <div className="gramel:flex gramel:items-center gramel:gap-2 gramel:px-3 gramel:py-2">
          {/* Traffic lights */}
          <div className="gramel:flex gramel:gap-2">
            <div className="gramel:h-3 gramel:w-3 gramel:rounded-full gramel:bg-red-500" />
            <div className="gramel:h-3 gramel:w-3 gramel:rounded-full gramel:bg-yellow-500" />
            <div className="gramel:h-3 gramel:w-3 gramel:rounded-full gramel:bg-green-500" />
          </div>

          {/* Address bar */}
          <div className="gramel:bg-background gramel:mx-4 gramel:flex gramel:flex-1 gramel:items-center gramel:rounded-md gramel:px-3 gramel:py-1">
            <svg
              className="gramel:text-muted-foreground gramel:mr-2 gramel:h-4 gramel:w-4"
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
            <span className="gramel:text-muted-foreground gramel:truncate gramel:text-sm">
              {url}
            </span>
          </div>
        </div>

        {/* Loading bar */}
        {isLoading && (
          <div className="gramel:h-0.5 gramel:w-full gramel:overflow-hidden">
            <div
              className="gramel:h-full gramel:w-1/3 gramel:animate-pulse gramel:bg-blue-500"
              style={{
                animation: 'slide 1.5s ease-in-out infinite',
              }}
            />
          </div>
        )}
      </div>

      {/* Content */}
      <div className="gramel:bg-background gramel:h-96">
        <iframe
          srcDoc={result as string}
          className="gramel:h-full gramel:w-full"
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
