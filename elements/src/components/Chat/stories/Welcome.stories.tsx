import React from 'react'
import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Welcome',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
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

export const CustomMessage: Story = () => <Chat />
CustomMessage.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        title: 'Hello there!',
        subtitle: "How can I serve your organization's needs today?",
        suggestions: [
          {
            title: 'Write a SQL query',
            label: 'to find top customers',
            action: 'Write a SQL query to find top customers',
          },
        ],
      },
    },
  },
}
