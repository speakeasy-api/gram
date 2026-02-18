import { MessageFeedback } from '@/components/assistant-ui/message-feedback'
import type { Meta, StoryFn } from '@storybook/react-vite'
import { LazyMotion, domAnimation } from 'motion/react'
import { Chat } from '..'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Message Feedback',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
} satisfies Meta<typeof Chat>

export default meta

type Story = StoryFn<typeof Chat>

/**
 * The feedback buttons appear on the last assistant message after it finishes streaming.
 * Send a message to see the feedback UI animate in.
 */
export const Default: Story = () => <Chat />
Default.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      thread: {
        showFeedback: true,
      },
      welcome: {
        title: 'Message Feedback Demo',
        subtitle: 'Send a message to see the feedback buttons',
        suggestions: [
          {
            title: 'Say hello',
            label: 'A simple greeting',
            prompt: 'Hello!',
          },
        ],
      },
    },
  },
}
Default.decorators = [
  (Story) => (
    <div className="m-auto flex h-screen w-full max-w-3xl flex-col">
      <Story />
    </div>
  ),
]

/**
 * Widget variant with feedback buttons.
 */
export const Widget: Story = () => <Chat />
Widget.parameters = {
  elements: {
    config: {
      variant: 'widget',
      modal: { defaultOpen: true },
      thread: {
        showFeedback: true,
      },
      welcome: {
        title: 'Message Feedback Demo',
        subtitle: 'Send a message to see the feedback buttons',
        suggestions: [
          {
            title: 'Say hello',
            label: 'A simple greeting',
            prompt: 'Hello!',
          },
        ],
      },
    },
  },
}

/**
 * Sidecar variant with feedback buttons.
 */
export const Sidecar: Story = () => (
  <div className="mr-[400px] p-10">
    <h1 className="text-2xl font-bold">Sidecar with Message Feedback</h1>
    <p>The sidebar is always visible on the right.</p>
    <Chat />
  </div>
)
Sidecar.parameters = {
  elements: {
    config: {
      variant: 'sidecar',
      thread: {
        showFeedback: true,
      },
      welcome: {
        title: 'Message Feedback Demo',
        subtitle: 'Send a message to see the feedback buttons',
        suggestions: [
          {
            title: 'Say hello',
            label: 'A simple greeting',
            prompt: 'Hello!',
          },
        ],
      },
    },
  },
}

/**
 * Demonstrates feedback UI combined with follow-up suggestions.
 * After the assistant responds, you'll see both AI-generated follow-up questions
 * and feedback buttons (like/dislike) to mark the conversation as resolved.
 */
export const WithFollowUpSuggestions: Story = () => <Chat />
WithFollowUpSuggestions.parameters = {
  elements: {
    config: {
      variant: 'widget',
      modal: { defaultOpen: true },
      systemPrompt:
        'You are a helpful customer support assistant. Keep ALL responses extremely brief - 1-2 sentences only. No lists, no elaboration.',
      thread: {
        showFeedback: true,
        followUpSuggestions: true,
      },
      welcome: {
        title: 'Support Chat',
        subtitle: 'How can we help you today?',
        suggestions: [
          {
            title: 'Order status',
            label: 'Where is my package?',
            prompt: 'Where is my package?',
          },
          {
            title: 'Returns',
            label: 'How do I return an item?',
            prompt: 'How do I return an item?',
          },
        ],
      },
    },
  },
}

/**
 * Standalone component demo showing the feedback buttons in isolation.
 */
export const ComponentOnly: StoryFn = () => (
  <LazyMotion features={domAnimation}>
    <div className="bg-background flex min-h-screen items-center justify-center p-10">
      <div className="flex flex-col items-center gap-8">
        <h2 className="text-foreground text-lg font-semibold">
          Message Feedback Buttons
        </h2>
        <MessageFeedback
          onFeedback={(type) => {
            console.log('Feedback:', type)
          }}
        />
      </div>
    </div>
  </LazyMotion>
)
ComponentOnly.parameters = {
  layout: 'fullscreen',
}
