import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Sidecar',
  component: Chat,
} satisfies Meta<typeof Chat>

export default meta

export const Sidecar: StoryFn<typeof Chat> = () => {
  return <Chat />
}

Sidecar.parameters = {
  elements: { config: { variant: 'sidecar' } },
}

export const SidecarWithTitle: StoryFn<typeof Chat> = () => {
  return <Chat />
}

SidecarWithTitle.parameters = {
  elements: {
    config: { variant: 'sidecar', sidecar: { title: 'Chat with me' } },
  },
}
