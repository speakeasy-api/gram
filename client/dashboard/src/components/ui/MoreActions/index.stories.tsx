import type { Meta, StoryObj } from "@storybook/react-vite";

import { MoreActions } from ".";

const meta: Meta<typeof MoreActions> = {
  title: "Design System/MoreActions",
  component: MoreActions,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof MoreActions>;

export const Default: Story = {
  args: {
    actions: [
      { label: "Rename", icon: "pencil", onClick: () => {} },
      { label: "Duplicate", icon: "copy", onClick: () => {} },
      {
        label: "Delete",
        icon: "trash",
        onClick: () => {},
        destructive: true,
        separatorBefore: true,
      },
    ],
  },
};

export const WithDisabledReason: Story = {
  args: {
    actions: [
      { label: "Publish", icon: "upload", onClick: () => {} },
      {
        label: "Delete",
        icon: "trash",
        onClick: () => {},
        disabled: true,
        description: "Attached to a live deployment.",
      },
    ],
  },
};
