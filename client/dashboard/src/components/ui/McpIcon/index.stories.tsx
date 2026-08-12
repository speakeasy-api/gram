import type { Meta, StoryObj } from "@storybook/react-vite";

import { McpIcon } from ".";

const meta: Meta<typeof McpIcon> = {
  title: "Design System/McpIcon",
  component: McpIcon,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof McpIcon>;

export const Sizes: Story = {
  render: () => (
    <div className="flex items-center gap-4">
      <McpIcon size={16} />
      <McpIcon size={24} />
      <McpIcon size={48} />
    </div>
  ),
};
