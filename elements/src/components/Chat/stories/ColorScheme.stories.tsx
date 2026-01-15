import React from 'react'
import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Color Scheme',
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
