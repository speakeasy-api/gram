import type { Meta, StoryObj } from '@storybook/react-vite'
import { Progress } from './Progress'
import { Stack } from './Stack'
import { Card } from './Card'
import { GenerativeUIWrapper } from './storybook-utils'

const meta: Meta<typeof Progress> = {
  title: 'Generative UI/Progress',
  component: Progress,
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
    value: {
      control: { type: 'range', min: 0, max: 100 },
    },
    max: {
      control: { type: 'number' },
    },
  },
}

export default meta
type Story = StoryObj<typeof Progress>

export const Default: Story = {
  args: {
    value: 65,
    max: 100,
  },
}

export const WithLabel: Story = {
  args: {
    value: 75,
    max: 100,
    label: 'Upload Progress',
  },
}

export const Empty: Story = {
  args: {
    value: 0,
    max: 100,
    label: 'Not Started',
  },
}

export const Complete: Story = {
  args: {
    value: 100,
    max: 100,
    label: 'Complete',
  },
}

export const CustomMax: Story = {
  args: {
    value: 30,
    max: 50,
    label: '30 of 50 tasks',
  },
}

export const MultipleProgress: Story = {
  render: () => (
    <Stack direction="vertical">
      <Progress value={25} label="Project Alpha" />
      <Progress value={50} label="Project Beta" />
      <Progress value={75} label="Project Gamma" />
      <Progress value={100} label="Project Delta" />
    </Stack>
  ),
}

export const InCard: Story = {
  render: () => (
    <Card title="Sprint Progress">
      <Stack direction="vertical">
        <Progress value={8} max={10} label="Stories Completed" />
        <Progress value={15} max={20} label="Story Points" />
        <Progress value={3} max={5} label="Days Remaining" />
      </Stack>
    </Card>
  ),
}

export const LowProgress: Story = {
  args: {
    value: 5,
    max: 100,
    label: 'Just Started',
  },
}
