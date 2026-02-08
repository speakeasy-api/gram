import type { Meta, StoryObj } from '@storybook/react-vite'
import { Grid } from './Grid'
import { Card } from './Card'
import { GenerativeUIWrapper } from './storybook-utils'

const meta: Meta<typeof Grid> = {
  title: 'Generative UI/Grid',
  component: Grid,
  tags: ['autodocs'],
  parameters: {
    layout: 'centered',
  },
  decorators: [
    (Story) => (
      <GenerativeUIWrapper>
        <div className="w-[600px]">
          <Story />
        </div>
      </GenerativeUIWrapper>
    ),
  ],
  argTypes: {
    columns: {
      control: { type: 'range', min: 1, max: 6 },
    },
  },
}

export default meta
type Story = StoryObj<typeof Grid>

export const TwoColumns: Story = {
  args: {
    columns: 2,
    children: (
      <>
        <div className="bg-muted rounded p-4">Item 1</div>
        <div className="bg-muted rounded p-4">Item 2</div>
        <div className="bg-muted rounded p-4">Item 3</div>
        <div className="bg-muted rounded p-4">Item 4</div>
      </>
    ),
  },
}

export const ThreeColumns: Story = {
  args: {
    columns: 3,
    children: (
      <>
        <div className="bg-muted rounded p-4">Item 1</div>
        <div className="bg-muted rounded p-4">Item 2</div>
        <div className="bg-muted rounded p-4">Item 3</div>
        <div className="bg-muted rounded p-4">Item 4</div>
        <div className="bg-muted rounded p-4">Item 5</div>
        <div className="bg-muted rounded p-4">Item 6</div>
      </>
    ),
  },
}

export const FourColumns: Story = {
  args: {
    columns: 4,
    children: (
      <>
        <div className="bg-muted rounded p-4 text-center">1</div>
        <div className="bg-muted rounded p-4 text-center">2</div>
        <div className="bg-muted rounded p-4 text-center">3</div>
        <div className="bg-muted rounded p-4 text-center">4</div>
      </>
    ),
  },
}

export const WithCards: Story = {
  args: {
    columns: 2,
    children: (
      <>
        <Card title="Card 1">Content for card 1</Card>
        <Card title="Card 2">Content for card 2</Card>
        <Card title="Card 3">Content for card 3</Card>
        <Card title="Card 4">Content for card 4</Card>
      </>
    ),
  },
}
