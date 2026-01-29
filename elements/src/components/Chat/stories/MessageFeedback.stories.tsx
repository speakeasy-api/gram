import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'
import { MessageFeedback } from '@/components/assistant-ui/message-feedback'

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
        experimental_showFeedback: true,
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
        experimental_showFeedback: true,
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
        experimental_showFeedback: true,
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
 * Standalone component demo showing the feedback buttons in isolation.
 */
export const ComponentOnly: StoryFn = () => (
  <div className="flex min-h-screen items-center justify-center bg-background p-10">
    <div className="flex flex-col items-center gap-8">
      <h2 className="text-lg font-semibold text-foreground">
        Message Feedback Buttons
      </h2>
      <MessageFeedback
        onFeedback={(type) => {
          console.log('Feedback:', type)
        }}
      />
    </div>
  </div>
)
ComponentOnly.parameters = {
  layout: 'fullscreen',
}
