import React from 'react'
import type { Meta, StoryFn } from '@storybook/react-vite'
import { ToolUI } from './tool-ui'

// NOTE: this component is not used within the elements library, but keeping it around for
// for reference and development purposes as this most closely resembles the Figma designs
// However, to use this design variant, we'd have to add lots of metadata to the tool parts

const meta: Meta<typeof ToolUI> = {
  component: ToolUI,
  parameters: {
    layout: 'centered',
  },
  decorators: [
    (Story) => (
      <div className="gramel:w-[400px] gramel:p-4">
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof ToolUI>

export default meta

type Story = StoryFn<typeof ToolUI>

export const Complete: Story = () => (
  <ToolUI
    provider="Notion"
    name="notion-search"
    status="complete"
    request={{
      query: 'PRD Gram Elements',
      query_type: 'internal',
    }}
    result={{
      results: [
        { id: 'page-1', title: 'PRD: Gram Elements v2' },
        { id: 'page-2', title: 'PRD: Gram Elements Architecture' },
      ],
    }}
  />
)

export const Running: Story = () => (
  <ToolUI
    provider="GitHub"
    name="search-repos"
    status="running"
    request={{
      query: 'gram-elements',
      org: 'speakeasy-api',
    }}
  />
)

export const Pending: Story = () => (
  <ToolUI provider="Slack" name="send-message" status="pending" />
)

export const Error: Story = () => (
  <ToolUI
    provider="Database"
    name="execute-query"
    status="error"
    request={{
      sql: 'SELECT * FROM users WHERE id = ?',
      params: [123],
    }}
    result={{
      error: 'Connection timeout after 30s',
      code: 'ETIMEDOUT',
    }}
  />
)

export const WithoutProvider: Story = () => (
  <ToolUI
    name="calculate-total"
    status="complete"
    request={{ items: [10, 20, 30] }}
    result={{ total: 60 }}
  />
)

export const WithCustomIcon: Story = () => (
  <ToolUI
    provider="Notion"
    icon={<span className="gramel:text-base">📝</span>}
    name="create-page"
    status="complete"
    request={{ title: 'Meeting Notes', parent: 'Workspace' }}
    result={{ pageId: 'abc-123', url: 'https://notion.so/abc-123' }}
  />
)

export const DefaultExpanded: Story = () => (
  <ToolUI
    provider="API"
    name="fetch-user"
    status="complete"
    defaultExpanded
    request={{ userId: 'user_123' }}
    result={{
      id: 'user_123',
      name: 'John Doe',
      email: 'john@example.com',
    }}
  />
)

export const LongContent: Story = () => (
  <ToolUI
    provider="OpenAI"
    name="generate-embedding"
    status="complete"
    request={{
      model: 'text-embedding-3-small',
      input:
        'This is a very long piece of text that needs to be embedded into a vector representation for semantic search purposes.',
    }}
    result={{
      embedding: [0.123, -0.456, 0.789, 0.012, -0.345, 0.678],
      usage: { prompt_tokens: 24, total_tokens: 24 },
    }}
  />
)

export const MultipleTools: Story = () => (
  <div className="gramel:flex gramel:flex-col gramel:gap-3">
    <ToolUI
      provider="Notion"
      name="notion-search"
      status="complete"
      request={{ query: 'meeting notes' }}
      result={{ results: [] }}
    />
    <ToolUI
      provider="Notion"
      name="notion-create-page"
      status="running"
      request={{ title: 'New Page' }}
    />
    <ToolUI provider="Slack" name="slack-post" status="pending" />
  </div>
)
