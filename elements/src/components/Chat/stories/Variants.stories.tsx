import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Variants',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
} satisfies Meta<typeof Chat>

export default meta

type Story = StoryFn<typeof Chat>

export const Default: Story = () => (
  <div className="flex h-full w-full flex-col gap-4 p-10">
    <h1 className="text-2xl font-bold">Modal example</h1>
    <p>Click the button in the bottom right corner to open the chat.</p>
    <Chat />
  </div>
)

export const Standalone: Story = () => <Chat />
Standalone.parameters = {
  elements: { config: { variant: 'standalone' } },
}
Standalone.decorators = [
  (Story) => (
    <div className="m-auto flex h-screen w-full max-w-3xl flex-col">
      <Story />
    </div>
  ),
]

export const Sidecar: Story = () => (
  <div className="mr-[400px] p-10">
    <h1 className="text-2xl font-bold">Sidecar Variant</h1>
    <p>The sidebar is always visible on the right.</p>
    <Chat />
  </div>
)
Sidecar.parameters = {
  elements: { config: { variant: 'sidecar' } },
}

export const ModalWithHistory: Story = () => (
  <div className="flex h-full w-full flex-col gap-4 p-10">
    <h1 className="text-2xl font-bold">Modal with Chat History</h1>
    <p>
      Click the button in the bottom right corner. The thread list sidebar shows
      your chat history.
    </p>
    <Chat />
  </div>
)
ModalWithHistory.parameters = {
  elements: {
    config: {
      variant: 'widget',
      modal: {
        defaultOpen: true,
        expandable: true,
        dimensions: {
          default: { width: '700px', height: '600px', maxHeight: '100vh' },
          expanded: { width: '90vw', height: '90vh' },
        },
      },
      history: {
        enabled: true,
        showThreadList: true,
      },
    },
  },
}

export const SidecarWithHistory: Story = () => (
  <div className="mr-[600px] p-10">
    <h1 className="text-2xl font-bold">Sidecar with Chat History</h1>
    <p>The sidecar includes a thread list sidebar for chat history.</p>
    <Chat />
  </div>
)
SidecarWithHistory.parameters = {
  elements: {
    config: {
      variant: 'sidecar',
      history: {
        enabled: true,
        showThreadList: true,
      },
    },
  },
}
