import type { Meta, StoryObj } from "@storybook/react-vite";

import { CopyButton } from ".";

const meta: Meta<typeof CopyButton> = {
  title: "Design System/CopyButton",
  component: CopyButton,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof CopyButton>;

export const Default: Story = {
  args: { text: "https://app.getgram.ai/mcp/petstore", tooltip: "Copy URL" },
};

export const Small: Story = {
  args: { text: "gram_sk_…", size: "xs" },
};
