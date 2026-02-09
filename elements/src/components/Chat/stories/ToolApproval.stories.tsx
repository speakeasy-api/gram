import type { Meta, StoryFn } from '@storybook/react-vite'
import z from 'zod'
import { Chat } from '..'
import { defineFrontendTool } from '../../../lib/tools'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Tool Approval',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
} satisfies Meta<typeof Chat>

export default meta

type Story = StoryFn<typeof Chat>

export const SingleTool: Story = () => <Chat />
SingleTool.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        suggestions: [
          {
            title: 'Call a tool requiring approval',
            label: 'Add to cart',
            prompt: 'List products and add the first one to my cart',
          },
        ],
      },
      tools: {
        toolsRequiringApproval: ['ecommerce_api_add_to_cart'],
      },
    },
  },
}

export const SingleToolWithFunction: Story = () => <Chat />
SingleToolWithFunction.storyName =
  'Single Tool Requiring Approval with Function'
SingleToolWithFunction.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      tools: {
        toolsRequiringApproval: ({ toolName }: { toolName: string }) =>
          toolName.includes('order') || toolName.includes('cart'),
      },
      welcome: {
        suggestions: [
          {
            title: 'Call a tool requiring approval',
            label: 'Create an order',
            prompt: 'List products and create an order for the first one',
          },
        ],
      },
    },
  },
}

export const MultipleGroupedTools: Story = () => <Chat />
MultipleGroupedTools.storyName = 'Multiple Grouped Tools'
MultipleGroupedTools.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        suggestions: [
          {
            title: 'Call both tools requiring approval',
            label: 'Add to cart and create order',
            prompt:
              'List products, add the first one to my cart, and then create an order',
          },
        ],
      },
      tools: {
        toolsRequiringApproval: [
          'ecommerce_api_add_to_cart',
          'ecommerce_api_create_order',
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

export const FrontendTool: Story = () => <Chat />
FrontendTool.storyName = 'Frontend Tool Requiring Approval'
FrontendTool.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        suggestions: [
          {
            title: 'Delete a file',
            label: 'Delete a file',
            prompt: 'Delete file with ID 123',
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

export const FrontendToolWithFunction: Story = () => <Chat />
FrontendToolWithFunction.storyName =
  'Frontend Tool Requiring Approval with Function'
FrontendToolWithFunction.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      tools: {
        frontendTools: {
          deleteFile,
        },
        toolsRequiringApproval: ({ toolName }: { toolName: string }) =>
          toolName.startsWith('delete'),
      },
      welcome: {
        suggestions: [
          {
            title: 'Delete a file',
            label: 'Delete a file',
            prompt: 'Delete file with ID 123',
          },
        ],
      },
    },
  },
}
