import React from 'react'
import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Model',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
} satisfies Meta<typeof Chat>

export default meta

type Story = StoryFn<typeof Chat>

export const ModelPicker: Story = () => <Chat />
ModelPicker.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      model: { showModelPicker: true },
    },
  },
}
