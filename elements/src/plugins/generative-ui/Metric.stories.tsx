import type { Meta, StoryObj } from '@storybook/react-vite'
import { Metric } from './Metric'
import { GenerativeUIWrapper } from './storybook-utils'

const meta: Meta<typeof Metric> = {
  title: 'Generative UI/Metric',
  component: Metric,
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
    format: {
      control: 'select',
      options: ['number', 'currency', 'percent'],
    },
  },
}

export default meta
type Story = StoryObj<typeof Metric>

export const Default: Story = {
  args: {
    label: 'Total Users',
    value: 1234,
  },
}

export const Currency: Story = {
  args: {
    label: 'Revenue',
    value: 45678.9,
    format: 'currency',
  },
}

export const Percent: Story = {
  args: {
    label: 'Conversion Rate',
    value: 0.1234,
    format: 'percent',
  },
}

export const LargeNumber: Story = {
  args: {
    label: 'API Calls',
    value: 1234567890,
    format: 'number',
  },
}

export const MetricsGrid: Story = {
  render: () => (
    <div className="grid grid-cols-2 gap-4">
      <Metric label="Total Users" value={12345} />
      <Metric label="Revenue" value={98765.43} format="currency" />
      <Metric label="Growth" value={0.234} format="percent" />
      <Metric label="API Calls" value={987654} />
    </div>
  ),
}
