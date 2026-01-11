import React from 'react'
import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Plugins',
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

const countryData = JSON.stringify({
  countries: [
    { name: 'USA', gdp: 22000 },
    { name: 'Canada', gdp: 16000 },
    { name: 'Mexico', gdp: 10000 },
  ],
})

export const ChartPlugin: Story = () => <Chat />
ChartPlugin.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        suggestions: [
          {
            title: 'Create a chart',
            label: 'Visualize your data',
            action: `Create a bar chart for the following country + GDP data:
            ${countryData}
            `,
          },
        ],
      },
    },
  },
}
