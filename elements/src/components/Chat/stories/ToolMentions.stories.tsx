import React from 'react'
import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'

const meta: Meta<typeof Chat> = {
  title: 'Chat/ToolMentions',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
} satisfies Meta<typeof Chat>

export default meta

type Story = StoryFn<typeof Chat>

/**
 * Default behavior - tool mentions are enabled automatically when tools are available.
 * Type @ followed by a tool name to see the autocomplete dropdown.
 */
export const Default: Story = () => <Chat />
Default.parameters = {
  elements: {
    config: {
      variant: 'standalone',
    },
  },
}

/**
 * Tool mentions can be explicitly disabled via configuration.
 */
export const Disabled: Story = () => <Chat />
Disabled.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      composer: {
        toolMentions: false,
      },
    },
  },
}

/**
 * Tool mentions with custom configuration.
 * Limits the number of suggestions shown in the dropdown.
 */
export const CustomConfig: Story = () => <Chat />
CustomConfig.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      composer: {
        toolMentions: {
          enabled: true,
          maxSuggestions: 5,
        },
      },
    },
  },
}
