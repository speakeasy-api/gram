import type { Meta, StoryFn } from '@storybook/react-vite'
import { Chat } from '..'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Agents',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
    docs: {
      description: {
        component: `
## Agentic Workflows

When agents are enabled, the LLM can spawn sub-agents to handle specialized tasks.
The spawn_agent tool is automatically injected by the server when the \`Gram-Agents-Enabled\`
header is sent.

**Note:** These stories require a running Gram server with agent support to function properly.
Without a server, the spawn_agent tool will not be available.
        `,
      },
    },
  },
}

export default meta

type Story = StoryFn<typeof Chat>

/**
 * Basic agents configuration enabled.
 * When the spawn_agent tool is available, the LLM can spawn sub-agents
 * to handle specialized tasks.
 */
export const AgentsEnabled: Story = () => <Chat />
AgentsEnabled.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      agents: {
        enabled: true,
      },
      systemPrompt: `You are a helpful assistant. Use the spawn_agent tool to delegate tasks to specialized sub-agents.

When given a multi-part request, spawn separate agents for each part to work in parallel.

IMPORTANT: Keep all responses concise - use bullet points, not lengthy prose. Aim for 3-5 key points per topic.`,
      welcome: {
        title: 'Agentic Assistant',
        subtitle: 'I can spawn sub-agents to help with complex tasks',
        suggestions: [
          {
            title: 'Plan a trip',
            label: 'Multi-step planning',
            prompt:
              'Help me plan a weekend trip to San Francisco. Give me a brief overview with top 3 attractions, top 3 restaurants, and a simple 2-day itinerary.',
          },
          {
            title: 'Compare products',
            label: 'Research task',
            prompt:
              'Briefly compare Asana, Trello, and Monday.com. Just give me key differences and a recommendation.',
          },
          {
            title: 'Quick summary',
            label: 'Simple task',
            prompt:
              'What are 3 key benefits of TypeScript over JavaScript?',
          },
        ],
      },
    },
  },
}
