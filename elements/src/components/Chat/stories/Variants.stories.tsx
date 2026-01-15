import { Chat } from '..'
import type { Meta, StoryFn, StoryObj } from '@storybook/react-vite'
import { ThreadList } from '@/components/assistant-ui/thread-list'

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
  <div className="gramel:flex gramel:h-full gramel:w-full gramel:flex-col gramel:gap-4 gramel:p-10">
    <h1 className="gramel:text-2xl gramel:font-bold">Modal example</h1>
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
    <div className="gramel:m-auto gramel:flex gramel:h-screen gramel:w-full gramel:max-w-3xl gramel:flex-col">
      <Story />
    </div>
  ),
]

export const StandaloneWithHistory: StoryObj<typeof Chat> = {
  name: 'Standalone with History',
  args: {},
  render: () => (
    <div className="gramel:bg-background gramel:flex gramel:h-10/12 gramel:max-h-[800px] gramel:w-1/2 gramel:flex-row gramel:gap-4 gramel:overflow-hidden gramel:rounded-lg gramel:border gramel:shadow-xl gramel:sm:w-3/4">
      <ThreadList className="gramel:w-56 gramel:flex-none gramel:shrink-0 gramel:border-r" />
      <Chat className="gramel:flex-3 gramel:grow" />
    </div>
  ),
}
StandaloneWithHistory.decorators = [
  (Story) => (
    <div className="gramel:m-auto gramel:flex gramel:h-screen gramel:w-full gramel:items-center gramel:justify-center gramel:border gramel:bg-linear-to-r gramel:from-violet-600 gramel:to-indigo-800">
      <Story />
    </div>
  ),
]
StandaloneWithHistory.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      history: { enabled: true, showThreadList: true },
    },
  },
}

export const Sidecar: Story = () => (
  <div className="gramel:mr-[400px] gramel:p-10">
    <h1 className="gramel:text-2xl gramel:font-bold">Sidecar Variant</h1>
    <p>The sidebar is always visible on the right.</p>
    <Chat />
  </div>
)
Sidecar.parameters = {
  elements: { config: { variant: 'sidecar' } },
}

export const ModalWithHistory: Story = () => (
  <div className="gramel:flex gramel:h-full gramel:w-full gramel:flex-col gramel:gap-4 gramel:p-10">
    <h1 className="gramel:text-2xl gramel:font-bold">Modal with Chat History</h1>
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
  <div className="gramel:mr-[600px] gramel:p-10">
    <h1 className="gramel:text-2xl gramel:font-bold">Sidecar with Chat History</h1>
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
