import React from 'react'
import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Radius',
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

export const Round: Story = () => <Chat />
Round.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      theme: { radius: 'round' },
    },
  },
}

export const Soft: Story = () => <Chat />
Soft.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      theme: { radius: 'soft' },
    },
  },
}

export const Sharp: Story = () => <Chat />
Sharp.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      theme: { radius: 'sharp' },
    },
  },
}
