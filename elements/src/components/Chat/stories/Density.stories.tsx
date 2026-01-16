import React from 'react'
import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Density',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
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
