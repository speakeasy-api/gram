import React from 'react'
import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'
import { useAssistantState } from '@assistant-ui/react'
import { google } from '@ai-sdk/google'
import { ComponentOverrides } from '../../../types'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Customization',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
  decorators: [
    (Story) => (
      <div className="m-auto flex h-screen w-full max-w-3xl flex-col">
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof Chat>

export default meta

type Story = StoryFn<typeof Chat>

export const SystemPrompt: Story = () => <Chat />
SystemPrompt.storyName = 'Custom System Prompt'
SystemPrompt.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      systemPrompt: 'Please speak like a pirate',
    },
  },
}

const customComponents: ComponentOverrides = {
  Text: () => {
    const message = useAssistantState(({ message }) => message)
    return (
      <div className="text-red-500">
        {message.parts
          .map((part) => (part.type === 'text' ? part.text : ''))
          .join('')}
      </div>
    )
  },
}

export const ComponentOverridesStory: Story = () => <Chat />
ComponentOverridesStory.storyName = 'Component Overrides'
ComponentOverridesStory.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      components: customComponents,
    },
  },
}

// NOTE: add Gemini API key to .env.local with the key VITE_GOOGLE_GENERATIVE_AI_API_KEY
export const LanguageModel: Story = () => <Chat />
LanguageModel.storyName = 'Custom Language Model (Gemini)'
LanguageModel.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        title: 'Using Google Gemini',
        subtitle: 'Using Google Gemini 3 Flash Preview',
        suggestions: [
          {
            title: 'Generate a chart',
            label: 'Generate a chart',
            action: 'Generate a chart of these values: 1, 2, 3, 4, 5',
          },
          {
            title: 'Call all tools',
            label: 'Call all tools',
            action: 'Call all tools',
          },
        ],
      },
      languageModel: google('gemini-3-flash-preview'),
    },
  },
}
