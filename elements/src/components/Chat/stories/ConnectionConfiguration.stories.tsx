import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'
import { google } from '@ai-sdk/google'
import { GetSessionFn } from '@/types'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Connection Configuration',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
  decorators: [
    (Story) => (
      <div className="gramel:m-auto gramel:flex gramel:h-screen gramel:w-full gramel:max-w-3xl gramel:flex-col">
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
export const WithExplicitAPIKeyAuth: Story = () => <Chat />
WithExplicitAPIKeyAuth.storyName = 'With Explicit API Key Auth'
WithExplicitAPIKeyAuth.parameters = {
  elements: {
    config: {
      api: { UNSAFE_apiKey: 'test' },
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
