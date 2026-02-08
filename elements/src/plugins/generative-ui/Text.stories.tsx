import type { Meta, StoryObj } from '@storybook/react-vite'
import { Text } from './Text'
import { Stack } from './Stack'
import { GenerativeUIWrapper } from './storybook-utils'

const meta: Meta<typeof Text> = {
  title: 'Generative UI/Text',
  component: Text,
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
      options: ['heading', 'body', 'caption', 'code'],
    },
  },
}

export default meta
type Story = StoryObj<typeof Text>

export const Heading: Story = {
  args: {
    variant: 'heading',
    content: 'This is a heading',
  },
}

export const Body: Story = {
  args: {
    variant: 'body',
    content: 'This is body text for regular content.',
  },
}

export const Caption: Story = {
  args: {
    variant: 'caption',
    content: 'This is caption text for secondary information.',
  },
}

export const Code: Story = {
  args: {
    variant: 'code',
    content: 'const foo = "bar"',
  },
}

export const AllVariants: Story = {
  render: () => (
    <Stack direction="vertical">
      <Text variant="heading">Heading Text</Text>
      <Text variant="body">
        Body text is used for regular content and paragraphs.
      </Text>
      <Text variant="caption">Caption text for metadata and hints.</Text>
      <Text variant="code">npm install @gram-ai/elements</Text>
    </Stack>
  ),
}

export const WithChildren: Story = {
  args: {
    variant: 'body',
    children: (
      <>
        Text with <strong>bold</strong> and <em>italic</em> children.
      </>
    ),
  },
}
