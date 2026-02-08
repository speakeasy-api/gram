import type { Meta, StoryObj } from '@storybook/react-vite'
import { Divider } from './Divider'
import { Text } from './Text'
import { Stack } from './Stack'
import { Card } from './Card'
import { GenerativeUIWrapper } from './storybook-utils'

const meta: Meta<typeof Divider> = {
  title: 'Generative UI/Divider',
  component: Divider,
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
type Story = StoryObj<typeof Divider>

export const Default: Story = {
  render: () => (
    <div>
      <Text variant="body">Content above the divider</Text>
      <Divider />
      <Text variant="body">Content below the divider</Text>
    </div>
  ),
}

export const BetweenSections: Story = {
  render: () => (
    <Stack direction="vertical">
      <div>
        <Text variant="heading">Section 1</Text>
        <Text variant="body">This is the first section of content.</Text>
      </div>
      <Divider />
      <div>
        <Text variant="heading">Section 2</Text>
        <Text variant="body">This is the second section of content.</Text>
      </div>
      <Divider />
      <div>
        <Text variant="heading">Section 3</Text>
        <Text variant="body">This is the third section of content.</Text>
      </div>
    </Stack>
  ),
}

export const InCard: Story = {
  render: () => (
    <Card title="User Profile">
      <Stack direction="vertical">
        <div>
          <Text variant="caption">Name</Text>
          <Text variant="body">John Doe</Text>
        </div>
        <Divider />
        <div>
          <Text variant="caption">Email</Text>
          <Text variant="body">john@example.com</Text>
        </div>
        <Divider />
        <div>
          <Text variant="caption">Role</Text>
          <Text variant="body">Administrator</Text>
        </div>
      </Stack>
    </Card>
  ),
}
