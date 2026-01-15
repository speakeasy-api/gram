import { Chat, ComponentOverrides } from '@/index'
import { useAssistantState } from '@assistant-ui/react'
import { Meta, StoryFn } from '@storybook/react-vite'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Customization',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
  decorators: [
    (Story) => (
      <div className="gramel:m-auto gramel:flex gramel:h-screen gramel:w-full gramel:max-w-3xl gramel:flex-col">
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof Chat>

export default meta

type Story = StoryFn<typeof Chat>

const customComponents: ComponentOverrides = {
  Text: () => {
    const message = useAssistantState(({ message }) => message)
    return (
      <div className="gramel:text-red-500">
        {message.parts
          .map((part) => (part.type === 'text' ? part.text : ''))
          .join('')}
      </div>
    )
  },
}

export const ComponentOverridesStory: Story = () => <Chat />
ComponentOverridesStory.storyName = 'Component Overrides'
ComponentOverridesStory.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      components: customComponents,
    },
  },
}
