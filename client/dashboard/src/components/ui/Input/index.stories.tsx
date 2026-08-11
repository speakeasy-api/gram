import { Input, InputProps } from ".";
import type { Meta, StoryObj } from "@storybook/react-vite";

const meta: Meta<typeof Input> = {
  title: "Design System/Input",
  component: Input,
  decorators: [
    (Story) => (
      <div className="m-auto mt-20 max-w-96">
        <Story />
      </div>
    ),
  ],
};

export default meta;

type Story = StoryObj<typeof Input>;

const baseArgs: Partial<InputProps> = {
  placeholder: "Write something...",
  onChange: () => {},
};

export const Default: Story = {
  args: {
    ...baseArgs,
  },
};

export const WithIcon: Story = {
  args: {
    ...baseArgs,
    icon: "search",
  },
};

export const Multiline: Story = {
  args: {
    ...baseArgs,
    multiline: true,
  },
};
