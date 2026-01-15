import React from 'react'
import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Density',
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
