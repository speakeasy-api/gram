import React from 'react'
import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Variants',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
  args: {
    projectSlug:
      import.meta.env.VITE_GRAM_ELEMENTS_STORYBOOK_PROJECT_SLUG ?? '',
    mcpUrl: import.meta.env.VITE_GRAM_ELEMENTS_STORYBOOK_MCP_URL ?? '',
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
