import React from 'react'
import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Density',
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

export const Compact: Story = () => <Chat />
Compact.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      theme: { density: 'compact' },
    },
  },
}

export const Normal: Story = () => <Chat />
Normal.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      theme: { density: 'normal' },
    },
  },
}

export const Spacious: Story = () => <Chat />
Spacious.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      theme: { density: 'spacious' },
    },
  },
}
