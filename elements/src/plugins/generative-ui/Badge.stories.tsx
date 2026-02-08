import type { Meta, StoryObj } from '@storybook/react-vite'
import { Badge } from './Badge'
import { Stack } from './Stack'
import { GenerativeUIWrapper } from './storybook-utils'

const meta: Meta<typeof Badge> = {
  title: 'Generative UI/Badge',
  component: Badge,
  tags: ['autodocs'],
  parameters: {
    layout: 'centered',
  },
  decorators: [
    (Story) => (
      <GenerativeUIWrapper>
        <Story />
      </GenerativeUIWrapper>
    ),
  ],
  argTypes: {
    variant: {
      control: 'select',
      options: ['default', 'secondary', 'success', 'warning', 'error'],
    },
  },
}

export default meta
type Story = StoryObj<typeof Badge>

export const Default: Story = {
  args: {
    variant: 'default',
    content: 'Default',
  },
}

export const Secondary: Story = {
  args: {
    variant: 'secondary',
    content: 'Secondary',
  },
}

export const Success: Story = {
  args: {
    variant: 'success',
    content: 'Active',
  },
}

export const Warning: Story = {
  args: {
    variant: 'warning',
    content: 'Pending',
  },
}

export const Error: Story = {
  args: {
    variant: 'error',
    content: 'Failed',
  },
}

export const AllVariants: Story = {
  render: () => (
    <Stack direction="horizontal">
      <Badge variant="default">Default</Badge>
      <Badge variant="secondary">Secondary</Badge>
      <Badge variant="success">Success</Badge>
      <Badge variant="warning">Warning</Badge>
      <Badge variant="error">Error</Badge>
    </Stack>
  ),
}

export const StatusLabels: Story = {
  render: () => (
    <Stack direction="vertical">
      <div className="flex items-center gap-2">
        <Badge variant="success">Online</Badge>
        <span className="text-sm">Server is healthy</span>
      </div>
      <div className="flex items-center gap-2">
        <Badge variant="warning">Degraded</Badge>
        <span className="text-sm">High latency detected</span>
      </div>
      <div className="flex items-center gap-2">
        <Badge variant="error">Offline</Badge>
        <span className="text-sm">Server unreachable</span>
      </div>
    </Stack>
  ),
}
