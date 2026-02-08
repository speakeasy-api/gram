import type { Meta, StoryObj } from '@storybook/react-vite'
import { ActionButton } from './ActionButton'
import { Stack } from './Stack'
import { Card } from './Card'
import { ToolExecutionProvider } from '@/contexts/ToolExecutionContext'
import { GenerativeUIWrapper } from './storybook-utils'

// Mock tool execution for stories
const mockTools = {
  complete_task: {
    execute: async () => {
      await new Promise((resolve) => setTimeout(resolve, 500))
      return { content: [{ type: 'text', text: 'Task marked as complete!' }] }
    },
  },
  delete_item: {
    execute: async () => {
      await new Promise((resolve) => setTimeout(resolve, 500))
      return { content: [{ type: 'text', text: 'Item deleted' }] }
    },
  },
  save_changes: {
    execute: async () => {
      await new Promise((resolve) => setTimeout(resolve, 800))
      return { content: [{ type: 'text', text: 'Changes saved successfully' }] }
    },
  },
  failing_action: {
    execute: async () => {
      await new Promise((resolve) => setTimeout(resolve, 500))
      throw new Error('Network error: Failed to connect')
    },
  },
}

const withProviders = (Story: React.ComponentType) => (
  <GenerativeUIWrapper>
    <ToolExecutionProvider tools={mockTools}>
      <Story />
    </ToolExecutionProvider>
  </GenerativeUIWrapper>
)

const meta: Meta<typeof ActionButton> = {
  title: 'Generative UI/ActionButton',
  component: ActionButton,
  tags: ['autodocs'],
  parameters: {
    layout: 'centered',
  },
  decorators: [withProviders],
  argTypes: {
    variant: {
      control: 'select',
      options: ['default', 'secondary', 'outline', 'destructive'],
    },
  },
}

export default meta
type Story = StoryObj<typeof ActionButton>

export const Default: Story = {
  args: {
    label: 'Complete Task',
    action: 'complete_task',
    args: { taskId: '123' },
  },
}

export const Secondary: Story = {
  args: {
    label: 'Save Changes',
    action: 'save_changes',
    variant: 'secondary',
  },
}

export const Outline: Story = {
  args: {
    label: 'Cancel',
    action: 'save_changes',
    variant: 'outline',
  },
}

export const Destructive: Story = {
  args: {
    label: 'Delete Item',
    action: 'delete_item',
    args: { itemId: '456' },
    variant: 'destructive',
  },
}

export const Unavailable: Story = {
  args: {
    label: 'Unknown Action',
    action: 'unknown_tool',
  },
}

export const WillFail: Story = {
  args: {
    label: 'Try Action (Will Fail)',
    action: 'failing_action',
  },
}

export const AllVariants: Story = {
  render: () => (
    <Stack direction="horizontal">
      <ActionButton label="Default" action="complete_task" variant="default" />
      <ActionButton
        label="Secondary"
        action="save_changes"
        variant="secondary"
      />
      <ActionButton label="Outline" action="save_changes" variant="outline" />
      <ActionButton
        label="Destructive"
        action="delete_item"
        variant="destructive"
      />
    </Stack>
  ),
}

export const InCard: Story = {
  render: () => (
    <Card title="Task: Review PR #123">
      <Stack direction="vertical">
        <p className="text-muted-foreground text-sm">
          Pull request needs review before merging.
        </p>
        <Stack direction="horizontal">
          <ActionButton
            label="Approve"
            action="complete_task"
            args={{ action: 'approve', prId: 123 }}
            variant="default"
          />
          <ActionButton
            label="Request Changes"
            action="save_changes"
            args={{ action: 'request_changes', prId: 123 }}
            variant="outline"
          />
        </Stack>
      </Stack>
    </Card>
  ),
}

export const TaskList: Story = {
  render: () => (
    <Card title="Today's Tasks">
      <Stack direction="vertical">
        {[
          { id: 1, title: 'Review documentation' },
          { id: 2, title: 'Fix login bug' },
          { id: 3, title: 'Update dependencies' },
        ].map((task) => (
          <div key={task.id} className="flex items-center justify-between">
            <span className="text-sm">{task.title}</span>
            <ActionButton
              label="Complete"
              action="complete_task"
              args={{ taskId: task.id }}
              variant="outline"
            />
          </div>
        ))}
      </Stack>
    </Card>
  ),
}
