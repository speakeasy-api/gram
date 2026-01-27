import { ToolCallMessagePartProps } from '@assistant-ui/react'
import type { Meta, StoryFn } from '@storybook/react-vite'
import React from 'react'
import z from 'zod'
import { Chat } from '..'
import { defineFrontendTool } from '../../../lib/tools'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Tools',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
} satisfies Meta<typeof Chat>

export default meta

type Story = StoryFn<typeof Chat>

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

export const CustomToolComponent: Story = () => <Chat />
CustomToolComponent.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        suggestions: [
          {
            title: 'Get card details',
            label: 'for your card',
            prompt: 'Get card details for your card number 4532 •••• •••• 1234',
          },
        ],
      },
      tools: {
        components: {
          kitchen_sink_get_get_card_details: CardPinRevealComponent,
        },
      },
    },
  },
}

/**
 * Demonstrates the generativeUI plugin which renders `ui` code blocks
 * as dynamic UI widgets.
 *
 * The LLM outputs JSON in a ```ui code fence, and the plugin renders it
 * using the built-in component catalog (Card, Grid, Metric, Table, etc.)
 */
export const GenerativeUI: Story = () => <Chat />
GenerativeUI.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        title: 'Data Explorer',
        subtitle: 'Ask questions about your data',
        suggestions: [
          {
            title: 'Sales metrics',
            label: 'This month',
            prompt:
              'What are our sales numbers this month? Revenue is $125,000, conversion rate is 3.2%, and we have 1,420 orders.',
          },
          {
            title: 'Team members',
            label: 'Directory',
            prompt:
              'List our team members: Alice (alice@co.com, Admin, Active), Bob (bob@co.com, Editor, Active), Charlie (charlie@co.com, Viewer, Pending)',
          },
          {
            title: 'Project status',
            label: 'Sprint progress',
            prompt:
              'How is our current sprint going? We have 12 tasks total, 8 completed, 3 in progress, and 1 blocked. The team has 4 developers.',
          },
          {
            title: 'Website analytics',
            label: 'Last 7 days',
            prompt:
              "Show me last week's website stats: 45,000 page views, 2.1% bounce rate, 3m 24s average session, top pages are /home, /pricing, /docs",
          },
        ],
      },
    },
  },
}

// Frontend tools for the ActionButton demo
const ApproveRequestTool = defineFrontendTool<{ id: number }, string>(
  {
    description: 'Approve a pending request',
    parameters: z.object({
      id: z.number().describe('The request ID to approve'),
    }),
    execute: async ({ id }) => {
      // Simulate API call
      await new Promise((resolve) => setTimeout(resolve, 500))
      return `Request #${id} has been approved successfully.`
    },
  },
  'approve_request'
)

const RejectRequestTool = defineFrontendTool<
  { id: number; reason?: string },
  string
>(
  {
    description: 'Reject a pending request',
    parameters: z.object({
      id: z.number().describe('The request ID to reject'),
      reason: z.string().optional().describe('Reason for rejection'),
    }),
    execute: async ({ id, reason }) => {
      await new Promise((resolve) => setTimeout(resolve, 500))
      return `Request #${id} has been rejected.${reason ? ` Reason: ${reason}` : ''}`
    },
  },
  'reject_request'
)

const actionTools = {
  approve_request: ApproveRequestTool,
  reject_request: RejectRequestTool,
}

/**
 * Demonstrates ActionButton in generative UI that triggers tool calls.
 *
 * The LLM generates UI with ActionButton components that, when clicked,
 * directly execute the tool without an LLM roundtrip.
 */
export const GenerativeUIWithActions: Story = () => <Chat />
GenerativeUIWithActions.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        title: 'Expense Approvals',
        subtitle: 'Review and process pending requests',
        suggestions: [
          {
            title: 'Pending expenses',
            label: 'Needs review',
            prompt: `I need to review these pending expense requests:

Request #1247: Sarah Chen submitted $450 for conference registration
Request #1248: Mike Johnson submitted $89 for software subscription
Request #1249: Lisa Park submitted $1,200 for client dinner

I need to be able to approve or reject each one.`,
          },
        ],
      },
      tools: {
        frontendTools: actionTools,
      },
    },
  },
}
