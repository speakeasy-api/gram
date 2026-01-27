import type { Meta, StoryFn } from '@storybook/react-vite'
import { Chat } from '..'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Plugins',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
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
            prompt: `Create a bar chart for the following country + GDP data:
            ${countryData}
            `,
          },
        ],
      },
    },
  },
}

const salesData = JSON.stringify({
  headers: ['Product', 'Q1', 'Q2', 'Q3', 'Q4'],
  rows: [
    ['Widget A', '$12,500', '$15,200', '$18,900', '$22,100'],
    ['Widget B', '$8,300', '$9,100', '$11,400', '$14,200'],
    ['Widget C', '$5,600', '$6,800', '$7,900', '$9,500'],
  ],
})

export const GenerativeUI: Story = () => <Chat />
GenerativeUI.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        suggestions: [
          {
            title: 'Sales Dashboard',
            label: 'Show metrics with widgets',
            prompt: `Show me a sales dashboard with the following data using generative UI widgets:
            ${salesData}

            Please display:
            - A card with the title "Q4 Sales Summary"
            - A grid with 3 metrics showing total revenue, growth rate, and top product
            - A table with the quarterly breakdown
            - A progress bar showing Q4 target completion (85%)
            `,
          },
          {
            title: 'Task List',
            label: 'Interactive action buttons',
            prompt: `Create a task management UI using generative widgets that includes:
            - A card titled "Today's Tasks"
            - A list of 3 pending tasks
            - Action buttons to mark tasks as complete
            `,
          },
          {
            title: 'Status Overview',
            label: 'Badges and indicators',
            prompt: `Show a system status overview using generative UI with:
            - A card showing system health
            - Status badges (success, warning, error variants)
            - Progress indicators for different services
            `,
          },
        ],
      },
    },
  },
}
