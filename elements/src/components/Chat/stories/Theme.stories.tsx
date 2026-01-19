import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Theme',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
} satisfies Meta<typeof Chat>

export default meta

type Story = StoryFn<typeof Chat>

const StoryWrapper = ({ children }: { children: React.ReactNode }) => (
  <div className="m-auto flex h-screen w-full max-w-3xl flex-col">
    {children}
  </div>
)

export const Light: Story = () => (
  <StoryWrapper>
    <Chat />
  </StoryWrapper>
)
Light.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      theme: { colorScheme: 'light' },
    },
  },
  // Disable Chromatic's automatic dark mode snapshot for this story
  chromatic: { modes: { 'light desktop': { theme: 'light' } } },
}

export const Dark: Story = () => (
  <StoryWrapper>
    <Chat />
  </StoryWrapper>
)
Dark.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      theme: { colorScheme: 'dark' },
    },
  },
  backgrounds: { default: 'dark' },
  // Only capture dark mode for this story
  chromatic: { modes: { 'dark desktop': { theme: 'dark' } } },
}

export const System: Story = () => (
  <StoryWrapper>
    <Chat />
  </StoryWrapper>
)
System.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      theme: { colorScheme: 'system' },
    },
  },
  // System will follow browser preference, test both modes
  chromatic: {
    modes: {
      'light desktop': { theme: 'light' },
      'dark desktop': { theme: 'dark' },
    },
  },
}
