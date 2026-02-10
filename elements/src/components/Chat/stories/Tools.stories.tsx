import { ToolCallMessagePartProps } from '@assistant-ui/react'
import type { Meta, StoryFn } from '@storybook/react-vite'
import React, { useState, useCallback } from 'react'
import z from 'zod'
import { Chat } from '..'
import { useToolExecution } from '../../../contexts/ToolExecutionContext'
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

const ProductCardComponent = ({ result }: ToolCallMessagePartProps) => {
  const { executeTool, isToolAvailable } = useToolExecution()
  const [isLoading, setIsLoading] = useState(false)
  const [addedToCart, setAddedToCart] = useState(false)

  // Parse the result to get product details
  let product = {
    id: '',
    name: 'Loading...',
    description: '',
    price: 0,
    category: '',
    rating: 0,
    reviewCount: 0,
    imageUrl: '',
    inStock: true,
  }

  try {
    if (result) {
      const parsed = typeof result === 'string' ? JSON.parse(result) : result
      if (parsed?.content?.[0]?.text) {
        const content = JSON.parse(parsed.content[0].text)
        product = { ...product, ...content }
      } else if (parsed?.name) {
        product = { ...product, ...parsed }
      }
    }
  } catch {
    // Fallback to default
  }

  const canAddToCart = isToolAvailable('ecommerce_api_add_to_cart')

  const handleAddToCart = useCallback(async () => {
    if (!product.id || !canAddToCart) return

    setIsLoading(true)
    try {
      // HTTP tools from OpenAPI expect body content wrapped in a 'body' field
      const toolResult = await executeTool('ecommerce_api_add_to_cart', {
        body: {
          productId: product.id,
          quantity: 1,
        },
      })

      if (toolResult.success) {
        setAddedToCart(true)
      } else {
        console.error('[ProductCard] Tool failed:', toolResult.error)
      }
    } catch (err) {
      console.error('[ProductCard] Exception:', err)
    } finally {
      setIsLoading(false)
    }
  }, [product.id, canAddToCart, executeTool])

  return (
    <div className="my-4 w-80">
      <div className="overflow-hidden rounded-xl bg-white shadow-lg dark:bg-slate-800">
        {/* Product Image */}
        <div className="relative h-48 bg-gradient-to-br from-indigo-100 to-purple-100 dark:from-indigo-900 dark:to-purple-900">
          {product.imageUrl ? (
            <img
              src={product.imageUrl}
              alt={product.name}
              className="h-full w-full object-cover"
            />
          ) : (
            <div className="flex h-full items-center justify-center">
              <span className="text-6xl">📦</span>
            </div>
          )}
          {!product.inStock && (
            <div className="absolute top-2 right-2 rounded-full bg-red-500 px-2 py-1 text-xs font-semibold text-white">
              Out of Stock
            </div>
          )}
        </div>

        {/* Product Details */}
        <div className="p-4">
          <div className="mb-1 text-xs font-medium tracking-wide text-indigo-500 uppercase dark:text-indigo-400">
            {product.category}
          </div>
          <h3 className="mb-2 text-lg font-bold text-slate-900 dark:text-white">
            {product.name}
          </h3>
          <p className="mb-3 line-clamp-2 text-sm text-slate-600 dark:text-slate-300">
            {product.description}
          </p>

          {/* Rating */}
          <div className="mb-3 flex items-center gap-1">
            <div className="flex">
              {[1, 2, 3, 4, 5].map((star) => (
                <span
                  key={star}
                  className={
                    star <= Math.round(product.rating)
                      ? 'text-yellow-400'
                      : 'text-slate-300'
                  }
                >
                  ★
                </span>
              ))}
            </div>
            <span className="text-sm text-slate-500">
              ({product.reviewCount} reviews)
            </span>
          </div>

          {/* Price and Add to Cart */}
          <div className="flex items-center justify-between">
            <span className="text-2xl font-bold text-slate-900 dark:text-white">
              ${product.price?.toFixed(2)}
            </span>
            <button
              onClick={handleAddToCart}
              disabled={
                isLoading || addedToCart || !canAddToCart || !product.inStock
              }
              className={`rounded-lg px-4 py-2 text-sm font-semibold text-white transition-colors ${
                addedToCart
                  ? 'bg-green-500'
                  : isLoading
                    ? 'bg-indigo-400'
                    : 'bg-indigo-600 hover:bg-indigo-700'
              } disabled:cursor-not-allowed disabled:opacity-50`}
            >
              {addedToCart
                ? '✓ Added'
                : isLoading
                  ? 'Adding...'
                  : 'Add to Cart'}
            </button>
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
            title: 'Get product details',
            label: 'View a product',
            prompt: 'List products and then show me details for the first one',
          },
        ],
      },
      tools: {
        components: {
          ecommerce_api_get_product: ProductCardComponent,
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
