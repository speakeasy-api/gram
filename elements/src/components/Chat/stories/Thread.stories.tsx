import type { Meta, StoryFn } from '@storybook/react-vite'
import { Chat } from '..'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Thread',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
} satisfies Meta<typeof Chat>

export default meta

type Story = StoryFn<typeof Chat>

/**
 * Demonstrates follow-up suggestions that appear after the assistant responds.
 * Send a message and watch as AI-generated follow-up questions appear below the response.
 */
export const FollowUpSuggestions: Story = () => <Chat />
FollowUpSuggestions.parameters = {
  elements: {
    config: {
      variant: 'widget',
      modal: {
        defaultOpen: true,
      },
      systemPrompt:
        'You are a helpful assistant. Keep ALL responses extremely brief - 1-2 sentences only. No lists, no elaboration.',
      welcome: {
        title: 'Explore Paris',
        subtitle: 'Ask me anything about the City of Light',
        suggestions: [
          {
            title: 'Cool places',
            label: 'to visit in Paris',
            prompt: 'Tell me about cool places to visit in Paris',
          },
        ],
      },
      thread: {
        followUpSuggestions: true,
      },
    },
  },
}

/**
 * Shows the thread with follow-up suggestions disabled.
 * No suggestions will appear after the assistant responds.
 */
export const FollowUpSuggestionsDisabled: Story = () => <Chat />
FollowUpSuggestionsDisabled.parameters = {
  elements: {
    config: {
      variant: 'widget',
      modal: {
        defaultOpen: true,
      },
      systemPrompt:
        'You are a helpful assistant. Keep ALL responses extremely brief - 1-2 sentences only. No lists, no elaboration.',
      welcome: {
        title: 'Explore Paris',
        subtitle: 'Ask me anything about the City of Light',
        suggestions: [
          {
            title: 'Cool places',
            label: 'to visit in Paris',
            prompt: 'Tell me about cool places to visit in Paris',
          },
        ],
      },
      thread: {
        followUpSuggestions: false,
      },
    },
  },
}
