import type { Meta, StoryObj } from "@storybook/react-vite";

import { Label } from ".";

const meta: Meta<typeof Label> = {
  title: "Design System/Label",
  component: Label,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Label>;

export const Default: Story = {
  args: { children: "Server slug" },
};
