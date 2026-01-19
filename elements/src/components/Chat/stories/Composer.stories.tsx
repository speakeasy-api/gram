import React from 'react'
import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Composer',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
} satisfies Meta<typeof Chat>

export default meta

type Story = StoryFn<typeof Chat>

export const CustomPlaceholder: Story = () => <Chat />
CustomPlaceholder.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      composer: { placeholder: 'What would you like to know?' },
    },
  },
}

export const AttachmentsDisabled: Story = () => <Chat />
AttachmentsDisabled.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      composer: { attachments: false },
    },
  },
}
