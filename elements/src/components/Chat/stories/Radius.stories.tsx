import React from 'react'
import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Radius',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
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
