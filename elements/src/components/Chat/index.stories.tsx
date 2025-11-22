import { Chat } from ".";
import type { Meta, StoryFn } from "@storybook/react-vite";

const meta: Meta<typeof Chat> = {
  title: "Components/Chat",
  component: Chat,
  parameters: {
    layout: "fullscreen",
  },
  decorators: [
    (Story) => (
      <div className="w-full h-full p-10">
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof Chat>;

export default meta;

type Story = StoryFn<typeof Chat>;

export const Default: Story = () => <Chat />;
