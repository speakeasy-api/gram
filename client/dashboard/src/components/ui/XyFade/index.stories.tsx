import type { Meta, StoryObj } from "@storybook/react-vite";

import { XYFade } from ".";

const meta: Meta<typeof XYFade> = {
  title: "Design System/XyFade",
  component: XYFade,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof XYFade>;

export const Vertical: Story = {
  render: () => (
    <XYFade className="h-40 w-96 overflow-auto border p-4">
      <div className="flex flex-col gap-2 text-sm">
        {Array.from({ length: 20 }).map((_, i) => (
          <div key={i}>Row {i + 1}</div>
        ))}
      </div>
    </XYFade>
  ),
};
