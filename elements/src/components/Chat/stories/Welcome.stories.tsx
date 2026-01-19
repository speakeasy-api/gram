import type { Meta, StoryFn } from '@storybook/react-vite'
import { Chat } from '..'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Welcome',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
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
            prompt: 'Write a SQL query to find top customers',
          },
        ],
      },
    },
  },
}
