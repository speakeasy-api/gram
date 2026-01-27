import type { Meta, StoryFn } from '@storybook/react-vite'
import { z } from 'zod'
import { Chat } from '..'
import { defineFrontendTool } from '../../../lib/tools'

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
            label: 'Q4 performance metrics',
            prompt: `Here's our Q4 sales data:
${salesData}

Summarize the Q4 performance - show total revenue, quarter-over-quarter growth, and which product performed best. We're at 85% of our Q4 target.`,
          },
          {
            title: 'Task List',
            label: 'My pending tasks',
            prompt: `Show me my pending tasks for today. I have 3 tasks:
1. Review PR #234 - high priority
2. Update documentation - medium priority
3. Team sync meeting - scheduled for 3pm`,
          },
          {
            title: 'System Status',
            label: 'Service health check',
            prompt: `What's the current status of our services?
- API: healthy, 99.9% uptime
- Database: healthy, 45% capacity used
- Cache: degraded, high latency detected
- Queue: healthy, 1,240 jobs processed`,
          },
        ],
      },
    },
  },
}

// Frontend tools for ActionButton demo
const completeTaskTool = defineFrontendTool<{ taskId: number }, string>(
  {
    description: 'Mark a task as complete',
    parameters: z.object({
      taskId: z.number().describe('The task ID to mark as complete'),
    }),
    execute: async ({ taskId }) => {
      await new Promise((resolve) => setTimeout(resolve, 500))
      return `Task #${taskId} has been marked as complete.`
    },
  },
  'complete_task'
)

const deleteTaskTool = defineFrontendTool<{ taskId: number }, string>(
  {
    description: 'Delete a task',
    parameters: z.object({
      taskId: z.number().describe('The task ID to delete'),
    }),
    execute: async ({ taskId }) => {
      await new Promise((resolve) => setTimeout(resolve, 500))
      return `Task #${taskId} has been deleted.`
    },
  },
  'delete_task'
)

const actionTools = {
  complete_task: completeTaskTool,
  delete_task: deleteTaskTool,
}

/**
 * Demonstrates ActionButton in generative UI that triggers frontend tool calls.
 * When a button is clicked, it directly executes the tool without an LLM roundtrip.
 */
export const GenerativeUIWithActions: Story = () => <Chat />
GenerativeUIWithActions.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        title: 'Task Manager',
        subtitle: 'Manage your tasks with quick actions',
        suggestions: [
          {
            title: 'My Tasks',
            label: 'View pending work',
            prompt: `Show me my task list. I have these pending tasks:

Task #1: "Update user authentication" - in progress
Task #2: "Fix pagination bug" - pending review
Task #3: "Write API documentation" - not started

I should be able to mark tasks as complete or delete them.`,
          },
        ],
      },
      tools: {
        frontendTools: actionTools,
      },
    },
  },
}
