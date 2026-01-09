import React from 'react'
import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Color Scheme',
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

export const Light: Story = () => <Chat />
Light.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      theme: { colorScheme: 'light' },
    },
  },
}

export const Dark: Story = () => <Chat />
Dark.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      theme: { colorScheme: 'dark' },
    },
  },
}

export const System: Story = () => <Chat />
System.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      theme: { colorScheme: 'system' },
    },
  },
}
