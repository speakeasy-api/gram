import type { Meta, StoryObj } from "@storybook/react-vite";

import { StatusDot } from ".";

const meta: Meta<typeof StatusDot> = {
  title: "Design System/StatusDot",
  component: StatusDot,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof StatusDot>;

export const Tones: Story = {
  render: () => (
    <div className="flex flex-col gap-2 text-sm">
      {(
        ["success", "warning", "destructive", "information", "neutral"] as const
      ).map((tone) => (
        <span key={tone} className="flex items-center gap-2">
          <StatusDot tone={tone} />
          {tone}
        </span>
      ))}
    </div>
  ),
};
