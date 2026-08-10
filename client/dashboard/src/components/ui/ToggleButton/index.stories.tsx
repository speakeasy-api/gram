import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import { ToggleButton } from ".";

const meta: Meta<typeof ToggleButton> = {
  title: "Design System/ToggleButton",
  component: ToggleButton,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof ToggleButton>;

export const Default: Story = {
  render: function Render() {
    const [granularity, setGranularity] = useState<"day" | "week">("day");

    return (
      <div className="flex gap-1">
        {(["day", "week"] as const).map((option) => (
          <ToggleButton
            key={option}
            active={granularity === option}
            onClick={() => setGranularity(option)}
          >
            {option}
          </ToggleButton>
        ))}
      </div>
    );
  },
};
