import type { Meta, StoryObj } from "@storybook/react-vite";

import { ToolCapabilityBadge } from "./ToolCapabilityBadge";

const meta: Meta<typeof ToolCapabilityBadge> = {
  title: "Components/ToolCapabilityBadge",
  component: ToolCapabilityBadge,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof ToolCapabilityBadge>;

export const Read: Story = {
  args: {
    annotations: { readOnlyHint: true },
  },
};

export const Write: Story = {
  args: {
    annotations: { readOnlyHint: false, destructiveHint: false },
  },
};

export const Destructive: Story = {
  args: {
    annotations: { readOnlyHint: false, destructiveHint: true },
  },
};

/** No chip is rendered when the source made no read/write assertion. */
export const Unknown: Story = {
  args: {
    annotations: {},
  },
};

export const AllCapabilities: Story = {
  render: () => (
    <div className="flex items-center gap-2">
      <ToolCapabilityBadge annotations={{ readOnlyHint: true }} />
      <ToolCapabilityBadge
        annotations={{ readOnlyHint: false, destructiveHint: false }}
      />
      <ToolCapabilityBadge
        annotations={{ readOnlyHint: false, destructiveHint: true }}
      />
    </div>
  ),
};
