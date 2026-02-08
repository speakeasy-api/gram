import type { Meta, StoryObj } from '@storybook/react-vite'
import { Stack } from './Stack'
import { Card } from './Card'
import { Badge } from './Badge'
import { GenerativeUIWrapper } from './storybook-utils'

const meta: Meta<typeof Stack> = {
  title: 'Generative UI/Stack',
  component: Stack,
  tags: ['autodocs'],
  parameters: {
    layout: 'centered',
  },
  decorators: [
    (Story) => (
      <GenerativeUIWrapper>
        <div className="w-[400px]">
          <Story />
        </div>
      </GenerativeUIWrapper>
    ),
  ],
  argTypes: {
    direction: {
      control: 'select',
      options: ['vertical', 'horizontal'],
    },
  },
}

export default meta
type Story = StoryObj<typeof Stack>

export const Vertical: Story = {
  args: {
    direction: 'vertical',
    children: (
      <>
        <div className="bg-muted rounded p-3">Item 1</div>
        <div className="bg-muted rounded p-3">Item 2</div>
        <div className="bg-muted rounded p-3">Item 3</div>
      </>
    ),
  },
}

export const Horizontal: Story = {
  args: {
    direction: 'horizontal',
    children: (
      <>
        <div className="bg-muted rounded p-3">Item 1</div>
        <div className="bg-muted rounded p-3">Item 2</div>
        <div className="bg-muted rounded p-3">Item 3</div>
      </>
    ),
  },
}

export const WithBadges: Story = {
  args: {
    direction: 'horizontal',
    children: (
      <>
        <Badge variant="success">Active</Badge>
        <Badge variant="warning">Pending</Badge>
        <Badge variant="error">Failed</Badge>
      </>
    ),
  },
}

export const NestedStacks: Story = {
  render: () => (
    <Stack direction="vertical">
      <Card title="Section 1">
        <Stack direction="horizontal">
          <div className="bg-muted rounded p-2">A</div>
          <div className="bg-muted rounded p-2">B</div>
          <div className="bg-muted rounded p-2">C</div>
        </Stack>
      </Card>
      <Card title="Section 2">
        <Stack direction="horizontal">
          <div className="bg-muted rounded p-2">X</div>
          <div className="bg-muted rounded p-2">Y</div>
          <div className="bg-muted rounded p-2">Z</div>
        </Stack>
      </Card>
    </Stack>
  ),
}
