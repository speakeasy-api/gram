import { GetSessionFn } from '@/types'
import { google } from '@ai-sdk/google'
import type { Meta, StoryFn } from '@storybook/react-vite'
import { Chat } from '..'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Connection Configuration',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
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

export const WithImplicitSessionAuth: Story = () => <Chat />
WithImplicitSessionAuth.storyName = 'With Implicit Session Auth'
WithImplicitSessionAuth.parameters = {
  elements: {
    config: {
      // This story tests the case where no API config is provided, which should default to implicit session auth
      api: undefined,
    },
  },
}

const sessionFn: GetSessionFn = async () => {
  const response = await fetch('/chat/session', {
    method: 'POST',
    headers: {
      'Gram-Project':
        import.meta.env.VITE_GRAM_ELEMENTS_STORYBOOK_PROJECT_SLUG ?? '',
    },
  })
  const data = await response.json()
  return data.client_token
}

export const WithExplicitSessionAuth: Story = () => <Chat />
WithExplicitSessionAuth.storyName = 'With Explicit Session Auth'
WithExplicitSessionAuth.parameters = {
  elements: {
    config: {
      api: { sessionFn },
    },
  },
}

// NOTE: api key auth is currently non functional due to concerns with security
// Update this story when api key auth is secure
export const WithStaticSessionAuth: Story = () => <Chat />
WithStaticSessionAuth.storyName = 'With Static Session Auth'
WithStaticSessionAuth.parameters = {
  elements: {
    config: {
      api: { sessionToken: 'test' },
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
            title: 'Browse Products',
            label: "See what's available",
            prompt: 'What products do you have?',
          },
          {
            title: 'Sales Chart',
            label: 'Visualize data',
            prompt: 'Show me a chart of product prices',
          },
        ],
      },
      languageModel: google('gemini-3-flash-preview'),
    },
  },
}
