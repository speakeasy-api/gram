import type { Meta, StoryObj } from '@storybook/react-vite'
import { List } from './List'
import { Card } from './Card'
import { Stack } from './Stack'
import { GenerativeUIWrapper } from './storybook-utils'

const meta: Meta<typeof List> = {
  title: 'Generative UI/List',
  component: List,
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
type Story = StoryObj<typeof List>

export const Unordered: Story = {
  args: {
    items: ['First item', 'Second item', 'Third item', 'Fourth item'],
    ordered: false,
  },
}

export const Ordered: Story = {
  args: {
    items: [
      'Step one: Configure settings',
      'Step two: Install dependencies',
      'Step three: Run the application',
      'Step four: Verify output',
    ],
    ordered: true,
  },
}

export const SingleItem: Story = {
  args: {
    items: ['Only one item'],
    ordered: false,
  },
}

export const LongItems: Story = {
  args: {
    items: [
      'This is a very long list item that spans multiple lines to test text wrapping behavior',
      'Another long item with detailed information about something important',
      'Short item',
    ],
    ordered: false,
  },
}

export const InCard: Story = {
  render: () => (
    <Card title="Shopping List">
      <List
        items={['Milk', 'Eggs', 'Bread', 'Butter', 'Cheese']}
        ordered={false}
      />
    </Card>
  ),
}

export const TodoList: Story = {
  render: () => (
    <Card title="Today's Tasks">
      <List
        items={[
          'Review pull requests',
          'Update documentation',
          'Fix bug in login flow',
          'Deploy to staging',
        ]}
        ordered={true}
      />
    </Card>
  ),
}

export const MultipleLists: Story = {
  render: () => (
    <Stack direction="vertical">
      <Card title="Pros">
        <List items={['Fast performance', 'Easy to use', 'Great support']} />
      </Card>
      <Card title="Cons">
        <List items={['Limited features', 'Higher price']} />
      </Card>
    </Stack>
  ),
}
