/**
 * Test fixtures for Playwright e2e tests.
 * Each fixture renders a specific configuration of the Chat component.
 */

import { ElementsProvider } from '@/contexts/ElementsProvider'
import { Chat } from '@/components/Chat'
import type { ElementsConfig } from '@/types'

// Simple mock language model for testing
function createSimpleMock() {
  return {
    specificationVersion: 'v1' as const,
    provider: 'mock',
    modelId: 'mock-model',
    defaultObjectGenerationMode: 'json' as const,
    doGenerate: async () => ({
      rawCall: { rawPrompt: null, rawSettings: {} },
      finishReason: 'stop' as const,
      usage: { promptTokens: 10, completionTokens: 20 },
      text: 'This is a mock response from the assistant.',
    }),
    doStream: async () => ({
      rawCall: { rawPrompt: null, rawSettings: {} },
      stream: new ReadableStream({
        start(controller) {
          controller.enqueue({
            type: 'text-delta',
            textDelta: 'This is a mock response.',
          })
          controller.enqueue({
            type: 'finish',
            finishReason: 'stop',
            usage: { promptTokens: 10, completionTokens: 20 },
          })
          controller.close()
        },
      }),
    }),
  }
}

const mockLanguageModel = createSimpleMock()

// Base config used by all fixtures
const baseConfig: Partial<ElementsConfig> = {
  projectSlug: 'test-project',
  languageModel: mockLanguageModel as any,
  errorTracking: { enabled: false },
  api: {
    sessionToken: 'mock-token',
  },
}

// Fixture configurations
const fixtures: Record<string, ElementsConfig> = {
  // Welcome screen with custom message and suggestions
  welcome: {
    ...baseConfig,
    projectSlug: 'test-project',
    variant: 'standalone',
    welcome: {
      title: 'Hello there!',
      subtitle: "How can I help you today?",
      suggestions: [
        {
          title: 'Write a SQL query',
          label: 'to find top customers',
          prompt: 'Write a SQL query to find top customers',
        },
        {
          title: 'Show me a chart',
          label: 'of sales data',
          prompt: 'Show me a chart of sales data',
        },
      ],
    },
  },

  // Standalone variant
  standalone: {
    ...baseConfig,
    projectSlug: 'test-project',
    variant: 'standalone',
  },

  // Modal/widget variant
  modal: {
    ...baseConfig,
    projectSlug: 'test-project',
    variant: 'widget',
    modal: {
      defaultOpen: true,
    },
  },

  // Sidecar variant
  sidecar: {
    ...baseConfig,
    projectSlug: 'test-project',
    variant: 'sidecar',
  },

  // Compact density
  'density-compact': {
    ...baseConfig,
    projectSlug: 'test-project',
    variant: 'standalone',
    theme: {
      density: 'compact',
    },
  },

  // Spacious density
  'density-spacious': {
    ...baseConfig,
    projectSlug: 'test-project',
    variant: 'standalone',
    theme: {
      density: 'spacious',
    },
  },

  // Light theme
  'theme-light': {
    ...baseConfig,
    projectSlug: 'test-project',
    variant: 'standalone',
    theme: {
      colorScheme: 'light',
    },
  },

  // Dark theme
  'theme-dark': {
    ...baseConfig,
    projectSlug: 'test-project',
    variant: 'standalone',
    theme: {
      colorScheme: 'dark',
    },
  },

  // Custom placeholder
  'composer-placeholder': {
    ...baseConfig,
    projectSlug: 'test-project',
    variant: 'standalone',
    composer: {
      placeholder: 'Type your question here...',
    },
  },

  // Tool mentions enabled
  'tool-mentions': {
    ...baseConfig,
    projectSlug: 'test-project',
    variant: 'standalone',
    composer: {
      toolMentions: true,
    },
  },

  // Chart rendering test
  chart: {
    ...baseConfig,
    projectSlug: 'test-project',
    variant: 'standalone',
  },
}

interface TestFixtureProps {
  name: string
}

export function TestFixture({ name }: TestFixtureProps) {
  // For debugging: simple fixture first
  if (name === 'simple') {
    return (
      <div style={{ padding: 20 }} data-testid="test-fixture" data-fixture="simple">
        <h1>Simple Test Fixture</h1>
        <p>If you can see this, React is working.</p>
      </div>
    )
  }

  const config = fixtures[name]

  if (!config) {
    return (
      <div style={{ padding: 20, color: 'red' }} data-testid="test-fixture" data-fixture="error">
        <h1>Unknown fixture: {name}</h1>
        <p>Available fixtures:</p>
        <ul>
          {Object.keys(fixtures).map((f) => (
            <li key={f}>
              <a href={`?fixture=${f}`}>{f}</a>
            </li>
          ))}
        </ul>
      </div>
    )
  }

  return (
    <div style={{ height: '100vh', width: '100%' }} data-testid="test-fixture" data-fixture={name}>
      <ElementsProvider config={config}>
        <Chat />
      </ElementsProvider>
    </div>
  )
}
