import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Style Isolation',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
} satisfies Meta<typeof Chat>

export default meta

type Story = StoryFn<typeof Chat>

export const Default: Story = () => (
  <div>
    <p>This is a demo of Shadow DOM isolation</p>
    <p>Outer paragraph receives global styles</p>
    <style>
      {`
        * {
          background-color: red;
          color: white;
          font-family: Comic Sans MS;
        }
      `}
    </style>
    <Chat />
  </div>
)
Default.parameters = {
  elements: {
    config: {
      welcome: {
        title: 'Style isolation via Shadow DOM',
        subtitle: 'Demo of style isolation via Shadow DOM',
      },
    },
  },
}
