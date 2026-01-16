import React from 'react'
import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'
import { defineFrontendTool } from '../../../lib/tools'
import z from 'zod'

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
            label: 'Get a salutation',
            action: 'Get a salutation',
          },
        ],
      },
      tools: {
        toolsRequiringApproval: ['kitchen_sink_get_salutation'],
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
          toolName.endsWith('salutation'),
      },
      welcome: {
        suggestions: [
          {
            title: 'Call a tool requiring approval',
            label: 'Get a salutation',
            action: 'Get a salutation',
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
            label: 'Call both tools requiring approval',
            action:
              'Call both kitchen_sink_get_salutation and kitchen_sink_get_get_card_details',
          },
        ],
      },
      tools: {
        toolsRequiringApproval: [
          'kitchen_sink_get_salutation',
          'kitchen_sink_get_get_card_details',
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
            action: 'Delete file with ID 123',
          },
        ],
      },
    },
  },
}
