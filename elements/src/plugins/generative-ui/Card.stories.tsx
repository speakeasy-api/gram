import type { Meta, StoryObj } from '@storybook/react-vite'
import { Card } from './Card'
import { GenerativeUIWrapper } from './storybook-utils'

const meta: Meta<typeof Card> = {
  title: 'Generative UI/Card',
  component: Card,
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
}

export default meta
type Story = StoryObj<typeof Card>

export const Default: Story = {
  args: {
    children: 'This is card content.',
  },
}

export const WithTitle: Story = {
  args: {
    title: 'Card Title',
    children: 'This card has a title and content.',
  },
}

export const WithComplexContent: Story = {
  args: {
    title: 'User Profile',
    children: (
      <div className="flex flex-col gap-2">
        <p className="text-sm">Name: John Doe</p>
        <p className="text-muted-foreground text-sm">Email: john@example.com</p>
        <p className="text-muted-foreground text-sm">Role: Administrator</p>
      </div>
    ),
  },
}

export const Nested: Story = {
  render: () => (
    <Card title="Parent Card">
      <div className="space-y-3">
        <p className="text-sm">Parent content</p>
        <Card title="Nested Card">
          <p className="text-sm">Nested content</p>
        </Card>
      </div>
    </Card>
  ),
}
